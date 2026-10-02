-- gotyper is the Neovim front end of a typing game that teaches Go.
--
-- It is deliberately thin. The Go engine (engine/ in this repo) judges every
-- keystroke; this plugin only:
--
--   1. opens a game tab with a scratch buffer the learner types into,
--   2. sends the whole buffer to the engine on every change ("update"),
--   3. paints the answer (gotyper.ui): ghost text, red mistakes, stats.
--
-- gotyper.engine owns the engine process and the wire protocol. This file
-- owns the session: the game buffer, its keys and hooks, restart and teardown.
local M = {}

local api, engine, ui = vim.api, require("gotyper.engine"), require("gotyper.ui")

-- The game keys: buffer-local mappings in the game buffer, active in both
-- normal and insert mode. To use other keys, map :GotyperRestart and
-- :GotyperPanel yourself.
local RESTART_KEY = "<F5>" -- throw the attempt away and type the step again
local PANEL_KEY = "<F2>" -- show or hide the explanation panel

-- ns_key identifies our vim.on_key hook so stop() can remove exactly it.
local ns_key = api.nvim_create_namespace("gotyper_keys")

-- S is the running session, or nil when no game is open. One session at a
-- time: starting a new one stops the old. Its fields:
--   client        the engine connection (gotyper.engine.connect)
--   buf, win      the game buffer and the window showing it
--   info          the step layout from the engine's start response
--   last          the latest render that was painted
--   keys          keystrokes in this attempt, sent with every update
--   seq           id of the newest update sent; older answers are dropped
--   done          whether the latest render said the step is complete
--   panel         the explanation panel window, see ui.show_panel
--   panel_hidden  the learner hid the panel with the toggle key
--   panel_content what the panel shows (or would show, when hidden)
--   flush_pending an update is already scheduled (see on_lines)
--   restarting    a restart was sent and not answered yet; updates are held
--   clearing      the plugin itself is emptying the buffer; not typing
--   during_startup the game was started before Neovim finished starting up
--   wiping        the game buffer is being wiped (see install_hooks)
local S

-- help_lines lists the game keys, for the bottom of the panel.
local function help_lines()
  return { "", RESTART_KEY .. "  restart the step    " .. PANEL_KEY .. "  hide/show this panel" }
end

-- show_panel shows `lines` in the panel unless the learner has hidden it. The
-- content is remembered either way, so the toggle key can bring it back.
local function show_panel(lines, title, hl)
  S.panel_content = { lines = lines, title = title, hl = hl }
  if S.panel_hidden then return end
  S.panel = ui.show_panel(S.panel, S.win, S.info.width, lines, title, hl)
end

local function show_intro()
  show_panel(vim.list_extend(vim.deepcopy(S.info.intro), help_lines()), S.info.title)
end

-- on_done runs once when the step becomes complete: leave insert mode so
-- stray keys do not edit the finished code, and say how it went.
local function on_done(stats)
  vim.cmd.stopinsert()
  S.panel_hidden = false -- the result is worth showing even if the intro was hidden
  show_panel({
    "You typed the whole step.",
    "",
    ("WPM %.0f   accuracy %.1f%%   keystrokes %d   %.0fs"):format(stats.wpm, stats.accuracy, stats.keys, stats.seconds),
    "",
    "Press " .. RESTART_KEY .. " to type it again.",
  }, "step done", "GotyperDone")
end

-- apply paints a render from the engine and updates the session from it.
local function apply(render)
  ui.paint(S.buf, render)
  S.last = render
  local was_done = S.done
  S.done = render.done
  ui.set_winbar(S.win, render.stats, render.done and "  [DONE]" or "")
  if render.done and not was_done then on_done(render.stats) end
end

