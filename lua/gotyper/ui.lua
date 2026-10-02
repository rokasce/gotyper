-- gotyper.ui paints what the engine says: ghost text, red mistakes, the stats
-- bar and the explanation panel. It decides nothing about right or wrong; the
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
  api.nvim_set_hl(0, "GotyperDone", { default = true, link = "DiagnosticOk" })
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

-- set_winbar shows the stats in the game window's own winbar (window-local, so
-- other windows keep theirs). suffix is extra text such as "[DONE]".
function M.set_winbar(win, stats, suffix)
  if not api.nvim_win_is_valid(win) then return end
  local text = (" gotyper  WPM %3.0f  ACC %5.1f%%  KEYS %d  line %d/%d  errors %d  %3.0fs%s"):format(
    stats.wpm, stats.accuracy, stats.keys, stats.line, stats.lines, stats.errors, stats.seconds, suffix or "")
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
-- recolours its text and border, as for the "done" message.
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

-- close_panel closes the panel window and wipes its scratch buffer.
function M.close_panel(panel)
  if panel and api.nvim_win_is_valid(panel.win) then api.nvim_win_close(panel.win, true) end
  if panel and api.nvim_buf_is_valid(panel.buf) then api.nvim_buf_delete(panel.buf, { force = true }) end
end

return M
