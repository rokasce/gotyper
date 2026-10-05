-- gotyper.ui paints what the engine says: ghost text, red mistakes, the stats
-- bar, the explanation panel and a drill's goal. It decides nothing about right or wrong; the
-- engine's render (engine/PROTOCOL.md, "update") already holds every position.
local M = {}

local api = vim.api

-- ns groups every highlight and virtual text gotyper adds to the game buffer,
-- so one call can clear them all before the next paint.
M.ns = api.nvim_create_namespace("gotyper")

-- set_highlights defines gotyper's highlight groups. default = true means a
-- colorscheme or the learner's config can override them.
function M.set_highlights()
  -- Ghost text uses the colorscheme's NonText colour, in italics. Not Comment:
  -- many themes (rose-pine, for one) give punctuation the Comment colour, so
  -- typed `{},` would look untyped.
  local nontext = api.nvim_get_hl(0, { name = "NonText", link = false })
  api.nvim_set_hl(0, "GotyperGhost", { default = true, fg = nontext.fg, italic = true })
  -- The background makes a wrong space visible.
  api.nvim_set_hl(0, "GotyperError", { default = true, fg = "#ff6b6b", bg = "#4a1c1c", bold = true })
  -- A check result: the panel turns green for a pass, red for a fail.
  api.nvim_set_hl(0, "GotyperPass", { default = true, link = "DiagnosticOk" })
  api.nvim_set_hl(0, "GotyperFail", { default = true, link = "DiagnosticError" })
end

-- paint replaces the game buffer's ghosts and red spans with those in render.
-- The buffer itself only ever holds what the learner typed; the target code
-- is drawn as virtual text that the cursor and motions do not see.
function M.paint(buf, render)
  api.nvim_buf_clear_namespace(buf, M.ns, 0, -1)
  -- strict = false: if the buffer changed again since this render was asked
  -- for, a position may be past the end of a line. Draw what fits instead of
  -- raising an error; the next render corrects it.
  for _, e in ipairs(render.error_spans or {}) do
    api.nvim_buf_set_extmark(buf, M.ns, e.row, e.col, {
      end_col = e.end_col, hl_group = "GotyperError", priority = 200, strict = false,
    })
  end
  -- The untyped rest of a started line, drawn inline right after the typed
  -- text, so the cursor sits just before it.
  for _, g in ipairs(render.ghosts or {}) do
    api.nvim_buf_set_extmark(buf, M.ns, g.row, g.col, {
      virt_text = { { g.text, "GotyperGhost" } }, virt_text_pos = "inline", strict = false,
    })
  end
  -- Target lines not reached yet, hung as virtual lines below the last real
  -- line. An empty virtual line needs one space, or Neovim drops it.
  local lines = {}
  for _, t in ipairs(render.ghost_lines or {}) do
    lines[#lines + 1] = { { t == "" and " " or t, "GotyperGhost" } }
  end
  if #lines > 0 then
    api.nvim_buf_set_extmark(buf, M.ns, api.nvim_buf_line_count(buf) - 1, 0, { virt_lines = lines, strict = false })
  end
end

-- MODE_LABELS names vim's modes by the first character of
-- nvim_get_mode().mode, as Neovim's own -- INSERT -- message does. Only the
-- first character matters: "no" is operator-pending (after d, c, y ...),
-- still a normal-mode command being typed, so it reads NORMAL, and "ic" is
-- insert mode with the completion menu open.
local MODE_LABELS = {
  n = "NORMAL", i = "INSERT", R = "REPLACE", c = "COMMAND",
  v = "VISUAL", V = "V-LINE", ["\22"] = "V-BLOCK", -- \22 is CTRL-V
  s = "SELECT", S = "S-LINE", ["\19"] = "S-BLOCK", -- \19 is CTRL-S
}

-- mode_label turns a mode from nvim_get_mode().mode, such as "i", "no" or
-- "V", into the short label the winbar shows, such as INSERT or V-LINE.
-- Seeing the mode helps a learner who jumps around with motions instead of
-- only typing: a key typed in the wrong mode is a command, not text.
function M.mode_label(mode)
  return MODE_LABELS[mode:sub(1, 1)] or mode:upper()
end

-- set_winbar shows the stats in the game window's own winbar (window-local, so
-- other windows keep theirs). info is the step layout from the engine's start
-- answer, for the mode and the par, and mode is vim's current mode (see
-- mode_label). Every step shows its keystrokes against par: for a drill a
-- good way to do it, otherwise the fewest keys that type the code. A recall
-- step has only keystrokes and time to show: without a target there is no
-- accuracy or line count. suffix is extra text such as "[DONE]".
function M.set_winbar(win, stats, info, mode, suffix)
  if not api.nvim_win_is_valid(win) then return end
  local label, keys = M.mode_label(mode), ("KEYS %d/%d par"):format(stats.keys, info.par)
  local text
  if info.mode == "recall" then
    text = (" gotyper  %s  recall  %s  %3.0fs%s"):format(label, keys, stats.seconds, suffix or "")
  elseif info.mode == "drill" then
    text = (" gotyper  %s  drill  %s  %3.0fs%s"):format(label, keys, stats.seconds, suffix or "")
  else
    text = (" gotyper  %s  WPM %3.0f  ACC %5.1f%%  %s  line %d/%d  errors %d  %3.0fs%s"):format(
      label, stats.wpm, stats.accuracy, keys, stats.line, stats.lines, stats.errors, stats.seconds, suffix or "")
  end
  -- In a statusline expression % starts an item, so a literal % is written %%.
  vim.wo[win].winbar = "%#TabLineSel#" .. text:gsub("%%", "%%%%") .. "%#Normal#"
