local ctx_mod = require('quietdm.ctx')
local config = require('quietdm.config')

local cfg = config.build()

return {
  ['truncate measures display width, not characters'] = function()
    -- Eight CJK characters are sixteen cells wide.
    local s = '晚上要吃什麼呢好'
    local out = ctx_mod.truncate(s, 10)
    T.truthy(vim.fn.strdisplaywidth(out) <= 10, 'truncated text must fit the budget')
    T.truthy(out:sub(-3) == '…', 'truncation is marked with an ellipsis')
  end,

  ['truncate leaves short text alone'] = function()
    T.eq(ctx_mod.truncate('hello', 20), 'hello')
  end,

  ['relative time reads like git blame'] = function()
    local now = os.time()
    T.eq(ctx_mod.relative(now), '剛剛')
    T.eq(ctx_mod.relative(now - 180), '3 分鐘前')
    T.eq(ctx_mod.relative(now - 7200), '2 小時前')
    T.eq(ctx_mod.relative(now - 172800), '2 天前')
  end,

  ['hl resolves roles to existing groups only'] = function()
    local ctx = ctx_mod.new(cfg)
    T.eq(ctx.hl('text'), 'Comment')
    -- An unknown group must not become a custom color (invariant I3).
    local custom = ctx_mod.new(config.build({ highlights = { text = 'QuietdmNoSuchGroup' } }))
    T.eq(custom.hl('text'), 'Comment')
  end,

  ['virt_text never changes buffer contents (I1)'] = function()
    local buf = vim.api.nvim_create_buf(false, true)
    vim.api.nvim_buf_set_lines(buf, 0, -1, false, { 'line one', 'line two' })
    local before = vim.api.nvim_buf_get_lines(buf, 0, -1, false)
    local ctx = ctx_mod.new(cfg)
    local id = ctx.virt_text(buf, 0, { { 'm.chen · hi', 'Comment' } }, { align = 'right' })
    T.truthy(id, 'extmark was created')
    T.eq(vim.api.nvim_buf_get_lines(buf, 0, -1, false), before)

    local marks = vim.api.nvim_buf_get_extmarks(buf, ctx_mod.namespace(), 0, -1, { details = true })
    T.eq(#marks, 1)
    local details = marks[1][4]
    T.eq(details.virt_text_pos, 'right_align')
    T.falsy(details.virt_lines, 'virt_lines would move the layout (invariant I2)')

    ctx_mod.clear_all(buf)
    T.eq(#vim.api.nvim_buf_get_extmarks(buf, ctx_mod.namespace(), 0, -1, {}), 0)
  end,

  ['virt_text ignores lines outside the buffer'] = function()
    local buf = vim.api.nvim_create_buf(false, true)
    vim.api.nvim_buf_set_lines(buf, 0, -1, false, { 'only line' })
    local ctx = ctx_mod.new(cfg)
    T.falsy(ctx.virt_text(buf, 99, { { 'x', 'Comment' } }, {}))
  end,
}
