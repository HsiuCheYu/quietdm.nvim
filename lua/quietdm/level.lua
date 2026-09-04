-- The L0-L3 exposure state machine.
--
-- Upgrading always takes a deliberate user action; downgrading happens on its
-- own, from many directions. That asymmetry is the design
-- (docs/design/01-covert-model.md section 3).

local guard = require('quietdm.guard')
local log = require('quietdm.log')
local registry = require('quietdm.registry')
local state = require('quietdm.state')

local M = {}

local uv = vim.uv or vim.loop

M.current = 'L0'

local cfg, ctx
local glance_timer, idle_timer
local mark_read -- injected by init: fun(room, event)

local function stop(timer)
  if timer then
    timer:stop()
  end
end

local function start(timer, ms, fn)
  if not timer or ms <= 0 then
    return
  end
  timer:stop()
  timer:start(ms, 0, function()
    vim.schedule(fn)
  end)
end

---@param c quietdm.Config
---@param context quietdm.Ctx
---@param opts table { mark_read = fun(room, event) }
function M.setup(c, context, opts)
  cfg = c
  ctx = context
  mark_read = (opts or {}).mark_read or function() end
  glance_timer = glance_timer or uv.new_timer()
  idle_timer = idle_timer or uv.new_timer()
  M.current = 'L0'
end

local function renderer(level)
  return registry.get_renderer(cfg.renderers[level], level)
end

---Clear every renderer and drop back to L0. Idempotent, and safe to call from
---an autocommand, from panic, or on exit.
function M.clear()
  stop(glance_timer)
  stop(idle_timer)
  for _, level in ipairs({ 'glance', 'read', 'panorama' }) do
    local r = renderer(level)
    if r then
      pcall(r.clear, r)
    end
  end
  M.current = 'L0'
end

---L1. Called from CursorHold, never from message arrival (invariant I5).
function M.glance()
  if not guard.allow() then
    return
  end
  local r = renderer('glance')
  if not r then
    return
  end
  local msg = state.next_unglanced()
  if not msg then
    -- Nothing new. An empty screen is the most covert screen there is.
    return
  end
  local ok, err = pcall(r.render, r, { msg }, ctx)
  if not ok then
    log.error('glance render: ' .. tostring(err))
    return
  end
  state.mark_glanced(msg)
  mark_read(msg.room, msg.event)
  M.current = 'L1'
  start(glance_timer, cfg.level.glance_timeout, function()
    if M.current == 'L1' then
      M.clear()
    end
  end)
end

---Hide L1 without touching the higher levels. Bound to cursor movement and
---insert mode, which is exactly how gitsigns' blame text behaves.
function M.hide_glance()
  if M.current ~= 'L1' then
    return
  end
  stop(glance_timer)
  local r = renderer('glance')
  if r then
    pcall(r.clear, r)
  end
  M.current = 'L0'
end

---L2. Explicit user action: show the recent conversation in a hover window.
function M.read()
  if not guard.allow_explicit() then
    return
  end
  local r = renderer('read')
  if not r then
    return
  end
  local room = state.active_room()
  if not room then
    return
  end
  local msgs = state.recent(room, 8)
  if #msgs == 0 then
    return
  end
  local ok, err = pcall(r.render, r, msgs, ctx)
  if not ok then
    log.error('read render: ' .. tostring(err))
    return
  end
  for _, msg in ipairs(msgs) do
    state.mark_glanced(msg)
  end
  local last = msgs[#msgs]
  mark_read(last.room, last.event)
  M.current = 'L2'
  M.touch()
end

---L3. Explicit user action: the whole conversation as a quickfix list.
function M.panorama()
  if not guard.allow_explicit() then
    return
  end
  local r = renderer('panorama')
  if not r then
    log.debug('no panorama renderer registered')
    return
  end
  local room = state.active_room()
  if not room then
    return
  end
  local msgs = state.recent(room, cfg.history)
  if #msgs == 0 then
    return
  end
  local ok, err = pcall(r.render, r, msgs, ctx)
  if not ok then
    log.error('panorama render: ' .. tostring(err))
    return
  end
  for _, msg in ipairs(msgs) do
    state.mark_glanced(msg)
  end
  local last = msgs[#msgs]
  mark_read(last.room, last.event)
  M.current = 'L3'
  M.touch()
end

---Restart the idle countdown: leaving anything on screen while the user is
---away from the desk is exactly the scenario this plugin exists to avoid.
function M.touch()
  if M.current == 'L0' then
    stop(idle_timer)
    return
  end
  start(idle_timer, cfg.level.idle_downgrade, function()
    M.clear()
  end)
end

---Install the autocommands that drive the state machine.
---@param group integer
function M.attach(group)
  vim.api.nvim_create_autocmd('CursorHold', {
    group = group,
    callback = function() M.glance() end,
    desc = 'quietdm: L1 appears only once the cursor has stopped',
  })
  vim.api.nvim_create_autocmd({ 'CursorMoved', 'CursorMovedI' }, {
    group = group,
    callback = function()
      M.hide_glance()
      M.touch()
    end,
    desc = 'quietdm: L1 disappears the moment the cursor moves',
  })
  vim.api.nvim_create_autocmd('InsertEnter', {
    group = group,
    callback = function() M.hide_glance() end,
    desc = 'quietdm: nothing on screen while typing',
  })
end

return M
