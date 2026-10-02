-- gotyper.engine starts the Go engine and talks to it.
--
-- The engine is a separate program (engine/ in this repo). This module builds
-- it when needed, runs it as a Neovim job, and speaks the NDJSON protocol from
-- engine/PROTOCOL.md: one JSON object per line on the job's stdin, one JSON
-- answer per line on its stdout. Everything here is asynchronous, so Neovim
-- never waits for the engine.
local M = {}

-- PROTOCOL is the protocol version this front end speaks. It is sent in the
-- hello handshake and must equal protocol.Version in engine/protocol/protocol.go.
M.PROTOCOL = 1

-- root is the repository root. This file is <root>/lua/gotyper/engine.lua, so
-- strip three path components from its own location. Lua's debug.getinfo
-- reports the source as "@/path/to/file", hence the sub(2).
local root = vim.fn.fnamemodify(debug.getinfo(1, "S").source:sub(2), ":h:h:h")
M.root = root

-- go_binary returns the path of the go command, or nil when Go is not
-- installed. Besides PATH it checks the usual install locations, because
-- Neovim started from a desktop launcher may not see the shell's PATH.
local function go_binary()
  local found = vim.fn.exepath("go")
  if found ~= "" then return found end
  for _, p in ipairs({ "~/.local/go/bin/go", "~/.local/bin/go", "/usr/local/go/bin/go", "~/go/bin/go" }) do
    local path = vim.fn.expand(p)
    if vim.fn.executable(path) == 1 then return path end
  end
  return nil
end

-- newest_source returns the modification time of the newest file under
-- engine/, so ensure() can tell whether the built binary is out of date.
local function newest_source()
  local newest = 0
  for _, f in ipairs(vim.fn.glob(root .. "/engine/**/*", false, true)) do
    newest = math.max(newest, vim.fn.getftime(f))
  end
  return newest
end

-- ensure returns the path of an up-to-date engine binary, building it into
-- <root>/bin/ first when it is missing or older than any engine source file.
-- On failure it returns nil and a message for the learner.
function M.ensure()
  local bin = root .. "/bin/gotyper-engine"
  local built = vim.fn.getftime(bin) -- -1 when the file does not exist
  if built >= newest_source() then return bin end

  local go = go_binary()
  if not go then
    if built >= 0 then
      vim.notify("gotyper: engine sources changed but Go is not installed; using the old engine",
        vim.log.levels.WARN)
      return bin
    end
    return nil, "gotyper needs Go to build its engine, but the go command was not found.\n"
      .. "Install Go (https://go.dev/dl/) and make sure `go` is on your PATH."
  end

  vim.notify("gotyper: building the engine...")
  vim.cmd.redraw() -- show the message now; the build below blocks the UI
  -- vim.system runs a command; :wait() blocks until it exits. A cold build
  -- takes a few seconds, later ones are cached by Go and are much faster.
  local r = vim.system({ go, "build", "-o", bin, "./cmd/gotyper-engine" }, { cwd = root .. "/engine" }):wait()
  if r.code ~= 0 then
    return nil, "gotyper: building the engine failed:\n" .. (r.stderr or "")
  end
  return bin
end

-- connect starts the engine binary as a job and returns a client with:
--
--   client.request(op, fields, cb)  send {"id":N,"op":op,...fields}; cb(response)
--                                   is called with the decoded response line
--   client.stop()                   end the job (closing stdin makes it exit)
--
-- on_exit(code) is called if the engine exits on its own.
function M.connect(bin, on_exit)
  local client = {}
  local pending = {} -- request id -> callback waiting for that response
  local next_id = 0
  local partial = "" -- an incomplete line left over from the last stdout chunk
  local stopped = false

  local function on_line(line)
    if line == "" then return end
    -- luanil turns JSON null into Lua nil instead of the vim.NIL sentinel,
    -- so absent and null fields both read as nil.
    local ok, msg = pcall(vim.json.decode, line, { luanil = { object = true, array = true } })
    if not ok or type(msg) ~= "table" then
      vim.notify("gotyper: unreadable engine line: " .. line, vim.log.levels.ERROR)
      return
    end
    local cb = msg.id and pending[msg.id]
    if msg.id then pending[msg.id] = nil end
    if cb then
      cb(msg)
    elseif msg.error then
      vim.notify("gotyper engine: " .. msg.error.message, vim.log.levels.ERROR)
    end
  end

  client.chan = vim.fn.jobstart({ bin }, {
    -- Job output arrives in chunks that need not end on a line boundary.
    -- Neovim splits each chunk on "\n": every element but the last is the
    -- end of a complete line, and the last is the start of the next one.
    on_stdout = function(_, data)
      data[1] = partial .. data[1]
      partial = table.remove(data)
      for _, line in ipairs(data) do on_line(line) end
    end,
    on_stderr = function(_, data)
      local s = table.concat(data, "\n")
      if s:match("%S") then
        vim.schedule(function() vim.notify("gotyper engine: " .. s, vim.log.levels.WARN) end)
      end
    end,
    on_exit = function(_, code)
      if not stopped and on_exit then on_exit(code) end
    end,
  })
  if client.chan <= 0 then return nil end

  function client.request(op, fields, cb)
    next_id = next_id + 1
    pending[next_id] = cb
    local msg = vim.tbl_extend("force", { id = next_id, op = op }, fields or {})
    vim.fn.chansend(client.chan, vim.json.encode(msg) .. "\n")
  end

  function client.stop()
    stopped = true
    pcall(vim.fn.jobstop, client.chan)
  end

  return client
end

return M
