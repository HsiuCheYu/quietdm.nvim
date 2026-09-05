-- The composers are the one place the user types message text, so they are the
-- one place I1 and I4 are easiest to break: a real buffer, a real file, a
-- leftover window.

local registry = require('quietdm.registry')

require('quietdm.composers.cmdline')
require('quietdm.composers.gitcommit')
require('quietdm.composers.prompt')

---Other specs can leave windows open, so a global window count is not a
---reliable measure here. Track the composer's own window instead.
local function alive(winid)
  return winid ~= nil and vim.api.nvim_win_is_valid(winid)
end

---Run fn against a composer, collecting what it submitted or cancelled.
local function drive(name, fn)
  local c = registry.composers[name]
  local out = { submitted = nil, cancelled = false }
  fn(c, function(body) out.submitted = body end, function() out.cancelled = true end)
  return c, out
end

return {
  ['every builtin composer registers and answers exactly once'] = function()
    registry.load_builtins()
    for _, name in ipairs({ 'cmdline', 'gitcommit', 'prompt' }) do
      T.truthy(registry.composers[name], name .. ' is missing')
    end
  end,

  ['gitcommit sends on :w and never reaches a file'] = function()
    local c, out = drive('gitcommit', function(comp, submit, cancel)
      comp:open('!r:localhost', submit, cancel)
    end)
    T.truthy(alive(c.winid), 'the composer opened a window')

    local buf = c.bufnr
    -- acwrite, not nofile: it is what makes :w fire BufWriteCmd instead of
    -- writing. Nothing here ever reaches the disk either way (I4).
    T.eq(vim.bo[buf].buftype, 'acwrite')
    T.eq(vim.bo[buf].bufhidden, 'wipe', 'the text must not outlive the window')
    T.eq(vim.bo[buf].filetype, 'gitcommit')
    T.eq(vim.bo[buf].swapfile, false)

    vim.api.nvim_buf_set_lines(buf, 0, -1, false, { '七點拉麵店見', '', '你先到就先點' })
    vim.cmd('write')

    T.eq(out.submitted, '七點拉麵店見 你先到就先點', 'blank lines go, the rest is one line')
    T.falsy(out.cancelled)
    T.falsy(alive(c.winid), 'the window is gone')
  end,

  ['gitcommit closing the window without writing abandons the reply'] = function()
    local c, out = drive('gitcommit', function(comp, submit, cancel)
      comp:open('!r:localhost', submit, cancel)
    end)
    local winid = c.winid
    vim.api.nvim_buf_set_lines(c.bufnr, 0, -1, false, { 'never mind' })
    vim.api.nvim_win_close(winid, true)
    T.eq(out.submitted, nil, 'nothing may be sent that the user did not send')
    T.truthy(out.cancelled, 'the caller has to learn the composition ended')
    T.falsy(alive(winid))
  end,

  ['a panic during composition drops the text'] = function()
    for _, name in ipairs({ 'gitcommit', 'prompt' }) do
      local c, out = drive(name, function(comp, submit, cancel)
        comp:open('!r:localhost', submit, cancel)
      end)
      local winid = c.winid
      T.truthy(alive(winid), name .. ' opened no window')
      -- close() is what panic calls. Whatever was typed goes nowhere.
      c:close()
      T.eq(out.submitted, nil, name .. ' sent text after a panic')
      T.falsy(alive(winid), name .. ' left a window behind')
    end
  end,

  ['prompt opens a one-line floating input'] = function()
    local c = drive('prompt', function(comp, submit, cancel)
      comp:open('!r:localhost', submit, cancel)
    end)
    T.truthy(alive(c.winid))
    local cfg = vim.api.nvim_win_get_config(c.winid)
    T.truthy(cfg.relative ~= '', 'it floats')
    T.eq(cfg.height, 1, 'one line, like LSP rename')
    T.eq(vim.bo[c.bufnr].buftype, 'prompt')
    local winid = c.winid
    c:close()
    T.falsy(alive(winid))
  end,
}
