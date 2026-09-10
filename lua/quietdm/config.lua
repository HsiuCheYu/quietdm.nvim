-- Default configuration and merging.
--
-- Every default here is a suggestion, not a rule: working environments differ
-- enough that a single set of choices cannot suit everyone
-- (docs/design/01-covert-model.md section 8).

local M = {}

---@class quietdm.Config
M.defaults = {
  -- Socket path. nil resolves to $XDG_RUNTIME_DIR/quietdm/sock.
  socket = nil,

  -- Exposure levels.
  level = {
    default = 'L0',
    glance_delay = 500, -- CursorHold delay (ms); should match 'updatetime'
    glance_timeout = 4000, -- L1 disappears on its own after this
    idle_downgrade = 60000, -- back to L0 after this long without input
    hint_timeout = 3000, -- how long the failed-send hint stays on screen
  },

  -- Which implementation draws each level, and how replies are typed.
  renderers = { glance = 'blame', read = 'float', panorama = 'quickfix' },
  composer = 'cmdline',
  notifier = 'statusline',

  -- Presentation rules.
  display = {
    max_width = 60, -- display cells, not characters
    time_format = 'relative', -- 'relative' | 'clock'
    separator = ' · ',
  },

  -- When presenting is allowed at all.
  guard = {
    filetypes = { 'go', 'lua', 'python', 'rust', 'typescript', 'javascript', 'c', 'cpp', 'java', 'sh' },
    exclude_buftypes = { 'terminal', 'quickfix', 'help', 'prompt', 'nofile' },
    silence_minutes = 10,
    min_width = 80, -- narrower than this and right-aligned text crowds the code
  },

  -- The panic key is deliberately unbound: it has to be a key the user can
  -- hit without thinking, and only they know which one that is.
  panic_key = nil,

  -- How many messages per room the frontend keeps in memory.
  history = 50,
}

-- Highlight roles resolve to groups that already exist in the user's
-- colorscheme. Custom colors are rejected at setup (invariant I3).
M.hl_roles = {
  text = 'Comment',
  name = 'Comment',
  time = 'Comment',
  sep = 'NonText',
  hint = 'DiagnosticVirtualTextHint',
}

---Deep-merge user options over the defaults.
---@param opts table|nil
---@return quietdm.Config
function M.build(opts)
  return vim.tbl_deep_extend('force', vim.deepcopy(M.defaults), opts or {})
end

---The daemon's last resort for a runtime directory is Go's os.TempDir():
---$TMPDIR with trailing slashes stripped (but never down to nothing), else
---/tmp. Mirror it exactly.
---
---vim.fn.tempname() would point at nvim's own private subdirectory, which the
---daemon has never heard of — and a path the two sides disagree on fails
---silently, which is the hardest kind of failure to notice here. Exported so
---the rule can be tested on any machine, including one where the earlier
---branches mean it is never reached.
---@return string
function M.temp_dir()
  local tmp = vim.env.TMPDIR
  if not tmp or tmp == '' then
    return '/tmp'
  end
  while #tmp > 1 and tmp:sub(-1) == '/' do
    tmp = tmp:sub(1, -2)
  end
  return tmp
end

---Join a directory with the rest of the socket path the way Go's
---filepath.Join does: exactly one separator, whatever the directory ends in.
local function join(dir, rest)
  return (dir:gsub('/+$', '')) .. '/' .. rest
end

---Resolve the socket path, mirroring the daemon's own default.
---@param cfg quietdm.Config
---@return string
function M.socket_path(cfg)
  if cfg.socket and cfg.socket ~= '' then
    return vim.fn.expand(cfg.socket)
  end
  local runtime = vim.env.XDG_RUNTIME_DIR
  if not runtime or runtime == '' then
    -- getuid has no Windows equivalent, so luv leaves it undefined there
    -- rather than raising: guard both vim.uv and the vim.loop fallback
    -- before calling, and fall back to temp_dir() when neither exists.
    local getuid = (vim.uv and vim.uv.getuid) or (vim.loop and vim.loop.getuid)
    if getuid then
      runtime = '/run/user/' .. tostring(getuid())
      if vim.fn.isdirectory(runtime) == 0 then
        runtime = M.temp_dir()
      end
    else
      runtime = M.temp_dir()
    end
  end
  return join(runtime, 'quietdm/sock')
end

return M
