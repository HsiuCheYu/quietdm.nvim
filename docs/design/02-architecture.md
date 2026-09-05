# 02 — 系統架構

## 一、整體資料流

```
┌─────────────────┐
│  IG / Messenger │
└────────┬────────┘
         │ Meta 私有協定
┌────────▼────────┐
│  mautrix-meta   │  使用者自架，本專案不維護
└────────┬────────┘
         │ Matrix Application Service API
┌────────▼────────┐
│ Matrix homeserver│ 使用者自架（Conduit / Dendrite / Synapse）
└────────┬────────┘
         │ Matrix Client-Server API（/sync 長輪詢）+ E2EE
┌────────▼────────┐
│    quietdmd     │  本專案（Go）
│  ┌───────────┐  │
│  │ Transport │  │  matrix / mock
│  ├───────────┤  │
│  │  Session  │  │  聯絡人別名、未讀狀態、訊息正規化
│  ├───────────┤  │
│  │   Store   │  │  SQLite
│  ├───────────┤  │
│  │  IPC Hub  │  │  Unix socket，broadcast 給所有 client
│  └───────────┘  │
└────────┬────────┘
         │ Unix domain socket + NDJSON
    ┌────┴────┬─────────┐
┌───▼───┐ ┌───▼───┐ ┌───▼───┐
│ nvim  │ │ nvim  │ │ nvim  │  本專案（Lua）
│  #1   │ │  #2   │ │  #3   │
└───────┘ └───────┘ └───────┘
```

## 二、為什麼要有 daemon

最直覺的做法是讓 nvim 外掛直接連 Matrix（用 `jobstart` 跑一個子程序）。不採用的理由：

1. **nvim 關著時也要收訊。** 沒有 daemon，關掉編輯器就等於離線，重開後要重新 sync 一大段歷史，第一次呈現會延遲數秒。
2. **E2EE 的 device identity 必須穩定。** Matrix 的加密以 device 為單位管理金鑰。若每次開 nvim 都建立新 device，會不斷產生新的未驗證裝置、對方的用戶端會跳警告、金鑰共享也會失敗。daemon 讓整台機器只有一個穩定 device。
3. **多個 nvim 實例要共享同一份狀態。** 工程師常同時開好幾個 nvim。若各自連線，未讀狀態會不一致、同一則訊息會在每個實例重複呈現、送出的訊息順序也可能錯亂。
4. **同步負擔不該落在編輯器。** `/sync` 是長輪詢，加解密有 CPU 成本。這些跑在 nvim 的 event loop 旁邊會拖慢編輯體驗——而編輯器變慢是最明顯的破綻之一。
5. **前端可以無狀態且可拋棄。** nvim 崩潰、重開、換終端機都不影響訊息接收。

## 三、Go 端模組切分

```
cmd/
  quietdmd/          # main：旗標解析、設定載入、生命週期
internal/
  model/             # Message / Room 等共用型別，不相依任何其他套件
  transport/         # Transport 介面 + matrix 實作 + mock 實作
  session/           # 訊息正規化、聯絡人別名、未讀狀態機
  store/             # Store 介面 + 記憶體實作（M1）、SQLite（M2）
  ipc/               # Unix socket server、NDJSON 編解碼、client 管理
  config/            # TOML 設定檔
  sanitize/          # emoji 過濾、多媒體佔位符
```

`model` 獨立出來的理由：`transport`、`store`、`session`、`ipc` 都要談論同一個
訊息型別，若把它放在其中任何一層，其餘各層就得互相 import。

### Transport 介面

抽象化「訊息從哪來」，讓 Matrix 只是其中一種實作。

```go
// Transport delivers messages from a backing chat network and accepts
// outgoing messages. Implementations must be safe for concurrent use.
type Transport interface {
    // Start begins receiving. Incoming messages are pushed to the returned
    // channel until ctx is cancelled or the channel is closed.
    Start(ctx context.Context) (<-chan Event, error)

    // Send delivers a message to a room and returns the assigned event ID.
    Send(ctx context.Context, roomID, body string) (string, error)

    // MarkRead advances the read receipt for a room.
    MarkRead(ctx context.Context, roomID, eventID string) error

    // Rooms lists the conversations currently available.
    Rooms(ctx context.Context) ([]Room, error)

    Close() error
}
```

