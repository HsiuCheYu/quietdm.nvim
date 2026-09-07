# 自架 homeserver 與 IG bridge

這份文件把一台空機器變成「quietdm 可以連的東西」。做完之後接上 daemon 的步驟在
[matrix-setup.md](matrix-setup.md)。

## 一句話版本

```sh
quietdmd setup
```

`quietdmd setup` 做的事跟底下的手動步驟完全一樣，只是自動化：它會架
Postgres、Synapse、bridge，建立內部用的 Matrix 帳號，跑完 bridge 的三段式起
法，最後跟 `@instagrambot` 開房間、把 `login` 送出去、把 bot 的回覆即時印在
終端機讓你照著貼。唯一還是要你自己來的，就是貼 IG 憑證那一步。

底下的逐步說明留著，是為了：(a) 想知道 `quietdmd setup` 到底做了什麼，或
(b) 它某個階段失敗了，需要照著對應的段落手動接手繼續。每個階段失敗時的錯誤
訊息都會指到這份文件的對應段落。

```
IG → mautrix-instagram → Synapse → quietdmd → nvim
     （這份文件）        （這份文件）  （matrix-setup.md）
```

> ⚠️ 這套 compose 是我寫出來的，但**沒有真的從頭跑過一遍**——沒有 IG 帳號可以拿來
> 測。每個指令與設定欄位都對照過 Synapse 與 mautrix-meta 上游的原始碼與官方
> Dockerfile，但順序或權限上仍可能有需要你調整的地方。遇到卡住的地方，
> [docs.mau.fi](https://docs.mau.fi/bridges/general/docker-setup.html?bridge=meta)
> 是 bridge 那一半的權威文件。

## 這套東西不是什麼

**它不是一個 Matrix 伺服器部署。** 沒有 federation、沒有 TLS、沒有反向代理、沒有對
外的 port。唯一的 client 是同一台機器上的 `quietdmd`，socket 綁在 `127.0.0.1`。

這是刻意的：一個對外開放的 homeserver 是一個要維護的東西，而這個專案不需要它。代價
是你沒辦法從手機或別台機器連這個帳號——要的話請去讀 Synapse 自己的部署文件，那超出
這份文件的範圍。

## 〇、需要的東西

- Docker 與 Docker Compose
- 一個 Instagram 帳號
- 大約 1 GB 的磁碟

以下所有指令都在 `contrib/docker/` 底下執行。

```sh
cd contrib/docker
cp .env.example .env
$EDITOR .env          # 填一個 POSTGRES_PASSWORD
mkdir -p data/synapse data/bridge data/postgres
```

`data/` 與 `.env` 都在 `.gitignore` 裡。整套東西的狀態就只有這個目錄。

## 一、產生 Synapse 設定

`SYNAPSE_SERVER_NAME` 會變成你 MXID 的網域部分，之後**改不掉**。因為不 federate，
名字取什麼都行；`quietdm.local` 這種就可以。

```sh
docker compose run --rm \
  -e SYNAPSE_SERVER_NAME=quietdm.local \
  -e SYNAPSE_REPORT_STATS=no \
  synapse generate
```

編輯 `data/synapse/homeserver.yaml`：

```yaml
# 換掉預設的 sqlite
database:
  name: psycopg2
  args:
    user: synapse
    password: 你在 .env 填的那個
    database: synapse
    host: postgres
    cp_min: 5
    cp_max: 10

# bridge 等一下會把註冊檔寫進這裡（compose 已經把它唯讀掛進來）
app_service_config_files:
  - /appservices/registration.yaml

# 只有你一個人用
enable_registration: false
```

```sh
docker compose up -d postgres synapse
docker compose logs -f synapse      # 等它說 listening
```

## 二、開你的帳號

```sh
docker compose exec synapse register_new_matrix_user \
  -c /data/homeserver.yaml -u me -a http://localhost:8008
```

`-a` 是 admin。你的 MXID 會是 `@me:quietdm.local`。

## 三、產生 bridge 設定與註冊檔

mautrix 的容器是**分三次**跑起來的：第一次產生設定檔就結束，第二次產生註冊檔就結
束，第三次才真的執行。這是它 `docker-run.sh` 的設計，不是壞掉。

```sh
docker compose up bridge      # 第一次：產生 data/bridge/config.yaml 後結束
```

編輯 `data/bridge/config.yaml`：

```yaml
homeserver:
  address: http://synapse:8008
  domain: quietdm.local

appservice:
  address: http://bridge:29330
  hostname: 0.0.0.0        # 容器裡一定要改，預設的 127.0.0.1 連不進來
  port: 29330

database:
  type: sqlite3-fk-wal
  uri: file:/data/mautrix-instagram.db?_txlock=immediate

bridge:
  permissions:
    "@me:quietdm.local": admin

encryption:
  allow: true
  default: true
```

`encryption` 那三行要打開。quietdm 這端預設就是加密的，兩邊都開才對得起來。

```sh
docker compose up bridge      # 第二次：產生 data/bridge/registration.yaml 後結束
docker compose restart synapse   # 讓 Synapse 讀到註冊檔
docker compose up -d bridge      # 第三次：真的跑起來
```

## 四、登入 Instagram

bridge 的登入是在 Matrix 房間裡用指令完成的，所以你需要一個 Matrix client。用
Element 連 `http://localhost:8008`，登入 `@me:quietdm.local`。

跟 `@instagrambot:quietdm.local` 開一個私訊，然後：

```
login
```

照它的指示走完（目前是貼 IG 的 cookie）。成功之後 bridge 會把你的 IG 對話一個一個
變成 Matrix 房間。

**確認到這裡都是好的**：在 Element 裡收得到、送得出去。這件事跟 quietdm 沒關係，先
確定它成立，再往下走——不然之後出問題你會分不清是哪一半壞了。

## 五、把 quietdm 接上去

到這裡 homeserver 就是一台普通的 homeserver 了。取 access token、設定 daemon、
E2EE 的注意事項，都在 [matrix-setup.md](matrix-setup.md)。

一句話版本：

```toml
[transport]
kind = "matrix"

[matrix]
homeserver = "http://localhost:8008"
user_id    = "@me:quietdm.local"
```

## 六、維護

```sh
docker compose logs -f bridge     # IG 那一半出問題時第一個要看的地方
docker compose pull && docker compose up -d
```

備份就是備份 `data/`。裡面有你的訊息、bridge 的 IG session、以及 Synapse 的簽章
金鑰——換句話說，**那個目錄就是整個帳號**。

砍掉重來：

```sh
docker compose down
rm -rf data/
```

## 已知會卡住的地方

- **Synapse 抱怨 collation**：Postgres 的 locale 必須是 `C`。compose 裡的
  `POSTGRES_INITDB_ARGS` 已經處理了，但那只在**第一次**建立資料目錄時生效。如果你
  是在既有的 `data/postgres` 上撞到這個，要砍掉重建。
- **bridge 起來但 Synapse 說 appservice 不存在**：註冊檔產生後沒有重啟 Synapse。
- **bridge 連不上 homeserver**：`appservice.hostname` 還是 `127.0.0.1`。在容器裡那
  是它自己，Synapse 連不到。
- **房間是空的、訊息讀不到**：兩邊的加密設定沒對上，或者 quietdm 的 device 沒有拿
  到金鑰。見 [matrix-setup.md](matrix-setup.md#五e2ee-與-device-驗證)。
