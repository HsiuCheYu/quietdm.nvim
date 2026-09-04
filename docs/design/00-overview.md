# 00 — 專案總覽

## 這是什麼

`quietdm` 讓工程師在 Neovim 裡收發即時訊息（IG / Messenger 為主），而畫面在旁人眼中仍然是一個正常的編輯器。

它不是一個「聊天室外掛」。市面上的 nvim 聊天外掛（IRC、Slack client）都會開一個明顯的聊天視窗——那正是本專案要避免的東西。quietdm 的整個設計目標是**讓聊天不佔用任何看起來像聊天的畫面空間**。

## 使用情境

工程師在辦公室用 nvim 打開專案工作。

1. 朋友傳訊息進來。畫面上唯一的變化是 statusline 上一個既有符號換了狀態——沒有彈窗、沒有聲音、沒有新視窗。
2. 工程師想看，於是把游標停在某一行不動。約半秒後，該行右側浮出一段暗灰色文字：`mia · 3 分鐘前 · 晚上要吃什麼`。看起來就是 git blame。
3. 游標一動，文字消失。
4. 工程師按下 `<leader>` 開頭的一組鍵，一個浮動視窗出現，樣式與 LSP hover 完全相同，裡面是最近幾則對話。
5. 回覆時他敲 `:`，在 cmdline 打字送出——旁人看到的是有人在下 vim 指令。
6. 主管走過來。工程師按 panic key，畫面上所有痕跡瞬間清空，並靜默十分鐘。

整個過程沒有任何一刻，畫面上存在「一個聊天軟體」。

## 系統組成

```
IG / Messenger
      │
      ▼
mautrix-meta（bridge，使用者自架）
      │
      ▼
Matrix homeserver（使用者自架，如 Conduit / Dendrite）
      │
      ▼
quietdmd（Go 常駐程式，本專案）
      │  Unix domain socket + NDJSON
      ▼
quietdm.nvim（Lua 前端，本專案）
```

使用者自架 homeserver 與 bridge，代表訊息不經過第三方服務；quietdm 只跟本機的 homeserver 講話。

## 名詞定義

| 名詞 | 意義 |
|---|---|
| **daemon** | `quietdmd`，常駐的 Go 程式。負責 Matrix 連線、E2EE、狀態儲存 |
| **前端** | `quietdm.nvim`，Neovim 的 Lua 外掛。負責呈現與輸入 |
| **Renderer** | 一種「把訊息畫到畫面上」的方式。git blame 樣式、診斷樣式、quickfix 樣式各是一個 renderer |
| **Composer** | 一種「輸入回覆」的方式。cmdline 輸入、浮動輸入框各是一個 composer |
| **Notifier** | L0 等級下傳達「有新訊息」的暗號機制 |
| **曝光等級（L0–L3）** | 訊息在畫面上的可見程度。詳見 [01-covert-model.md](01-covert-model.md) |
| **panic** | 一鍵清空所有畫面痕跡並進入靜默 |

## 設計原則

1. **形式偽裝優先於內容加密。** 使用者要能一眼讀懂訊息，內容就必須是明文。偽裝來自「它長得像編輯器本來就會產生的東西」。
2. **不存在勝過偽裝得好。** 最安全的畫面是空的。訊息預設不顯示，只在使用者主動索取時短暫出現。
3. **版面永遠不動。** 餘光抓得到的是版面跳動，不是文字內容。
4. **絕不污染使用者的檔案。** 訊息永遠不進入 buffer 文字，因此不可能被存檔、不可能進 git。
5. **一切可插拔。** Renderer、Composer、Notifier、Transport、Store 都是介面。使用者的工作環境差異很大，預設值不可能適合所有人。

## 非目標

- **不對抗會認真讀你螢幕的人。** 有人站在你背後逐字讀，任何形式偽裝都會失效。
- **不對抗公司的監控軟體 / MDM。** 螢幕錄影、鍵盤側錄、網路流量分析都不在防禦範圍內。
- **不隱藏 IG 端的行為。** 對方看到的仍是正常的已讀與回覆。
- **不做群組聊天的完整體驗。** 多人對話在單行 blame 裡呈現效果很差，初期只支援一對一與極簡群組。
- **不做多媒體。** 圖片、語音、貼圖在程式碼畫面上無法偽裝，一律只顯示佔位文字（如 `[圖片]`）。

## 相關文件

- [01-covert-model.md](01-covert-model.md) — **隱晦模型（核心）**
- [02-architecture.md](02-architecture.md) — 系統架構
- [03-ipc-protocol.md](03-ipc-protocol.md) — daemon 與前端的通訊協定
- [04-plugin-api.md](04-plugin-api.md) — Lua 外掛介面
- [05-roadmap.md](05-roadmap.md) — 開發里程碑
