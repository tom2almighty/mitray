# MiTray

Windows 上的轻量 mihomo 托盘控制器，使用 Go 和 Windows 原生控件编写。

> [!NOTE]
> 使用 Golang 重写。1.0.15 版本为 AHK 最后版本，不再维护。

## 功能

- 托盘菜单：启动、重启、停止 mihomo 核心，自动连接已在运行的核心，一键打开 WebUI
- 系统代理与 TUN 模式独立开关，TUN 支持启动记忆
- 多配置管理：每个档案独立保存来源类型、本地路径或远程 URL，托盘菜单一键切换
- 开机自启：支持普通或管理员权限，可设置启动延迟
- 复制代理环境变量：右键菜单选择 PowerShell、CMD 或 Bash，一键写入剪贴板

## 使用

1. 将 `mitray.exe` 放在可写目录中运行。
2. 首次打开时，在设置面板选择 mihomo 核心，添加本地 YAML 档案或远程 URL 档案，点击「保存并应用」。
3. 左键托盘图标打开设置，右键打开快捷菜单。
4. 需要 TUN 时使用「以管理员身份重新启动」，并可配合「管理员权限」开机自启。

修改核心或当前激活配置档案后保存会重启正在运行的核心，仅编辑其他未激活档案不会自动切换当前配置。关闭设置窗口后继续在托盘运行，未保存的修改会保留；「退出 MiTray」保留核心及系统代理，「停止核心并退出」会先停止核心。系统代理不跟随核心关闭，因此停止核心前若代理指向当前核心端口，可选择同时关闭系统代理或仅停止核心。

设置保存在程序目录的 `mitray.toml`（相对路径以程序目录为基准）。核心路径和当前配置在 `[core]`，每个配置档案使用 `[profiles.名称]` 保存 `type`、`path`、`url`；远程配置始终直连下载（使用 `global-ua` 或默认的 `clash.meta` User-Agent）并按 URL 缓存在 `cache/`，下载、校验或缓存写入失败时回退到上次成功缓存；删除或修改远程档案后会自动清理不再使用的旧缓存。

## TUN 行为

MiTray 不修改原始 YAML，仅通过 mihomo API 切换 TUN 开关。

- 开关状态保存在 `mitray.toml` 的 `[state] tun_enabled`；没有该字段时遵循 YAML 的 `tun.enable`。
- 仅对 MiTray 自己启动的核心进程恢复启动记忆；接管或检测到外部已运行的核心时保留其现有状态。
- 切换时发送 `PATCH /configs`，请求体仅含 `{"tun":{"enable":true}}` 或 `false`；网卡、协议栈、DNS 和路由遵循配置文件。
- 系统代理与 TUN 可同时启用，互不影响。
- 托盘菜单「清除 TUN 记忆」后，下次启动核心遵循配置文件。

## 开发构建

需要 Go 1.24 或更新版本。Windows amd64：

```powershell
.\build.ps1 -Version 2.0.0-dev
```

生成 `dist/mitray.exe`，构建时由 `mitray.svg` 自动渲染生成三种状态的多分辨率托盘图标，并内嵌 Windows 控件样式和 DPI manifest。构建不启动 mihomo。

也可以在 Linux/macOS 交叉编译：

```sh
go run ./tools/resources -arch amd64
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
  -ldflags '-H windowsgui -s -w -X main.version=2.0.0-dev' \
  -o dist/mitray.exe ./cmd/mitray
```

GitHub Actions 在推送和拉取请求时执行格式检查、`go vet`、测试和 Windows EXE 构建；推送 `v*` tag 时附加到 Release。

## 许可证

[AGPL-3.0](LICENSE)
