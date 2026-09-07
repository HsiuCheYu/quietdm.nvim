# quietdm.nvim

在 Neovim 裡收發即時訊息，而畫面在旁人眼中仍然是一個正常的編輯器。

> ⚠️ 程式碼到 M4 都寫完了：前端 L0–L3、Matrix transport（含 E2EE）、SQLite 記錄、
> systemd 與 docker-compose 範本、release binary。**但驗收都還沒做**——那需要真的用
> 一整天、真的跟一個人聊完一段完整對話，不是測試能代替的。自架那套 compose 也還沒
> 有人從頭跑過一遍。

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

需要 Neovim 0.9+ 與 Go 1.25+（只有 daemon 需要 Go）。

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

daemon 有三條路，挑一條：

```sh
# 一、下載 release binary（不需要 Go）
curl -LO https://github.com/HsiuCheYu/quietdm.nvim/releases/latest/download/quietdmd-linux-amd64
install -m 755 quietdmd-linux-amd64 ~/.local/bin/quietdmd

# 二、go install
go install -tags goolm github.com/HsiuCheYu/quietdm.nvim/cmd/quietdmd@latest

# 三、自己編
git clone https://github.com/HsiuCheYu/quietdm.nvim
cd quietdm.nvim && make build      # 產生 ./quietdmd
```

**`-tags goolm` 不能漏掉。** mautrix-go 預設連 libolm（一個 C 函式庫），少了這個
tag，`go build` 會抱怨找不到 `olm/olm.h`。加上之後用的是純 Go 的實作，daemon 就是
一個不需要 cgo 的單一執行檔。`make build` 已經帶了。

## 先跑跑看（不需要 Matrix）

想先確認隱晦模型對自己成不成立，整套東西可以完全離線跑：

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

## 接上真的 IG

一句話版本，沒有 homeserver 也沒關係：

```sh
quietdmd setup
```

它會把下面兩份文件裡除了「貼上 IG 憑證」以外的每一步都做完：架
Synapse 與 mautrix-instagram、建內部用的 Matrix 帳號、拿 access
token、寫 `config.toml`。跑到跟 bridge bot 對話那一步時，它會把 bot
的回覆印在終端機，照著貼就好——不用再另外開 Element。

想知道它做了什麼、或它卡住時要怎麼自己動手，兩份文件照順序走：

1. **[docs/self-host.md](docs/self-host.md)** — 用 `contrib/docker/compose.yaml`
   把 Synapse 與 mautrix-instagram 架起來。沒有 federation、沒有 TLS、沒有對外的
   port：唯一的 client 是同一台機器上的 daemon。
2. **[docs/matrix-setup.md](docs/matrix-setup.md)** — 取得 access token、把它放在哪
   裡、E2EE 的 device 在別的 client 眼中為什麼是未驗證的。

已經有 homeserver 的話，最短版本：

```sh
export QUIETDM_TOKEN=syt_...
quietdmd -config examples/matrix.toml -v
```

長期跑用 systemd user service，範本在
[`contrib/systemd/quietdmd.service`](contrib/systemd/quietdmd.service)。

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
| `:QuietdmPanorama` | 升到 L3（quickfix 列表） |
| `:QuietdmReply` | 開啟 composer |
| `:QuietdmSilence [分鐘]` | 手動靜默（開會、螢幕分享前用） |
| `:QuietdmPanic` | 清空並靜默 |
| `:QuietdmDebug` | 在 scratch buffer 顯示 IPC 記錄 |

statusline 的暗號要自己插進去，外掛不會接管你的 statusline：

```lua
-- lualine
sections = { lualine_x = { function() return require('quietdm').token() end } }
```

## 換一套偽裝

哪一種偽裝比較安靜，取決於你的畫面本來就有什麼。預設是 `blame`——如果你沒有
gitsigns 但有一個很吵的 language server，`diagnostic` 反而更不起眼：

```lua
require('quietdm').setup {
  renderers = { glance = 'diagnostic', read = 'float', panorama = 'quickfix' },
}
```

| renderer | 等級 | 長相 |
|---|---|---|
| `blame` | L1 | 游標行右對齊：`m.chen · 3 分鐘前 · 晚上要吃什麼` |
| `diagnostic` | L1 | 行尾提示：`■ m.chen: 晚上要吃什麼` |
| `float` | L2 | LSP hover 樣式浮動視窗 |
| `quickfix` | L3 | `internal/mia/mia.go|142 col 3| m.chen: 晚上要吃什麼` |

L3 的檔名與行號都是道具，能對它們動作的按鍵在那個 buffer 裡都被綁成空的——不然
`<CR>` 會把你丟進一個不存在的檔案。

composer 也可以換：

| composer | 說明 |
|---|---|
| `cmdline` | **預設。** 在 cmdline 打字，提示符偽裝成替換指令 |
| `prompt` | LSP rename 樣式的單行浮動輸入框 |
| `gitcommit` | `filetype=gitcommit` 的 scratch buffer，`:w` 送出、關掉視窗取消。長回覆用 |

自己寫一個也可以，介面見 [04-plugin-api](docs/design/04-plugin-api.md)。

## 開發

```sh
make test           # go test -race + nvim headless 前端測試 + 端對端測試
make test-go
make test-lua
make test-e2e       # 真的把 daemon 與 nvim 接起來跑完一輪
```

不變式有兩道檢查，都不靠人工審查：`tests/spec/invariants_spec.lua` 掃描原始碼，確認
沒有任何模組**寫得出**會改 buffer、插虛擬行或彈通知的呼叫；
`tests/spec/runtime_invariants_spec.lua` 則在 API 上裝攔截器，把 L1→L3、回覆、panic
整條流程跑一遍，確認沒有任何模組**真的走到**那裡——包裝過一層、查表叫出來、或交給
別的函式庫去做的，靜態掃描是看不到的。

## 文件

| 文件 | 內容 |
|---|---|
| [00-overview](docs/design/00-overview.md) | 使用情境、名詞定義、非目標 |
| [01-covert-model](docs/design/01-covert-model.md) | **隱晦模型**：三軸、L0–L3 曝光分級、不變式、panic |
| [02-architecture](docs/design/02-architecture.md) | 系統架構與模組切分 |
| [03-ipc-protocol](docs/design/03-ipc-protocol.md) | daemon 與前端的 NDJSON 協定 |
| [04-plugin-api](docs/design/04-plugin-api.md) | Renderer / Composer / Notifier 介面 |
| [05-roadmap](docs/design/05-roadmap.md) | 開發里程碑 |
| [self-host](docs/self-host.md) | 自架 Synapse 與 IG bridge 的逐步教學 |
| [matrix-setup](docs/matrix-setup.md) | 接上 homeserver、token、E2EE 與已知限制 |
| [threat-model](docs/threat-model.md) | **它不防什麼**，以及明文放在磁碟的哪裡 |

## 它不做什麼

quietdm 防的是**餘光**，不是**注意力**。

- 不對抗會認真讀你螢幕的人——訊息是明文，就在那裡
- 不對抗公司的監控軟體與螢幕錄影——畫面上有明文，錄影裡就有明文
- 不隱藏 IG 端的行為（對方看到的仍是正常已讀）
- 不做圖片、語音、貼圖
- 訊息記錄以明文存在磁碟上（`0600`，可以關掉）

完整版本、以及最可能害你出事的四種失敗方式，見
[威脅模型](docs/threat-model.md)。

本工具可能違反你所屬組織的規範。是否使用、在什麼場合使用，由使用者自行判斷與承擔。
