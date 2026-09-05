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

  -- The failed-send hint and the glance share a namespace. Clearing the whole
  -- namespace when the cursor moves would wipe the hint too, and that hint is
  -- the one thing the user must not miss (01-covert-model.md section 7).
  ['hiding the glance leaves other marks alone'] = function()
    local buf = fresh()
    arrive('晚上要吃什麼')
    level.glance()
    T.eq(#marks(buf), 1)

    quietdm.send_failed()
    T.eq(#marks(buf), 2, 'the failure hint is drawn alongside the glance')

    level.hide_glance()
    local left = marks(buf)
    T.eq(#left, 1, 'only the glance goes away')
    local text = ''
    for _, chunk in ipairs(left[1][4].virt_text) do
      text = text .. chunk[1]
    end
    T.truthy(text:find('unsaved', 1, true), 'the surviving mark is the hint, got ' .. text)
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

  -- Both renderers go through ctx.time, so display.time_format means the same
  -- thing at L1 and L2 rather than only at L1.
  ['the float honours display.time_format'] = function()
    for _, case in ipairs({ { 'clock', '%d%d:%d%d' }, { 'relative', '剛剛' } }) do
      fresh({ display = { time_format = case[1] } })
      arrive('晚上要吃什麼')
      level.read()
      local wins = vim.api.nvim_list_wins()
      local lines = vim.api.nvim_buf_get_lines(vim.api.nvim_win_get_buf(wins[#wins]), 0, -1, false)
      T.truthy(lines[1]:find(case[2]), case[1] .. ' produced ' .. lines[1])
      level.clear()
    end
  end,

  -- A quickfix list needs no disguise: it is already a screenful of text with
  -- names and line numbers in it. What it does need is for the props to stay
  -- props (01-covert-model.md section 3).
  ['panorama fills a quickfix list that reads as a lint report'] = function()
    fresh({ renderers = { panorama = 'quickfix' } })
    local user_win = vim.api.nvim_get_current_win()
    arrive('晚上要吃什麼')
    arrive('七點好嗎', { own = true, display = 'me' })

    level.panorama()
    T.eq(level.current, 'L3')
    local qf = vim.fn.getqflist({ items = 1, title = 1 })
    T.eq(#qf.items, 2)
    T.eq(qf.title, 'diagnostics', 'the window title has to read as something ordinary')

    local text = vim.fn.getqflist({ items = 1 }).items[1].text
    T.truthy(text:find('m.chen: 晚上要吃什麼', 1, true), 'got ' .. text)
    T.eq(vim.fn.getqflist({ items = 1 }).items[2].text, 'you: 七點好嗎')

    -- Every entry is invalid, so :cnext cannot walk into a file that is not
    -- there, and the keys that act on an entry are bound to nothing.
    for _, item in ipairs(qf.items) do
      T.eq(item.valid, 0, 'entries must not be navigable')
    end
    local qfwin
    for _, w in ipairs(vim.api.nvim_list_wins()) do
      if vim.bo[vim.api.nvim_win_get_buf(w)].buftype == 'quickfix' then
        qfwin = w
      end
    end
    T.truthy(qfwin, 'the quickfix window is open')
    local maps = vim.api.nvim_buf_get_keymap(vim.api.nvim_win_get_buf(qfwin), 'n')
    local blocked = {}
    for _, m in ipairs(maps) do
      blocked[m.lhs] = true
    end
    T.truthy(blocked['<CR>'], 'the jump key must be intercepted')

    -- Opening the list is not the same as moving the user into it (I2).
    T.eq(vim.api.nvim_get_current_win(), user_win, 'the cursor stays where it was')

    level.clear()
    T.falsy(vim.api.nvim_win_is_valid(qfwin), 'clear closes the window it opened')
    T.eq(#vim.fn.getqflist(), 0, 'and leaves no props behind in the list')
  end,

  ['the diagnostic renderer draws an lsp-styled hint at end of line'] = function()
    local buf = fresh({ renderers = { glance = 'diagnostic' } })
    local before = vim.api.nvim_buf_get_lines(buf, 0, -1, false)
    arrive('晚上要吃什麼')
    level.glance()

    local m = marks(buf)
    T.eq(#m, 1)
    local details = m[1][4]
    T.eq(details.virt_text_pos, 'eol', 'a diagnostic sits after the code, not at the margin')
    T.falsy(details.virt_lines, 'no virt_lines: the layout must not move (I2)')
    local text = details.virt_text[1][1]
    T.truthy(text:find('m.chen: 晚上要吃什麼', 1, true), 'got ' .. text)
    T.eq(details.virt_text[1][2], 'DiagnosticVirtualTextHint')
    T.eq(vim.api.nvim_buf_get_lines(buf, 0, -1, false), before, 'buffer text is untouched (I1)')

    level.hide_glance()
    T.eq(#marks(buf), 0)
  end,

  -- The other direction of the same rule: the hint expiring must not take a
  -- glance drawn in the meantime with it.
  ['the failed-send hint expiring leaves the glance alone'] = function()
    local buf = fresh({ level = { hint_timeout = 30 } })
    quietdm.send_failed()
    T.eq(#marks(buf), 1)
    arrive('晚上要吃什麼')
    level.glance()
    T.eq(#marks(buf), 2)

    T.truthy(vim.wait(1000, function() return #marks(buf) == 1 end, 10), 'the hint never expired')
    local left = marks(buf)[1][4].virt_text
    local text = ''
    for _, chunk in ipairs(left) do
      text = text .. chunk[1]
    end
    T.truthy(text:find('m.chen', 1, true), 'the surviving mark is the glance, got ' .. text)
    level.clear()
  end,

  -- ipc.stop() answers in-flight requests with an error on the next tick, and
  -- that error draws a hint. It must not land on a screen the user just asked
  -- to be emptied.
  ['stop clears the screen even for a hint drawn on the next tick'] = function()
    local buf = fresh()
    vim.schedule(function() quietdm.send_failed() end)
    quietdm.stop()
    vim.wait(100)
    T.eq(#marks(buf), 0, 'a hint drawn after the clear must not survive it')
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
