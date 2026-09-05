-- Composer for long replies: a scratch buffer that claims to be a commit
-- message. `:w` sends, `:q` abandons.
--
-- The other two composers are single-line. A reply that needs a paragraph typed
-- into a cmdline is a reply the user gives up on — and giving up on the tool is
-- the failure mode the covert model cares about second-most, after being seen
-- (docs/design/01-covert-model.md section 8).
--
-- filetype=gitcommit because a developer staring at a commit message is the
-- least remarkable thing on this list.

local registry = require('quietdm.registry')

local gitcommit = {
  name = 'gitcommit',
  generation = 0,
  winid = nil,
  bufnr = nil,
}

function gitcommit:setup(ctx)
  self.ctx = ctx
end

function gitcommit:open(room, submit, cancel)
  self.generation = self.generation + 1
  local gen = self.generation
  self:close_window()

  -- Nothing is read from disk and nothing is ever written to one: buftype
  -- acwrite is what makes `:w` fire BufWriteCmd instead of touching a file,
  -- and bufhidden wipe means the text is gone the moment the window closes
  -- (invariant I4).
  local bufnr = vim.api.nvim_create_buf(false, true)
  vim.bo[bufnr].buftype = 'acwrite'
  vim.bo[bufnr].bufhidden = 'wipe'
  vim.bo[bufnr].swapfile = false
  vim.bo[bufnr].undofile = false
  vim.bo[bufnr].filetype = 'gitcommit'
  pcall(vim.api.nvim_buf_set_name, bufnr, 'COMMIT_EDITMSG')

  local width = math.min(72, math.max(30, vim.o.columns - 8))
  local ok, winid = pcall(vim.api.nvim_open_win, bufnr, true, {
    relative = 'editor',
    row = math.max(0, math.floor((vim.o.lines - 8) / 2)),
    col = math.max(0, math.floor((vim.o.columns - width) / 2)),
    width = width,
    height = 6,
    style = 'minimal',
    border = vim.g.quietdm_border or 'rounded',
  })
  if not ok then
    if cancel then
      cancel()
    end
    return
  end
  self.bufnr, self.winid = bufnr, winid

  local function finish(send)
    if gen ~= self.generation then
      return
    end
    self.generation = self.generation + 1
    local body
    if send and vim.api.nvim_buf_is_valid(bufnr) then
      local lines = vim.api.nvim_buf_get_lines(bufnr, 0, -1, false)
      -- A commit message is many lines; a message on the wire is one.
      -- Blank lines go, the rest is joined with spaces.
      local kept = {}
      for _, line in ipairs(lines) do
        line = vim.trim(line)
        if line ~= '' then
          kept[#kept + 1] = line
        end
      end
      body = table.concat(kept, ' ')
    end
    if vim.api.nvim_buf_is_valid(bufnr) then
      -- The write is handled; leaving the buffer modified would make Vim
      -- argue about closing it.
      vim.bo[bufnr].modified = false
    end
    self:close_window()
    if body and body ~= '' then
      submit(body)
    elseif cancel then
      cancel()
    end
  end

  vim.api.nvim_create_autocmd('BufWriteCmd', {
    buffer = bufnr,
    callback = function() finish(true) end,
    desc = 'quietdm: :w sends the reply',
  })
  vim.api.nvim_create_autocmd('BufWipeout', {
    buffer = bufnr,
    once = true,
    callback = function() finish(false) end,
    desc = 'quietdm: closing the buffer abandons the reply',
  })
  vim.cmd('startinsert')
end

function gitcommit:close()
  self.generation = self.generation + 1
  self:close_window()
end

function gitcommit:close_window()
  if self.winid and vim.api.nvim_win_is_valid(self.winid) then
    pcall(vim.api.nvim_win_close, self.winid, true)
  end
  self.winid, self.bufnr = nil, nil
end

return registry.composer(gitcommit)
