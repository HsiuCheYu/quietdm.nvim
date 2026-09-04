local config = require('quietdm.config')
local guard = require('quietdm.guard')

local cfg = config.build()

local function code_buffer(filetype)
  local buf = vim.api.nvim_create_buf(true, false)
  vim.api.nvim_set_current_buf(buf)
  vim.api.nvim_buf_set_lines(buf, 0, -1, false, { 'package main', 'func main() {}' })
  vim.bo[buf].filetype = filetype
  vim.bo[buf].buftype = ''
  return buf
end

return {
  ['a code file in a wide window is allowed'] = function()
    guard.setup(cfg, function() end)
    code_buffer('go')
    vim.api.nvim_win_set_width(0, 100)
    T.truthy(guard.allow())
  end,

  ['a filetype outside the whitelist is not'] = function()
    guard.setup(cfg, function() end)
    code_buffer('markdown')
    T.falsy(guard.allow())
  end,

  ['a scratch buffer is not'] = function()
    guard.setup(cfg, function() end)
    local buf = code_buffer('go')
    vim.bo[buf].buftype = 'nofile'
    T.falsy(guard.allow())
  end,

  ['a narrow window is not: right-aligned text would crowd the code'] = function()
    -- The window cannot be shrunk below &columns in a headless run, so raise
    -- the threshold above the window instead; the comparison is the same one.
    guard.setup(config.build({ guard = { min_width = 999 } }), function() end)
    code_buffer('go')
    T.falsy(guard.allow())
  end,

  ['silence blocks everything and clears the screen'] = function()
    local cleared = 0
    guard.setup(cfg, function() cleared = cleared + 1 end)
    code_buffer('go')
    vim.api.nvim_win_set_width(0, 100)
    guard.silence(5)
    T.truthy(guard.silenced())
    T.falsy(guard.allow())
    T.falsy(guard.allow_explicit())
    T.eq(cleared, 1, 'silencing wipes whatever was on screen')
    guard.unsilence()
    T.truthy(guard.allow())
  end,

  ['an explicit request ignores the filetype whitelist but not the buftype'] = function()
    guard.setup(cfg, function() end)
    local buf = code_buffer('markdown')
    T.truthy(guard.allow_explicit())
    vim.bo[buf].buftype = 'nofile'
    T.falsy(guard.allow_explicit())
  end,
}
