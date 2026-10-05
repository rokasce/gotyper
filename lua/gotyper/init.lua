-- gotyper is the Neovim front end of a typing game that teaches Go.
--
-- It is deliberately thin. The Go engine (engine/ in this repo) judges every
-- keystroke; this plugin only:
--
--   1. opens a game tab with a scratch buffer the learner types into,
--   2. sends the whole buffer to the engine on every change ("update"),
--   3. paints the answer (gotyper.ui): ghost text, red mistakes, stats,
--   4. asks the engine to compile and test the buffer ("check") and shows
--      the result,
--   5. in a drill, opens the buffer with the drill's start text and shows
--      the goal in a read-only split below it.
--
-- gotyper.engine owns the engine process and the wire protocol. This file
-- owns the session: the game buffer, its keys and hooks, restart and teardown.
local M = {}

local api, engine, ui = vim.api, require("gotyper.engine"), require("gotyper.ui")

-- The game keys: buffer-local mappings in the game buffer, active in both
-- normal and insert mode. To use other keys, map :GotyperRestart,
-- :GotyperPanel and :GotyperSubmit yourself.
local RESTART_KEY = "<F5>" -- throw the attempt away and type the step again
local PANEL_KEY = "<F2>" -- show or hide the explanation panel
local SUBMIT_KEY = "<F6>" -- recall steps: compile and test what is typed (go vet + go test)

-- INDENTEXPR is the type-along indentexpr: indentation comes from the engine
-- through M.indent.
local INDENTEXPR = "v:lua.require'gotyper'.indent(v:lnum)"

-- ns_key identifies our vim.on_key hook so stop() can remove exactly it.
local ns_key = api.nvim_create_namespace("gotyper_keys")

-- S is the running session, or nil when no game is open. One session at a
-- time: starting a new one stops the old. Its fields:
--   client        the engine connection (gotyper.engine.connect)
--   buf, win      the game buffer and the window showing it
--   info          the step layout from the engine's start response
--   recall        the step is a recall step (info.mode == "recall")
--   drill         the step is a drill (info.mode == "drill")
--   goal          the drill's goal window, see ui.show_goal
--   last          the latest render that was painted
--   keys          keystrokes in this attempt, sent with every update
--   seq           id of the newest update sent; older answers are dropped
--   attempt       counts attempts (start and restart), so a check answer
--                 that arrives after a restart is recognised and dropped
--   done          the step is complete: in a type-along step the latest
--                 render said so, in a recall step a check passed
--   checking      a check was sent and not answered yet
--   check         the latest check result (engine/PROTOCOL.md, "check")
--   panel         the explanation panel window, see ui.show_panel
--   panel_hidden  the learner hid the panel with the toggle key
--   panel_content what the panel shows (or would show, when hidden)
--   flush_pending an update is already scheduled (see on_lines)
--   restarting    a restart was sent and not answered yet; updates are held
--   clearing      the plugin itself is emptying the buffer; not typing
--   during_startup the game was started before Neovim finished starting up
--   wiping        the game buffer is being wiped (see install_hooks)
local S

