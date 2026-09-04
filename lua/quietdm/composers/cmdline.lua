-- Default composer: type the reply on the cmdline.
--
-- Typing on the cmdline is something a vim user does every minute; it costs no
-- screen space, changes no layout, and sits at the bottom of the screen, as
-- far from a passer-by's eye as the display allows.

local registry = require('quietdm.registry')

local cmdline = {
  name = 'cmdline',
  -- The prompt is a plausible substitute command. Nothing is ever executed:
  -- vim.ui.input only reads a line of text.
  prompt = ':%s/',
  generation = 0,
}

function cmdline:open(room, submit, cancel)
  self.generation = self.generation + 1
  local gen = self.generation
  vim.ui.input({ prompt = self.prompt }, function(input)
    -- A panic or a FocusLost during composition bumps the generation, and the
    -- text the user had typed is dropped on the floor rather than sent.
    if gen ~= self.generation then
      return
    end
    if not input or input == '' then
      if cancel then
        cancel()
      end
      return
    end
    submit(input)
  end)
end

function cmdline:close()
  -- Abandon whatever is in flight. The cmdline itself cannot be closed from
  -- Lua while it is reading, but its result will now be ignored.
  self.generation = self.generation + 1
end

return registry.composer(cmdline)
