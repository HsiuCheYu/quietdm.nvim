# 接上 Matrix

這份文件講的是怎麼把 daemon 接到一個**已經存在**的 homeserver。自架 homeserver
與 mautrix-meta bridge 的 `docker-compose` 範本是 M4 的事（見
[05-roadmap.md](design/05-roadmap.md)）。

架構上 daemon 只認得 Matrix：

```
IG / Messenger → mautrix-meta → Matrix homeserver → quietdmd → nvim
                  （自架）         （自架）
```

如果你是用 `quietdmd setup` 自架的（見 [self-host.md](self-host.md) 的
「一句話版本」），這份文件的第一、二節（拿 token、放對地方）它已經幫你做完
了，`config.toml` 跟 token 檔也都寫好了——直接跳到
[三、設定檔](#三設定檔) 看它寫出來的長什麼樣子即可。這份文件仍然完整保留，
是給「接的是別人已經架好的 homeserver」或想手動除錯的人看的。

## 一、取得 access token

daemon 需要一個屬於**它自己的 device** 的 token。不要沿用你手機或 Element 上那個
device——共用 device 表示兩邊的 Olm session 會互相踩。

```sh
curl -XPOST https://your.homeserver/_matrix/client/v3/login \
  -H 'Content-Type: application/json' \
  -d '{
        "type": "m.login.password",
        "identifier": {"type": "m.id.user", "user": "me"},
        "password": "...",
        "initial_device_display_name": "quietdm"
      }'
```

回應裡的 `access_token` 就是要的東西，`device_id` 可以記下來（不記也行，daemon
會自己問 homeserver）。

## 二、把 token 放對地方

**token 不進設定檔。** 設定檔會被複製進 dotfiles repo、貼進 issue、被人從肩膀後面
看到；token 是這裡唯一一個直接把整個帳號交出去的東西。

兩種來源，環境變數優先：

```sh
export QUIETDM_TOKEN=syt_...
```

或一個 `0600` 的檔案：

```sh
install -m 600 /dev/null ~/.config/quietdm/token
printf '%s' 'syt_...' > ~/.config/quietdm/token
```

```toml
[matrix]
token_file = "/home/you/.config/quietdm/token"
```

只要 group 或 other 讀得到，daemon 就直接拒絕啟動。一個全機器可讀的 token 檔不比
沒有 token 好，默默照跑只會教你權限無所謂。

## 三、設定檔

`$XDG_CONFIG_HOME/quietdm/config.toml`，完整範例見
[`examples/matrix.toml`](../examples/matrix.toml)：

```toml
[transport]
kind = "matrix"

[matrix]
homeserver = "https://your.homeserver"
user_id    = "@me:your.homeserver"
encrypt    = true   # 預設值

[aliases]
"@instagram_1234567:your.homeserver" = "m.chen"
```

`[aliases]` 值得花點時間填。bridge 給的 user ID 通常長成
`@instagram_1234567:...`，在 blame 文字裡一眼就很怪；映射成一個看起來像 git
帳號的名字才是重點。

## 四、跑起來

前景試跑：

```sh
quietdmd -v
```

長期跑用 systemd user service，範本在
[`contrib/systemd/quietdmd.service`](../contrib/systemd/quietdmd.service)：

```sh
mkdir -p ~/.config/systemd/user
cp contrib/systemd/quietdmd.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now quietdmd
```

token 放在 `~/.config/quietdm/env`（`0600`）裡：

```
QUIETDM_TOKEN=syt_...
```

## 五、E2EE 與 device 驗證

daemon 第一次連上時會用 token 對應的那個 device 建立 Olm 身分，並把 device key 與
one-time key 上傳到 homeserver。金鑰存在
`$XDG_STATE_HOME/quietdm/matrix.db`（`0600`），加密它的 pickle key 存在同一個目錄的
`pickle.key`（也是 `0600`，第一次執行時隨機產生）。

**誠實說：pickle key 就放在它保護的資料庫旁邊。** 這擋不住任何能讀你 state 目錄
的人，它擋的是「只有那個 .db 檔外流」的情況（例如備份工具只抓了資料庫）。

### 這個 device 在別的 client 眼中是未驗證的

quietdm 目前**沒有互動式的 SAS 驗證流程**（emoji 對照那種）。實際影響：

- 你在 Element 上會看到多一個未驗證的 session，並且會被提示。這是預期的。
- **quietdm 上線之後**對方送出的訊息，正常情況下讀得到——送訊端會把 Megolm
  session key 送給收訊帳號的所有 device。
- **quietdm 上線之前**的歷史訊息大多讀不到。金鑰當時沒有送給這個 device，之後也
  不會回頭補。這不是 bug，是 Megolm 的設計。
- 如果你或對方的 client 開了「只傳給已驗證的裝置」，quietdm 會收不到金鑰，那個房間
  就整個是空的。

想避開最後一項，就在 Element 裡把這個 session 標記為已驗證（cross-signing 需要
互動式驗證，quietdm 這端還沒實作，所以只能從另一端手動處理）。

### 不加密的選項

`encrypt = false` 可以完全關掉加密層。這幾乎沒有用：bridge 開出來的房間都是加密
的，關掉之後 daemon 一則訊息都讀不到。它存在的理由是為了在沒有加密的測試房間裡
除錯。

## 六、已知限制

- **群組房間效果差。** 一行 blame 文字放得下一個人名加一句話，放不下三個人輪流
  講。設計上就是為一對一最佳化的。
- **不做圖片、語音、貼圖。** 它們會變成 `[圖片]` `[語音]` `[貼圖]` 佔位符。一張圖
  沒辦法偽裝成程式碼。
- **對方看到的是正常的已讀。** `:QuietdmRead` 會送出已讀回條，這是刻意的——一個
  永遠不已讀的帳號本身就很可疑。
- **訊息在 `state.db` 裡是明文。** 這個工具偽裝的是訊息在螢幕上的形狀，不是它在
  磁碟上的狀態。不想留下記錄就設 `[daemon] store = "memory"`。
- **不對抗會認真讀你螢幕的人，也不對抗公司的監控軟體。** 見
  [01-covert-model.md](design/01-covert-model.md)。
