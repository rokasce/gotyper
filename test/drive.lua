-- Drives a running gotyper Neovim over RPC the way a learner would, and
-- checks what it paints. Started by test/run.sh; exits non-zero on failure.
--
-- usage: nvim -l test/drive.lua <socket> <repo root> [delay_ms]
local sock, root, delay = arg[1], arg[2], tonumber(arg[3] or "20")

-- Connect, retrying while the game Neovim starts (and builds the engine).
local ch
for _ = 1, 600 do
  local ok, c = pcall(vim.fn.sockconnect, "pipe", sock, { rpc = true })
  if ok and c > 0 then
    ch = c
    break
  end
  vim.uv.sleep(100)
end
assert(ch, "could not connect to " .. sock)

local function rq(...) return vim.rpcrequest(ch, ...) end
local function lua(code) return rq("nvim_exec_lua", code, {}) end
local function state()
  local s = lua("return require('gotyper').state()")
  return s ~= vim.NIL and s or nil
end

-- wait polls the session until pred(state) holds, or fails after ms.
local function wait(pred, ms)
  local t = vim.uv.now()
  while vim.uv.now() - t < (ms or 60000) do
    local s = state()
    if s and pred(s) then return s end
    vim.uv.sleep(20)
    vim.uv.update_time()
  end
  error("timeout waiting; state=" .. vim.inspect(state()))
end

local function key(k)
  rq("nvim_input", k)
  vim.uv.sleep(delay)
end
local function type_text(s)
  for c in s:gmatch(".") do key(c == "<" and "<lt>" or c) end
end

-- marks counts what is painted in the game buffer.
local function marks()
  local m = lua([[
    local s = require('gotyper').state()
    return vim.api.nvim_buf_get_extmarks(s.buf, require('gotyper.ui').ns, 0, -1, { details = true })]])
  local out = { err = 0, ghost = 0, vlines = 0 }
  for _, x in ipairs(m) do
    local d = x[4]
    if d.hl_group == "GotyperError" then out.err = out.err + 1 end
    if d.virt_text then out.ghost = out.ghost + 1 end
    if d.virt_lines then out.vlines = #d.virt_lines end
  end
  return out
end

local function check(cond, msg)
  print((cond and "ok   " or "FAIL ") .. msg)
  if not cond then
    pcall(rq, "nvim_command", "qa!")
    os.exit(1)
  end
end

