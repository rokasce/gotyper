-- Commands for gotyper. Neovim sources every plugin/*.lua file at startup, so
-- this file only defines commands; the plugin itself (lua/gotyper/) is loaded
-- the first time one of them runs.

vim.api.nvim_create_user_command("Gotyper", function()
  require("gotyper").start()
end, { desc = "gotyper: start the typing game in a new tab" })

vim.api.nvim_create_user_command("GotyperRestart", function()
  require("gotyper").restart()
end, { desc = "gotyper: throw away this attempt and type the step again" })

vim.api.nvim_create_user_command("GotyperPanel", function()
  require("gotyper").toggle_panel()
end, { desc = "gotyper: show or hide the explanation panel" })
