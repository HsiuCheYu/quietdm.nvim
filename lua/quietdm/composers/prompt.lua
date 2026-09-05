-- Alternative composer: a one-line floating input, shaped like LSP rename.
--
-- The cmdline composer is quieter and stays the default. This one exists for
-- the case the cmdline cannot cover: a user whose statusline or notification
-- plugin has taken the cmdline over, where a floating input is the thing that
-- already appears on their screen several times a day.

local registry = require('quietdm.registry')

local prompt = {
  name = 'prompt',
  -- The same prompt LSP rename uses, so the window reads as one.
  label = 'New Name: ',
  generation = 0,
  winid = nil,
  bufnr = nil,
}

function prompt:setup(ctx)
  self.ctx = ctx
end

function prompt:open(room, submit, cancel)
  self.generation = self.generation + 1
  local gen = self.generation
  self:close_window()

  -- A prompt buffer: nothing is read from disk, nothing is written to it, and
  -- it is wiped the moment the window goes away (invariant I4).
  local bufnr = vim.api.nvim_create_buf(false, true)
  vim.bo[bufnr].buftype = 'prompt'
  vim.bo[bufnr].bufhidden = 'wipe'
  vim.bo[bufnr].swapfile = false
  vim.fn.prompt_setprompt(bufnr, self.label)

  local width = math.min(40, math.max(20, vim.o.columns - 10))
  local ok, winid = pcall(vim.api.nvim_open_win, bufnr, true, {
    relative = 'cursor',
    row = 1,
    col = 0,
    width = width,
    height = 1,
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

  local function finish(text)
    -- A panic or a FocusLost during composition bumps the generation, and
    -- whatever was typed is dropped rather than sent.
    if gen ~= self.generation then
      return
    end
    self.generation = self.generation + 1
    self:close_window()
    if text and text ~= '' then
      submit(text)
    elseif cancel then
      cancel()
    end
  end

  vim.fn.prompt_setcallback(bufnr, finish)
  vim.fn.prompt_setinterrupt(bufnr, function() finish(nil) end)
  vim.keymap.set({ 'i', 'n' }, '<Esc>', function() finish(nil) end, { buffer = bufnr, nowait = true })
  vim.api.nvim_create_autocmd('BufLeave', {
    buffer = bufnr,
    once = true,
    callback = function() finish(nil) end,
    desc = 'quietdm: leaving the input abandons the reply',
  })
  vim.cmd('startinsert')
end

function prompt:close()
  self.generation = self.generation + 1
  self:close_window()
end

function prompt:close_window()
  if self.winid and vim.api.nvim_win_is_valid(self.winid) then
    pcall(vim.api.nvim_win_close, self.winid, true)
  end
  self.winid, self.bufnr = nil, nil
end

return registry.composer(prompt)
