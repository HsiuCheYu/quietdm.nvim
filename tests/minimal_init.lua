-- Minimal init for headless test runs: nothing but this plugin on the rtp.
local root = vim.fn.fnamemodify(vim.fn.resolve(vim.fn.expand('<sfile>:p')), ':h:h')
vim.opt.runtimepath:prepend(root)
vim.opt.swapfile = false
vim.opt.shadafile = 'NONE'
vim.g.quietdm_test_root = root
