-- Alternative L1 renderer: the message as a diagnostic hint at end of line.
--
-- Where `blame` borrows gitsigns' right-aligned author line, this one borrows
-- the shape of an LSP hint: the same prefix character, the same highlight
-- group, sitting immediately after the code. Which disguise is quieter depends
-- entirely on what the user already has running — someone with no git signs
-- but a busy language server is better served by this one
-- (docs/design/01-covert-model.md section 8).

local registry = require('quietdm.registry')

local diagnostic = {
  name = 'diagnostic',
  level = 'glance',
  bufnr = nil,
  mark = nil,
}

-- The prefix Neovim's own virtual-text diagnostics use.
local prefix = '■ '

function diagnostic:setup(ctx)
  self.ctx = ctx
end

function diagnostic:render(msgs, ctx)
  local msg = msgs[#msgs]
  if not msg then
    return
  end

  -- A diagnostic reads "name: message", so the sender goes where the source
  -- name would be. No timestamp: real diagnostics do not carry one, and an
  -- unfamiliar field is exactly what draws a second look.
  local head = (msg.display or '') .. ': '
  local overhead = vim.fn.strdisplaywidth(prefix .. head)
  local body = ctx.truncate(msg.body or '', math.max(8, ctx.max_width - overhead))

  local bufnr = vim.api.nvim_get_current_buf()
  local lnum = vim.api.nvim_win_get_cursor(0)[1] - 1
  self:clear()
  self.bufnr = bufnr
  self.mark = ctx.virt_text(bufnr, lnum, {
    { ' ' .. prefix .. head .. body, ctx.hl('hint') },
  }, { align = 'eol' })
end

-- Only the one extmark this renderer drew: the namespace is shared with the
-- failed-send hint, which must survive a cursor move.
function diagnostic:clear()
  if self.mark and self.bufnr and vim.api.nvim_buf_is_valid(self.bufnr) then
    local ns = require('quietdm.ctx').namespace()
    pcall(vim.api.nvim_buf_del_extmark, self.bufnr, ns, self.mark)
  end
  self.bufnr, self.mark = nil, nil
end

return registry.renderer(diagnostic)
