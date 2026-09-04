local registry = require('quietdm.registry')

return {
  ['a renderer needs a name, a valid level and both methods'] = function()
    T.err(function() registry.renderer({ level = 'glance', render = function() end, clear = function() end }) end)
    T.err(function() registry.renderer({ name = 'x', level = 'nope', render = function() end, clear = function() end }) end)
    T.err(function() registry.renderer({ name = 'x', level = 'glance', clear = function() end }) end)
  end,

  ['a registered renderer is only returned for its own level'] = function()
    registry.renderer({ name = 'spec_glance', level = 'glance', render = function() end, clear = function() end })
    T.truthy(registry.get_renderer('spec_glance', 'glance'))
    T.falsy(registry.get_renderer('spec_glance', 'read'))
    T.falsy(registry.get_renderer('spec_missing', 'glance'))
  end,

  ['builtins register themselves'] = function()
    registry.load_builtins()
    T.truthy(registry.get_renderer('blame', 'glance'))
    T.truthy(registry.get_renderer('float', 'read'))
    T.truthy(registry.composers.cmdline)
    T.truthy(registry.notifiers.statusline)
  end,
}
