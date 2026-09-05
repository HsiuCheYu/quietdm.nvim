local state = require('quietdm.state')

local function msg(overrides)
  return vim.tbl_extend('force', {
    room = '!r:localhost',
    event = '$1',
    sender = '@mia:localhost',
    display = 'm.chen',
    body = 'hi',
    ts = os.time(),
    own = false,
    kind = 'text',
  }, overrides or {})
end

return {
  ['duplicate event ids are rejected'] = function()
    state.reset()
    state.setup(50)
    T.truthy(state.on_message(msg()))
    T.falsy(state.on_message(msg()), 'the same event must only count once')
    T.eq(#state.recent('!r:localhost'), 1)
  end,

  -- The de-duplication tables would otherwise grow for as long as the editor
  -- stays open, which for this plugin is all day.
  ['the de-duplication tables do not grow without bound'] = function()
    state.reset()
    state.setup(5)
    for i = 1, 6000 do
      state.on_message(msg({ event = '$' .. i, body = 'm' .. i }))
    end
    local seen = 0
    for _ in pairs(state.seen) do
      seen = seen + 1
    end
    T.truthy(seen <= 5000, 'seen holds ' .. seen .. ' ids')
    T.eq(#state.recent('!r:localhost'), 5, 'pruning must not touch the history itself')
    -- What is still on screen must still be de-duplicated.
    T.falsy(state.on_message(msg({ event = '$6000', body = 'm6000' })))
  end,

  ['history is bounded'] = function()
    state.reset()
    state.setup(3)
    for i = 1, 10 do
      state.on_message(msg({ event = '$' .. i, body = 'm' .. i }))
    end
    local recent = state.recent('!r:localhost')
    T.eq(#recent, 3)
    T.eq(recent[3].body, 'm10')
  end,

  ['unread comes from the daemon, not from counting locally'] = function()
    state.reset()
    state.setup(50)
    state.on_message(msg())
    T.eq(state.total_unread(), 0)
    state.on_room({ room = '!r:localhost', display = 'm.chen', unread = 2, last_ts = 1 })
    T.eq(state.total_unread(), 2)
  end,

  ['next_unglanced returns the newest unseen message, once'] = function()
    state.reset()
    state.setup(50)
    local now = os.time()
    state.on_message(msg({ event = '$1', ts = now - 10, body = 'old' }))
    state.on_message(msg({ event = '$2', ts = now, body = 'new' }))
    local m = state.next_unglanced()
    T.eq(m.body, 'new')
    state.mark_glanced(m)
    -- Older messages are not replayed at L1: catching up is L2's job.
    T.eq(state.next_unglanced(), nil)
  end,

  ['a newer message in another room wins'] = function()
    state.reset()
    state.setup(50)
    local now = os.time()
    state.on_message(msg({ room = '!a:localhost', event = '$a', ts = now - 5, body = 'from a' }))
    state.on_message(msg({ room = '!b:localhost', event = '$b', ts = now, body = 'from b' }))
    T.eq(state.next_unglanced().body, 'from b')
  end,

  ['own messages are never treated as unseen'] = function()
    state.reset()
    state.setup(50)
    state.on_message(msg({ event = '$own', own = true, body = 'mine' }))
    T.eq(state.next_unglanced(), nil)
  end,

  ['history replaces the local copy and counts as already seen'] = function()
    state.reset()
    state.setup(50)
    state.on_history('!r:localhost', { msg({ event = '$h1' }), msg({ event = '$h2' }) })
    T.eq(#state.recent('!r:localhost'), 2)
    T.eq(state.next_unglanced(), nil)
  end,

  ['the active room is the most recently active one'] = function()
    state.reset()
    state.setup(50)
    state.on_message(msg({ room = '!a:localhost', event = '$a' }))
    state.on_message(msg({ room = '!b:localhost', event = '$b' }))
    T.eq(state.active_room(), '!b:localhost')
  end,
}
