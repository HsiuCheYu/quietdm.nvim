# 03 — IPC 協定

daemon 與 Neovim 前端之間的通訊協定。

## 一、傳輸層

| 項目 | 值 |
|---|---|
| 通道 | Unix domain socket（SOCK_STREAM） |
| 路徑 | `$XDG_RUNTIME_DIR/quietdm/sock`，未設定時退回 `/run/user/$UID/quietdm/sock` |
| 權限 | socket `0600`、目錄 `0700` |
| 編碼 | NDJSON——每則訊息一行 UTF-8 JSON，以 `\n` 結尾，訊息內部不得含裸換行 |
| 方向 | 全雙工。client 送 **command**，daemon 送 **event** |
| 上限 | 單行 64 KiB。超過即視為協定錯誤並斷線 |

選 NDJSON 而非 msgpack 或 gRPC 的理由：Lua 端用 `vim.json` 就能處理，無額外相依；除錯時 `socat` 直接看得懂。訊息量極小（一則聊天訊息），效能不是考量點。

## 二、共同欄位

每則 JSON 物件都有：

| 欄位 | 型別 | 說明 |
|---|---|---|
| `t` | string | 訊息型別（見下方各表） |
| `id` | string | 選填。client 送出時帶上，daemon 在對應的 `ack` / `error` 中回填，用於配對請求與回應 |

## 三、Command（client → daemon）

### `hello`

連線後的第一則訊息。未收到 `hello` 前，daemon 不送任何 event。

```json
{"t":"hello","id":"1","proto":1,"client":"quietdm.nvim/0.1.0"}
```

| 欄位 | 型別 | 說明 |
|---|---|---|
| `proto` | int | 協定版本，目前為 `1`。版本不符時 daemon 回 `error` 並斷線 |
| `client` | string | 用於 daemon 記錄，無功能性用途 |

### `send`

送出訊息。

```json
{"t":"send","id":"2","room":"!abc:localhost","body":"七點拉麵店見"}
```

| 欄位 | 型別 | 說明 |
|---|---|---|
| `room` | string | room ID。必須是 daemon 已知的 room，否則回 `error` |
| `body` | string | 訊息內容，純文字 |

daemon 成功送出後回 `ack`，並在稍後透過正常的 `message` event 回播該訊息（讓所有前端都能顯示自己的回覆）。

### `mark_read`

```json
{"t":"mark_read","id":"3","room":"!abc:localhost","event":"$xyz"}
```

推進已讀位置。`event` 省略時代表標記該 room 全部已讀。daemon 會廣播更新後的 `room` event 給所有 client。

### `history`

```json
{"t":"history","id":"4","room":"!abc:localhost","limit":20}
```

要求最近 N 則訊息，供 L2/L3 呈現。daemon 以一則 `history` event 回覆（不是多則 `message`，避免與新訊息混淆）。`limit` 預設 20、上限 200。

### `rooms`

```json
{"t":"rooms","id":"5"}
```

要求目前的對話清單。daemon 以 `rooms` event 回覆。

## 四、Event（daemon → client）

### `ready`

`hello` 之後的第一則 event。

```json
{"t":"ready","id":"1","proto":1,"daemon":"quietdmd/0.1.0","connected":true}
```

| 欄位 | 型別 | 說明 |
|---|---|---|
| `connected` | bool | daemon 目前是否與 homeserver 連線正常。`false` 時前端**不得顯示任何錯誤**，僅記錄 |

### `message`

新訊息。這是唯一會主動推送的 event。

```json
{
  "t": "message",
  "room": "!abc:localhost",
  "event": "$xyz",
  "sender": "@mia:localhost",
  "display": "m.chen",
  "body": "晚上要吃什麼",
  "ts": 1757000000,
  "own": false,
  "kind": "text"
}
```

