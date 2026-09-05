# 05 — 開發里程碑

排序原則：**最不確定的東西最先做。**

本專案最大的風險不是 Matrix 串接（那是已知的工程問題，mautrix-go 都處理好了），而是**「隱晦」到底成不成立**——blame 樣式的訊息在真實工作環境中，是真的不引人注意，還是其實一眼就看得出來？這件事只能實際做出來、實際用一天才知道。

因此 M1 完全不碰 Matrix。

## M1 — 隱晦驗證（不碰 Matrix）　*實作完成，驗收未做*

**目標：能用假資料完整體驗一次收發流程，據此判斷隱晦模型是否成立。**

- daemon 骨架：`cmd/quietdmd`、設定載入、生命週期
- `mock` transport：從 TOML 讀一串假訊息，依時間表送出（模擬朋友陸續傳訊）
- IPC server：Unix socket、NDJSON、`hello` / `send` / `history` / `rooms` 與對應 event
- 記憶體版 `Store`（先不做 SQLite）
- 前端：`ipc.lua` 連線與重連、`state.lua`、`level.lua` 的 L0/L1/L2
- `blame` renderer（預設）、`float` renderer
- `cmdline` composer
- `guard.lua`：filetype 白名單、`FocusLost`、panic、靜默
- `statusline` notifier

程式碼已到位（`make test` 全綠，含一個把 daemon 與前端接起來跑完整流程的端對端
測試），但**驗收還沒做**——那需要一整天的真實使用，不是測試能代替的。

**驗收：** 在真實工作環境中用一整天。自問三個問題——
1. 有沒有任何一刻，我擔心旁邊的人看到了？
2. 有沒有任何一刻，我想看訊息卻覺得叫出來太麻煩？
3. blame 文字出現的瞬間，我自己的餘光有沒有被吸引？（如果連自己都會被吸引，旁人一定也會）

**這一步的結論可能會推翻 01 文件的設計。** 那是預期中的事，也是把它排在第一位的原因。

## M2 — 真的能聊天　*實作完成，驗收未做*

- `matrix` transport：mautrix-go、`/sync`、送訊、已讀回條
- E2EE：Olm/Megolm、crypto store、device 驗證流程文件（[matrix-setup.md](../matrix-setup.md)）
- SQLite `Store`
- daemon 斷線重連與 `connected: false` 語意
- systemd user unit 範本

`sanitize` 的三項（emoji 過濾、多媒體佔位符、聯絡人別名）在 M1 就一起做掉了。

E2EE 少了一件事：**互動式的 device 驗證（SAS）沒有實作**。daemon 的 device 在別的
client 眼中是未驗證的，實際影響寫在 [matrix-setup.md](../matrix-setup.md#五e2ee-與-device-驗證)。
補不補要看 M2 驗收時它到底礙不礙事——如果沒人開「只傳給已驗證裝置」，它就只是
Element 上一則提示。

**驗收：** 與一個真人用 IG 聊完一段完整對話，全程只用 nvim。這需要先有一台自架的
homeserver 與 bridge，而那個的安裝教學是 M4——所以 M2 的驗收實際上會跟 M4 一起做。

## M3 — 可插拔與其餘通道

M1 為了讓端對端流程跑得完整，已經先做掉了幾項：`registry.lua` 的註冊與驗證、
idle timer 與 `VimResized` 自動降級、`:QuietdmDebug` 與 IPC 環形記錄、
以及不變式的靜態檢查（`tests/spec/invariants_spec.lua`）。剩下的是：

- ~~`diagnostic` renderer、`quickfix` renderer（L3）~~ 已完成
- `prompt` composer、`gitcommit` composer
- 不變式的動態檢查（執行期攔截被禁止的 API 呼叫，而非只掃原始碼）

## M4 — 讓別人裝得起來

- mautrix-meta + homeserver 的 `docker-compose` 範本與逐步教學
- daemon 的 release binary 與 `go install` 說明
- README 的完整安裝流程
- 已知限制與威脅模型的使用者說明（誠實寫出不防什麼）

## 之後可能做的

- 多聯絡人的呈現策略（目前設計對一對一最佳，群組在單行 blame 中效果差）
- 依訊息時間調整呈現強度（深夜的訊息可以更隱晦一點，因為辦公室沒人）
- 更多 transport 實作（Signal、Telegram 也有 mautrix bridge，Transport 介面已預留）

## 不打算做的

- 偵測並規避公司監控軟體
- 圖片 / 語音 / 貼圖的呈現
- 內容層的編碼隱寫（把訊息藏進變數名稱）——閱讀成本高到工具失去意義，見 [01-covert-model.md](01-covert-model.md#一問題重述)