-- send_update sends the buffer to the engine and paints the answer.
local function send_update()
  if not S then return end
  S.flush_pending = false
  -- The buffer still holds the old attempt; begin_attempt will clear it.
  if S.restarting then return end
  S.seq = S.seq + 1
  local id = S.seq
  local cur = api.nvim_win_is_valid(S.win) and api.nvim_win_get_cursor(S.win) or { 1, 0 }
  -- The engine wants the position where the next typed character goes. In
  -- insert mode that is the cursor. In normal mode (after x, r, ...) the
  -- cursor sits on a character, so the insert position is one byte further.
  if not api.nvim_get_mode().mode:match("^[iR]") then cur[2] = cur[2] + 1 end
  S.client.request("update", {
    lines = api.nvim_buf_get_lines(S.buf, 0, -1, false),
    keys = S.keys,
    cursor = { cur[1] - 1, cur[2] }, -- Neovim rows are 1-based, the protocol's 0-based
  }, function(resp)
    -- Drop the answer if the session ended or a newer update is already on
    -- its way: painting a stale buffer state would flicker.
    if not S or id ~= S.seq then return end
    if resp.error then
      return vim.notify("gotyper engine: " .. resp.error.message, vim.log.levels.ERROR)
    end
    apply(resp.render)
  end)
end

-- on_lines is called by Neovim on every buffer change. Several changes can
-- happen in one go (a paste, a macro, `dd`), so instead of sending each one,
-- it schedules a single send_update for when Neovim is next idle.
local function on_lines()
  if not S then return true end -- returning true detaches this callback
  if S.clearing or S.flush_pending then return end
  S.flush_pending = true
  vim.schedule(send_update)
end

-- indent is the buffer's indentexpr: Neovim calls it when a new line is
-- opened (<Enter>, o, O) and indents the line by the returned number of
-- columns. The engine sent the target indentation of every line, so the
-- learner never types indentation. With noexpandtab and tabstop 4 Neovim
-- inserts it as real tabs, the way gofmt writes Go.
function M.indent(lnum)
  return S and S.info and S.info.indents[lnum] or 0
end

-- neutralise turns off, for the game buffer only, the learner's plugins that
-- would type for them or change what they typed. Nothing global is touched:
-- all of these are buffer variables or options read by those plugins.
local function neutralise(buf)
  local b, bo = vim.b[buf], vim.bo[buf]
  b.minipairs_disable = true -- mini.pairs: no auto-inserted ) ] } "
  b["nvim-autopairs"] = 1 -- nvim-autopairs: mark the buffer as already set up, so it adds no mappings
  b.completion = false -- blink.cmp: no completion menu
  b.copilot_enabled = false -- copilot.vim
  b.copilot_suggestion_hidden = true -- copilot.lua
  b.copilot_suggestion_auto_trigger = false -- copilot.lua
  b.autoformat = false -- LazyVim: no format on save
  bo.formatoptions, bo.textwidth = "", 0 -- no automatic line wrapping or comment leaders
  -- nvim-cmp keeps per-buffer settings; only touch it when it is installed.
  local ok, cmp = pcall(require, "cmp")
  if ok then pcall(cmp.setup.buffer, { enabled = false }) end
end

-- open_game_buffer creates the game buffer and shows it in a new tab.
local function open_game_buffer()
  local buf = api.nvim_create_buf(false, true)
  api.nvim_buf_set_name(buf, "gotyper://step")
  local bo = vim.bo[buf]
  -- nofile: never written to disk. wipe: deleted as soon as it is not shown,
  -- which is how closing the tab ends the session (see BufWipeout below).
  bo.buftype, bo.bufhidden, bo.swapfile = "nofile", "wipe", false
  -- Indentation comes from the engine through M.indent, as real tabs.
  bo.expandtab, bo.tabstop, bo.shiftwidth, bo.softtabstop = false, 4, 4, 0
  bo.autoindent, bo.smartindent, bo.cindent = true, false, false
  bo.indentexpr = "v:lua.require'gotyper'.indent(v:lnum)"
  bo.indentkeys = "o,O" -- recompute indent only when a line is opened
  -- The variables are set before the buffer is shown, because some plugins
  -- decide whether to attach when a buffer is first entered.
  neutralise(buf)
  -- filetype stays unset so gopls and Go ftplugins do not attach (they would
  -- add diagnostics, completion and formatting), but tree-sitter's Go parser
  -- still colours the code.
  pcall(vim.treesitter.start, buf, "go")

  vim.cmd("tab sbuffer " .. buf)
  local win = api.nvim_get_current_win()
  local wo = vim.wo[win]
  wo.wrap, wo.list, wo.spell = false, false, false
  return buf, win