-- help_lines lists the game keys, for the bottom of the panel. Only a
-- recall step is submitted by hand. A drill restarts from its start text.
local function help_lines()
  local lines = { "" }
  if S.recall then lines[#lines + 1] = SUBMIT_KEY .. "  submit: go vet + go test" end
  lines[#lines + 1] = RESTART_KEY .. (S.drill and "  start the drill again    " or "  restart the step    ")
    .. PANEL_KEY .. "  hide/show this panel"
  return lines
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

-- update_winbar shows the latest stats, marked while a check runs and once
-- the step is done.
local function update_winbar()
  local suffix = S.checking and "  [checking...]" or S.done and "  [DONE]" or ""
  ui.set_winbar(S.win, S.last.stats, S.info, suffix)
end

-- show_result shows a check result in the panel. A pass completes a recall
-- step, but only if the buffer is still the code that was checked: `tick` is
-- the buffer's changedtick when the check was sent, and the learner may have
-- kept typing since. A type-along step is only checked once its text
-- matches, so it is already complete.
local function show_result(c, tick)
  S.check = c
  local changed = S.recall and vim.b[S.buf].changedtick ~= tick
  if c.ok and S.recall and not S.done and not changed then
    S.done = true
    vim.cmd.stopinsert() -- the step is finished; stray keys should not edit it
  end
  local st = S.last.stats
  local lines = {
    c.ok and ("PASS  go vet + go test (%dms)"):format(c.ms) or ("FAIL  at go %s (%dms)"):format(c.stage, c.ms),
    "",
    S.recall and ("keystrokes %d   %.0fs"):format(st.keys, st.seconds)
      or ("WPM %.0f   accuracy %.1f%%   keystrokes %d   %.0fs"):format(st.wpm, st.accuracy, st.keys, st.seconds),
    "",
  }
  -- A buffer line cannot hold a newline, so the output becomes one panel
  -- line per output line.
  vim.list_extend(lines, vim.split(c.output ~= "" and c.output or "(no output)", "\n"))
  lines[#lines + 1] = ""
  if changed then
    lines[#lines + 1] = "The code changed since it was submitted; press " .. SUBMIT_KEY .. " to check it again."
  elseif not c.ok and S.recall then
    lines[#lines + 1] = "Fix it and press " .. SUBMIT_KEY .. " to check again."
  else
    lines[#lines + 1] = "Press " .. RESTART_KEY .. " to do the step again."
  end
  local title = changed and (c.ok and "check passed, code changed" or "check failed, code changed")
    or c.ok and "step passed" or "check failed"
  S.panel_hidden = false -- the result is worth showing even if the intro was hidden
  show_panel(lines, title, c.ok and "GotyperPass" or "GotyperFail")
  update_winbar()
end

-- run_check asks the engine to compile and test the buffer (the "check" op:
-- go vet, then the step's hidden tests) and shows the result. It takes a
-- second or more; the engine keeps answering updates meanwhile, so the
-- learner can keep typing.
local function run_check()
  if S.checking then return end -- one at a time; the panel already says it is running
  S.checking = true
  local client, attempt, tick = S.client, S.attempt, vim.b[S.buf].changedtick
  S.panel_hidden = false
  show_panel({ "Running go vet + go test..." }, "checking")
  update_winbar()
  client.request("check", { lines = api.nvim_buf_get_lines(S.buf, 0, -1, false) }, function(resp)
    -- Drop the answer if the game ended or was restarted meanwhile: it
    -- grades a buffer that is gone.
    if not S or S.client ~= client or S.attempt ~= attempt then return end
    S.checking = false
    if resp.error then
      update_winbar()
      return vim.notify("gotyper engine: " .. resp.error.message, vim.log.levels.ERROR)
    end
    show_result(resp.check, tick)
  end)
end

-- show_drill_result shows a finished drill in the panel: the keystrokes it
-- took against the drill's par. A drill is not compiled; reaching the goal
-- is what finishes it.
local function show_drill_result()
  local keys, par = S.last.stats.keys, S.info.par
  local verdict = keys < par and ("%d under par"):format(par - keys)
    or keys == par and "on par" or ("%d over par"):format(keys - par)
  S.panel_hidden = false -- the result is worth showing even if the intro was hidden
  show_panel({
    ("keystrokes %d   par %d   (%s)"):format(keys, par, verdict),
    ("%.0fs"):format(S.last.stats.seconds),
    "",
    "Press " .. RESTART_KEY .. " to do the drill again.",
  }, "drill done", "GotyperPass")
end

-- on_done runs when a type-along step or a drill becomes complete: leave
-- insert mode so stray keys do not edit the finished code. A type-along
-- step is then compiled and tested; a drill shows its keys against par.
local function on_done()
  vim.cmd.stopinsert()
  if S.drill then return show_drill_result() end
  run_check()
end

-- apply paints a render from the engine and updates the session from it.
local function apply(render)
  ui.paint(S.buf, render)
  S.last = render
  -- In a type-along step the engine says when the text matches, and in a
  -- drill when the buffer equals the goal. A recall step's render is never
  -- done; a passing check completes it instead.
  if not S.recall then
    local was_done = S.done
    S.done = render.done
    if S.done and not was_done then on_done() end
  end
  update_winbar()
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
  bo.indentexpr = INDENTEXPR
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

-- reset_buffer replaces the game buffer's text with `lines` (empty, or a
-- drill's start text) without it counting as typing, and without leaving the
-- old attempt in the undo history.
local function reset_buffer(lines)
  local ul = vim.bo[S.buf].undolevels
  vim.bo[S.buf].undolevels = -1 -- a change made with undolevels -1 cannot be undone
  S.clearing = true
  api.nvim_buf_set_lines(S.buf, 0, -1, false, lines)
  S.clearing = false
  vim.bo[S.buf].undolevels = ul
end

-- begin_attempt resets the front end for a fresh attempt and paints the
-- engine's render of the buffer it begins with: empty, or a drill's start
-- text. Used by both start and restart, which answer with the same shape
-- (engine/PROTOCOL.md).
local function begin_attempt(resp)
  S.info = resp.start
  S.recall = S.info.mode == "recall"
  S.drill = S.info.mode == "drill"
  S.keys = 0 -- the protocol requires the key count to restart from 0
  S.seq = S.seq + 1 -- answers to updates sent before this point are stale
  S.attempt = S.attempt + 1 -- and so is the answer to a check still running
  S.done = false
  S.checking = false
  S.check = nil
  S.flush_pending = false
  S.restarting = false
  -- In a recall step or a drill the lines need not line up with the
  -- target's, so the engine's per-line indentation would be wrong there.
  -- Neovim's smartindent indents after a { and dedents on } instead.
  local free = S.recall or S.drill
  local bo = vim.bo[S.buf]
  bo.indentexpr, bo.smartindent = free and "" or INDENTEXPR, free
  reset_buffer(S.drill and S.info.buffer or {})
  -- The goal stays on show for the whole drill; a restart reuses it.
  if S.drill and not (S.goal and api.nvim_win_is_valid(S.goal.win)) then
    S.goal = ui.show_goal(S.win, S.info.goal)
  end
  apply(resp.render)
  show_intro()
  if api.nvim_get_current_win() == S.win then
    api.nvim_win_set_cursor(S.win, { 1, 0 })
    -- A drill is played with vim's normal-mode commands, so it begins in
    -- normal mode; the other modes begin with typing.
    if S.drill then vim.cmd.stopinsert() else vim.cmd.startinsert() end
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
  -- The panel and submit keys are not counted: they are not typing.
  local panel_key, submit_key = vim.keycode(PANEL_KEY), vim.keycode(SUBMIT_KEY)
  vim.on_key(function(_, typed)
    if S and typed and typed ~= "" and typed ~= panel_key and typed ~= submit_key and not S.done
      and api.nvim_get_current_buf() == S.buf then
      S.keys = S.keys + 1
    end
  end, ns_key)

  vim.keymap.set({ "n", "i" }, RESTART_KEY, M.restart, { buffer = buf, desc = "gotyper: restart the step" })
  vim.keymap.set({ "n", "i" }, PANEL_KEY, M.toggle_panel, { buffer = buf, desc = "gotyper: toggle the explanation panel" })
  vim.keymap.set({ "n", "i" }, SUBMIT_KEY, M.submit, { buffer = buf, desc = "gotyper: submit a recall step (go vet + go test)" })

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
        -- relative == "" means a normal split, not a floating window like the
        -- panel. A drill's goal split is gotyper's own, so it stays.
        local goal = S.goal and S.goal.win
        if w ~= S.win and w ~= goal and api.nvim_win_get_config(w).relative == "" then pcall(api.nvim_win_close, w, true) end
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

-- list_steps returns every step the engine offers, in play order, each as
-- { id, title, mode, track }. On failure it returns nil and a message.
local function list_steps()
  local bin, err = engine.ensure()
  if not bin then return nil, err end
  local tracks, list_err = engine.list(bin)
  if not tracks then return nil, list_err end
  local steps = {}
  for _, track in ipairs(tracks) do
    for _, step in ipairs(track.steps) do
      steps[#steps + 1] = { id = step.id, title = step.title, mode = step.mode, track = track.id }
    end
  end
  return steps
end

-- step_ids returns the id of every step the engine offers, for completing
-- the argument of :Gotyper. It returns an empty list when the engine cannot
-- be built or run, since completion has no good place to show an error.
function M.step_ids()
  local ids = {}
  for _, step in ipairs(list_steps() or {}) do ids[#ids + 1] = step.id end
  return ids
end

-- pick lets the learner choose a step and starts it. It uses vim.ui.select,
-- so a picker plugin in the learner's config (telescope, fzf-lua, ...) shows
-- the list if it replaces vim.ui.select; plain Neovim shows a numbered list.
function M.pick()
  local steps, err = list_steps()
  if not steps then return vim.notify(err, vim.log.levels.ERROR) end
  vim.ui.select(steps, {
    prompt = "gotyper: pick a step",
    format_item = function(step) return ("%s: %s (%s)"):format(step.track, step.title, step.mode) end,
  }, function(step)
    if step then M.start(step.id) end -- step is nil when the learner cancels
  end)
end

-- start opens a new game of the step with id `step` (see list_steps): build
-- and launch the engine, check it speaks our protocol version (hello), then
-- ask for the step (start) and paint it.
function M.start(step)
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
  S = { client = client, buf = buf, win = win, seq = 0, attempt = 0, keys = 0, done = false,
    during_startup = vim.v.vim_did_enter == 0 }

  client.request("hello", { protocol = engine.PROTOCOL }, function(hello)
    if not S or S.client ~= client then return end
    if hello.error then
      M.stop()
      return vim.notify(engine.mismatch_message(hello), vim.log.levels.ERROR)
    end
    client.request("start", { step = step }, function(resp)
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
-- and timer, and the front end empties the buffer (or, in a drill, puts the
-- start text back) and resets its key count.
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

-- submit compiles and tests what is typed (go vet + go test) and shows the
-- result, in a recall step. A pass completes the step; a fail shows what went
-- wrong and the learner can keep editing. A type-along step is not submitted:
-- it is checked by itself when its text matches (see on_done).
function M.submit()
  if not (S and S.info) then
    return vim.notify("gotyper: no game is running; start one with :Gotyper", vim.log.levels.WARN)
  end
  if S.drill then
    return vim.notify("gotyper: a drill is done when the buffer matches the goal", vim.log.levels.INFO)
  end
  if not S.recall then
    return vim.notify("gotyper: type-along steps are checked automatically when finished", vim.log.levels.INFO)
  end
  run_check()
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

-- show_stats opens a floating window with the bests of every step completed
-- so far: best WPM and accuracy, fewest keystrokes, how often it was
-- completed and when it was last played. The engine keeps them in its stats
-- file (engine/PROTOCOL.md, "stats"); a short-lived engine reads them, so this
-- works with or without a game running.
function M.show_stats()
  local bin, err = engine.ensure()
  if not bin then return vim.notify(err, vim.log.levels.ERROR) end
  local steps, stats_err = engine.stats(bin)
  if not steps then return vim.notify(stats_err, vim.log.levels.ERROR) end
  ui.show_stats(steps)
end

-- stop ends the session: remove our hooks, close the panel and a drill's
-- goal, stop the engine and wipe the game buffer. Safe to call more than
-- once.
function M.stop()
  if not S then return end
  local s = S
  S = nil -- first, so callbacks that fire during teardown see no session
  vim.on_key(nil, ns_key)
  if s.augroup then pcall(api.nvim_del_augroup_by_id, s.augroup) end
  ui.close_panel(s.panel)
  pcall(ui.close_panel, s.goal) -- the goal is a { win, buf } pair like the panel
  s.client.stop()
  if not s.wiping and api.nvim_buf_is_valid(s.buf) then pcall(api.nvim_buf_delete, s.buf, { force = true }) end
end

-- state returns a snapshot of the session for tests and debugging, or nil.
function M.state()
  if not S then return nil end
  return {
    buf = S.buf, win = S.win, chan = S.client.chan, info = S.info, last = S.last, keys = S.keys, done = S.done,
    checking = S.checking, check = S.check,
    panel_open = S.panel ~= nil and api.nvim_win_is_valid(S.panel.win),
    panel_title = S.panel_content and S.panel_content.title,
    goal_open = S.goal ~= nil and api.nvim_win_is_valid(S.goal.win),
  }
end

return M
