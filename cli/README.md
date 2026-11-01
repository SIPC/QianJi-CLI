# qianji CLI

钱迹 CLI：一个用 Go 写的命令行工具，用你**自己的账号**访问钱迹（钱迹记账 / `com.mutangtech.qianji`）数据。纯标准库，`go build` 产出单文件可执行程序。

> 仅供个人访问自己的钱迹数据。请求签名与官方 App 一致，但这是非公开接口，钱迹随时可能改动；写操作会改动真实账单，务必谨慎。

## 构建

```bash
cd cli
go build -o qianji .        # Windows 下为 qianji.exe
```

交叉编译单文件示例：

```bash
GOOS=windows GOARCH=amd64 go build -o qianji.exe .
GOOS=linux   GOARCH=amd64 go build -o qianji .
GOOS=darwin  GOARCH=arm64 go build -o qianji .
```

## 使用

```
qianji login [--account <手机号/邮箱>] [--password <密码>]  登录；密码也可取自 $QIANJI_PASSWORD，否则交互输入
qianji me                                显示用户资料（昵称/邮箱/VIP/头像 + 本地 token；whoami 为其别名）
qianji logout                            清除本地 token
qianji books                             列出账本
qianji assets                            列出资产账户
qianji categories --book <id> [--type 0|1]  列出分类（0 支出 / 1 收入）
qianji bills [--book <id>] [--since/--until/--type/--cate/--min/--max/--keyword/--limit ...] [--pages N] [--no-cache]
                                         列出账单（最新在上，本地增量缓存 + 多种筛选，显示分类名）
qianji raw <ctrl> <act> [key=value ...]  对任意接口发签名请求，打印 JSON
qianji saving                            列出存钱计划
qianji currency                          列出货币
qianji logins                            列出登录记录 / 设备
qianji repeat                            列出周期记账
qianji installment                       列出分期
qianji billimage <图片id>... [--size origin|md|small]  由账单图片 id 生成图片 URL
qianji add --type <t> --money <m> --cate <id> --asset <id> [...] [--confirm]  记一笔（默认 dry-run）
qianji del <billid>... [--confirm]       删除账单（默认 dry-run）
```

登录（密码不回显，建议用环境变量）：

```bash
QIANJI_PASSWORD='你的密码' ./qianji login --account 13800000000
./qianji books
./qianji raw asset list status=1
```

配置与 token 存放在**可执行文件同目录**下的 `.qianji-cli/config.json`（文件权限 `0600`）。工具是绿色便携的：配置跟着二进制走，换台机器把整个目录拷过去即可。

### 账单与图片

- `qianji bills` 默认**最新在上**，并维护本地增量缓存 `.qianji-cli/bills_<账本>.json`：首次全量拉取建立缓存，之后用服务器返回的 `lasttimes` 游标只拉新增/变更并合并，秒回（实测 1839 条：首次约 5.8s，之后约 0.6s）。账单明细字段有 `id` / `money` / `time` / `type` / `cateid` / `assetid` / `descinfo` 等，`deletes` 是已删除账单的 id。`--book` 按账本筛选；`--no-cache` 强制全量；`--raw` / `--json` 直接输出接口响应（不走缓存）。
- 本地筛选（在缓存数据上过滤，不额外请求）：`--since` / `--until <日期>`（时间范围，日期格式 `2006-01-02` 或带时分）、`--type <0 支出 / 1 收入 / 2 转账 ...>`、`--cate <分类名或 id>`、`--min` / `--max <金额>`、`--keyword <词>`（搜备注 / 来源 / 账户）、`--limit <n>`（只看最近 n 条）。账单行会显示分类名——取自本地缓存的分类表 `categories_<账本>.json`（首次查询时拉取分类并缓存）。
- 账单若带图片，账单对象会有 `images` 字段，是图片 id 数组（如 `["_eeaa93c4..."]`）。用 `qianji billimage <id>` 把 id 转成可直接下载的 URL：