兩個實作：
- `matrix` — 基於 [mautrix-go](https://github.com/mautrix/go)，含 Olm/Megolm E2EE
- `mock` — 從設定檔讀一串假訊息，依時間表送出。**M1 階段先只做這個**，讓前端的隱晦呈現可以在完全不架 Matrix 的情況下開發與展示

### Store 介面

```go
// Store persists conversation state. All paths are under the daemon's
// XDG state directory with 0600 permissions.
type Store interface {
    AppendMessage(ctx context.Context, m model.Message) error
    RecentMessages(ctx context.Context, roomID string, limit int) ([]model.Message, error)
    UnreadCount(ctx context.Context, roomID string) (int, error)
    SetReadMarker(ctx context.Context, roomID, eventID string) error
    Close() error
}
```

兩個實作都在：記憶體版（每個 room 保留固定筆數，滿了丟最舊的），以及 SQLite 版
——單一檔案 `$XDG_STATE_HOME/quietdm/state.db`，權限 `0600`，預設就是它。未讀數由
已讀位置推導而非另存計數，這樣即使 `mark_read` 亂序抵達也不會出現負數。

訊息在資料庫裡是明文。隱晦模型保護的是訊息**在螢幕上的形狀**，不是它在磁碟上的
狀態；不想在磁碟留下聊天記錄的人請改用 `store = "memory"`。

**保留訊息歷史是必要的**：L2/L3 需要顯示上下文，而重新向 homeserver 拉取歷史會有延遲，延遲會逼使用者盯著螢幕等——那個「盯著等」的動作本身就很可疑。

### Session

Transport 與 IPC 之間的邏輯層：
- 套用聯絡人別名映射
- 呼叫 `sanitize` 過濾 emoji、把多媒體轉成佔位符
- 維護未讀計數
- 決定哪些事件該廣播給前端（例如自己送出的訊息也要回播，讓前端能顯示自己的回覆）

### IPC Hub

- 監聽 `$XDG_RUNTIME_DIR/quietdm/sock`，權限 `0600`
- 每個 client 一個 goroutine 讀、一個 goroutine 寫，寫入端有 buffered channel
- **慢速 client 不得阻塞 daemon**：buffer 滿了就丟棄該 client 最舊的事件並記錄，而非阻塞整個廣播迴圈
- client 斷線自動清理
- 協定細節見 [03-ipc-protocol.md](03-ipc-protocol.md)

## 四、Lua 端模組切分

```
plugin/
  quietdm.lua              # 建立使用者指令，不做任何自動啟動
lua/quietdm/
  init.lua                 # setup()、註冊 API、公開介面
  config.lua               # 預設設定與合併
  ipc.lua                  # vim.uv pipe 連線、NDJSON 編解碼、自動重連
  state.lua                # 記憶體中的訊息與未讀狀態
  level.lua                # L0-L3 曝光等級狀態機、自動降級計時器
  guard.lua                # 靜默、panic、filetype 白名單、FocusLost 處理
  registry.lua             # renderer / composer / notifier 註冊表
  renderers/
    blame.lua              # 預設，git blame 樣式虛擬文字
    diagnostic.lua         # 診斷樣式
    quickfix.lua           # L3 全景
    float.lua              # L2 hover 樣式
  composers/
    cmdline.lua            # 預設
    prompt.lua             # 浮動輸入框
    gitcommit.lua          # 假 commit message buffer
  notifiers/
    statusline.lua         # L0 暗號，提供給使用者的 statusline 取值
```

### 連線策略

前端用 `vim.uv.new_pipe()` 非阻塞連接 socket。連不上（daemon 沒跑）時**安靜失敗**——不顯示錯誤、不重試到干擾使用者，只在背景以遞增間隔（1s、2s、4s… 上限 30s）重試。

理由：一個「無法連線到聊天服務」的錯誤訊息跳在螢幕上，是這個工具能犯的最嚴重的錯誤。

### 執行緒安全

`vim.uv` callback 不在主執行緒的 API 安全區內，所有觸及 nvim API 的操作都必須經過 `vim.schedule()`。

## 五、部署形態

- daemon 以 systemd user service 執行（提供 unit 檔範本），`Restart=on-failure`
- 前端由任何 plugin manager 安裝此 repo；Lua 部分不需要編譯
- daemon 需使用者另行 `go install` 或下載 release binary
- 前端**不會**自動啟動 daemon：自動 spawn 一個背景程序是難以除錯的行為，而且會在使用者不預期時建立網路連線

## 六、設定檔

daemon：`$XDG_CONFIG_HOME/quietdm/config.toml`

```toml
[daemon]
store            = "sqlite"  # 或 "memory"（什麼都不落地）
history_capacity = 500       # 每個 room 保留的訊息數
# state_db = "..."           # 預設 $XDG_STATE_HOME/quietdm/state.db

[matrix]
homeserver   = "http://localhost:8008"
user_id      = "@me:localhost"
# 存取 token 從 $QUIETDM_TOKEN 環境變數或獨立的 0600 檔案讀取，不寫在設定檔裡

[aliases]
"@mia:localhost" = "m.chen"

[sanitize]
strip_emoji = true
max_body    = 8192   # 位元組上限，協定安全網
```

`max_body` 是為了守住 NDJSON 單行 64 KiB 的上限，**不是呈現用的截斷**：一則訊息
在畫面上能放多少，只有 renderer 知道（見 [03-ipc-protocol.md](03-ipc-protocol.md)
對 `body` 欄位的規定）。

前端設定透過 `require('quietdm').setup{}`，見 [04-plugin-api.md](04-plugin-api.md)。

## 七、安全考量

- 存取 token 不寫入設定檔，改由環境變數或獨立的 `0600` 檔案提供
- socket 與 state db 權限 `0600`，位於使用者專屬目錄
- E2EE 金鑰由 mautrix-go 的 crypto store 管理，與 state db 同目錄同權限
- daemon 只監聽 Unix socket，**不開任何 TCP port**
- 前端傳來的指令只能操作已知的 room，daemon 不接受任意 URL 或路徑參數
