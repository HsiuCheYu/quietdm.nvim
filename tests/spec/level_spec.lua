-- Exercises the invariants where they matter most: on screen.

local ctx_mod = require('quietdm.ctx')
local guard = require('quietdm.guard')
local level = require('quietdm.level')
local quietdm = require('quietdm')
local state = require('quietdm.state')

local function fresh(opts)
  quietdm.setup(opts or {})
  guard.unsilence()
  state.reset()
  state.setup(50)
  level.clear()
  local buf = vim.api.nvim_create_buf(true, false)
  vim.api.nvim_set_current_buf(buf)
  vim.api.nvim_buf_set_lines(buf, 0, -1, false, {
    'package main', '', 'func main() {', '\tprintln("x")', '}',
  })
  vim.bo[buf].filetype = 'go'
  vim.api.nvim_win_set_cursor(0, { 3, 0 })
  return buf
end

local function arrive(body, overrides)
  state.on_message(vim.tbl_extend('force', {
    room = '!r:localhost',
    event = '$' .. tostring(math.random(1e9)),
    sender = '@mia:localhost',
    display = 'm.chen',
    body = body,
    ts = os.time(),
    own = false,
    kind = 'text',
  }, overrides or {}))
end

local function marks(buf)
  return vim.api.nvim_buf_get_extmarks(buf, ctx_mod.namespace(), 0, -1, { details = true })
end

return {
  ['a message arriving draws nothing (I5)'] = function()
    local buf = fresh()
    arrive('晚上要吃什麼')
    T.eq(#marks(buf), 0, 'arrival must not put anything on screen')
    T.eq(level.current, 'L0')
  end,

  ['a glance draws blame-styled virtual text on the cursor line'] = function()
    local buf = fresh()
    local before = vim.api.nvim_buf_get_lines(buf, 0, -1, false)
    arrive('晚上要吃什麼')
    level.glance()
    local m = marks(buf)
    T.eq(#m, 1)
    T.eq(m[1][2], 2, 'the mark sits on the cursor line')
    local details = m[1][4]
    T.eq(details.virt_text_pos, 'right_align')
    T.falsy(details.virt_lines, 'no virt_lines: the layout must not move (I2)')
    T.eq(level.current, 'L1')
    T.eq(vim.api.nvim_buf_get_lines(buf, 0, -1, false), before, 'buffer text is untouched (I1)')

    local text = ''
    for _, chunk in ipairs(details.virt_text) do
      text = text .. chunk[1]
    end
    T.truthy(text:find('m.chen', 1, true), 'the alias is shown')
    T.truthy(text:find('晚上要吃什麼', 1, true), 'the body is shown')
    for _, chunk in ipairs(details.virt_text) do
      T.truthy(vim.fn.hlexists(chunk[2]) == 1, 'highlights link to existing groups (I3)')
    end
  end,

  ['the same message is not shown twice'] = function()
    local buf = fresh()
    arrive('once')
    level.glance()
    T.eq(#marks(buf), 1)
    level.hide_glance()
    level.glance()
    T.eq(#marks(buf), 0, 'nothing new means nothing on screen')
  end,

  ['moving the cursor hides the glance'] = function()
    local buf = fresh()
    arrive('hi')
    level.glance()
    T.eq(#marks(buf), 1)
    level.hide_glance()
    T.eq(#marks(buf), 0)
    T.eq(level.current, 'L0')
  end,

  ['a long body is truncated to the configured display width'] = function()
    local buf = fresh({ display = { max_width = 30 } })
    arrive(string.rep('晚上要吃什麼', 10))
    level.glance()
    local text = ''
    for _, chunk in ipairs(marks(buf)[1][4].virt_text) do
      text = text .. chunk[1]
    end
    T.truthy(vim.fn.strdisplaywidth(text) <= 30 + 4, 'the whole line stays near the budget')
    T.truthy(text:find('…', 1, true), 'truncation is marked')
  end,

  ['silence stops the glance entirely'] = function()
    local buf = fresh()
    arrive('hi')
    guard.silence(5)
    level.glance()
    T.eq(#marks(buf), 0)
    guard.unsilence()
  end,

  ['read opens a float and clear closes it'] = function()
    fresh()
    arrive('晚上要吃什麼')
    arrive('七點好嗎')
    local before = #vim.api.nvim_list_wins()
    level.read()
    local wins = vim.api.nvim_list_wins()
    T.eq(#wins, before + 1, 'exactly one window appeared')
    local float = wins[#wins]
    T.truthy(vim.api.nvim_win_get_config(float).relative ~= '', 'it is a floating window')
    T.eq(level.current, 'L2')
    level.clear()
    T.falsy(vim.api.nvim_win_is_valid(float), 'clear closes the float')
    T.eq(level.current, 'L0')
  end,

  ['panic clears the screen and goes silent'] = function()
    local buf = fresh()
    arrive('hi')
    level.glance()
    T.eq(#marks(buf), 1)
    quietdm.panic()
    T.eq(#marks(buf), 0)
    T.truthy(guard.silenced())
    T.eq(level.current, 'L0')
    guard.unsilence()
  end,
}
