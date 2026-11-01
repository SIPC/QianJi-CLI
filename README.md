# QianJi-CLI

钱迹 CLI：用你自己的账号访问钱迹（钱迹记账 / `com.mutangtech.qianji`）数据的命令行工具，Go 编写，纯标准库，单文件分发。

- 命令行工具与用法见 [cli/](cli/)
- 接口分析（签名方案、账单模型、接口清单）见 [docs/qianji-api.md](docs/qianji-api.md)

> 仅供个人访问自己的钱迹数据。请求签名与官方 App 一致，但这是非公开接口，可能随版本变动；写操作会改动真实账单，需谨慎。
