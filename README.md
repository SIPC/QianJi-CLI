# 钱迹 CLI · QianJi-CLI

用你**自己的账号**访问钱迹（钱迹记账 / `com.mutangtech.qianji`）数据的命令行工具 + MCP server。Go 编写，**纯标准库、零依赖**，单文件分发。

> ⚠️ 仅供访问你本人的钱迹账号。请求签名与官方 App 一致，但走的是非公开接口，钱迹随时可能改动；写操作会改动真实账单，务必谨慎。

## 功能

- **查询**（三种输出：默认人读 / `--json` / `--raw`）
  - 用户资料、账本、资产账户、分类
  - **账单**：本地增量缓存（首次全量、之后秒回），可按时间 / 类型 / 分类（名或 id）/ 金额 / 关键词筛选，显示分类名
  - 预算、存钱计划、货币、登录设备、周期记账、分期、账单图片 URL
- **写入**（默认 dry-run，`--confirm` + 手动 `yes` 才生效）：记一笔 `add`、删账单 `del`、改预算 `budget set`
- **MCP server**：`qianji mcp` 起一个 HTTP MCP server，把查询和写入暴露为工具，供 Claude / ChatGPT 等 MCP 客户端调用（密钥路径认证）
- **Docker**：现成镜像 [`sipcink/qianji-mcp`](https://hub.docker.com/r/sipcink/qianji-mcp)，配置用卷持久化
- **自动发版**：打 `vX.Y.Z` 标签，GitHub Actions 自动交叉编译 6 平台（Windows / Linux / macOS × amd64 / arm64）并创建带更新日志的 Release

## 快速开始

```bash
cd cli
go build -o qianji .        # Windows 下为 qianji.exe
```

```bash
# 登录（密码用环境变量避免回显）
QIANJI_PASSWORD='你的密码' ./qianji login --account 13800000000

# 查询
./qianji me                              # 用户资料（昵称/邮箱/会员/头像）
./qianji assets                          # 资产账户与余额
./qianji bills --since 2026-10-01 --type 0   # 10 月以来的支出
./qianji bills --cate 餐饮 --limit 10         # 最近 10 笔餐饮

# 记一笔（默认只预览，加 --confirm 才真正写入）
./qianji add --type 0 --money 12.5 --cate <分类id> --asset <资产id> --remark 午饭
```

配置与 token 存在**可执行文件同目录**的 `.qianji-cli/config.json`（文件权限 `0600`），工具绿色便携。

## 命令一览

| 命令 | 说明 |
| --- | --- |
| `login` / `logout` / `me` | 登录 / 登出 / 用户资料（`me` 也是 `whoami` 的别名） |
| `books` `assets` `categories` | 账本 / 资产 / 分类 |
| `bills` | 账单，支持 `--since/--until/--type/--cate/--min/--max/--keyword/--limit/--book` |
| `saving` `currency` `logins` `repeat` `installment` | 存钱 / 货币 / 登录设备 / 周期记账 / 分期 |
| `billimage <id>...` | 账单图片 id → 可下载 URL |
| `add` / `del` | 记一笔 / 删账单（默认 dry-run） |
| `budget` / `budget set` | 查预算 / 改一项预算（`set` 默认 dry-run），都要 `--book`，周期用 `--month` 或 `--year` |
| `raw <ctrl> <act> [k=v ...]` | 对任意接口发签名请求 |
| `mcp [--addr :8787]` | 启动 MCP server |
| `version` | 版本 |

全局：默认人类可读，加 `--json` 美化 JSON，`--raw` 原始响应。详见 **[cli/README.md](cli/README.md)**。

## MCP server

```bash
QIANJI_MCP_PATH=一长串随机密钥 ./qianji mcp --addr :8787
# 端点： POST http://localhost:8787/mcp/<密钥>
```

手写 JSON-RPC over Streamable HTTP，**认证即密钥路径**（`/mcp/<密钥>`，错误路径一律 404）。暴露 `qianji_me/books/assets/categories/bills/saving/currency/logins/repeat/add/del/budget/budget_set/raw` 等工具。

> ⚠️ 工具含写入，能改你的真实账单。密钥路径是唯一防线——别泄露带密钥的完整 URL，对公网务必加 HTTPS 反代。

## Docker

现成镜像 `sipcink/qianji-mcp`，配置用卷持久化（登录一次即复用）：

```bash
# 先起 server（未登录也不崩）
docker run -d --name qianji -p 8787:8787 -v qianji-data:/data -e QIANJI_MCP_PATH=密钥 sipcink/qianji-mcp

# 再登录（写进卷，下次调用即生效）
docker exec -it qianji qianji login --account 你的账号
```

也可不挂卷、直接 `-e QIANJI_UID=<uid> -e QIANJI_TOKEN=<token>` 注入登录态。细节见 [cli/README.md](cli/README.md) 的 Docker 一节。

## 发版

```bash
git tag v0.3.1 && git push origin v0.3.1
```

[GitHub Actions](.github/workflows/release.yml) 会交叉编译 6 平台、从上个 tag 以来的提交生成更新日志、创建 Release 并上传压缩包 + 校验和；版本号由标签注入。

## 文档

- **[cli/README.md](cli/README.md)** —— CLI 完整用法、输出格式、账单与图片、设备信息、安全说明
- **[docs/qianji-api.md](docs/qianji-api.md)** —— 接口逆向分析：签名方案、账单数据模型、~90 个接口清单、写入协议，以及踩过的坑（请求头大小写、游标分页、会员类型等）

## 工作原理（简）

- 主机 `https://api.qianjiapp.com/`，路径 `<ctrl>/<act>`，表单参数；鉴权头 `utoken`，每请求带签名头 `reqidv2`/`tok`
- 签名是从 `libfabricsuffer.so` 还原的 MD5 拼接，`go test ./...` 用金标准值锁定算法
- **关键坑**：服务器对带登录态的请求**大小写敏感地校验请求头名**，必须小写（`utoken` 而非 `Utoken`）——这是鉴权接口能用的根因，详见 docs

## 免责

个人学习与自用项目，只对你本人的钱迹账号使用，不要用于他人账号。接口为非公开接口，随版本可能失效。