-- The step's target code, the same file the engine loads from lessons/.
local target = {}
for l in io.lines(root .. "/lessons/json-api/01-greet-handler/handler.go") do target[#target + 1] = l end

-- 1. Ghost display: an empty buffer shows the first line inline and the rest
--    as virtual lines, with the explanation panel open.
local s = wait(function(st) return st.last ~= vim.NIL and st.last ~= nil end)
local m = marks()
check(m.ghost == 1 and m.vlines == #target - 1,
  ("ghost text: 1 inline + %d virtual lines (got %d inline, %d virtual)"):format(#target - 1, m.ghost, m.vlines))
check(rq("nvim_get_mode").mode == "i", "starts in insert mode")
check(s.panel_open, "explanation panel is open")
check(lua("return vim.wo[require('gotyper').state().win].winbar"):find("WPM", 1, true) ~= nil, "winbar shows the stats")

-- 2. A mistake turns red and is charged.
type_text("packxge")
s = wait(function(st) return st.last.stats.keys >= 7 and #st.last.error_spans == 1 end)
check(marks().err == 1, "mistake 'packxge' is highlighted red")
check(s.last.error_spans[1].col == 4 and s.last.error_spans[1].end_col == 5, "the red span is exactly the wrong byte")
check(s.last.stats.errors == 1, "the mistake is charged")

-- 3. The panel toggle key hides and shows the panel.
key("<F2>")
s = wait(function(st) return not st.panel_open end, 5000)
check(not s.panel_open, "<F2> hides the panel")
key("<F2>")
s = wait(function(st) return st.panel_open end, 5000)
check(s.panel_open, "<F2> shows it again")
check(s.keys == 7, "the panel key is not counted as typing")

-- 4. Restart throws the attempt away: empty buffer, no red, counts back to 0.
key("<F5>")
s = wait(function(st) return st.last.stats.errors == 0 and #st.last.error_spans == 0 end, 5000)
check(#rq("nvim_buf_get_lines", s.buf, 0, -1, false) == 1 and rq("nvim_buf_get_lines", s.buf, 0, -1, false)[1] == "",
  "restart empties the buffer")
m = marks()
check(m.err == 0 and m.ghost == 1 and m.vlines == #target - 1, "restart repaints the empty attempt")
check(s.keys == 0 and s.last.stats.keys == 0, "restart resets the key count")
check(rq("nvim_get_mode").mode == "i", "restart returns to insert mode")
-- A key typed in the same input chunk as <F5> must not leak into the new
-- attempt: its update is still scheduled when the restart is sent.
rq("nvim_input", "x<F5>")
vim.uv.sleep(200)
key("p")
s = wait(function(st) return st.last.stats.keys >= 1 end, 5000)
check(s.last.stats.errors == 0 and #s.last.error_spans == 0,
  "a change typed right before restart is not judged in the new attempt (errors " .. s.last.stats.errors .. ")")
key("<F5>")
s = wait(function(st) return st.keys == 0 and st.last.stats.keys == 0 end, 5000)
rq("nvim_command", "stopinsert")
vim.uv.sleep(50)
check(rq("nvim_get_mode").mode == "n", "(left insert mode)")
rq("nvim_command", "GotyperRestart")
local mode
for _ = 1, 100 do
  mode = rq("nvim_get_mode").mode
  if mode == "i" then break end
  vim.uv.sleep(20)
end
check(mode == "i", ":GotyperRestart restarts too")

-- 5. Type the whole step. Indentation is never typed: <Enter> inserts it.
type_text(target[1])
for i = 2, #target do
  key("<CR>")
  type_text((target[i]:gsub("^\t+", "")))
end
s = wait(function(st) return st.done end)
local buf = rq("nvim_buf_get_lines", s.buf, 0, -1, false)
check(buf[14] == target[14] and buf[15] == target[15], "auto-indent inserted real tabs (" .. vim.inspect(buf[15]) .. ")")
check(s.last.done and s.last.stats.errors == 0 and s.last.stats.line == #target, "the engine reports the step done")
check(rq("nvim_get_mode").mode == "n", "done leaves insert mode")
-- Finishing a type-along step runs the check (go vet + go test) by itself.
s = wait(function(st) return st.check ~= nil and not st.checking end, 120000)
check(s.check.ok and s.panel_title == "step passed",
  ("finishing runs go vet + go test, and it passes (%dms: %s)"):format(s.check.ms, s.check.output))
check(lua("return vim.wo[require('gotyper').state().win].winbar"):find("[DONE]", 1, true) ~= nil, "winbar says DONE")
print(("stats: wpm=%.1f acc=%.1f%% keys=%d"):format(s.last.stats.wpm, s.last.stats.accuracy, s.last.stats.keys))

-- 6. Closing the tab ends the session cleanly.
local game_buf, chan = s.buf, s.chan
rq("nvim_command", "tabclose")
vim.uv.sleep(200)
check(state() == nil, "closing the tab ends the session")
check(not rq("nvim_buf_is_valid", game_buf), "the game buffer is wiped")
check(rq("nvim_call_function", "jobwait", { { chan }, 1000 })[1] ~= -1, "the engine job has stopped")
check(lua("return pcall(vim.api.nvim_get_autocmds, { group = 'gotyper_session' })") == false, "its autocmds are removed")
check(lua("return #vim.api.nvim_list_tabpages()") == 1, "and its tab is gone")

-- 7. :Gotyper completes step ids, and without an argument offers the steps
--    in vim.ui.select. The test stands in for the picker by calling its
--    selection callback, as a learner choosing an item would.
local step_id = "json-api/01-greet-handler"
check(vim.tbl_contains(lua("return vim.fn.getcompletion('Gotyper json', 'cmdline')"), step_id),
  ":Gotyper completes step ids")
lua([[
  vim.ui.select = function(items, opts, on_choice)
    _G.gotyper_pick = { items = items, labels = vim.tbl_map(opts.format_item, items) }
    _G.gotyper_choose = on_choice
  end
  vim.cmd('Gotyper')
]])
local pick = lua("return _G.gotyper_pick")
check(pick.items[1].id == step_id, "the picker offers the steps by id")
check(pick.labels[1] == "json-api: Step 1 - a JSON handler with errors as values (type-along)",
  "each step shows its track, title and mode (" .. pick.labels[1] .. ")")
lua("_G.gotyper_choose(nil)")
check(state() == nil and #rq("nvim_list_tabpages") == 1, "cancelling the picker starts nothing")
lua("_G.gotyper_choose(_G.gotyper_pick.items[1])")
s = wait(function(st) return st.info ~= vim.NIL and st.info ~= nil and st.last ~= vim.NIL and st.last ~= nil end, 10000)
check(s.info.step == step_id, "choosing a step starts it")
rq("nvim_command", "tabclose")
vim.uv.sleep(200)
check(state() == nil, "(closed it again)")

-- 8. A recall step: no ghost text, no red, and <F6> submits what was written.
--    It is started the way the learner starts any step: from the picker.
lua([[
  for _, step in ipairs(_G.gotyper_pick.items) do
    if step.id == 'json-api/02-greet-handler-recall' then return _G.gotyper_choose(step) end
  end
]])
s = wait(function(st) return st.info ~= nil and st.last ~= nil end)
check(s.info.mode == "recall", "the recall step starts")
check(marks().ghost == 0 and marks().vlines == 0, "a recall step shows no ghost text")
type_text("packx")
s = wait(function(st) return st.last.stats.keys >= 5 end, 5000)
m = marks()
check(m.err == 0 and m.ghost == 0 and #s.last.error_spans == 0, "and nothing turns red")
check(lua("return vim.wo[require('gotyper').state().win].winbar"):find("recall  KEYS 5", 1, true) ~= nil,
  "winbar shows the keystrokes")
-- Write the handler but forget the return after http.Error: it compiles,
-- and the hidden test catches it.
local forgot, ret_row = {}, nil
for i, l in ipairs(target) do
  if l == "\t\treturn" then
    ret_row = i - 1 -- the line before it, 1-based, in the buffer without it
  else
    forgot[#forgot + 1] = l
  end
end
rq("nvim_buf_set_lines", s.buf, 0, -1, false, forgot)
key("<F6>")
s = wait(function(st) return st.check ~= nil and not st.checking end, 120000)
check(not s.check.ok and s.check.stage == "test" and s.check.output:find("did you return?", 1, true) ~= nil,
  "submitting without the return fails go test")
check(not s.done and s.panel_title == "check failed" and s.panel_open, "the failure is shown and the step goes on")
check(rq("nvim_get_mode").mode == "i", "the learner can keep editing")
-- Fix it with ordinary vim editing, then submit again.
key("<Esc>")
rq("nvim_win_set_cursor", s.win, { ret_row, 0 })
key("o")
type_text("return")
key("<Esc>")
check(rq("nvim_buf_get_lines", s.buf, ret_row, ret_row + 1, false)[1] == "\t\treturn",
  "smartindent keeps the indentation of the line above")
rq("nvim_command", "GotyperSubmit")
s = wait(function(st) return not st.checking and st.check.ok end, 120000)
check(s.done and s.panel_title == "step passed", "submitting the fixed code passes and completes the step")
check(lua("return vim.wo[require('gotyper').state().win].winbar"):find("[DONE]", 1, true) ~= nil, "winbar says DONE")
rq("nvim_command", "tabclose")
vim.uv.sleep(200)
check(state() == nil, "closing the tab ends the recall game")

-- 9. A front end speaking another protocol version gets a clear message.
lua([[
  _G.gotyper_msgs = {}
  vim.notify = function(msg) table.insert(_G.gotyper_msgs, msg) end
  require('gotyper.engine').PROTOCOL = 2
  vim.cmd('Gotyper json-api/01-greet-handler')
]])
local msgs
for _ = 1, 100 do
  msgs = lua("return table.concat(_G.gotyper_msgs, '\\n')")
  if msgs:find("do not match", 1, true) then break end
  vim.uv.sleep(50)
end
check(msgs:find("do not match", 1, true) and msgs:find("protocol 2", 1, true), "a version mismatch is reported")
check(state() == nil and #rq("nvim_list_tabpages") == 1, "and no game is left open")

pcall(rq, "nvim_command", "qa!")
