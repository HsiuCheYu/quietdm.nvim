-- End-to-end against a fake daemon: a real Unix socket, real NDJSON, and the
-- real reconnect path.

local ipc = require('quietdm.ipc')
local quietdm = require('quietdm')
local state = require('quietdm.state')

local uv = vim.uv or vim.loop

local function wait_for(fn, timeout)
  return vim.wait(timeout or 2000, fn, 10)
end

---A daemon stand-in. Lines are collected raw in the uv callback and decoded on
---the main thread, where touching the editor is safe.
local function fake_daemon()
  local path = vim.fn.tempname()
  local server = uv.new_pipe(false)
  server:bind(path)
  local d = { path = path, server = server, lines = {}, sock = nil }
  server:listen(4, function()
    local sock = uv.new_pipe(false)
    server:accept(sock)
    d.sock = sock
    local buf = ''
    sock:read_start(function(err, chunk)
      if err or not chunk then
        return
      end
      buf = buf .. chunk
      while true do
        local nl = buf:find('\n', 1, true)
        if not nl then
          break
        end
        d.lines[#d.lines + 1] = buf:sub(1, nl - 1)
        buf = buf:sub(nl + 1)
      end
    end)
  end)
  function d.push(obj)
    d.sock:write(vim.json.encode(obj) .. '\n')
  end
  function d.take()
    local raw = table.remove(d.lines, 1)
    return raw and vim.json.decode(raw) or nil
  end
  function d.next(kind)
    local cmd
    wait_for(function()
      cmd = d.take()
      return cmd ~= nil and cmd.t == kind
    end)
    return cmd
  end
  function d.close()
    if d.sock then
      pcall(function() d.sock:close() end)
    end
    pcall(function() server:close() end)
    os.remove(path)
  end
  return d
end

local function handshake(d)
  quietdm.stop()
  quietdm.setup({ socket = d.path })
  quietdm.start()
  local hello = d.next('hello')
  T.truthy(hello, 'the client greets first')
  T.eq(hello.proto, 1)
  d.push({ t = 'ready', id = hello.id, proto = 1, daemon = 'fake/1', connected = true })
  T.truthy(wait_for(function() return ipc.connected() end), 'ready completes the handshake')
end

return {
  ['a missing daemon fails silently'] = function()
    quietdm.stop()
    quietdm.setup({ socket = '/tmp/quietdm-does-not-exist.sock' })
    quietdm.start()
    vim.wait(200)
    T.falsy(ipc.connected(), 'no connection, and no error on screen')
    quietdm.stop()
  end,

  ['handshake, rooms and history rebuild the local state'] = function()
    local d = fake_daemon()
    handshake(d)

    local rooms = d.next('rooms')
    T.truthy(rooms, 'the client asks for the room list on connect')
    d.push({
      t = 'rooms',
      id = rooms.id,
      rooms = { { room = '!r:localhost', display = 'm.chen', unread = 2, last_ts = 100 } },
    })

    local hist = d.next('history')
    T.truthy(hist, 'the client primes history so L2 has no latency')
    T.eq(hist.room, '!r:localhost')
    d.push({
      t = 'history',
      id = hist.id,
      room = '!r:localhost',
      messages = {
        { room = '!r:localhost', event = '$1', sender = '@mia:localhost', display = 'm.chen',
          body = '晚上要吃什麼', ts = 100, own = false, kind = 'text' },
      },
    })

    T.truthy(wait_for(function() return #state.recent('!r:localhost') == 1 end))
    T.eq(state.total_unread(), 2)
    T.eq(quietdm.token(), '⟳', 'the statusline token switches on unread')

    -- A pushed message updates state without drawing anything.
    d.push({
      t = 'message', room = '!r:localhost', event = '$2', sender = '@mia:localhost',
      display = 'm.chen', body = '七點好嗎', ts = 101, own = false, kind = 'text',
    })
    T.truthy(wait_for(function() return #state.recent('!r:localhost') == 2 end))
    T.eq(quietdm.level(), 'L0', 'arrival never changes the exposure level')

    -- The same event id arriving again (another nvim on the same daemon)
    -- must not duplicate it.
    d.push({
      t = 'message', room = '!r:localhost', event = '$2', sender = '@mia:localhost',
      display = 'm.chen', body = '七點好嗎', ts = 101, own = false, kind = 'text',
    })
    vim.wait(100)
    T.eq(#state.recent('!r:localhost'), 2)

    quietdm.stop()
    d.close()
  end,

  ['a reply travels as a send command'] = function()
    local d = fake_daemon()
    handshake(d)
    d.push({
      t = 'message', room = '!r:localhost', event = '$9', sender = '@mia:localhost',
      display = 'm.chen', body = 'hi', ts = 1, own = false, kind = 'text',
    })
    T.truthy(wait_for(function() return state.active_room() == '!r:localhost' end))

    local saved = vim.ui.input
    vim.ui.input = function(_, on_confirm) on_confirm('七點拉麵店見') end
    quietdm.reply()
    vim.ui.input = saved

    local send = d.next('send')
    T.truthy(send, 'the composer hands the text to the daemon')
    T.eq(send.room, '!r:localhost')
    T.eq(send.body, '七點拉麵店見')

    quietdm.stop()
    d.close()
  end,

  ['stop leaves nothing behind'] = function()
    local d = fake_daemon()
    handshake(d)
    quietdm.stop()
    T.falsy(ipc.connected())
    T.eq(state.total_unread(), 0)
    d.close()
  end,
}
