# quietdm.nvim

在 Neovim 裡收發即時訊息，而畫面在旁人眼中仍然是一個正常的編輯器。

> ⚠️ M1（隱晦驗證）已實作：daemon 用 mock transport 送假訊息，前端做得完
> L0–L2 的收發。**還不能接 IG**——Matrix transport 是 M2 的事。

## 它長什麼樣子

有新訊息時，畫面上什麼都不會發生——只有 statusline 上一個既有符號換了狀態。

想看的時候，把游標停下來不動，該行右側浮出一段暗灰色文字：

```
    if err != nil {                          m.chen · 3 分鐘前 · 晚上要吃什麼
        return fmt.Errorf("parse: %w", err)
    }
```

看起來就是 git blame。游標一動就消失。

要回覆時敲 `:` 在 cmdline 打字送出——旁人看到的是有人在下 vim 指令。有人走過來，按 panic key，畫面上所有痕跡瞬間清空。

## 核心想法

**訊息內容藏不住，能藏的是形式與存在時間。**

使用者要能一眼讀懂訊息，內容就必須是明文。所以偽裝來自三條軸線：

1. **形式** — 長得像 git blame、lint 警告、LSP hover、quickfix 結果
2. **時間** — 預設不顯示，只在使用者主動注視時短暫出現
3. **版面** — 不開視窗、不捲動、不增減行數。餘光抓得到的是版面跳動，不是文字內容

完整說明見 [隱晦模型](docs/design/01-covert-model.md)。

## 架構

```
IG / Messenger → mautrix-meta → Matrix homeserver → quietdmd (Go) → nvim (Lua)
                  （自架）         （自架）           Unix socket
```

Homeserver 與 bridge 由使用者自架，訊息不經過第三方服務。

## 安裝

需要 Neovim 0.9+ 與 Go 1.24+（只有 daemon 需要 Go）。

前端用任何 plugin manager 裝這個 repo，例如 lazy.nvim：

```lua
{
  'HsiuCheYu/quietdm.nvim',
  config = function()
    require('quietdm').setup {
      -- panic 鍵預設不綁，請自己挑一個順手又不衝突的
      panic_key = '<C-\\>',
    }
  end,
}
```

`setup()` 不會連線，也不會自動啟動 daemon——一個外掛在你不知情時建立網路連線，
是不能接受的行為。

daemon 自己編：

```sh
git clone https://github.com/HsiuCheYu/quietdm.nvim
cd quietdm.nvim
make build          # 產生 ./quietdmd
```

## 先跑跑看（不需要 Matrix）

M1 的重點是驗證「隱晦」到底成不成立，所以整套東西可以完全離線跑：

```sh
make run-mock       # daemon + 內建假對話，走預設 socket 路徑
```

另一個終端機開 nvim，打開一個 `.go`（或其他白名單 filetype）的檔案：

```vim
:QuietdmStart
```

把游標停在某一行不要動。半秒後行尾會浮出 `m.chen · 3 分鐘前 · 晚上要吃什麼`，
游標一動就消失。`:QuietdmRead` 看最近幾則、`:QuietdmReply` 回覆、
`:QuietdmPanic` 清空並靜默。

socket 位於 `$XDG_RUNTIME_DIR/quietdm/sock`（權限 `0600`，目錄 `0700`），
前端預設就找這條路徑，不必額外設定。

## daemon 的設定

`$XDG_CONFIG_HOME/quietdm/config.toml`，沒有這個檔案也能跑（預設就是 mock 加上一段
內建假對話）。

```toml
[daemon]
store            = "sqlite"  # 預設。訊息記錄存在 $XDG_STATE_HOME/quietdm/state.db
history_capacity = 500       # 每個對話保留幾則
```

歷史記錄是必要的：L2 要顯示上下文，而每次都回頭向 homeserver 拉會有延遲，延遲會逼
你盯著螢幕等——那個「盯著等」的動作本身就很可疑。

資料庫是明文，權限 `0600`。這個工具偽裝的是訊息**在螢幕上的形狀**，不是它在磁碟上
的狀態。不想留下聊天記錄就設 `store = "memory"`，daemon 一關什麼都不剩。

## 指令

| 指令 | 作用 |
|---|---|
| `:QuietdmStart` / `:QuietdmStop` | 連線 / 斷線 |
| `:QuietdmRead` | 升到 L2（hover 樣式浮動視窗） |
| `:QuietdmPanorama` | 升到 L3（quickfix，M3 才有 renderer） |
| `:QuietdmReply` | 開啟 composer |
| `:QuietdmSilence [分鐘]` | 手動靜默（開會、螢幕分享前用） |
| `:QuietdmPanic` | 清空並靜默 |
| `:QuietdmDebug` | 在 scratch buffer 顯示 IPC 記錄 |

statusline 的暗號要自己插進去，外掛不會接管你的 statusline：

```lua
-- lualine
sections = { lualine_x = { function() return require('quietdm').token() end } }
```

## 開發

```sh
make test           # go test -race + nvim headless 前端測試 + 端對端測試
make test-go
make test-lua
make test-e2e       # 真的把 daemon 與 nvim 接起來跑完一輪
```

`tests/spec/invariants_spec.lua` 會掃描原始碼，確認沒有任何模組呼叫會寫入
buffer、插入虛擬行或彈出通知的 API——那些是[隱晦模型](docs/design/01-covert-model.md)
的硬性規則，不該靠人工審查來守。

## 文件

| 文件 | 內容 |
|---|---|
| [00-overview](docs/design/00-overview.md) | 使用情境、名詞定義、非目標 |
| [01-covert-model](docs/design/01-covert-model.md) | **隱晦模型**：三軸、L0–L3 曝光分級、不變式、panic |
| [02-architecture](docs/design/02-architecture.md) | 系統架構與模組切分 |
| [03-ipc-protocol](docs/design/03-ipc-protocol.md) | daemon 與前端的 NDJSON 協定 |
| [04-plugin-api](docs/design/04-plugin-api.md) | Renderer / Composer / Notifier 介面 |
| [05-roadmap](docs/design/05-roadmap.md) | 開發里程碑 |

## 它不做什麼

- 不對抗會認真讀你螢幕的人
- 不對抗公司的監控軟體與螢幕錄影
- 不隱藏 IG 端的行為（對方看到的仍是正常已讀）
- 不做圖片、語音、貼圖

本工具可能違反你所屬組織的規範。是否使用、在什麼場合使用，由使用者自行判斷與承擔。
