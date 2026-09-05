-- Default L2 renderer: the recent conversation in a window that is, quite
-- literally, an LSP hover window.
--
-- It is built with vim.lsp.util.open_floating_preview so the border, the
-- highlight groups and the width rules are whatever the user already has.

local registry = require('quietdm.registry')

local float = {
  name = 'float',
  level = 'read',
  winid = nil,
}

function float:setup(ctx)
  self.ctx = ctx
end

function float:render(msgs, ctx)
  local lines = {}
  for _, msg in ipairs(msgs) do
    local who = msg.own and 'you' or (msg.display or '')
    lines[#lines + 1] = string.format('%s   %s', who, ctx.time(msg.ts))
    lines[#lines + 1] = '  ' .. (msg.body or '')
  end
  if #lines == 0 then
    return
  end
  self:clear()
  self.winid = ctx.hover(lines)
end

function float:clear()
  if self.winid and vim.api.nvim_win_is_valid(self.winid) then
    pcall(vim.api.nvim_win_close, self.winid, true)
  end
  self.winid = nil
end

return registry.renderer(float)
