package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/lxn/win"
	"github.com/tom2almighty/mitray/internal/config"
	"github.com/tom2almighty/mitray/internal/controller"
	"github.com/tom2almighty/mitray/internal/platform"
	"github.com/tom2almighty/mitray/internal/ui"
	"golang.org/x/sys/windows"
)

var version = "dev"

func main() {
	runtime.LockOSThread()
	exe, err := os.Executable()
	if err != nil {
		platform.MessageBox("MiTray", err.Error())
		return
	}
	base := filepath.Dir(exe)
	hash := sha256.Sum256([]byte(exe))
	name := fmt.Sprintf("MiTray-%x", hash[:12])
	restart := slices.Contains(os.Args, "--restart-core")
	var mutex windows.Handle
	for i := 0; i < 50; i++ {
		var first bool
		mutex, first, err = platform.SingleInstance(name)
		if err != nil {
			platform.MessageBox("MiTray", err.Error())
			return
		}
		if first {
			break
		}
		windows.CloseHandle(mutex)
		mutex = 0
		if !restart {
			title, _ := windows.UTF16PtrFromString("MiTray 设置")
			if h := win.FindWindow(nil, title); h != 0 {
				win.ShowWindow(h, win.SW_SHOWNORMAL)
				win.SetForegroundWindow(h)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	if mutex == 0 {
		platform.MessageBox("MiTray", "已有 MiTray 实例尚未退出，请稍后重试。")
		return
	}
	defer windows.CloseHandle(mutex)
	store := config.Store{Path: filepath.Join(base, "config.ini")}
	cfg, err := store.Load()
	if err != nil {
		platform.MessageBox("MiTray", "读取 config.ini 失败，原文件已保留。\n"+err.Error())
		return
	}
	c := controller.New(base, cfg, store, platform.Core{}, platform.System{Executable: exe})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ui.Run(ctx, c, base, exe, version, restart); err != nil {
		platform.MessageBox("MiTray", err.Error())
	}
}