end

-- clear_buffer empties the game buffer without it counting as typing, and
-- without leaving the old attempt in the undo history.
local function clear_buffer()
  local ul = vim.bo[S.buf].undolevels
  vim.bo[S.buf].undolevels = -1 -- a change made with undolevels -1 cannot be undone
  S.clearing = true
  api.nvim_buf_set_lines(S.buf, 0, -1, false, {})
  S.clearing = false
  vim.bo[S.buf].undolevels = ul
end

-- begin_attempt resets the front end for a fresh attempt and paints the
-- engine's render of the empty buffer. Used by both start and restart, which
-- answer with the same shape (engine/PROTOCOL.md).
local function begin_attempt(resp)
  S.info = resp.start
  S.keys = 0 -- the protocol requires the key count to restart from 0
  S.seq = S.seq + 1 -- answers to updates sent before this point are stale
  S.done = false
  S.flush_pending = false
  S.restarting = false
  clear_buffer()
  apply(resp.render)
  show_intro()
  if api.nvim_get_current_win() == S.win then
    api.nvim_win_set_cursor(S.win, { 1, 0 })
    vim.cmd.startinsert()
  end
end

-- install_hooks wires the session to Neovim: change tracking, key counting,
-- the game keys, panel placement on resize, and teardown.
local function install_hooks()
  local buf = S.buf
  api.nvim_buf_attach(buf, false, { on_lines = on_lines })

  -- vim.on_key sees every key the learner presses, before mappings run,
  -- including normal-mode motions and <Esc>. Only keys pressed while the game
  -- buffer is focused are counted, and none once the step is done.
  -- The panel key is not counted: showing the explanation is not typing.
  local panel_key = vim.keycode(PANEL_KEY)
  vim.on_key(function(_, typed)
    if S and typed and typed ~= "" and typed ~= panel_key and not S.done and api.nvim_get_current_buf() == S.buf then
      S.keys = S.keys + 1
    end
  end, ns_key)

  vim.keymap.set({ "n", "i" }, RESTART_KEY, M.restart, { buffer = buf, desc = "gotyper: restart the step" })
  vim.keymap.set({ "n", "i" }, PANEL_KEY, M.toggle_panel, { buffer = buf, desc = "gotyper: toggle the explanation panel" })

  local group = api.nvim_create_augroup("gotyper_session", { clear = true })
  S.augroup = group
  -- The panel is placed relative to the window size, so place it again.
  api.nvim_create_autocmd({ "VimResized", "WinResized" }, {
    group = group,
    callback = function()
      if S and S.panel_content and not S.panel_hidden and api.nvim_win_is_valid(S.win) then
        local c = S.panel_content
        S.panel = ui.show_panel(S.panel, S.win, S.info.width, c.lines, c.title, c.hl)
      end
    end,
  })
  -- Started from the command line (nvim -c Gotyper), the game tab exists
  -- before the learner's config finishes its own startup, and plugins that
  -- open a window at startup (a file tree, a dashboard) open it here. For
  -- the first two seconds, close any such window in the game tab.
  if S.during_startup then
    local function tidy()
      if not S or not api.nvim_win_is_valid(S.win) then return end
      for _, w in ipairs(api.nvim_tabpage_list_wins(api.nvim_win_get_tabpage(S.win))) do
        -- relative == "" means a normal split, not a floating window like the panel
        if w ~= S.win and api.nvim_win_get_config(w).relative == "" then pcall(api.nvim_win_close, w, true) end
      end
    end
    local until_ms = vim.uv.now() + 2000
    api.nvim_create_autocmd("BufWinEnter", {
      group = group,
      callback = function()
        if not S or vim.uv.now() > until_ms then return true end -- true deletes this autocmd
        vim.schedule(tidy) -- after the plugin has finished setting up its window
      end,
    })
    tidy() -- a window may have opened before these hooks were installed
  end
  -- Closing the tab hides the buffer, bufhidden=wipe wipes it, and that ends
  -- the session.
  api.nvim_create_autocmd("BufWipeout", { group = group, buffer = buf, once = true, callback = function()
    if S then S.wiping = true end -- the buffer is already being wiped; stop must not delete it again
    M.stop()
  end })
