-- Runs :checkhealth gotyper inside a headless Neovim and checks that every
-- gotyper check reports OK. Started by test/run.sh; a failed assert makes
-- Neovim exit non-zero, and the report is printed either way.
--
-- usage: nvim --headless --clean -c "set rtp^=<repo root>" -c "luafile test/health.lua"
vim.cmd("checkhealth gotyper")
local report = table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), "\n")
io.stdout:write(report, "\n")

local function count(word)
  local _, n = report:gsub("%f[%w]" .. word .. "%f[%W]", "")
  return n
end
-- Four checks: Go, the engine binary, the handshake and the lessons.
local ok = count("OK") == 4 and count("ERROR") == 0 and count("WARNING") == 0
vim.cmd(ok and "qa!" or "cquit 1")
