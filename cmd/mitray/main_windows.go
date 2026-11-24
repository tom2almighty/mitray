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
	"github.com/tom2almighty/mitray/internal/i18n"
	"github.com/tom2almighty/mitray/internal/platform"
	"github.com/tom2almighty/mitray/internal/ui"
	"golang.org/x/sys/windows"
)

var version = "dev"

func main() {
	runtime.LockOSThread()
	i18n.Init(i18n.Auto)
	exe, err := os.Executable()
	if err != nil {
		platform.MessageBox("MiTray", err.Error())
		return
	}
	base := filepath.Dir(exe)
	hash := sha256.Sum256([]byte(exe))
	name := fmt.Sprintf("MiTray-%x", hash[:12])
	restartCore := slices.Contains(os.Args, "--restart-core")
	relaunch := slices.Contains(os.Args, "--relaunch")
	showSettings := slices.Contains(os.Args, "--show-settings")
	waitExit := restartCore || relaunch
	var mutex windows.Handle
	for range 50 {
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
		if !waitExit {
			for _, windowTitle := range i18n.AllSettingsTitles() {
				title, _ := windows.UTF16PtrFromString(windowTitle)
				if h := win.FindWindow(nil, title); h != 0 {
					hidden := !win.IsWindowVisible(h)
					if hidden || win.IsIconic(h) {
						win.ShowWindow(h, win.SW_RESTORE)
					}
					if hidden {
						if err := ui.CenterSettingsWindow(h); err != nil {
							platform.MessageBox("MiTray", err.Error())
						}
					}
					win.SetForegroundWindow(h)
					break
				}
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	if mutex == 0 {
		platform.MessageBox("MiTray", i18n.T().ErrInstanceExiting)
		return
	}
	defer windows.CloseHandle(mutex)
	store := config.Store{Path: filepath.Join(base, config.Filename)}
	cfg, err := store.Load()
	if err != nil {
		platform.MessageBox("MiTray", fmt.Sprintf(i18n.T().ErrReadConfigPrefix, config.Filename, err.Error()))
		return
	}
	i18n.Init(cfg.Language)
	c := controller.New(base, cfg, store, platform.NewCore(), platform.System{Executable: exe})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ui.Run(ctx, c, base, exe, version, restartCore, showSettings); err != nil {
		platform.MessageBox("MiTray", err.Error())
	}
}
