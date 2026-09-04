-- A static check of the hard rules in docs/design/01-covert-model.md section 5.
--
-- Every module is scanned for calls that would break an invariant. This is the
-- check the M3 roadmap item asks for, done early because it costs almost
-- nothing and catches the mistakes that matter most.

local root = vim.g.quietdm_test_root

local forbidden = {
  { pattern = 'nvim_buf_set_lines', why = 'I1: message text must never enter a buffer' },
  { pattern = 'nvim_buf_set_text', why = 'I1: message text must never enter a buffer' },
  { pattern = 'virt_lines', why = 'I2: virtual lines shift the layout' },
  { pattern = 'vim%.notify', why = 'I3: nothing may pop up on its own' },
  { pattern = 'nvim_command%(', why = 'I2: no ad-hoc commands that could split or scroll' },
  { pattern = 'nvim_win_set_cursor', why = 'I2: the cursor belongs to the user' },
  { pattern = 'writefile', why = 'I4: nothing is ever written to disk' },
}

-- init.lua is exempt from the buffer rule for exactly one reason: :QuietdmDebug
-- fills a nofile scratch buffer, which invariant I1 explicitly permits.
local exempt = {
  ['init.lua'] = { nvim_buf_set_lines = true },
}

local function sources()
  local files = {}
  local dirs = {
    '/lua/quietdm',
    '/lua/quietdm/renderers',
    '/lua/quietdm/composers',
    '/lua/quietdm/notifiers',
    '/plugin',
  }
  for _, dir in ipairs(dirs) do
    for _, f in ipairs(vim.fn.globpath(root .. dir, '*.lua', false, true)) do
      files[#files + 1] = f
    end
  end
  return files
end

---Comments explain the rules, so they must not trip them.
local function code_lines(file)
  local out = {}
  for _, line in ipairs(vim.fn.readfile(file)) do
    if not line:match('^%s*%-%-') then
      out[#out + 1] = line
    end
  end
  return out
end

return {
  ['no module reaches for a forbidden API'] = function()
    local offences = {}
    for _, file in ipairs(sources()) do
      local name = vim.fn.fnamemodify(file, ':t')
      for _, line in ipairs(code_lines(file)) do
        for _, rule in ipairs(forbidden) do
          local plain = rule.pattern:gsub('%%', ''):gsub('%(', '')
          if line:find(rule.pattern) and not (exempt[name] or {})[plain] then
            offences[#offences + 1] = string.format('%s: %s (%s)', name, vim.trim(line), rule.why)
          end
        end
      end
    end
    T.eq(offences, {}, 'invariant violations found')
  end,

  ['loading the plugin does nothing but define commands'] = function()
    local offences = {}
    for _, line in ipairs(code_lines(root .. '/plugin/quietdm.lua')) do
      -- Anything at column zero runs at load time.
      if line:match('^%S') and line:find('require%(') then
        offences[#offences + 1] = vim.trim(line)
      end
      if line:find('vim%.keymap') then
        offences[#offences + 1] = 'binds a key at load time: ' .. vim.trim(line)
      end
    end
    T.eq(offences, {}, 'plugin/quietdm.lua must only create user commands')
  end,

  ['every documented command exists'] = function()
    local body = table.concat(vim.fn.readfile(root .. '/plugin/quietdm.lua'), '\n')
    for _, name in ipairs({
      'QuietdmStart', 'QuietdmStop', 'QuietdmRead', 'QuietdmPanorama',
      'QuietdmReply', 'QuietdmSilence', 'QuietdmPanic', 'QuietdmDebug',
    }) do
      T.truthy(body:find(name, 1, true), name .. ' is missing')
    end
  end,
}
