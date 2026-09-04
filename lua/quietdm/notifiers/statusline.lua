-- Default L0 notifier.
--
-- It draws nothing at all: it keeps a token the user drops into their own
-- statusline. The whole point is that the token is an element that is already
-- there, changing state the way a language server indicator would
-- (docs/design/01-covert-model.md section 3, L0).

local registry = require('quietdm.registry')

local statusline = {
  name = 'statusline',
  unread = 0,
  -- Looks like an LSP status indicator: idle vs. working.
  quiet = '✓',
  busy = '⟳',
}

function statusline:update(unread)
  self.unread = unread or 0
end

function statusline:token()
  if self.unread > 0 then
    return self.busy
  end
  return self.quiet
end

return registry.notifier(statusline)
