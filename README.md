# mitray

A tray to manage mihomo core on windows.

## Features

- Restart/Stop mihomo core
- Enable/Disable system proxy
- Enable/Disable TUN mode
- Remember and restore TUN mode without modifying mihomo config files
- Refresh mihomo status
- Open mihomo webui
- Open mihomo directory
- Auto start on startup (or with administrator)

## Usage

1. Download the latest release from [Releases](https://github.com/tom2almighty/mitray/releases).
2. Run `mitray.exe`.
3. First time you run `mitray.exe`, it will create a `config.ini` in the same directory and open the initialization window.
4. Select the mihomo executable and a mihomo config file, then save.

> [!IMPORTANT]
> If you want to use TUN mode, you need to run `mitray.exe` as administrator.

## Configuration

MiTray can be configured from the tray menu:

- `初始化/编辑配置`: choose the mihomo executable, local config file or remote config URL, and runtime options.
- `选择 mihomo 配置`: switch between local mihomo config profiles.
- `选择 mihomo 配置 > 添加配置文件...`: add another local mihomo YAML config.

MiTray keeps its own settings in `config.ini` and does not modify your mihomo
YAML config files.

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

## TUN state memory

MiTray does not write to your mihomo YAML config. TUN mode is controlled through
mihomo's runtime API and the desired state is saved in MiTray's `config.ini`.

```ini
[Settings]
RememberTUN=1
TUNEnabled=0
AutoRestoreTUN=1
```

- `RememberTUN`: save the TUN state after a successful tray toggle.
- `TUNEnabled`: the desired TUN state remembered by MiTray.
- `AutoRestoreTUN`: re-apply the remembered TUN state after mihomo restart or
  WebUI config reload.

For existing `config.ini` files without `TUNEnabled`, MiTray initializes the
remembered state from the current mihomo runtime TUN state on first successful
API status read.
