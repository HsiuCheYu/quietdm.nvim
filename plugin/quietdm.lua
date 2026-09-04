-- User commands only.
--
-- Loading this file must not connect, draw, map a key, or read the network.
-- Everything starts when the user says so.

if vim.g.loaded_quietdm then
  return
end
vim.g.loaded_quietdm = true

local function quietdm()
  return require('quietdm')
end

-- Command names read like a development tool, not a chat client.
vim.api.nvim_create_user_command('QuietdmStart', function()
  quietdm().start()
end, { desc = 'quietdm: connect to the daemon' })

vim.api.nvim_create_user_command('QuietdmStop', function()
  quietdm().stop()
end, { desc = 'quietdm: disconnect' })

vim.api.nvim_create_user_command('QuietdmRead', function()
  quietdm().read()
end, { desc = 'quietdm: L2, recent conversation in a hover-styled float' })

vim.api.nvim_create_user_command('QuietdmPanorama', function()
  quietdm().panorama()
end, { desc = 'quietdm: L3, conversation as a quickfix list' })

vim.api.nvim_create_user_command('QuietdmReply', function()
  quietdm().reply()
end, { desc = 'quietdm: compose a reply' })

vim.api.nvim_create_user_command('QuietdmSilence', function(opts)
  quietdm().silence(tonumber(opts.args))
end, { nargs = '?', desc = 'quietdm: go silent for N minutes' })

vim.api.nvim_create_user_command('QuietdmPanic', function()
  quietdm().panic()
end, { desc = 'quietdm: clear everything and go silent' })

vim.api.nvim_create_user_command('QuietdmDebug', function()
  quietdm().debug()
end, { desc = 'quietdm: show the IPC log in a scratch buffer' })
