-- In-memory view of the conversation.
--
-- The frontend keeps no state across sessions: it is disposable and rebuilds
-- everything from the daemon on connect (docs/design/03-ipc-protocol.md
-- section 6). Nothing here is ever written to disk (invariant I4).

local M = {}

M.rooms = {} -- room id -> { room, display, unread, last_ts }
M.order = {} -- room ids, most recently active first
M.messages = {} -- room id -> quietdm.Message[], oldest first
M.seen = {} -- event id -> true, for de-duplication
M.glanced = {} -- event id -> true, messages the user has already been shown
M.id_count = 0 -- how many IDs have been remembered since the last prune

local history_limit = 50

-- Above this many remembered event IDs, the de-duplication tables are rebuilt
-- from the messages actually still held. Without it they grow for as long as
-- the editor stays open, which for this plugin is all day.
local id_limit = 5000

---@param limit integer
function M.setup(limit)
  history_limit = limit or 50
end

function M.reset()
  M.rooms, M.order, M.messages, M.seen, M.glanced = {}, {}, {}, {}, {}
  M.id_count = 0
end

local function touch_room(id, display, last_ts)
  local room = M.rooms[id]
  if not room then
    room = { room = id, display = display or id, unread = 0, last_ts = last_ts or 0 }
    M.rooms[id] = room
    M.order[#M.order + 1] = id
  end
  if display and display ~= '' then
    room.display = display
  end
  if last_ts and last_ts > room.last_ts then
    room.last_ts = last_ts
  end
  return room
end

---Drop the de-duplication entries for messages that have already fallen out
---of every room's history. Those events can never arrive again — the daemon
---does not replay — so remembering them buys nothing.
local function prune_ids()
  local seen, glanced = {}, {}
  for _, list in pairs(M.messages) do
    for _, msg in ipairs(list) do
      seen[msg.event] = true
      if M.glanced[msg.event] then
        glanced[msg.event] = true
      end
    end
  end
  M.seen, M.glanced = seen, glanced
end

local function remember(event)
  M.seen[event] = true
  M.id_count = M.id_count + 1
end

---Prune once the caller has finished updating M.messages. Doing it mid-update
---would rebuild the tables from a list that does not yet hold the message
---being recorded.
local function maybe_prune()
  if M.id_count > id_limit then
    prune_ids()
    -- Counting from zero, not from what survived. Seeding the counter with
    -- the surviving count would make the threshold permanently true once the
    -- rooms themselves hold more than the limit, and then every single
    -- message would rebuild both tables.
    M.id_count = 0
  end
end

local function bump(id)
  for i, other in ipairs(M.order) do
    if other == id then
      table.remove(M.order, i)
      break
    end
  end
  table.insert(M.order, 1, id)
end

---Record a message. Returns false when it is a duplicate, which happens
---whenever another nvim instance is connected to the same daemon.
---@param msg quietdm.Message
---@return boolean accepted
function M.on_message(msg)
  if not msg or not msg.event or M.seen[msg.event] then
    return false
  end
  remember(msg.event)
  touch_room(msg.room, nil, msg.ts)
  bump(msg.room)

  local list = M.messages[msg.room]
  if not list then
    list = {}
    M.messages[msg.room] = list
  end
  list[#list + 1] = msg
  while #list > history_limit do
    table.remove(list, 1)
  end
  -- The user's own messages were never unseen.
  if msg.own then
    M.glanced[msg.event] = true
  end
  maybe_prune()
  return true
end

---Apply a room event from the daemon (unread counts are authoritative there).
---@param room table
function M.on_room(room)
  if not room or not room.room then
    return
  end
  local r = touch_room(room.room, room.display, room.last_ts)
  r.unread = room.unread or 0
end

---Replace a room's history with the daemon's copy.
---@param room string
---@param msgs quietdm.Message[]
function M.on_history(room, msgs)
  touch_room(room)
  local list = {}
  for _, msg in ipairs(msgs or {}) do
    remember(msg.event)
    -- History is context the user asked for, not something new to show at
    -- L1; treat it as already glanced.
    M.glanced[msg.event] = true
    list[#list + 1] = msg
  end
  M.messages[room] = list
  maybe_prune()
end

---Total unread across every room, for the notifier.
---@return integer
function M.total_unread()
  local n = 0
  for _, room in pairs(M.rooms) do
    n = n + (room.unread or 0)
  end
  return n
end

---The room the user is implicitly talking to: the most recently active one.
---@return string|nil
function M.active_room()
  return M.order[1]
end

---The newest message the user has not been shown yet, across all rooms.
---
---Only the latest message of each room counts: once the user has seen it,
---showing an older one at L1 would read as out of order. Catching up on what
---came before is what L2 is for.
---@return quietdm.Message|nil
function M.next_unglanced()
  local newest
  for _, list in pairs(M.messages) do
    local msg = list[#list]
    if msg and not M.glanced[msg.event] then
      if not newest or msg.ts > newest.ts then
        newest = msg
      end
    end
  end
  return newest
end

---Mark a message as shown so L1 does not repeat it.
---@param msg quietdm.Message
function M.mark_glanced(msg)
  if msg and msg.event then
    M.glanced[msg.event] = true
  end
end

---Recent messages of a room, oldest first.
---@param room string|nil
---@param limit integer|nil
---@return quietdm.Message[]
function M.recent(room, limit)
  room = room or M.active_room()
  if not room then
    return {}
  end
  local list = M.messages[room] or {}
  limit = limit or #list
  local out = {}
  for i = math.max(1, #list - limit + 1), #list do
    out[#out + 1] = list[i]
  end
  return out
end

return M
