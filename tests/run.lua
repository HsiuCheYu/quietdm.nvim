-- A very small test runner: no external dependency, because the whole point
-- of this plugin is that it works on somebody else's machine.

local T = {}
local failures = 0
local total = 0

function T.eq(got, want, msg)
  if not vim.deep_equal(got, want) then
    error(string.format('%s\n  expected: %s\n  got:      %s',
      msg or 'values differ', vim.inspect(want), vim.inspect(got)), 2)
  end
end

function T.truthy(value, msg)
  if not value then
    error(msg or 'expected a truthy value', 2)
  end
end

function T.falsy(value, msg)
  if value then
    error((msg or 'expected a falsy value') .. ', got ' .. vim.inspect(value), 2)
  end
end

function T.err(fn, msg)
  local ok = pcall(fn)
  if ok then
    error(msg or 'expected an error', 2)
  end
end

_G.T = T

local root = vim.g.quietdm_test_root
local specs = vim.fn.globpath(root .. '/tests/spec', '*.lua', false, true)
table.sort(specs)

for _, file in ipairs(specs) do
  local name = vim.fn.fnamemodify(file, ':t:r')
  local ok, suite = pcall(dofile, file)
  if not ok then
    failures = failures + 1
    io.stderr:write(string.format('LOAD FAIL %s: %s\n', name, suite))
  else
    for case, fn in pairs(suite) do
      total = total + 1
      local passed, err = pcall(fn)
      if passed then
        io.stdout:write(string.format('ok   %s / %s\n', name, case))
      else
        failures = failures + 1
        io.stderr:write(string.format('FAIL %s / %s\n  %s\n', name, case, err))
      end
    end
  end
end

io.stdout:write(string.format('\n%d tests, %d failures\n', total, failures))
if failures > 0 then
  vim.cmd('cquit 1')
end
vim.cmd('qall!')
