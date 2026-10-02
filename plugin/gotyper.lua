-- Commands for gotyper. Neovim sources every plugin/*.lua file at startup, so
-- this file only defines commands; the plugin itself (lua/gotyper/) is loaded
-- the first time one of them runs.

-- :Gotyper starts the step whose id is given (completed with <Tab>), or
-- without an argument lets the learner pick one from a list.
vim.api.nvim_create_user_command("Gotyper", function(opts)
  if opts.args == "" then
    require("gotyper").pick()
  else
    require("gotyper").start(opts.args)
  end
end, {
  nargs = "?",
  -- Neovim does not filter what a completion function returns, so keep only
  -- the ids that begin with what has been typed so far.
  complete = function(typed)
    return vim.tbl_filter(function(id) return vim.startswith(id, typed) end, require("gotyper").step_ids())
  end,
  desc = "gotyper: start the typing game in a new tab",
})

vim.api.nvim_create_user_command("GotyperRestart", function()
  require("gotyper").restart()
end, { desc = "gotyper: throw away this attempt and type the step again" })

vim.api.nvim_create_user_command("GotyperPanel", function()
  require("gotyper").toggle_panel()
end, { desc = "gotyper: show or hide the explanation panel" })
