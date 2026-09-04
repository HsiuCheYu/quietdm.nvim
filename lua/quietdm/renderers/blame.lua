-- Default L1 renderer: right-aligned virtual text that looks like the git
-- blame line gitsigns puts there.
--
-- The disguise works because the semantics line up exactly: author name,
-- relative time, one-line subject (docs/design/01-covert-model.md section 3).

local registry = require('quietdm.registry')

local blame = {
  name = 'blame',
  level = 'glance',
  bufnr = nil,
}

function blame:setup(ctx)
  self.ctx = ctx
end

function blame:render(msgs, ctx)
  local msg = msgs[#msgs]
  if not msg then
    return
  end

  local sep = ctx.separator
  local name = msg.display or ''
  local time = ctx.time(msg.ts)

  -- Spend the width budget on the body: the name and the time are short and
  -- fixed, and a body cut too aggressively is unreadable.
  local overhead = vim.fn.strdisplaywidth(name .. sep .. time .. sep)
  local body = ctx.truncate(msg.body or '', math.max(8, ctx.max_width - overhead))

  local chunks = {
    { name, ctx.hl('name') },
    { sep, ctx.hl('sep') },
    { time, ctx.hl('time') },
    { sep, ctx.hl('sep') },
    { body, ctx.hl('text') },
  }

  local bufnr = vim.api.nvim_get_current_buf()
  local lnum = vim.api.nvim_win_get_cursor(0)[1] - 1
  self.bufnr = bufnr
  ctx.virt_text(bufnr, lnum, chunks, { align = 'right' })
end

function blame:clear()
  local ns = require('quietdm.ctx').namespace()
  if self.bufnr and vim.api.nvim_buf_is_valid(self.bufnr) then
    vim.api.nvim_buf_clear_namespace(self.bufnr, ns, 0, -1)
  end
  self.bufnr = nil
end

return registry.renderer(blame)
