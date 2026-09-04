-- Decides whether anything may be presented right now, and wipes the screen
-- when it may not.
--
-- Every rule here comes from docs/design/01-covert-model.md section 6: the
-- cheapest way to stay hidden is to have nothing on screen in the first place.

local log = require('quietdm.log')

local M = {}

local cfg
local silent_until = 0 -- os.time() value; 0 means not silenced
local on_clear = function() end

---@param c quietdm.Config
---@param clear fun() called whenever everything must disappear
function M.setup(c, clear)
  cfg = c
  on_clear = clear or on_clear
  silent_until = 0
end

---Silence for a number of minutes. No feedback of any kind: telling the user
---"silence mode on" would itself be the give-away.
---@param minutes number|nil
function M.silence(minutes)
  minutes = tonumber(minutes) or (cfg and cfg.guard.silence_minutes) or 10
  silent_until = os.time() + math.floor(minutes * 60)
  log.debug('silenced for ' .. minutes .. ' minutes')
  on_clear()
end

function M.unsilence()
  silent_until = 0
end

---@return boolean
function M.silenced()
  return os.time() < silent_until
end

local function contains(list, value)
  for _, v in ipairs(list or {}) do
    if v == value then
      return true
    end
  end
  return false
end

---May anything be drawn in the current window right now?
---@return boolean
function M.allow()
  if not cfg then
    return false
  end
  if M.silenced() then
    return false
  end
  local buf = vim.api.nvim_get_current_buf()
  local win = vim.api.nvim_get_current_win()

  local buftype = vim.bo[buf].buftype
  if contains(cfg.guard.exclude_buftypes, buftype) then
    return false
  end
  -- An empty buftype is a real file; anything else that slipped through the
  -- exclusion list is not somewhere blame text would ever appear.
  if buftype ~= '' then
    return false
  end
  if not contains(cfg.guard.filetypes, vim.bo[buf].filetype) then
    return false
  end
  if vim.wo[win].diff then
    return false
  end
  if vim.api.nvim_win_get_width(win) < cfg.guard.min_width then
    return false
  end
  -- Insert mode means the user is typing code; a line of text appearing next
  -- to the cursor there is both distracting and out of place.
  local mode = vim.api.nvim_get_mode().mode
  if mode:sub(1, 1) == 'i' or mode:sub(1, 1) == 't' then
    return false
  end
  return true
end

---May something the user explicitly asked for be drawn?
---
---An explicit :QuietdmRead is not bound by the filetype whitelist: the user
---looked at their own screen before asking. Silence and the buffer-type
---exclusions still apply — a float over a terminal is jarring wherever it
---comes from.
---@return boolean
function M.allow_explicit()
  if not cfg or M.silenced() then
    return false
  end
  local buftype = vim.bo[vim.api.nvim_get_current_buf()].buftype
  if contains(cfg.guard.exclude_buftypes, buftype) then
    return false
  end
  return true
end

---Install the autocommands that clear the screen on their own.
---@param group integer
function M.attach(group)
  vim.api.nvim_create_autocmd({ 'FocusLost', 'VimLeavePre' }, {
    group = group,
    callback = function() on_clear() end,
    desc = 'quietdm: leave nothing on screen when attention moves away',
  })
  vim.api.nvim_create_autocmd('VimResized', {
    group = group,
    callback = function()
      -- A resolution change usually means a projector or a screen share.
      on_clear()
    end,
    desc = 'quietdm: drop to L0 when the screen geometry changes',
  })
end

return M