| 欄位 | 型別 | 說明 |
|---|---|---|
| `event` | string | 事件 ID，去重與已讀標記用 |
| `sender` | string | 原始 Matrix user ID |
| `display` | string | 套用別名後的顯示名。**前端一律使用此欄位呈現**，不得自行處理 `sender` |
| `body` | string | 已由 daemon 過濾 emoji 的純文字。**未經截斷**——截斷是呈現層的決定，因為不同 renderer 的可用寬度不同 |
| `ts` | int | Unix 秒 |
| `own` | bool | 是否為自己送出的訊息 |
| `kind` | string | `text` / `image` / `audio` / `sticker` / `other`。非 `text` 時 `body` 是佔位符如 `[圖片]` |

### `room`

room 狀態變更（未讀數改變時推送）。

```json
{"t":"room","room":"!abc:localhost","display":"m.chen","unread":2,"last_ts":1757000000}
```

### `rooms`

對 `rooms` command 的回覆。

```json
{"t":"rooms","id":"5","rooms":[{"room":"!abc:localhost","display":"m.chen","unread":2,"last_ts":1757000000}]}
```

`rooms` 陣列的元素結構與 `room` event 的欄位一致（不含 `t`）。

### `history`

對 `history` command 的回覆。

```json
{"t":"history","id":"4","room":"!abc:localhost","messages":[ /* message 物件，不含 t，時間由舊到新 */ ]}
```

### `ack`

指令成功。

```json
{"t":"ack","id":"2","event":"$xyz"}
```

`event` 僅在 `send` 的 ack 中出現。

### `error`

指令失敗，或協定層錯誤。

```json
{"t":"error","id":"2","code":"unknown_room","msg":"room not found"}
```

| `code` | 意義 |
|---|---|
| `bad_proto` | 協定版本不符，daemon 將斷線 |
| `bad_request` | JSON 格式或欄位錯誤 |
| `unknown_room` | room ID 不存在 |
| `offline` | daemon 與 homeserver 斷線，指令無法完成 |
| `internal` | daemon 內部錯誤 |

**前端處理 `error` 的規則：** 不得使用 `vim.notify` 或任何會出現在畫面上的方式顯示。只寫入外掛內部的環形記錄，由 `:QuietdmDebug` 查看。唯一例外是 `send` 失敗——使用者需要知道訊息沒送出去，但呈現方式必須符合不變式 I3（見 [01-covert-model.md](01-covert-model.md#五不變式硬性規則)），例如一則 `DiagnosticHint` 樣式的虛擬文字。

## 五、多 client 語意

- daemon 對所有已完成 `hello` 的 client **廣播** `message` 與 `room` event
- `ack`、`error`、`history`、`rooms`、`ready` 只送給發起的那個 client
- 去重責任在前端：以 `event` ID 為鍵。同一個 nvim 收到重複 ID 的訊息時只呈現一次
- 已讀狀態是全域的：任一 client 送 `mark_read`，所有 client 都會收到更新後的 `room` event

## 六、連線與重連

### 前端

- 啟動時嘗試連線；失敗則以遞增間隔重試（1s、2s、4s、8s、16s、30s 上限）
- **重試過程完全靜默**，不顯示任何訊息
- 重連成功後送 `hello`，接著送 `rooms` 重建狀態
- 前端不保存跨 session 的訊息狀態，重連即從 daemon 重新取得

### daemon

- daemon 與 homeserver 斷線時**不斷開** client 連線，只在 `ready` 與後續狀態中標記 `connected: false`
- 重連期間收到 `send` 指令，回 `error` 且 `code` 為 `offline`
- daemon 啟動時若 socket 檔已存在但無程序監聽（前次未正常關閉），移除後重建

## 七、除錯

```sh
# 觀察 daemon 推送的事件
socat - UNIX-CONNECT:$XDG_RUNTIME_DIR/quietdm/sock
# 貼上：{"t":"hello","id":"1","proto":1,"client":"socat"}
```

前端提供 `:QuietdmDebug` 顯示最近的 IPC 記錄（在一個 scratch buffer 中，符合不變式 I1/I4）。
