-- Registry of renderers, composers and notifiers.
--
-- Registration validates the shape of an implementation up front, so a broken
-- third-party renderer fails at setup rather than halfway through drawing.

local log = require('quietdm.log')

local M = {}

M.renderers = {} -- name -> renderer
M.composers = {} -- name -> composer
M.notifiers = {} -- name -> notifier

local LEVELS = { glance = true, read = true, panorama = true }

local function check(cond, msg)
  if not cond then
    error('quietdm: ' .. msg, 0)
  end
end

---Register a renderer.
---@param r table
function M.renderer(r)
  check(type(r) == 'table', 'renderer must be a table')
  check(type(r.name) == 'string' and r.name ~= '', 'renderer needs a name')
  check(LEVELS[r.level], string.format('renderer %s has invalid level %s', r.name, tostring(r.level)))
  check(type(r.render) == 'function', 'renderer ' .. r.name .. ' needs render()')
  check(type(r.clear) == 'function', 'renderer ' .. r.name .. ' needs clear()')
  M.renderers[r.name] = r
  log.debug('registered renderer ' .. r.name)
  return r
end

---Register a composer.
---@param c table
function M.composer(c)
  check(type(c) == 'table', 'composer must be a table')
  check(type(c.name) == 'string' and c.name ~= '', 'composer needs a name')
  check(type(c.open) == 'function', 'composer ' .. c.name .. ' needs open()')
  check(type(c.close) == 'function', 'composer ' .. c.name .. ' needs close()')
  M.composers[c.name] = c
  log.debug('registered composer ' .. c.name)
  return c
end

---Register a notifier.
---@param n table
function M.notifier(n)
  check(type(n) == 'table', 'notifier must be a table')
  check(type(n.name) == 'string' and n.name ~= '', 'notifier needs a name')
  check(type(n.update) == 'function', 'notifier ' .. n.name .. ' needs update()')
  check(type(n.token) == 'function', 'notifier ' .. n.name .. ' needs token()')
  M.notifiers[n.name] = n
  log.debug('registered notifier ' .. n.name)
  return n
end

---Look up a renderer, checking that it serves the level it was asked for.
---@param name string|nil
---@param level string
---@return table|nil
function M.get_renderer(name, level)
  if not name then
    return nil
  end
  local r = M.renderers[name]
  if not r then
    log.warn('no renderer named ' .. name)
    return nil
  end
  if r.level ~= level then
    log.warn(string.format('renderer %s serves %s, not %s', name, r.level, level))
    return nil
  end
  return r
end

---Load the implementations that ship with the plugin.
function M.load_builtins()
  require('quietdm.renderers.blame')
  require('quietdm.renderers.diagnostic')
  require('quietdm.renderers.float')
  require('quietdm.renderers.quickfix')
  require('quietdm.composers.cmdline')
  require('quietdm.notifiers.statusline')
end

function M.reset()
  M.renderers, M.composers, M.notifiers = {}, {}, {}
end

return M