end

-- start opens a new game: build and launch the engine, check it speaks our
-- protocol version (hello), then ask for the step (start) and paint it.
function M.start()
  if S then M.stop() end
  ui.set_highlights()
  local bin, err = engine.ensure()
  if not bin then
    return vim.notify(err, vim.log.levels.ERROR)
  end
  local client
  client = engine.connect(bin, function(code)
    vim.notify(("gotyper: the engine exited unexpectedly (code %d)"):format(code), vim.log.levels.ERROR)
    if S and S.client == client then M.stop() end
  end)
  if not client then
    return vim.notify("gotyper: could not start the engine at " .. bin, vim.log.levels.ERROR)
  end

  -- The tab opens right away; the engine's answers arrive a few ms later.
  local buf, win = open_game_buffer()
  S = { client = client, buf = buf, win = win, seq = 0, keys = 0, done = false,
    during_startup = vim.v.vim_did_enter == 0 }

  client.request("hello", { protocol = engine.PROTOCOL }, function(hello)
    if not S or S.client ~= client then return end
    if hello.error then
      local found = hello.hello and (" (found engine %s, protocol %d)"):format(hello.hello.engine, hello.hello.protocol) or ""
      M.stop()
      return vim.notify(("gotyper: the engine and the plugin do not match%s:\n%s\n"
        .. "Rebuild the engine by deleting %s/bin and running :Gotyper again."):format(
          found, hello.error.message, engine.root), vim.log.levels.ERROR)
    end
    client.request("start", {}, function(resp)
      if not S or S.client ~= client then return end
      if not api.nvim_buf_is_valid(S.buf) then return M.stop() end -- the tab was closed already
      if resp.error then
        M.stop()
        return vim.notify("gotyper engine: " .. resp.error.message, vim.log.levels.ERROR)
      end
      install_hooks()
      begin_attempt(resp)
    end)
  end)
end

-- restart throws away the current attempt: the engine clears its error count
-- and timer, and the front end empties the buffer and its key count.
function M.restart()
  if not (S and S.info) then
    return vim.notify("gotyper: no game is running; start one with :Gotyper", vim.log.levels.WARN)
  end
  local client = S.client
  S.seq = S.seq + 1 -- drop answers to updates still in flight
  -- Hold back updates until the answer arrives: one scheduled now would send
  -- the old buffer after the restart, and the engine would judge it as the
  -- first change of the new attempt.
  S.restarting = true
  client.request("restart", {}, function(resp)
    if not S or S.client ~= client then return end
    if resp.error then
      S.restarting = false
      return vim.notify("gotyper engine: " .. resp.error.message, vim.log.levels.ERROR)
    end
    begin_attempt(resp)
  end)
end

-- toggle_panel hides or shows the explanation panel, so it never has to
-- cover code in a small terminal.
function M.toggle_panel()
  if not (S and S.panel_content) then return end
  S.panel_hidden = not S.panel_hidden
  if S.panel_hidden then
    ui.close_panel(S.panel)
    S.panel = nil
  else
    local c = S.panel_content
    show_panel(c.lines, c.title, c.hl)
  end
end

-- stop ends the session: remove our hooks, close the panel, stop the engine
-- and wipe the game buffer. Safe to call more than once.
function M.stop()
  if not S then return end
  local s = S
  S = nil -- first, so callbacks that fire during teardown see no session
  vim.on_key(nil, ns_key)
  if s.augroup then pcall(api.nvim_del_augroup_by_id, s.augroup) end
  ui.close_panel(s.panel)
  s.client.stop()
  if not s.wiping and api.nvim_buf_is_valid(s.buf) then pcall(api.nvim_buf_delete, s.buf, { force = true }) end
end

-- state returns a snapshot of the session for tests and debugging, or nil.
function M.state()
  if not S then return nil end
  return {
    buf = S.buf, win = S.win, chan = S.client.chan, info = S.info, last = S.last, keys = S.keys, done = S.done,
    panel_open = S.panel ~= nil and api.nvim_win_is_valid(S.panel.win),
  }
end

return M
