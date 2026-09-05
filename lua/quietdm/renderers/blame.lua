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
  mark = nil,
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
  self:clear()
  self.bufnr = bufnr
  self.mark = ctx.virt_text(bufnr, lnum, chunks, { align = 'right' })
end

-- Delete exactly the one extmark this renderer drew. Clearing the whole
-- namespace would also wipe the failed-send hint, which is the one thing the
-- user must be told about (docs/design/01-covert-model.md section 7) — and a
-- cursor move is enough to trigger it.
function blame:clear()
  if self.mark and self.bufnr and vim.api.nvim_buf_is_valid(self.bufnr) then
    local ns = require('quietdm.ctx').namespace()
    pcall(vim.api.nvim_buf_del_extmark, self.bufnr, ns, self.mark)
  end
  self.bufnr, self.mark = nil, nil
end

return registry.renderer(blame)
