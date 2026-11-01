# qianji CLI

一个用 Go 写的钱迹（钱迹记账 / `com.mutangtech.qianji`）私有 API 命令行工具，用你**自己的账号**调用。纯标准库，`go build` 产出单文件可执行程序。

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
qianji whoami                            显示当前登录用户
qianji logout                            清除本地 token
qianji books                             列出账本
qianji assets                            列出资产账户
qianji categories --book <id> [--type 0|1]  列出分类（0 支出 / 1 收入）
qianji bills [--book <id>] [--pages N]   拉取账单并打印 JSON
qianji raw <ctrl> <act> [key=value ...]  对任意接口发签名请求，打印 JSON
```

登录（密码不回显，建议用环境变量）：

```bash
QIANJI_PASSWORD='你的密码' ./qianji login --account 13800000000
./qianji books
./qianji raw asset list status=1
```

配置与 token 存放在**可执行文件同目录**下的 `.qianji-cli/config.json`（文件权限 `0600`）。工具是绿色便携的：配置跟着二进制走，换台机器把整个目录拷过去即可。

### 设备信息（重要）

每个请求都带一组设备头：`devbrand`、`devname`、`devid`、`osvs`、`mk`、时区、语言、地区。钱迹服务器会**按设备记录登录**（App 内「账号与安全 → 登录记录」能看到），并支持远程移除某台设备的登录。

- `devid` 不是硬件标识：App 本身用的也是一个随机 UUID 的 MD5、存在本地。本 CLI 同样生成一个随机 `devid` 存进配置，**不读取任何真实硬件 ID**，不泄露 IMEI / Android ID 之类。
- 默认 `devbrand`/`devname` 诚实标识为 **`QianJi-CLI` / `QianJi-CLI/<版本号>`**（版本号即 `qianji version`，当前随 CLI 走），不伪装成真机。用它登录后，你在登录记录里会看到一台名为 `QianJi-CLI/0.1.0` 的设备——一眼能认出是这个工具和它的版本。

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
- **暂未实现**：记账写入（`bill/syncall`）。写入要构造完整账单对象并走同步协议，风险高，放到下一轮，并会加二次确认和 dry-run。

## 后续：MCP

CLI 的签名与请求层（`sign.go` / `client.go`）与界面解耦，后续可直接在其上包一层 MCP server，把 `books`/`assets`/`bills`/`raw` 暴露为工具。
