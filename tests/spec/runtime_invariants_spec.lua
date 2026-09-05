-- The dynamic half of the invariant check (docs/design/05-roadmap.md, M3).
--
-- invariants_spec.lua scans the source for forbidden API names. That catches
-- the obvious mistake and nothing else: a call reached through a wrapper, a
-- dispatch table, or a library the plugin hands work to is invisible to it.
-- This one puts a trap on the API itself and then drives the whole flow.
--
-- The traps are deliberately narrower than the static list, because the real
-- rules are narrower. I1 forbids message text entering the user's buffer, not
-- every buffer: the LSP hover window the L2 renderer borrows fills a scratch
-- buffer of its own, and that is the point of it.

local guard = require('quietdm.guard')
local level = require('quietdm.level')
local quietdm = require('quietdm')
local state = require('quietdm.state')

---Is this a buffer the user is editing, as opposed to one of ours?
local function is_user_buffer(bufnr)
  if bufnr == nil or bufnr == 0 then
    bufnr = vim.api.nvim_get_current_buf()
  end
  if not vim.api.nvim_buf_is_valid(bufnr) then
    return false
  end
  return vim.bo[bufnr].buftype == ''
end

---Run fn with every forbidden call recorded, and return what it tripped.
local function with_traps(fn)
  local hits = {}
  local saved = {}

  local function trap(name, offends)
    saved[name] = vim.api[name]
    vim.api[name] = function(...)
      if offends(...) then
        hits[#hits + 1] = name
      end
      return saved[name](...)
    end
  end

  -- I1: message text must never enter a buffer the user is editing.
  trap('nvim_buf_set_lines', function(bufnr) return is_user_buffer(bufnr) end)
  trap('nvim_buf_set_text', function(bufnr) return is_user_buffer(bufnr) end)
  -- I2: the layout and the cursor belong to the user.
  trap('nvim_win_set_cursor', function() return true end)
  trap('nvim_buf_set_extmark', function(_, _, _, _, opts)
    return type(opts) == 'table' and opts.virt_lines ~= nil
  end)

  -- I3: nothing may pop up on its own.
  local notify = vim.notify
  vim.notify = function() hits[#hits + 1] = 'vim.notify' end
  -- I4: nothing is ever written to disk.
  local open = io.open
  io.open = function(path, mode)
    if mode and mode:find('[wa+]') then
      hits[#hits + 1] = 'io.open(' .. tostring(path) .. ', ' .. mode .. ')'
    end
    return open(path, mode)
  end

  local ok, err = pcall(fn)

  for name, original in pairs(saved) do
    vim.api[name] = original
  end
  vim.notify = notify
  io.open = open

  if not ok then
    error(err, 0)
  end
  return hits
end

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

---Everything a user can ask the plugin to do, in one pass.
---
---It asserts as it goes: a pass that quietly did nothing would satisfy every
---check below without proving anything.
local function drive()
  local ns = require('quietdm.ctx').namespace()
  arrive('晚上要吃什麼')
  level.glance()
  T.truthy(#vim.api.nvim_buf_get_extmarks(0, ns, 0, -1, {}) > 0, 'the glance drew nothing')
  level.hide_glance()
  arrive('七點好嗎')
  level.read()
  level.clear()
  arrive('拉麵店見')
  level.panorama()
  T.truthy(#vim.fn.getqflist() > 0, 'the panorama listed nothing')
  level.clear()

  local saved_input = vim.ui.input
  vim.ui.input = function(_, on_confirm) on_confirm('好') end
  quietdm.reply()
  vim.ui.input = saved_input
  -- The reply cannot go anywhere with no daemon, so this also exercises the
  -- failed-send hint, which is the one thing the plugin draws unprompted.
  vim.wait(200, function()
    return #vim.api.nvim_buf_get_extmarks(0, ns, 0, -1, {}) > 0
  end, 10)

  quietdm.panic()
  guard.unsilence()
end

return {
  ['no exposure level reaches a forbidden API at run time'] = function()
    local buf = fresh({ renderers = { glance = 'blame', read = 'float', panorama = 'quickfix' } })
    local before = vim.api.nvim_buf_get_lines(buf, 0, -1, false)
    local cursor = vim.api.nvim_win_get_cursor(0)

    local hits = with_traps(drive)

    T.eq(hits, {}, 'forbidden calls: ' .. table.concat(hits, ', '))
    T.eq(vim.api.nvim_buf_get_lines(buf, 0, -1, false), before, 'the buffer was modified (I1)')
    T.eq(vim.api.nvim_win_get_cursor(0), cursor, 'the cursor moved (I2)')
  end,

  ['the alternative renderers are held to the same rules'] = function()
    local buf = fresh({ renderers = { glance = 'diagnostic', read = 'float', panorama = 'quickfix' } })
    local before = vim.api.nvim_buf_get_lines(buf, 0, -1, false)

    local hits = with_traps(drive)

    T.eq(hits, {}, 'forbidden calls: ' .. table.concat(hits, ', '))
    T.eq(vim.api.nvim_buf_get_lines(buf, 0, -1, false), before, 'the buffer was modified (I1)')
  end,

  ['the trap itself catches a violation'] = function()
    local buf = fresh()
    local hits = with_traps(function()
      vim.api.nvim_buf_set_lines(buf, 0, 0, false, { 'oops' })
      vim.notify('boo')
    end)
    T.eq(#hits, 2, 'a check that cannot fail proves nothing')
  end,
}
