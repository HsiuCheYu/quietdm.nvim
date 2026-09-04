# 04 — Lua 外掛介面

前端的可插拔架構。所有介面的設計目標，是讓**遵守不變式成為預設路徑，違反不變式需要刻意繞過**。

## 一、設定

```lua
require('quietdm').setup {
  -- 曝光等級
  level = {
    default        = 'L0',   -- 常駐等級
    glance_delay   = 500,    -- CursorHold 延遲（ms），應與 'updatetime' 一致
    glance_timeout = 4000,   -- L1 自動消失（ms）
    idle_downgrade = 60000,  -- 無操作多久自動降回 L0（ms）
  },

  -- 呈現與輸入的實作選擇
  renderers = { glance = 'blame', read = 'float', panorama = 'quickfix' },
  composer  = 'cmdline',
  notifier  = 'statusline',

  -- 呈現規則
  display = {
    max_width   = 60,        -- 顯示寬度單位，非字元數
    time_format = 'relative',-- 'relative' | 'clock'
    separator   = ' · ',
  },

  -- 什麼場合可以呈現
  guard = {
    filetypes = { 'go', 'lua', 'python', 'rust', 'typescript', 'c', 'cpp' },
    exclude_buftypes = { 'terminal', 'quickfix', 'help', 'prompt', 'nofile' },
    silence_minutes  = 10,   -- panic 後的靜默時長
  },

  -- panic 鍵預設不綁定，使用者必須自行選一個順手且不衝突的鍵
  panic_key = nil,
}
```

`setup()` **不會自動連線也不會綁定任何鍵**。啟動由 `:QuietdmStart` 或使用者自行在 config 中呼叫 `require('quietdm').start()` 觸發。理由：一個外掛在使用者不知情時建立網路連線，是不能接受的行為。

## 二、Message

前端內部的訊息表示，欄位與 [03-ipc-protocol.md](03-ipc-protocol.md) 的 `message` event 一一對應：

```lua
---@class quietdm.Message
---@field room    string   Room ID
---@field event   string   Event ID, unique; used for de-duplication
---@field display string   Alias-applied display name. Always render this,
---                        never the raw sender ID.
---@field body    string   Emoji-stripped plain text, NOT truncated
---@field ts      integer  Unix seconds
---@field own     boolean  True if sent by the user
---@field kind    string   'text' | 'image' | 'audio' | 'sticker' | 'other'
```

## 三、Renderer

一個 renderer 負責把訊息畫到畫面上。每個曝光等級各綁一個 renderer。

```lua
---@class quietdm.Renderer
---@field name  string
---@field level string  'glance' | 'read' | 'panorama'  (L1 | L2 | L3)
local Renderer = {}

--- Called once when the renderer is activated.
---@param ctx quietdm.Ctx
function Renderer:setup(ctx) end

--- Render messages. Called only in response to a user action
--- (CursorHold, keypress, command) — never on message arrival.
---@param msgs quietdm.Message[]  Oldest first.
---@param ctx  quietdm.Ctx
function Renderer:render(msgs, ctx) end

--- Remove everything this renderer put on screen. Must be idempotent and
--- safe to call from FocusLost, VimLeavePre, and panic.
function Renderer:clear() end
```

### ctx 提供的 helper

`ctx` 是核心提供給 renderer 的工具箱。**它刻意不提供任何能修改 buffer 文字或版面的函式**——這是不變式 I1 與 I2 在 API 層的保證。

```lua
---@class quietdm.Ctx
---@field ns integer  Dedicated extmark namespace, cleared on panic

--- Attach virtual text to a line. The only sanctioned way to draw inline.
--- Always uses virt_text (never virt_lines, which would shift the layout).
---@param bufnr integer
---@param lnum  integer  0-indexed
---@param chunks table    { {text, hlgroup}, ... }
---@param opts  table?    { align = 'right'|'eol' }
function ctx.virt_text(bufnr, lnum, chunks, opts) end

--- Open a float styled identically to the user's real LSP hover window:
--- same border, same highlight groups, same width rules.
---@param lines string[]
---@return integer winid
function ctx.hover(lines) end

--- Truncate to a display width (not a character count), appending an
--- ellipsis. Uses strdisplaywidth so CJK text aligns correctly.
---@param s     string
---@param width integer
---@return string
function ctx.truncate(s, width) end

--- Format a timestamp per the display.time_format setting.
---@param ts integer
---@return string
function ctx.time(ts) end

--- Resolve a highlight group name. Only accepts names that link to an
--- existing group; custom colors are rejected at setup time.
---@param role string  'text' | 'name' | 'time' | 'sep'
---@return string
function ctx.hl(role) end
```

### 內建 renderer

| 名稱 | 等級 | 說明 |
|---|---|---|
| `blame` | glance (L1) | **預設。** 游標行右對齊虛擬文字，`m.chen · 3 分鐘前 · 晚上要吃什麼` |
| `diagnostic` | glance (L1) | 行尾診斷樣式虛擬文字，highlight 用 `DiagnosticVirtualTextHint` |
| `float` | read (L2) | **預設。** LSP hover 樣式浮動視窗，顯示近 N 則 |
| `quickfix` | panorama (L3) | 對話歷史填入 quickfix 列表。**必須攔截 `<CR>` 跳轉**，否則會跳到不存在的假檔案路徑 |

