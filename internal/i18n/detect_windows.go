//go:build windows

package i18n

import "golang.org/x/sys/windows"

const langChinesePrimary = 0x04

func detectSystemLang() string {
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")
	r, _, _ := proc.Call()
	if uint16(r)&0x03ff == langChinesePrimary {
		return ZH
	}
	return EN
}
