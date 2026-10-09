# MiTray

Windows 上的轻量 mihomo 托盘控制器，使用 Go 和 Windows 原生控件编写。

> [!NOTE]
> 使用 Golang 重写，二进制文件和内存占用 10MB 左右。
> v1.0.15 版本为 AHK 最后版本，不再维护。


## 使用

1. 将 `mitray.exe` 放在可写文件夹中运行。
2. 首次打开时，在设置面板选择 mihomo 核心，以及本地 YAML 文件或远程配置 URL。
3. 点击「保存并应用」。左键托盘图标打开设置，右键打开快捷菜单。
4. 需要 TUN 时，让 mihomo 以管理员权限运行。可点击「以管理员身份重启」，重新启动 MiTray 和所选核心，再设置「管理员权限」开机自启。

设置面板保留核心选择、本地配置列表、远程 URL、自动启动核心及开机自启延迟。修改核心或配置后保存，会重启正在运行的核心；修改自启等其他选项不会重启核心。没有配置检测或解析预览面板。

关闭设置窗口后继续在托盘运行。「退出 MiTray」保留核心及系统代理；「停止核心并退出」停止核心。停止核心不会自动关闭系统代理。

## TUN 行为

- **启动记忆**：`config.ini` 的 `[State] TUNEnabled` 保存核心启动时的默认选择。没有该字段时遵循 mihomo YAML 的 `tun.enable`。
- **手动切换**：在 MiTray 托盘或设置面板切换 TUN，PATCH 并回读确认成功后才更新记忆。托盘始终显示核心实际状态。
- **连接已有核心**：打开 MiTray 时，如果核心已经运行，只连接并读取状态，保留 WebUI 已做的调整。
- **新核心启动**：通过 MiTray 启动、重启或切换配置产生的新核心，以及 MiTray 运行期间检测到的外部新核心进程，API 就绪后按记忆恢复一次；状态已经一致时不发送 PATCH。
- **只修改开关**：发送 `PATCH /configs`，请求体仅为 `{"tun":{"enable":true}}` 或 `false`。网卡名、协议栈、DNS 和路由参数继续由 mihomo 配置提供。
- **独立开关**：系统代理与 TUN 可以同时启用；切换一项不会自动修改另一项。
- **同一进程只读**：完成本次启动恢复后，定时和手动刷新只更新显示。WebUI 手动切换、普通配置重载及 API 断连重连均不触发恢复，也不更新 MiTray 记忆。
- **失败不覆盖记忆**：只有回读确认成功才保存；保存失败会单独提示。API 不可达时显示「未知」。
- **回到 YAML**：托盘菜单「清除 TUN 记忆」仅清除记录，下次启动核心时遵循配置文件。

MiTray 不修改原始 YAML。启动恢复等待 API 就绪最多 45 秒，PATCH 最多尝试 3 次并受 12 秒总期限限制。失败会提示并保留记忆，同一进程的后续刷新不再自动重试；需要重试时可通过 MiTray 重启核心。核心进程更换后，旧恢复流程取消。

定时刷新间隔为 10 秒。核心会先加载 YAML，因此恢复前可能短暂开启 TUN；恰好在自动恢复前通过 WebUI 切换，也可能被这一次恢复覆盖。普通配置重载继续遵循 YAML。WebUI 的临时选择不写入启动记忆，下次核心启动仍采用 MiTray 的记忆。MiTray 未运行时无法观察外部重启，之后打开只连接当时已运行的核心。

## 配置与升级

程序目录中的 `config.ini` 保存 MiTray 设置，支持旧版 AHK 的 UTF-8/UTF-16 配置。升级时将新 EXE 放回原程序目录，保留原来的 `config.ini`，先退出旧版 MiTray。

旧版明确保存的 `TUNEnabled` 会迁移；旧版 `TUNControl=file` 或 `RememberTUN=0` 不导入 TUN 记忆。v1.0.15 没有记忆字段时，保持「跟随 YAML」。已有自启任务会被读取；保存自启设置后更新为 Go 程序的启动方式。

```ini
[Mihomo]
CorePath=D:\Program\Mihomo\mihomo.exe
ConfigPath=D:\Program\Mihomo\config.yaml
ConfigURL=
ActiveProfile=default

[Profiles]
default=D:\Program\Mihomo\config.yaml

[Settings]
AutoStartCore=1
AutoStartupDelaySec=15
AutoStartupLevel=off

[State]
Schema=1
; 不存在 TUNEnabled 表示没有覆盖 YAML
; TUNEnabled=1
```

相对路径以 MiTray 所在目录为基准。核心的工作目录和 `-d` 均使用核心所在目录，与 v1.0.15 一致。本地配置直接交给核心，MiTray 只读取连接 API 和系统代理所需的字段。

远程配置按 URL 分别缓存在 `cache/` 中；网络不可用时使用已有缓存，不提前删除缓存。首次使用的 URL 没有缓存时，需要联网下载。远程 URL 和本地配置同时填写时兼容旧版行为，URL 优先；在面板或托盘选择本地配置会清除 URL。

## 错误提示

MiTray 不生成日志文件，也不保存 mihomo 的核心输出。启动和操作错误直接在设置面板或通知中提示；mihomo 的日志通过 WebUI 查看。

## 构建

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