end

-- panel_config places the explanation panel: right of the code when the
-- window is wide enough for both, otherwise in its bottom-right corner.
-- code_width is the display width of the widest target line.
local function panel_config(win, lines, code_width)
  local ww, wh = api.nvim_win_get_width(win), api.nvim_win_get_height(win)
  local width = 20
  for _, l in ipairs(lines) do width = math.max(width, vim.fn.strdisplaywidth(l)) end
  width = math.max(1, math.min(width, ww - 4, 90))
  local height = 0 -- long lines wrap, so count the screen rows each one needs
  for _, l in ipairs(lines) do height = height + math.max(1, math.ceil(vim.fn.strdisplaywidth(l) / width)) end
  local textoff = vim.fn.getwininfo(win)[1].textoff -- columns used by the number/sign columns
  local beside = ww - textoff - code_width - 4 >= width + 2
  return {
    relative = "win", win = win, width = width, height = math.max(1, math.min(height, wh - 2)),
    anchor = beside and "NE" or "SE", col = ww - 1, row = beside and 0 or wh - 1,
  }
end

-- show_panel opens (or updates) a floating window over the game window with
-- the given lines and title, and returns the panel { win, buf }. hl, when set,
-- recolours its text and border, as for a check result.
function M.show_panel(panel, win, code_width, lines, title, hl)
  local cfg = panel_config(win, lines, code_width)
  cfg.title = " " .. title .. " "
  if panel and api.nvim_win_is_valid(panel.win) then
    api.nvim_buf_set_lines(panel.buf, 0, -1, false, lines)
    api.nvim_win_set_config(panel.win, cfg)
  else
    local buf = api.nvim_create_buf(false, true)
    api.nvim_buf_set_lines(buf, 0, -1, false, lines)
    -- focusable = false keeps the cursor out of it: the learner's keys always
    -- go to the code.
    local pwin = api.nvim_open_win(buf, false, vim.tbl_extend("force", cfg, {
      style = "minimal", border = "rounded", focusable = false, zindex = 50, title_pos = "left",
    }))
    vim.wo[pwin].wrap = true
    panel = { win = pwin, buf = buf }
  end
  vim.wo[panel.win].winhighlight = hl and ("Normal:" .. hl .. ",FloatBorder:" .. hl) or ""
  return panel
end