### 註冊自訂 renderer

```lua
require('quietdm').register_renderer {
  name  = 'my_renderer',
  level = 'glance',
  setup  = function(self, ctx) self.ctx = ctx end,
  render = function(self, msgs, ctx)
    local m = msgs[#msgs]
    ctx.virt_text(0, vim.fn.line('.') - 1, {
      { ctx.truncate(m.display .. ': ' .. m.body, 60), ctx.hl('text') },
    }, { align = 'right' })
  end,
  clear = function(self) vim.api.nvim_buf_clear_namespace(0, self.ctx.ns, 0, -1) end,
}
```

## 四、Composer

```lua
---@class quietdm.Composer
---@field name string
local Composer = {}

--- Prompt for input and hand the result to submit(). Must not touch any
--- real file buffer: use the cmdline or a scratch buffer with
--- buftype=nofile, bufhidden=wipe, swapfile=false, undofile off.
---@param room     string
---@param submit   fun(body: string)  Call with the composed text.
---@param cancel   fun()              Call if the user aborts.
function Composer:open(room, submit, cancel) end

--- Abort any in-progress composition. Called on panic and FocusLost.
function Composer:close() end
```

送出成功後**核心不顯示任何提示**（見 [01-covert-model.md](01-covert-model.md#七回覆通道)）。送出失敗時，核心會透過目前的 glance renderer 呈現一則 `DiagnosticHint` 樣式的短提示。

### 內建 composer

| 名稱 | 說明 |
|---|---|
| `cmdline` | **預設。** cmdline 輸入，提示符偽裝成搜尋 / 替換指令，送出後自動清空 |
| `prompt` | LSP rename 樣式的單行浮動輸入框 |
| `gitcommit` | scratch buffer 且 `filetype=gitcommit`，`:w` 送出、`:q` 取消。適合長回覆 |

## 五、Notifier

L0 等級下傳達「有新訊息」的暗號。Notifier **不繪製任何東西**——它只維護狀態，由使用者的 statusline 取值。

```lua
---@class quietdm.Notifier
local Notifier = {}

--- Called when unread state changes. Must not draw anything.
---@param unread integer  Total unread across all rooms.
function Notifier:update(unread) end

--- The disguised token for the user's statusline to render.
--- Must return a string that is plausible in the user's statusline even
--- when there are no messages.
---@return string
function Notifier:token() end
```

內建的 `statusline` notifier 回傳一個 LSP 狀態樣式的字元：無訊息時 `✓`，有未讀時 `⟳`。使用者在自己的 statusline 設定中插入：

```lua
-- lualine 範例
sections = { lualine_x = { function() return require('quietdm').token() end } }
```

**為什麼由使用者自行插入而非外掛自動接管 statusline：** 每個人的 statusline 外掛與版面都不同，自動接管一定會產生突兀的區段——而突兀正是本專案要避免的東西。讓使用者自己決定放哪，才能真正融入。

## 六、使用者指令

| 指令 | 作用 |
|---|---|
| `:QuietdmStart` / `:QuietdmStop` | 連線 / 斷線 |
| `:QuietdmRead` | 升到 L2 |
| `:QuietdmPanorama` | 升到 L3 |
| `:QuietdmReply` | 開啟 composer |
| `:QuietdmSilence [分鐘]` | 手動進入靜默（開會、螢幕分享前用） |
| `:QuietdmPanic` | 清空並靜默 |
| `:QuietdmDebug` | 在 scratch buffer 顯示 IPC 記錄 |

指令名稱刻意寫得像一個開發工具而非聊天軟體。

## 七、介面對不變式的保證

| 不變式 | API 層的保證 |
|---|---|
| I1 永不寫入 buffer 文字 | `ctx` 不提供任何寫入 buffer 的函式；composer 介面文件明確要求只能用 cmdline 或 scratch buffer |
| I2 永不改變版面 | `ctx.virt_text` 內部固定使用 `virt_text`，不暴露 `virt_lines`；`ctx` 不提供開 split / 移動游標的函式 |
| I3 永不主動吸睛 | `ctx.hl` 只接受 link 到既有 group 的角色名，setup 時驗證；核心不呼叫 `vim.notify` |
| I4 永不落地 | 核心不寫檔；composer 的 scratch buffer 規格在介面文件中明列 |
| I5 未經注視不呈現 | `render()` 只由 `level.lua` 在使用者動作時呼叫；訊息到達的路徑只會走到 `Notifier:update()` |

## 八、guard 的責任

`guard.lua` 在每次呈現前檢查，任一條不通過就跳過呈現：

- 目前是否在靜默期
- 目前 buffer 的 `filetype` 是否在白名單
- 目前 buffer 的 `buftype` 是否在排除清單
- 是否在 `diff` 模式
- 視窗是否夠寬（太窄時右對齊的 blame 會擠壓程式碼，反而顯眼）

並註冊自動事件：`FocusLost`、`VimResized`、`VimLeavePre`、`InsertEnter`、`CursorMoved`。
