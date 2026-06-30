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
3. First time you run `mitray.exe`, it will create a `config.ini` in the same directory. Edit it and set the path to your mihomo executable and config file.

> [!IMPORTANT]
> If you want to use TUN mode, you need to run `mitray.exe` as administrator.

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
