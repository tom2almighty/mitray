# MiTray

Windows 上的轻量 mihomo 托盘控制器，使用 Go 和 Windows 原生控件编写。

> [!NOTE]
> 使用 Golang 重写。1.0.15 版本为 AHK 最后版本，不再维护。

## 功能

- 托盘菜单：启动、重启、停止 mihomo 核心，自动连接已在运行的核心，一键打开 WebUI
- 系统代理与 TUN 模式独立开关，TUN 支持启动记忆
- 多配置管理：本地 YAML 或远程 URL，远程自动缓存，托盘菜单一键切换
- 开机自启：支持普通或管理员权限，可设置启动延迟
- 复制代理环境变量：右键菜单选择 PowerShell、CMD 或 Bash，一键写入剪贴板

## 使用

1. 将 `mitray.exe` 放在可写目录中运行。
2. 首次打开时，在设置面板选择 mihomo 核心，以及本地 YAML 文件或远程配置 URL，点击「保存并应用」。
3. 左键托盘图标打开设置，右键打开快捷菜单。
4. 需要 TUN 时使用「以管理员身份重新启动」，并可配合「管理员权限」开机自启。

修改核心或配置后保存会重启正在运行的核心，其他选项不会。关闭设置窗口后继续在托盘运行；「退出 MiTray」保留核心及系统代理，「停止核心并退出」会先停止核心。

设置保存在程序目录的 `config.ini`（相对路径以程序目录为基准），远程配置按 URL 缓存在 `cache/`，离线时使用缓存。升级时先退出 MiTray，用新 EXE 替换旧文件并保留 `config.ini` 即可，旧版 AHK 配置可直接沿用。

## TUN 行为

MiTray 不修改原始 YAML，仅通过 mihomo API 切换 TUN 开关。

- 开关状态保存在 `config.ini` 的 `[State] TUNEnabled`；没有该字段时遵循 YAML 的 `tun.enable`。
- 切换时发送 `PATCH /configs`，请求体仅含 `{"tun":{"enable":true}}` 或 `false`；网卡、协议栈、DNS 和路由遵循配置文件。
- 系统代理与 TUN 可同时启用，互不影响。
- 托盘菜单「清除 TUN 记忆」后，下次启动核心遵循配置文件。

## 开发构建

需要 Go 1.24 或更新版本。Windows amd64：

```powershell
.\build.ps1 -Version 2.0.0-dev
```

生成 `dist/mitray.exe`，内嵌三种托盘图标、Windows 控件样式和 DPI manifest。构建不启动 mihomo。

也可以在 Linux/macOS 交叉编译：

```sh
go run ./tools/resources -arch amd64
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
  -ldflags '-H windowsgui -s -w -X main.version=2.0.0-dev' \
  -o dist/mitray.exe ./cmd/mitray
```

GitHub Actions 在推送和拉取请求时编译 Windows EXE；推送 `v*` tag 时附加到 Release。

## 许可证

[AGPL-3.0](LICENSE)
