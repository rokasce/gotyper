-- gotyper.health is gotyper's :checkhealth report. Running
-- `:checkhealth gotyper` makes Neovim load this module and call check(),
-- which walks through what the game needs, in the order it needs it: Go to
-- build the engine, the engine binary, the engine agreeing with the plugin
-- on the protocol, and the lessons. Each failed check says how to fix it.
local engine = require("gotyper.engine")

local M = {}

-- check_go reports whether the go command is on PATH and which version it
-- is. It returns true when Go was found, so later checks know whether the
-- engine can be built.
local function check_go()
  local go = engine.go_binary()
  if not go then
    vim.health.error("the go command was not found on PATH", {
      "Install Go (https://go.dev/dl/) and make sure `go` is on your PATH; gotyper needs it to build its engine and to check your code.",
    })
    return false
  end
  local r = vim.system({ go, "version" }, { text = true }):wait()
  if r.code ~= 0 then
    vim.health.error(("`%s version` failed: %s"):format(go, r.stderr or ""), {
      "Check that your Go installation works: run `go version` in a terminal.",
    })
    return false
  end
  vim.health.ok(vim.trim(r.stdout) .. " (" .. go .. ")")
  return true
end

-- check_binary reports whether the engine binary is built and up to date.
-- When it is not, it builds it the way :Gotyper would, so the report shows
-- whether building works. It returns the binary's path, or nil when there
-- is no usable binary.
local function check_binary(have_go)
  if engine.up_to_date() then
    vim.health.ok("engine binary is up to date: " .. engine.bin)
    return engine.bin
  end
  if not have_go then
    vim.health.error("engine binary is missing or older than its sources: " .. engine.bin, {
      "Install Go (see above); :Gotyper then builds the engine.",
    })
    return nil
  end
  local bin, err = engine.ensure()
  if not bin then
    vim.health.error("engine binary is missing or out of date, and building it failed:\n" .. err, {
      "Fix the build error above, or reinstall the plugin to get clean engine sources.",
    })
    return nil
  end
  vim.health.ok("engine binary was missing or out of date and is now built: " .. bin)
  return bin
end

-- check_engine runs the engine, sends it the hello handshake and asks for
-- its lessons (the list op). It reports whether the handshake succeeded and
-- returns the engine's tracks, or nil when the engine did not answer.
local function check_engine(bin)
  if not bin then
    vim.health.error("engine handshake skipped: there is no engine binary", {
      "Fix the engine binary check above first.",
    })
    return nil
  end
  local tracks, err = engine.list(bin)
  if not tracks then
    vim.health.error("engine handshake failed:\n" .. err, {
      ("Rebuild the engine: delete %s/bin and run :checkhealth gotyper again."):format(engine.root),
    })
    return nil
  end
  vim.health.ok(("engine handshake succeeded with protocol version %d"):format(engine.PROTOCOL))
  return tracks
end

-- check_lessons reports whether the lessons directory is where the engine
-- looks for it (<root>/lessons, beside bin/) and how many tracks and steps
-- the engine loaded from it. `tracks` is nil when the engine did not answer.
local function check_lessons(tracks)
  local dir = engine.root .. "/lessons"
  if vim.fn.isdirectory(dir) == 0 then
    vim.health.error("lessons directory not found: " .. dir, {
      "Reinstall the plugin; the lessons ship in its lessons/ directory.",
    })
    return
  end
  if not tracks then
    vim.health.error("lessons directory found at " .. dir .. ", but the engine could not list its steps", {
      "Fix the engine checks above; a broken lesson also stops the engine before it answers, with the lesson's error shown there.",
    })
    return
  end
  local steps = 0
  for _, track in ipairs(tracks) do steps = steps + #track.steps end
  vim.health.ok(("lessons directory found at %s: %d tracks, %d steps"):format(dir, #tracks, steps))
end

-- check is called by :checkhealth gotyper.
function M.check()
  vim.health.start("gotyper")
  local have_go = check_go()
  local bin = check_binary(have_go)
  local tracks = check_engine(bin)
  check_lessons(tracks)
end

return M
