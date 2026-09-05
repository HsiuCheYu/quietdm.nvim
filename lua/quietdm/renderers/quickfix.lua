-- L3 renderer: the conversation as a quickfix list.
--
-- A quickfix list is the one thing on this list that needs no disguise. It is
-- already a screenful of text with names and line numbers in it, and to anyone
-- glancing over it reads as a lint report or a search result
-- (docs/design/01-covert-model.md section 3).
--
-- The file name and line numbers are props. Everything that would act on them
-- has to be blocked, or <CR> drops the user into a file that does not exist.

local registry = require('quietdm.registry')

local quickfix = {
  name = 'quickfix',
  level = 'panorama',
  bufnr = nil, -- the scratch buffer the entries point at
  winid = nil, -- the quickfix window, if we opened it
}

-- Keys that act on a quickfix entry. Bound to nothing in our list.
local jump_keys = { '<CR>', '<2-LeftMouse>', 'o', 'O', '<C-w><CR>', '<C-w>o' }

function quickfix:setup(ctx)
  self.ctx = ctx
end

-- A plausible path for a file the conversation could have been about. The room
-- ID is not usable as one: "!abc:localhost" is obviously not a source file.
local function prop_path(room)
  local name = tostring(room or ''):match('^!?([%w_-]+)') or 'notes'
  return 'internal/' .. name:lower() .. '/' .. name:lower() .. '.go'
end

function quickfix:render(msgs, ctx)
  self:clear()

  -- The entries point at a scratch buffer of our own rather than at a path on
  -- disk. A quickfix entry with a bare filename makes Vim conjure a buffer for
  -- it; ours is at least real, empty, and ours to delete.
  local bufnr = vim.api.nvim_create_buf(false, true)
  vim.bo[bufnr].buftype = 'nofile'
  vim.bo[bufnr].swapfile = false
  pcall(vim.api.nvim_buf_set_name, bufnr, prop_path(msgs[1] and msgs[1].room))
  self.bufnr = bufnr

  local items = {}
  for i, msg in ipairs(msgs) do
    local who = msg.own and 'you' or (msg.display or '')
    items[#items + 1] = {
      bufnr = bufnr,
      lnum = 100 + i * 2,
      col = 3,
      -- valid = 0 keeps :cnext from walking into the props. It does not
      -- change how the line is rendered.
      valid = 0,
      text = who .. ': ' .. (msg.body or ''),
    }
  end
  if #items == 0 then
    return
  end

  -- "diagnostics" is what the title reads as to anyone who sees the window.
  vim.fn.setqflist({}, ' ', { title = 'diagnostics', items = items })

  local before = vim.api.nvim_get_current_win()
  vim.cmd('botright copen')
  local winid = vim.api.nvim_get_current_win()
  if winid ~= before then
    self.winid = winid
  end
  self:block_jumps(vim.api.nvim_win_get_buf(winid))
  -- The cursor belongs to the user (invariant I2): reading the list is their
  -- decision, so leave them where they were.
  if vim.api.nvim_win_is_valid(before) then
    vim.api.nvim_set_current_win(before)
  end
end

---Bind every key that would act on an entry to nothing, in this buffer only.
---@param qfbuf integer
function quickfix:block_jumps(qfbuf)
  for _, key in ipairs(jump_keys) do
    pcall(vim.keymap.set, 'n', key, '<Nop>', {
      buffer = qfbuf,
      nowait = true,
      desc = 'quietdm: the file names in this list are props',
    })
  end
end

function quickfix:clear()
  if self.winid and vim.api.nvim_win_is_valid(self.winid) then
    pcall(vim.api.nvim_win_close, self.winid, true)
  end
  self.winid = nil
  if self.bufnr and vim.api.nvim_buf_is_valid(self.bufnr) then
    -- Emptying the list first: an entry pointing at a deleted buffer would
    -- leave the file name sitting in :copen for the next person who opens it.
    pcall(vim.fn.setqflist, {}, 'r')
    pcall(vim.api.nvim_buf_delete, self.bufnr, { force = true })
  end
  self.bufnr = nil
end

return registry.renderer(quickfix)
