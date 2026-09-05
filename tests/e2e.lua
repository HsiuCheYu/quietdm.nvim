-- A full-stack run: the real daemon, a real Unix socket, and a real Neovim.
--
-- Driven by `make test-e2e`, which starts quietdmd with tests/e2e.toml first.
-- It walks the whole M1 flow: connect, receive, glance, reply, read, panic.

local ctx_mod = require('quietdm.ctx')
local level = require('quietdm.level')
local quietdm = require('quietdm')
local state = require('quietdm.state')

local function check(cond, msg)
  if not cond then
    io.stderr:write('e2e FAIL: ' .. msg .. '\n')
    vim.cmd('cquit 1')
  end
end

quietdm.setup({ socket = vim.env.QUIETDM_SOCK })
quietdm.start()
check(vim.wait(5000, function() return quietdm.connected() end, 20), 'never connected to the daemon')
check(quietdm.token() == '✓', 'the statusline token should start quiet')

check(vim.wait(5000, function() return state.next_unglanced() ~= nil end, 20), 'no message arrived')
-- The message and the room's unread count arrive as two separate IPC lines, so
-- the token can still be a step behind when the message itself has landed.
check(vim.wait(2000, function() return quietdm.token() == '⟳' end, 20),
  'unread should change the token')
check(quietdm.level() == 'L0', 'arrival must not change the exposure level (I5)')

local buf = vim.api.nvim_create_buf(true, false)
vim.api.nvim_set_current_buf(buf)
vim.api.nvim_buf_set_lines(buf, 0, -1, false, {
  'package main', 'func main() {', '\tprintln("hi")', '}',
})
vim.bo[buf].filetype = 'go'
vim.api.nvim_win_set_cursor(0, { 2, 0 })
local before = vim.api.nvim_buf_get_lines(buf, 0, -1, false)

level.glance()
local marks = vim.api.nvim_buf_get_extmarks(buf, ctx_mod.namespace(), 0, -1, { details = true })
check(#marks == 1, 'the glance drew nothing')
local text = ''
for _, chunk in ipairs(marks[1][4].virt_text) do
  text = text .. chunk[1]
end
check(marks[1][4].virt_text_pos == 'right_align', 'the blame line must be right aligned')
check(text:find('m.chen', 1, true) ~= nil, 'the alias is missing from ' .. text)
check(vim.deep_equal(vim.api.nvim_buf_get_lines(buf, 0, -1, false), before), 'the buffer was modified (I1)')
print('L1: ' .. text)

check(vim.wait(2000, function() return quietdm.token() == '✓' end, 20),
  'glancing should mark the message read on the daemon')

vim.ui.input = function(_, on_confirm) on_confirm('七點拉麵店見') end
quietdm.reply()
check(vim.wait(5000, function()
  local recent = state.recent(state.active_room())
  return recent[#recent] ~= nil and recent[#recent].own
end, 20), 'the reply never came back from the daemon')

level.read()
local wins = vim.api.nvim_list_wins()
local float = wins[#wins]
check(vim.api.nvim_win_get_config(float).relative ~= '', 'L2 did not open a float')
print('L2:')
for _, line in ipairs(vim.api.nvim_buf_get_lines(vim.api.nvim_win_get_buf(float), 0, -1, false)) do
  print('  | ' .. line)
end

quietdm.panic()
check(#vim.api.nvim_buf_get_extmarks(buf, ctx_mod.namespace(), 0, -1, {}) == 0, 'panic left marks behind')
check(not vim.api.nvim_win_is_valid(float), 'panic left the float open')
check(quietdm.level() == 'L0', 'panic must return to L0')
print('e2e ok')

quietdm.stop()
vim.cmd('qall!')
