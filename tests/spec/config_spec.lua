local config = require('quietdm.config')

return {
  ['defaults are complete'] = function()
    local cfg = config.build()
    T.eq(cfg.level.default, 'L0')
    T.eq(cfg.renderers.glance, 'blame')
    T.eq(cfg.composer, 'cmdline')
    T.eq(cfg.panic_key, nil)
  end,

  ['user options override defaults without dropping the rest'] = function()
    local cfg = config.build({ display = { max_width = 30 } })
    T.eq(cfg.display.max_width, 30)
    T.eq(cfg.display.separator, ' · ')
  end,

  ['socket path honours an explicit override'] = function()
    local cfg = config.build({ socket = '/tmp/quietdm-test.sock' })
    T.eq(config.socket_path(cfg), '/tmp/quietdm-test.sock')
  end,

  ['socket path follows XDG_RUNTIME_DIR'] = function()
    local saved = vim.env.XDG_RUNTIME_DIR
    vim.env.XDG_RUNTIME_DIR = '/tmp/xdg-test'
    local path = config.socket_path(config.build())
    vim.env.XDG_RUNTIME_DIR = saved
    T.eq(path, '/tmp/xdg-test/quietdm/sock')
  end,

  -- The daemon's last resort is Go's os.TempDir(): $TMPDIR with trailing
  -- slashes stripped, else /tmp. If the two sides disagree on the path the
  -- frontend simply never connects — silently, because that is what this
  -- plugin does with connection failures.
  --
  -- Tested against temp_dir() directly rather than through socket_path,
  -- because on a machine with /run/user/$UID the fallback is never reached
  -- and a test that goes through socket_path would quietly verify nothing.
  ['the temp-dir fallback mirrors Go os.TempDir'] = function()
    local tmp = vim.env.TMPDIR

    vim.env.TMPDIR = ''
    T.eq(config.temp_dir(), '/tmp')

    vim.env.TMPDIR = '/var/tmp/mine'
    T.eq(config.temp_dir(), '/var/tmp/mine')

    vim.env.TMPDIR = '/var/tmp/mine///'
    T.eq(config.temp_dir(), '/var/tmp/mine', 'trailing slashes are stripped')

    vim.env.TMPDIR = '/'
    T.eq(config.temp_dir(), '/', 'but never down to nothing')

    vim.env.TMPDIR = tmp
  end,

  -- Go joins path elements with exactly one separator whatever the directory
  -- ends in; string concatenation does not.
  ['a runtime dir with a trailing slash still gives one separator'] = function()
    local saved = vim.env.XDG_RUNTIME_DIR
    vim.env.XDG_RUNTIME_DIR = '/run/user/test///'
    local path = config.socket_path(config.build())
    vim.env.XDG_RUNTIME_DIR = saved
    T.eq(path, '/run/user/test/quietdm/sock')
  end,
}
