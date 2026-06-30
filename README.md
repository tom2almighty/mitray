# mitray

A tray to manage mihomo core on windows.

## Features

- Restart/Stop mihomo core
- Enable/Disable system proxy
- Enable/Disable TUN mode
- Control TUN mode at runtime without modifying mihomo config files
- Refresh mihomo status
- Open mihomo webui
- Open mihomo directory
- Auto start on startup (or with administrator)

## Usage

1. Download the latest release from [Releases](https://github.com/tom2almighty/mitray/releases).
2. Run `mitray.exe`.
3. First time you run `mitray.exe`, it will create a `config.ini` in the same directory and open the MiTray setup window.
4. Select the mihomo executable, then provide either a local mihomo config file or a remote config URL.

> [!IMPORTANT]
> If you want to use TUN mode, you need to run `mitray.exe` as administrator.

## Configuration

MiTray can be configured from the tray menu:

- `MiTray 设置...`: choose the mihomo executable, local config file or remote config URL, and runtime options.
- `选择 mihomo 配置`: switch between local mihomo config profiles.
- `选择 mihomo 配置 > 添加配置文件...`: add another local mihomo YAML config.

MiTray keeps its own settings in `config.ini` and does not modify your mihomo
YAML config files.

The settings window shows parsed mihomo fields from the selected local YAML
config and provides a `测试配置` button. MiTray reads only the top-level scalar
fields it needs:

- `external-controller`
- `secret`
- `mixed-port` / `port`
- `external-ui`
- `external-ui-name`

Local config profiles are stored in the `[Profiles]` section:

```ini
[Mihomo]
CorePath=D:\Program\Mihomo\mihomo-windows-amd64.exe
ActiveProfile=default
ConfigPath=D:\Program\Mihomo\config.yaml
; ConfigURL=https://example.com/clash/config.yaml

[Profiles]
default=D:\Program\Mihomo\config.yaml
work=D:\Program\Mihomo\work.yaml
```

`ConfigURL` keeps the old priority: if it is set, MiTray downloads and uses the
remote config instead of the active local profile. Switching a local profile from
the tray menu clears `ConfigURL`.

## TUN control

MiTray does not write to your mihomo YAML config. You can choose who controls
TUN state:

```ini
[Settings]
TUNControl=runtime
TUNEnabled=0
```

- `TUNControl=runtime`: MiTray controls TUN through mihomo's runtime API. Tray
  toggles update `TUNEnabled`, and MiTray reapplies it after restart or WebUI
  reload.
- `TUNControl=file`: MiTray follows `tun.enable` from your YAML config. Tray
  toggles are runtime-only and are not saved by MiTray.
- `TUNEnabled`: the target TUN state when `TUNControl=runtime`.

The setup window shows this as `MiTray 运行时接管` or `跟随 YAML 的 tun.enable`.
