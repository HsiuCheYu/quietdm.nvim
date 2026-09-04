-- The toolbox handed to renderers.
--
-- What it deliberately does NOT offer is the point: there is no way here to
-- write buffer text, insert visual lines, split a window or move the cursor.
-- Invariants I1 and I2 are enforced by the shape of this API, not by review
-- (docs/design/04-plugin-api.md section 7).

local config = require('quietdm.config')
local log = require('quietdm.log')

local M = {}

local ns = vim.api.nvim_create_namespace('quietdm')

---The extmark namespace every renderer draws into. Panic clears it wholesale.
---@return integer
function M.namespace() return ns end

---Clear everything this plugin drew, in one buffer or in all of them.
---@param bufnr integer|nil
function M.clear_all(bufnr)
  if bufnr then
    if vim.api.nvim_buf_is_valid(bufnr) then
      vim.api.nvim_buf_clear_namespace(bufnr, ns, 0, -1)
    end
    return
  end
  for _, b in ipairs(vim.api.nvim_list_bufs()) do
    if vim.api.nvim_buf_is_valid(b) then
      vim.api.nvim_buf_clear_namespace(b, ns, 0, -1)
    end
  end
end

---Relative time in the style git blame uses.
---@param ts integer
---@return string
function M.relative(ts)
  local delta = os.time() - (ts or 0)
  if delta < 60 then
    return '剛剛'
  elseif delta < 3600 then
    return string.format('%d 分鐘前', math.floor(delta / 60))
  elseif delta < 86400 then
    return string.format('%d 小時前', math.floor(delta / 3600))
  end
  return string.format('%d 天前', math.floor(delta / 86400))
end

---Truncate to a display width, not a character count: CJK text is two cells
---wide and a mis-measured right-aligned line is far more visible than the
---text itself (docs/design/01-covert-model.md section 4).
---@param s string
---@param width integer
---@return string
function M.truncate(s, width)
  if width <= 0 then
    return ''
  end
  if vim.fn.strdisplaywidth(s) <= width then
    return s
  end
  local ellipsis = '…'
  local budget = width - vim.fn.strdisplaywidth(ellipsis)
  if budget <= 0 then
    return ellipsis
  end
  local out = {}
  local used = 0
  for _, char in ipairs(vim.fn.split(s, '\\zs')) do
    local w = vim.fn.strdisplaywidth(char)
    if used + w > budget then
      break
    end
    out[#out + 1] = char
    used = used + w
  end
  return table.concat(out) .. ellipsis
end

---Build the context table renderers receive.
---@param cfg quietdm.Config
---@return quietdm.Ctx
function M.new(cfg)
  local ctx = { ns = ns }

  ---Attach virtual text to a line. The only sanctioned way to draw inline.
  ---Always uses virt_text: virt_lines would shift every line below it.
  ctx.virt_text = function(bufnr, lnum, chunks, opts)
    opts = opts or {}
    if bufnr == 0 or bufnr == nil then
      bufnr = vim.api.nvim_get_current_buf()
    end
    if not vim.api.nvim_buf_is_valid(bufnr) then
      return nil
    end
    local last = vim.api.nvim_buf_line_count(bufnr) - 1
    if lnum < 0 or lnum > last then
      return nil
    end
    local ok, id = pcall(vim.api.nvim_buf_set_extmark, bufnr, ns, lnum, 0, {
      virt_text = chunks,
      virt_text_pos = opts.align == 'right' and 'right_align' or 'eol',
      hl_mode = 'combine',
      priority = 1,
    })
    if not ok then
      log.warn('virt_text: ' .. tostring(id))
      return nil
    end
    return id
  end

  ---Open a float styled exactly like the user's real LSP hover window.
  ---Reusing vim.lsp.util.open_floating_preview is the whole trick: whatever
  ---border and highlights the user configured for hover, we inherit.
  ctx.hover = function(lines, opts)
    opts = vim.tbl_extend('force', {
      focusable = false,
      focus = false,
      border = vim.g.quietdm_border or 'rounded',
      max_width = cfg.display.max_width,
      wrap = true,
      close_events = {
        'CursorMoved', 'CursorMovedI', 'InsertEnter', 'BufLeave', 'WinLeave', 'FocusLost',
      },
    }, opts or {})
    local ok, _, winid = pcall(vim.lsp.util.open_floating_preview, lines, '', opts)
    if not ok then
      log.warn('hover failed')
      return nil
    end
    return winid
  end

  ctx.truncate = M.truncate

  ---Format a timestamp per the display.time_format setting.
  ctx.time = function(ts)
    if cfg.display.time_format == 'clock' then
      return os.date('%H:%M', ts)
    end
    return M.relative(ts)
  end

  ---Resolve a highlight role to a group name. Only groups that already exist
  ---are accepted: a custom color would clash with the user's colorscheme and
  ---stand out (invariant I3).
  ctx.hl = function(role)
    local overrides = cfg.highlights or {}
    local group = overrides[role] or config.hl_roles[role] or config.hl_roles.text
    if vim.fn.hlexists(group) == 0 then
      log.warn('highlight group ' .. tostring(group) .. ' missing, using Comment')
      return 'Comment'
    end
    return group
  end

  ctx.separator = cfg.display.separator
  ctx.max_width = cfg.display.max_width

  return ctx
end

return M
