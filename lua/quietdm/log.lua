-- A ring buffer for diagnostics.
--
-- Nothing here ever reaches the screen on its own: errors are recorded and
-- shown only when the user asks with :QuietdmDebug (invariant I3, and the
-- error handling rules in docs/design/03-ipc-protocol.md).

local M = {}

local CAPACITY = 200

M.entries = {}

---Record one line. Never prints, never calls vim.notify.
---@param level string
---@param msg string
function M.add(level, msg)
  local entry = string.format('%s [%s] %s', os.date('%H:%M:%S'), level, msg)
  M.entries[#M.entries + 1] = entry
  if #M.entries > CAPACITY then
    table.remove(M.entries, 1)
  end
end

function M.debug(msg) M.add('debug', msg) end

function M.warn(msg) M.add('warn', msg) end

function M.error(msg) M.add('error', msg) end

---Return a copy of the log for display.
---@return string[]
function M.lines()
  return vim.deepcopy(M.entries)
end

function M.clear()
  M.entries = {}
end

return M
