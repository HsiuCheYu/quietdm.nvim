-- Public entry point.
--
-- setup() configures and nothing else: it opens no socket and binds no key.
-- A plugin that connects to the network before the user asks is not something
-- this project is willing to ship (docs/design/04-plugin-api.md section 1).

local config = require('quietdm.config')
local ctx_mod = require('quietdm.ctx')
local guard = require('quietdm.guard')
local ipc = require('quietdm.ipc')
local level = require('quietdm.level')
local log = require('quietdm.log')
local registry = require('quietdm.registry')
local state = require('quietdm.state')

local M = {}

M.config = nil

local ctx
local group
local started = false

local function notifier()
  return registry.notifiers[M.config and M.config.notifier or '']
end

local function composer()
  return registry.composers[M.config and M.config.composer or '']
end

---Push the current unread total to the notifier. This is the only thing that
---happens when a message arrives (invariant I5).
local function refresh_notifier()
  local n = notifier()
  if n then
    pcall(n.update, n, state.total_unread())
  end
end

---Wipe everything: extmarks, floats, in-progress replies.
local function clear_all()
  level.clear()
  local c = composer()
  if c then
    pcall(c.close, c)
  end
  ctx_mod.clear_all()
end

local function mark_read(room, event)
  ipc.send({ t = 'mark_read', room = room, event = event })
end

local function install_handlers()
  ipc.on('ready', function(ev)
    log.debug('daemon ready, connected=' .. tostring(ev.connected))
    -- Rebuild state from scratch: the frontend keeps nothing across a
    -- reconnect, the daemon is the source of truth.
    state.reset()
    ipc.send({ t = 'rooms' }, function(reply)
      if reply.t == 'error' then
        return
      end
      for _, room in ipairs(reply.rooms or {}) do
        state.on_room(room)
        ipc.send({ t = 'history', room = room.room, limit = M.config.history }, function(hist)
          -- A request that never reached the daemon is answered with an
          -- error, which carries no room and no messages.
          if hist.t == 'error' then
            return
          end
          state.on_history(hist.room, hist.messages)
          refresh_notifier()
        end)
      end
      refresh_notifier()
    end)
  end)

  ipc.on('message', function(ev)
    local msg = {
      room = ev.room,
      event = ev.event,
      sender = ev.sender,
      display = ev.display,
      body = ev.body,
      ts = ev.ts,
      own = ev.own,
      kind = ev.kind,
    }
    if state.on_message(msg) then
      -- Arrival changes a counter and nothing else. No draw, no sound, no
      -- window: only the notifier token moves.
      refresh_notifier()
    end
  end)

  ipc.on('room', function(ev)
    state.on_room(ev)
    refresh_notifier()
  end)
end

---Configure the plugin. Does not connect and does not map any key.
---@param opts table|nil
function M.setup(opts)
  M.config = config.build(opts)
  state.setup(M.config.history)
  registry.load_builtins()
  ctx = ctx_mod.new(M.config)
  -- Hand every registered implementation its toolbox once, up front.
  for _, r in pairs(registry.renderers) do
    if type(r.setup) == 'function' then
      pcall(r.setup, r, ctx)
    end
  end
  guard.setup(M.config, clear_all)
  level.setup(M.config, ctx, { mark_read = mark_read })

  group = vim.api.nvim_create_augroup('quietdm', { clear = true })
  guard.attach(group)
  level.attach(group)

  if M.config.panic_key then
    for _, mode in ipairs({ 'n', 'i', 'v' }) do
      vim.keymap.set(mode, M.config.panic_key, function()
        M.panic()
      end, { silent = true, desc = 'quietdm panic' })
    end
  end
  return M
end

local function ensure_setup()
  if not M.config then
    M.setup({})
  end
end

---Connect to the daemon. Silent on failure, retrying in the background.
function M.start()
  ensure_setup()
  if started then
    return
  end
  started = true
  ipc.reset_handlers()
  install_handlers()
  ipc.start(config.socket_path(M.config))
end

---Disconnect and clear the screen.
function M.stop()
  started = false
  ipc.stop()
  clear_all()
  state.reset()
  refresh_notifier()
  -- ipc.stop() answers every in-flight request with an error, and those
  -- callbacks run on the next tick — after the clear above. Without this, the
  -- last abandoned reply would draw a failed-send hint onto a screen the user
  -- just asked to be emptied.
  vim.schedule(clear_all)
end

---L2: read the recent conversation in a hover-styled float.
function M.read()
  ensure_setup()
  level.read()
end

---L3: the whole conversation as a quickfix list.
function M.panorama()
  ensure_setup()
  level.panorama()
end

---Open the composer for the active room.
function M.reply()
  ensure_setup()
  local c = composer()
  if not c then
    log.warn('no composer named ' .. tostring(M.config.composer))
    return
  end
  local room = state.active_room()
  if not room then
    return
  end
  c:open(room, function(body)
    if not body or body == '' then
      return
    end
    ipc.send({ t = 'send', room = room, body = body }, function(reply)
      if reply.t == 'error' then
        M.send_failed()
      end
    end)
  end, function() end)
end

---A failed send is the one thing the user must be told about. It is told in
---the quietest way available: a hint-styled piece of virtual text on the
---cursor line, never vim.notify (docs/design/01-covert-model.md section 7).
function M.send_failed()
  if not guard.allow() then
    return
  end
  local bufnr = vim.api.nvim_get_current_buf()
  local lnum = vim.fn.line('.') - 1
  local mark = ctx.virt_text(bufnr, lnum, { { '  unsaved changes', ctx.hl('hint') } }, { align = 'eol' })
  if not mark then
    return
  end
  -- Only this hint goes away. Clearing the namespace would take a glance
  -- drawn in the meantime with it, which is the same mistake the renderers
  -- had in the other direction.
  vim.defer_fn(function()
    if vim.api.nvim_buf_is_valid(bufnr) then
      pcall(vim.api.nvim_buf_del_extmark, bufnr, ctx_mod.namespace(), mark)
    end
  end, M.config.level.hint_timeout)
end

---Silence for the given number of minutes (default guard.silence_minutes).
---@param minutes number|nil
function M.silence(minutes)
  ensure_setup()
  guard.silence(minutes)
end

---Panic: clear everything and go quiet. No feedback whatsoever.
function M.panic()
  ensure_setup()
  clear_all()
  guard.silence(M.config.guard.silence_minutes)
end

---The disguised token for the user's statusline.
---@return string
function M.token()
  local n = notifier()
  if not n then
    return ''
  end
  local ok, tok = pcall(n.token, n)
  return ok and tok or ''
end

---Show the IPC ring log in a scratch buffer (nofile, wiped on close, never
---written to disk: invariants I1 and I4).
function M.debug()
  local lines = log.lines()
  if #lines == 0 then
    lines = { 'quietdm: no log entries' }
  end
  local buf = vim.api.nvim_create_buf(false, true)
  vim.bo[buf].buftype = 'nofile'
  vim.bo[buf].bufhidden = 'wipe'
  vim.bo[buf].swapfile = false
  vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
  vim.bo[buf].modifiable = false
  vim.api.nvim_win_set_buf(vim.api.nvim_get_current_win(), buf)
end

---Register a custom renderer.
function M.register_renderer(r) return registry.renderer(r) end

---Register a custom composer.
function M.register_composer(c) return registry.composer(c) end

---Register a custom notifier.
function M.register_notifier(n) return registry.notifier(n) end

---Current exposure level, for tests and for a curious statusline.
---@return string
function M.level() return level.current end

---True when the socket is connected and the handshake is done.
---@return boolean
function M.connected() return ipc.connected() end

return M