```bash
qianji billimage _eeaa93c4790f4dcd9e16f62bc6109368            # 原图
qianji billimage _eeaa93c4790f4dcd9e16f62bc6109368 --size md  # 中图 640
```

  图片存在 `http://billimg.qianjiapp.com/<id>!<规格>`，规格 `webporigin`（原图）/ `webpmd640`（中 640）/ `webpsmall240`（缩略 240），返回 webp，凭图片 id 公开访问、不需要 token。

### 用户资料与头像

`qianji me` 显示线上资料（昵称、邮箱、性别、注册时间、VIP 类型/起止、微信绑定、头像 URL），数据取自 `vip/config` 的 `config.userinfo`，即 App「我的」页。头像 `avatar` 是完整 URL（`http://img.qianjiapp.com/<id>!qianjiwebp`），可直接访问。`whoami` 则只显示本地保存的登录态。

### 输出格式

所有查询命令默认输出**人类可读**格式——枚举（账单类型 / 状态、性别、会员等）、时间戳、金额都会转成人话。两个全局开关：

- `--json`：美化后的 JSON（字段原样，便于脚本处理）
- `--raw`：服务器返回的原始响应（含 `ec` / `em` / `data`，未加工）

比如：

- `qianji me` → 人读的昵称 / 邮箱 / 会员 / 头像
- `qianji me --json` → 美化 JSON
- `qianji bills --raw` → 原始响应

默认人读覆盖 `me` / `books` / `assets` / `bills` / `categories` / `logins` / `saving` / `currency` / `repeat` / `installment`；`raw` 命令默认走美化 JSON。

### 设备信息（重要）

每个请求都带一组设备头：`devbrand`、`devname`、`devid`、`osvs`、`mk`、时区、语言、地区。钱迹服务器会**按设备记录登录**（App 内「账号与安全 → 登录记录」能看到），并支持远程移除某台设备的登录。

- `devid` 不是硬件标识：App 本身用的也是一个随机 UUID 的 MD5、存在本地。本 CLI 同样生成一个随机 `devid` 存进配置，**不读取任何真实硬件 ID**，不泄露 IMEI / Android ID 之类。
- 默认 `devbrand` / `devname` 诚实标识为 **厂商 `QianJi-CLI` / 型号 `<版本号>`**（型号即 `qianji version` 的版本号），不伪装成真机。登录记录里会看到厂商 `QianJi-CLI`、型号 `0.2.0` 的设备——一眼认出是这个工具和它的版本。

如果你更希望它和手机上的现有记录看起来一致、减少被风控或安全提醒打扰，可以在 `config.json` 里改成你真机的值：

```json
{
  "device": {
    "brand": "XIAOMI",
    "name": "23127PN0CC",
    "osver": "34",
    "market": "guanwang",
    "tzoffset": "28800",
    "lang": "zh",
    "region": "CN"
  }
}
```

留空的字段用默认值。注意：`devid` 与真机的那台仍然不同（真机的 `devid` 存在手机 App 的私有目录里，不 root 拿不到），所以 CLI 至少会是「一台新设备」。这是接口本身的设计，无法完全避免。

## 安全说明

- 密码不落盘：登录时取 MD5 后发送，本地只保存服务器返回的 `token` 和 `uid`。
- `$QIANJI_PASSWORD` 方式不会在终端回显；交互输入目前会回显（纯标准库、零依赖的取舍），介意就用环境变量或管道。
- `--password` 参数图方便，但密码会进 shell 历史和进程列表，共享机器上优先用 `$QIANJI_PASSWORD`。密码取 MD5 后发送，优先级：`--password` > `$QIANJI_PASSWORD` > 交互输入。
- 只用于你本人的账号，不要对他人账号使用。

## 工作原理