-- show_goal opens a drill's goal, the code the learner edits the buffer
-- into, in a read-only split below the game window `win`, and returns it as
-- { win, buf }. The cursor stays in the game window. A split rather than a
-- floating window, because the goal is as tall as the code and must not
-- cover it; the explanation panel keeps floating over the game window.
function M.show_goal(win, goal)
  local buf = api.nvim_create_buf(false, true)
  api.nvim_buf_set_lines(buf, 0, -1, false, goal)
  local bo = vim.bo[buf]
  bo.modifiable, bo.bufhidden, bo.tabstop = false, "wipe", 4
  pcall(vim.treesitter.start, buf, "go") -- colour it like the game buffer
  local gwin = api.nvim_open_win(buf, false, { split = "below", win = win, height = #goal })
  local wo = vim.wo[gwin]
  wo.wrap, wo.list, wo.spell, wo.number, wo.relativenumber = false, false, false, false, false
  wo.winbar = "%#TabLine# goal: edit the buffer above until it matches this %#Normal#"
  return { win = gwin, buf = buf }
end

-- stats_lines formats the engine's per-step bests (the `stats` op) as a
-- table, one row per step, for show_stats. A recall step has no WPM or
-- accuracy (there is no target to compare against), and a drill is scored by
-- keystrokes alone, so for both those show "-".
local function stats_lines(steps)
  if #steps == 0 then return { "No step completed yet. Finish one with :Gotyper and it shows up here." } end
  local width = #"step"
  for _, st in ipairs(steps) do width = math.max(width, #st.step) end
  local row = "%-" .. width .. "s  %8s  %8s  %11s  %5s  %s"
  local lines = { row:format("step", "best WPM", "best acc", "fewest keys", "done", "last played") }
  for _, st in ipairs(steps) do
    local no_wpm = st.mode == "recall" or st.mode == "drill"
    -- last_played is RFC 3339, "2026-10-01T10:00:00.123+03:00": keep the
    -- date and the hour and minute.
    local when = st.last_played:sub(1, 10) .. " " .. st.last_played:sub(12, 16)
    lines[#lines + 1] = row:format(st.step,
      no_wpm and "-" or ("%.0f"):format(st.best_wpm),
      no_wpm and "-" or ("%.1f%%"):format(st.best_accuracy),
      st.fewest_keys, st.completions, when)
  end
  return lines
end

-- show_stats opens a centred floating window listing `steps`, the engine's
-- per-step bests, and moves the cursor into it. q or <Esc> closes it. It
-- returns the window.
function M.show_stats(steps)
  local lines = stats_lines(steps)
  local width = 0
  for _, l in ipairs(lines) do width = math.max(width, vim.fn.strdisplaywidth(l)) end
  width = math.max(1, math.min(width, vim.o.columns - 4))
  local height = math.max(1, math.min(#lines, vim.o.lines - 4))
  local buf = api.nvim_create_buf(false, true)
  api.nvim_buf_set_lines(buf, 0, -1, false, lines)
  vim.bo[buf].modifiable = false
  vim.bo[buf].bufhidden = "wipe" -- closing the window deletes the buffer too
  local win = api.nvim_open_win(buf, true, {
    relative = "editor", width = width, height = height, style = "minimal", border = "rounded",
    row = math.floor((vim.o.lines - height) / 2) - 1, col = math.floor((vim.o.columns - width) / 2),
    title = " gotyper stats ", title_pos = "left",
  })
  for _, k in ipairs({ "q", "<Esc>" }) do
    vim.keymap.set("n", k, function() api.nvim_win_close(win, true) end, { buffer = buf, nowait = true, desc = "gotyper: close the stats" })
  end
  return win
end

-- close_panel closes the panel window and wipes its scratch buffer.
function M.close_panel(panel)
  if panel and api.nvim_win_is_valid(panel.win) then api.nvim_win_close(panel.win, true) end
  if panel and api.nvim_buf_is_valid(panel.buf) then api.nvim_buf_delete(panel.buf, { force = true }) end
end

return M
