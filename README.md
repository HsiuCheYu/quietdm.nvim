# quietdm.nvim

在 Neovim 裡收發即時訊息，而畫面在旁人眼中仍然是一個正常的編輯器。

> ⚠️ 目前僅有設計文件，尚無實作。

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