- 主机 `https://api.qianjiapp.com/`，路径 `<ctrl>/<act>`，表单参数，登录后带 `uid`/`fr`。
- 鉴权请求头 `utoken`；每个请求都带签名头 `reqidv2` 和 `tok`。
- 签名是标准 MD5 拼接（见 [sign.go](sign.go) 与 `../docs/qianji-api.md`），已从 `libfabricsuffer.so` 还原并对线上免鉴权接口 `client/init` 验证通过。
- `go test ./...` 用独立实现算出的金标准值锁定签名，避免算法被无意改动。

## 已验证 / 未验证

- **已验证**：登录 + 只读接口在真实账号上跑通——`login`、`books`、`assets`、`bills`、`client/init`（带登录态）均返回正常数据。
- **关键坑（已修）**：服务器对带登录态的请求**大小写敏感地校验请求头**，头名必须全小写（`utoken` 而非 `Utoken`）。Go 的 `http.Header.Set` 会自动规范成首字母大写，导致所有鉴权接口返回 `当前版本太旧`，而免鉴权接口正常——极易误判成版本号或签名问题。本 CLI 直接写 `Header[name]` 绕过规范化。详见 [../docs/qianji-api.md](../docs/qianji-api.md)。
- **写入（已实现并实测通过，默认 dry-run + 二次确认）**：`add` 记一笔、`del` 删账单，走 `bill/syncall`（`v = {"bills": {changelist:[账单], dellist:[billid]}}`，新账单 `status=2`、17 位 id、带 `updatetime`）。命令默认只打印将发送的 `v`，`--confirm` 并输入 `yes` 才真正写入；已在真实账号验证 add 写入 / del 删除往返成功。
- **仍待做**：预算设置（`budget/submitv2`）、预算查询（`budget/list`，需账本 `range` + `DateFilter`）。

## MCP server

`qianji mcp` 启动一个 HTTP MCP server，把查询和写入包成 MCP 工具（`qianji_me` / `books` / `assets` / `categories` / `bills` / `saving` / `currency` / `logins` / `repeat` / `add` / `del` / `raw`），供支持 MCP 的客户端调用。纯标准库手写 JSON-RPC 2.0（`initialize` / `tools/list` / `tools/call`）。

- **认证 = 密钥路径**：端点是 `/mcp/<密钥>`，只有 POST 到正确密钥路径才响应，其它一律 404。用 `$QIANJI_MCP_PATH` 固定密钥，留空则每次启动随机生成并打印。

```bash
QIANJI_MCP_PATH=你的一长串随机密钥 ./qianji mcp --addr :8787
# 端点： POST http://localhost:8787/mcp/你的一长串随机密钥
```

> ⚠️ 工具含写入（`add` / `del`），能改你的真实账单。密钥路径是唯一防线——别把带密钥的完整 URL 泄露出去，对公网建议再加 HTTPS 反代。

## Docker

[Dockerfile](../Dockerfile) 多阶段构建，产出 alpine 小镜像（带 shell，方便 `docker exec` 进容器 login / 排查）跑 MCP server。配置和账单/分类缓存都落在容器里的 `/data`（`QIANJI_CONFIG_DIR=/data`），挂个卷就能持久化，登录一次即可反复用。

```bash
docker build -t qianji-mcp .
```

先起 MCP server（未登录也能先跑起来，不会崩）：

```bash
docker run -d --name qianji -p 8787:8787 -v qianji-data:/data -e QIANJI_MCP_PATH=一长串随机密钥 qianji-mcp
```

再登录（写进卷；server 每次调用都读最新登录态，登录后立即可用）：

```bash
docker exec -it -e QIANJI_PASSWORD=你的密码 qianji qianji login --account 你的账号
```

端点：`POST http://<host>:8787/mcp/<密钥>`。卷 `qianji-data` 存着 `config.json` 和缓存，容器重建也不丢；登录前调用工具会返回友好提示，而不是让 server 崩溃重启。

> 备选（不挂卷）：直接用环境变量注入登录态 `-e QIANJI_UID=<uid> -e QIANJI_TOKEN=<token>`（`loadConfig` 也认 `QIANJI_HOST`），省掉 login 步骤，但每次都要给 token。
