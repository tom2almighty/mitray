package platform

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

type System struct{ Executable string }

const internetSettings = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

func (System) Proxy() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettings, registry.QUERY_VALUE)
	if err != nil {
		return false, err
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("ProxyEnable")
	if err == registry.ErrNotExist {
		return false, nil
	}
	return v != 0, err
}

func (System) SetProxy(enabled bool, port int) error {
	if enabled && (port < 1 || port > 65535) {
		return fmt.Errorf("mihomo 没有可用的 mixed-port 或 port")
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettings, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	var value uint32
	if enabled {
		value = 1
		if err := k.SetStringValue("ProxyServer", fmt.Sprintf("127.0.0.1:%d", port)); err != nil {
			return err
		}
		if err := k.SetStringValue("ProxyOverride", "localhost;127.*;10.*;172.16.*;172.17.*;172.18.*;172.19.*;172.20.*;172.21.*;172.22.*;172.23.*;172.24.*;172.25.*;172.26.*;172.27.*;172.28.*;172.29.*;172.30.*;172.31.*;192.168.*;<local>"); err != nil {
			return err
		}
	}
	if err := k.SetDWordValue("ProxyEnable", value); err != nil {
		return err
	}
	fn := windows.NewLazySystemDLL("wininet.dll").NewProc("InternetSetOptionW")
	for _, option := range []uintptr{39, 37} {
		r, _, err := fn.Call(0, option, 0, 0)
		if r == 0 {
			return fmt.Errorf("代理已写入，通知 Windows 刷新失败: %w", err)
		}
	}
	return nil
}

func Open(target string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	path, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, path, nil, nil, windows.SW_SHOWNORMAL)
}

func IsAdmin() bool { return windows.GetCurrentProcessToken().IsElevated() }

// Hold the mutex for the lifetime of the UI. The caller brings the existing
// settings window to the foreground on a second launch.
func SingleInstance(name string) (windows.Handle, bool, error) {
	p, err := windows.UTF16PtrFromString(`Local\` + name)
	if err != nil {
		return 0, false, err
	}
	h, err := windows.CreateMutex(nil, false, p)
	if err == windows.ERROR_ALREADY_EXISTS {
		return h, false, nil
	}
	return h, err == nil, err
}

func HiddenCommand(cmd *syscall.SysProcAttr) *syscall.SysProcAttr {
	if cmd == nil {
		cmd = &syscall.SysProcAttr{}
	}
	cmd.HideWindow = true
	cmd.CreationFlags |= windows.CREATE_NO_WINDOW
	return cmd
}

func MessageBox(title, message string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(message)
	windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x10)
}
