-- Unix socket client for the daemon.
--
-- The single most important rule in this file: failing to connect is silent.
-- A visible "cannot reach chat service" error is the worst thing this plugin
-- could ever put on screen (docs/design/02-architecture.md section 4).

local log = require('quietdm.log')

local uv = vim.uv or vim.loop

local M = {}

local PROTO = 1
local CLIENT = 'quietdm.nvim/0.1.0'
local MAX_LINE = 64 * 1024
local BACKOFF = { 1000, 2000, 4000, 8000, 16000, 30000 }

local state = {
  path = nil,
  pipe = nil,
  buffer = '',
  connected = false, -- socket is up and hello acknowledged
  online = false, -- daemon reports a working link to the homeserver
  want = false, -- the user asked to be connected
  attempt = 0,
  timer = nil,
  seq = 0,
  pending = {}, -- request id -> callback
  handlers = {}, -- event type -> fn[]
}

---Register an event handler. Handlers run inside vim.schedule.
---@param kind string
---@param fn fun(event: table)
function M.on(kind, fn)
  state.handlers[kind] = state.handlers[kind] or {}
  table.insert(state.handlers[kind], fn)
end

---Drop every handler. Called before re-installing them on a restart, so a
---stop/start cycle cannot leave two copies of each handler behind.
function M.reset_handlers()
  state.handlers = {}
end

local function emit(kind, event)
  for _, fn in ipairs(state.handlers[kind] or {}) do
    local ok, err = pcall(fn, event)
    if not ok then
      log.error('handler ' .. kind .. ': ' .. tostring(err))
    end
  end
end

function M.connected() return state.connected end

function M.online() return state.online end

local function stop_timer()
  if state.timer then
    state.timer:stop()
    state.timer:close()
    state.timer = nil
  end
end

local function close_pipe()
  if state.pipe then
    local pipe = state.pipe
    state.pipe = nil
    pcall(function()
      if not pipe:is_closing() then
        pipe:read_stop()
        pipe:close()
      end
    end)
  end
  state.buffer = ''
  state.connected = false
  state.online = false
  state.pending = {}
end

local connect -- forward declaration

---Schedule the next reconnect attempt with increasing backoff, silently.
local function retry()
  if not state.want then
    return
  end
  stop_timer()
  state.attempt = math.min(state.attempt + 1, #BACKOFF)
  local delay = BACKOFF[state.attempt]
  log.debug('reconnect in ' .. delay .. 'ms')
  state.timer = uv.new_timer()
  state.timer:start(delay, 0, function()
    stop_timer()
    connect()
  end)
end

---Write one NDJSON line. Silently drops the message when disconnected: the
---daemon will have the real state anyway once we reconnect.
---@param obj table
---@return boolean written
local function write(obj)
  if not state.pipe then
    return false
  end
  if not state.connected and obj.t ~= 'hello' then
    return false
  end
  local ok, line = pcall(vim.json.encode, obj)
  if not ok then
    log.error('encode: ' .. tostring(line))
    return false
  end
  if #line + 1 > MAX_LINE then
    log.error('outgoing line too long, dropped')
    return false
  end
  state.pipe:write(line .. '\n', function(err)
    if err then
      log.warn('write: ' .. tostring(err))
    end
  end)
  return true
end

---Send a command, optionally with a reply callback.
---@param cmd table
---@param cb fun(event: table)|nil
---@return boolean
function M.send(cmd, cb)
  if cb then
    state.seq = state.seq + 1
    cmd.id = tostring(state.seq)
    state.pending[cmd.id] = cb
  end
  return write(cmd)
end

local function dispatch(event)
  local kind = event.t
  if not kind then
    return
  end
  if kind == 'ready' then
    state.connected = true
    state.online = event.connected == true
    state.attempt = 0
  end
  if event.id and state.pending[event.id] then
    local cb = state.pending[event.id]
    state.pending[event.id] = nil
    local ok, err = pcall(cb, event)
    if not ok then
      log.error('reply handler: ' .. tostring(err))
    end
  end
  if kind == 'error' then
    -- Never rendered by the core; :QuietdmDebug is the only way to see it.
    log.warn(string.format('daemon error %s: %s', event.code or '?', event.msg or ''))
  end
  emit(kind, event)
end

---Split the read buffer into lines. Over-long lines mean a protocol breach,
---so the connection is dropped and retried.
local function consume(chunk)
  state.buffer = state.buffer .. chunk
  while true do
    local nl = state.buffer:find('\n', 1, true)
    if not nl then
      break
    end
    local line = state.buffer:sub(1, nl - 1)
    state.buffer = state.buffer:sub(nl + 1)
    if #line > 0 then
      local ok, event = pcall(vim.json.decode, line)
      if ok and type(event) == 'table' then
        dispatch(event)
      else
        log.warn('malformed line from daemon')
      end
    end
  end
  if #state.buffer > MAX_LINE then
    log.warn('daemon line exceeds ' .. MAX_LINE .. ' bytes, dropping connection')
    close_pipe()
    retry()
  end
end

connect = function()
  if not state.want or state.pipe then
    return
  end
  local pipe = uv.new_pipe(false)
  state.pipe = pipe
  pipe:connect(state.path, function(err)
    if err then
      vim.schedule(function()
        log.debug('connect: ' .. tostring(err))
        close_pipe()
        retry()
      end)
      return
    end
    pipe:read_start(function(rerr, chunk)
      if rerr or not chunk then
        vim.schedule(function()
          log.debug('read ended: ' .. tostring(rerr))
          close_pipe()
          retry()
        end)
        return
      end
      -- uv callbacks are outside the API-safe zone; everything that can
      -- touch nvim goes through vim.schedule.
      vim.schedule(function() consume(chunk) end)
    end)
    vim.schedule(function()
      write({ t = 'hello', id = '0', proto = PROTO, client = CLIENT })
    end)
  end)
end

---Begin connecting. Safe to call repeatedly.
---@param path string
function M.start(path)
  state.path = path
  state.want = true
  state.attempt = 0
  if state.pipe then
    return
  end
  connect()
end

---Disconnect and stop retrying.
function M.stop()
  state.want = false
  stop_timer()
  close_pipe()
end

---Test seam: current socket path.
function M.path() return state.path end

return M
