package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"github.com/tom2almighty/mitray/internal/config"
	"github.com/tom2almighty/mitray/internal/controller"
	"github.com/tom2almighty/mitray/internal/platform"
	"github.com/tom2almighty/mitray/internal/proxyenv"
	"github.com/tom2almighty/mitray/internal/resources"
	"golang.org/x/sys/windows"
)

type app struct {
	ctx                                                              context.Context
	cancel                                                           context.CancelFunc
	controller                                                       *controller.Controller
	base, exe, version                                               string
	window                                                           *walk.MainWindow
	tray                                                             *walk.NotifyIcon
	icons                                                            [3]*walk.Icon
	busy                                                             bool
	refreshing                                                       bool
	dirty                                                            bool
	quitting                                                         bool
	start, stop, restart, tun, proxy, refresh, forget, web, profiles *walk.Action
	diagnostics                                                      *walk.Action
	startup                                                          map[string]*walk.Action
	mutations                                                        []*walk.Action
	profileActions                                                   []*walk.Action
	profileMenu                                                      *walk.Menu
	profileSignature                                                 string
	coreEdit, profileNameEdit, localEdit, urlEdit                    *walk.LineEdit
	profileCombo, sourceCombo, startupCombo                          *walk.ComboBox
	autoStart                                                        *walk.CheckBox
	delay                                                            *walk.NumberEdit
	statusLabel, tunLabel, proxyLabel, memoryLabel                   *walk.Label
	feedback                                                         *walk.TextLabel
	settingsBody                                                     *walk.Composite
	saveButton, tunButton, proxyButton, startButton                  *walk.PushButton
	diagnosticsButton, deleteProfileButton                           *walk.PushButton
	localBody, remoteBody                                            *walk.Composite
	draftProfiles                                                    map[string]config.Profile
	editingProfile                                                   string
	filling                                                          bool
	lastRevision                                                     uint64
}

func Run(ctx context.Context, ctl *controller.Controller, base, exe, version string, restartCore bool) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	a := &app{ctx: ctx, cancel: cancel, controller: ctl, base: base, exe: exe, version: version, startup: map[string]*walk.Action{}}
	if err := a.createWindow(); err != nil {
		return err
	}
	defer a.window.Dispose()
	for i := range a.icons {
		icon, err := walk.NewIconFromResourceId(resources.IconIDs[i])
		if err != nil {
			return fmt.Errorf("加载程序图标失败，请使用 build.ps1 构建: %w", err)
		}
		a.icons[i] = icon
		defer icon.Dispose()
	}
	_ = a.window.SetIcon(a.icons[0])
	ni, err := walk.NewNotifyIcon(a.window)
	if err != nil {
		return err
	}
	a.tray = ni
	defer ni.Dispose()
	if err := a.buildTray(); err != nil {
		return err
	}
	if err := ni.SetIcon(a.icons[0]); err != nil {
		return err
	}
	if err := ni.SetVisible(true); err != nil {
		return err
	}
	ni.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			a.showSettings()
		}
	})
	ni.MessageClicked().Attach(a.showSettings)
	a.window.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		if !a.quitting {
			*canceled = true
			a.window.Hide()
		}
	})
	a.window.KeyDown().Attach(func(key walk.Key) {
		if key == walk.KeyEscape {
			a.window.Hide()
		}
	})
	a.fillSettings()
	a.render()
	firstRun := ctl.Snapshot().Settings.Validate(base) != nil
	// Queue startup after the native message loop has started.
	go a.window.Synchronize(func() {
		if firstRun {
			a.showSettings()
		}
		a.run("正在连接核心…", func(ctx context.Context) error {
			if restartCore {
				return ctl.RestartOnBoot(ctx)
			}
			return ctl.Boot(ctx)
		}, firstRun, func(err error) {
			// First-run path selection is guidance, not a startup error dialog.
			if firstRun {
				a.feedback.SetText("选择 mihomo 核心和配置文件，然后保存即可开始使用。")
			}
			a.fillStartup()
		})
	})
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.window.Synchronize(func() {
					a.backgroundRefresh()
				})
			}
		}
	}()
	a.window.Run()
	return nil
}

func (a *app) action(menu *walk.Menu, text string, checkable bool, fn func()) (*walk.Action, error) {
	x := walk.NewAction()
	if err := x.SetText(text); err != nil {
		return nil, err
	}
	if err := x.SetCheckable(checkable); err != nil {
		return nil, err
	}
	if fn != nil {
		x.Triggered().Attach(fn)
	}
	if err := menu.Actions().Add(x); err != nil {
		return nil, err
	}
	return x, nil
}

func (a *app) buildTray() error {
	m := a.tray.ContextMenu()
	var err error
	add := func(dst **walk.Action, text string, check bool, fn func()) {
		if err != nil {
			return
		}
		x, e := a.action(m, text, check, fn)
		err = e
		if dst != nil {
			*dst = x
		}
	}
	sep := func() {
		if err == nil {
			err = m.Actions().Add(walk.NewSeparatorAction())
		}
	}
	add(nil, "MiTray 设置…", false, a.showSettings)
	add(&a.web, "打开 WebUI", false, func() { a.open(a.controller.Snapshot().WebURL) })
	sep()
	add(&a.proxy, "系统代理", true, func() { a.run("正在切换系统代理…", a.controller.ToggleProxy, false, nil) })
	add(&a.tun, "TUN 模式", true, func() { a.run("正在切换 TUN…", a.controller.ToggleTUN, false, nil) })
	envMenu, err := walk.NewMenu()
	if err != nil {
		return err
	}
	envAction, err := m.Actions().AddMenu(envMenu)
	if err != nil {
		return err
	}
	envAction.SetText("复制代理环境变量")
	for _, item := range []struct{ shell, label string }{
		{proxyenv.ShellPowerShell, "PowerShell"},
		{proxyenv.ShellCmd, "CMD"},
		{proxyenv.ShellBash, "Bash"},
	} {
		x, e := a.action(envMenu, item.label, false, func() { a.copyEnv(item.shell, item.label) })
		if e != nil {
			return e
		}
		a.mutations = append(a.mutations, x)
	}
	add(&a.refresh, "刷新状态", false, func() { a.run("正在刷新状态…", a.controller.Refresh, false, nil) })
	add(&a.diagnostics, "核心诊断…", false, a.showDiagnostics)
	add(&a.forget, "清除 TUN 记忆", false, func() {
		a.run("正在清除 TUN 记忆…", func(context.Context) error { return a.controller.ForgetTUN() }, false, func(err error) {
			if err == nil {
				a.feedback.SetText("已清除 TUN 记忆，下次启动核心时遵循配置文件。")
			}
		})
	})
	sep()
	if err != nil {
		return err
	}
	a.profileMenu, err = walk.NewMenu()
	if err != nil {
		return err
	}
	a.profiles, err = m.Actions().AddMenu(a.profileMenu)
	if err != nil {
		return err
	}
	a.profiles.SetText("选择 mihomo 配置")
	sm, err := walk.NewMenu()
	if err != nil {
		return err
	}
	sa, err := m.Actions().AddMenu(sm)
	if err != nil {
		return err
	}
	sa.SetText("开机自启")
	for _, item := range []struct{ key, text string }{{"off", "关闭"}, {"normal", "普通权限"}, {"admin", "管理员权限"}} {
		level := item.key
		x, e := a.action(sm, item.text, true, func() {
			a.run("正在更新开机自启…", func(ctx context.Context) error { return a.controller.SetStartup(ctx, level) }, false, func(error) { a.fillStartup() })
		})
		if e != nil {
			return e
		}
		a.startup[level] = x
		a.mutations = append(a.mutations, x)
	}
	sep()
	add(nil, "打开程序目录", false, func() { a.open(a.base) })
	add(nil, "打开核心目录", false, func() {
		cfg := a.controller.Snapshot().Settings
		if cfg.CorePath != "" {
			a.open(filepath.Dir(cfg.Resolve(a.base, cfg.CorePath)))
		}
	})
	if !platform.IsAdmin() {
		add(nil, "以管理员身份重新启动…", false, a.elevate)
	}
	sep()
	add(&a.start, "启动核心", false, func() { a.run("正在启动核心…", a.controller.Start, false, nil) })
	add(&a.restart, "重启核心", false, func() { a.run("正在重启核心…", a.controller.Restart, false, nil) })
	add(&a.stop, "停止核心", false, func() { a.stopCore(false) })
	sep()
	add(nil, "退出 MiTray", false, a.quit)
	add(nil, "停止核心并退出", false, func() { a.stopCore(true) })
	return err
}

func (a *app) run(label string, op func(context.Context) error, silent bool, after func(error)) {
	if a.busy || a.quitting {
		return
	}
	a.busy = true
	if label != "" {
		a.feedback.SetText(label)
	}
	a.render()
	go func() {
		err := op(a.ctx)
		a.window.Synchronize(func() {
			if a.quitting {
				return
			}
			a.busy = false
			if err != nil {
				a.feedback.SetText(err.Error())
				if !silent || errors.Is(err, controller.ErrTUNRestore) {
					a.tray.ShowError("MiTray", err.Error())
				}
			} else {
				if label != "" {
					a.feedback.SetText("已完成")
				}
				if notice := a.controller.Snapshot().Notice; notice != "" {
					a.feedback.SetText(notice)
				}
			}
			a.render()
			if after != nil {
				after(err)
			}
		})
	}()
}

func (a *app) backgroundRefresh() {
	if a.busy || a.refreshing || a.quitting {
		return
	}
	a.refreshing = true
	go func() {
		err := a.controller.Refresh(a.ctx)
		a.window.Synchronize(func() {
			a.refreshing = false
			if a.quitting {
				return
			}
			if err != nil {
				if errors.Is(err, controller.ErrTUNRestore) {
					a.feedback.SetText(err.Error())
					a.tray.ShowError("MiTray", err.Error())
				} else if a.window.Visible() {
					a.feedback.SetText(err.Error())
				}
			}
			if s := a.controller.Snapshot(); s.Revision != a.lastRevision {
				a.render()
			}
		})
	}()
}

func (a *app) stopCore(exitAfter bool) {
	s := a.controller.Snapshot()
	if s.ProxyTargetsCore {
		message := fmt.Sprintf("系统代理仍开启并指向 %s。\n\n系统代理不会随核心关闭，停止后该代理端口将不可用。是否继续？", s.ProxyServer)
		if walk.MsgBox(a.window, "停止核心", message, walk.MsgBoxYesNo|walk.MsgBoxIconWarning) != walk.DlgCmdYes {
			return
		}
	}
	a.run("正在停止核心…", a.controller.Stop, false, func(err error) {
		if err == nil && exitAfter {
			a.quit()
		}
	})
}

func stateName(value *bool) string {
	if value == nil {
		return "未知"
	}
	if *value {
		return "已开启"
	}
	return "已关闭"
}

func (a *app) render() {
	if a.tray == nil {
		return
	}
	s := a.controller.Snapshot()
	core := "未运行"
	if s.Running {
		core = "运行中"
	}
	a.statusLabel.SetText(core)
	a.tunLabel.SetText(stateName(s.TUN))
	proxyState := stateName(s.Proxy)
	if s.Proxy != nil && *s.Proxy && s.ProxyTargetsCore {
		proxyState += " · 指向核心"
	}
	a.proxyLabel.SetText(proxyState)
	memory := "TUN 尚无记忆，下次启动遵循配置文件。"
	if s.Settings.TUNEnabled != nil {
		memory = "核心启动时的 TUN 记忆：" + stateName(s.Settings.TUNEnabled) + "。"
	}
	if s.ProxyTargetsCore {
		memory += " 系统代理当前指向核心端口，停止核心后仍会保持开启。"
		a.memoryLabel.SetTextColor(systemAccentColor())
	} else {
		a.memoryLabel.SetTextColor(systemMutedColor())
	}
	a.memoryLabel.SetText(memory)
	a.tun.SetText("TUN 模式 · " + stateName(s.TUN))
	a.tun.SetChecked(s.TUN != nil && *s.TUN)
	a.proxy.SetText("系统代理 · " + stateName(s.Proxy))
	a.proxy.SetChecked(s.Proxy != nil && *s.Proxy)
	a.tun.SetEnabled(!a.busy && s.Running && s.TUN != nil)
	a.proxy.SetEnabled(!a.busy && s.Proxy != nil && (s.Running || *s.Proxy))
	a.start.SetEnabled(!a.busy && !s.Running)
	a.restart.SetEnabled(!a.busy)
	a.stop.SetEnabled(!a.busy && s.Running)
	a.refresh.SetEnabled(!a.busy)
	a.forget.SetEnabled(!a.busy && s.Settings.TUNEnabled != nil)
	a.diagnostics.SetEnabled(!a.busy && s.Diagnostics != "")
	a.web.SetEnabled(s.Running && s.WebURL != "")
	for _, x := range a.mutations {
		x.SetEnabled(!a.busy)
	}
	for level, x := range a.startup {
		x.SetChecked(s.Settings.StartupLevel == level)
	}
	a.settingsBody.SetEnabled(!a.busy)
	a.saveButton.SetEnabled(!a.busy)
	a.diagnosticsButton.SetEnabled(!a.busy && s.Diagnostics != "")
	a.tunButton.SetEnabled(a.tun.Enabled())
	a.proxyButton.SetEnabled(a.proxy.Enabled())
	a.startButton.SetEnabled(!a.busy)
	if s.Running {
		a.startButton.SetText("重启核心")
	} else {
		a.startButton.SetText("启动核心")
	}
	a.renderProfiles(s)
	icon := a.icons[2]
	if s.Running {
		icon = a.icons[0]
		if (s.TUN != nil && *s.TUN) || (s.Proxy != nil && *s.Proxy) {
			icon = a.icons[1]
		}
	}
	a.tray.SetIcon(icon)
	a.tray.SetToolTip("MiTray · " + core + "\nTUN " + stateName(s.TUN) + " · 系统代理 " + stateName(s.Proxy))
	a.lastRevision = s.Revision
}

func (a *app) renderProfiles(s controller.Snapshot) {
	names := s.Settings.ProfileNames()
	sig := strings.Join(names, "\x00")
	if sig != a.profileSignature || a.profileMenu.Actions().Len() == 0 {
		a.profileMenu.Actions().Clear()
		a.profileActions = nil
		for _, name := range names {
			x, err := a.action(a.profileMenu, name, true, func() {
				a.run("正在切换配置…", func(ctx context.Context) error { return a.controller.SelectProfile(ctx, name) }, false, nil)
			})
			if err != nil {
				a.feedback.SetText("更新配置菜单失败: " + err.Error())
				continue
			}
			a.profileActions = append(a.profileActions, x)
		}
		if len(names) != 0 {
			a.profileMenu.Actions().Add(walk.NewSeparatorAction())
		}
		a.action(a.profileMenu, "添加本地配置…", false, func() { a.showSettings(); a.addLocalProfile() })
		a.profileSignature = sig
	}
	for _, x := range a.profileActions {
		x.SetChecked(x.Text() == s.Settings.ActiveProfile)
		x.SetEnabled(!a.busy)
	}
}

func (a *app) open(target string) {
	if target == "" {
		return
	}
	if err := platform.Open(target); err != nil {
		a.feedback.SetText("打开失败: " + err.Error())
		a.tray.ShowError("MiTray", "打开失败: "+err.Error())
	}
}

func (a *app) copyEnv(shell, label string) {
	var port int
	var command string
	a.run("正在准备环境变量…", func(ctx context.Context) error {
		p, err := a.controller.ProxyPort(ctx)
		if err != nil {
			return err
		}
		port, command = p, proxyenv.Script(shell, p)
		return nil
	}, false, func(err error) {
		if err != nil {
			return
		}
		if err := walk.Clipboard().SetText(command); err != nil {
			a.feedback.SetText("写入剪贴板失败: " + err.Error())
			a.tray.ShowError("MiTray", "写入剪贴板失败: "+err.Error())
			return
		}
		message := fmt.Sprintf("已复制 %s 代理环境变量（127.0.0.1:%d）。", label, port)
		a.feedback.SetText(message)
		a.tray.ShowInfo("MiTray", message)
	})
}

func (a *app) quit() {
	if a.busy {
		a.feedback.SetText("正在完成当前操作，请稍后退出。")
		return
	}
	a.quitting = true
	a.cancel()
	a.window.Close()
}

func (a *app) elevate() {
	if a.busy {
		return
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	exe, _ := windows.UTF16PtrFromString(a.exe)
	args, _ := windows.UTF16PtrFromString("--restart-core")
	dir, _ := windows.UTF16PtrFromString(a.base)
	if err := windows.ShellExecute(windows.Handle(a.window.Handle()), verb, exe, args, dir, windows.SW_SHOWNORMAL); err != nil {
		a.feedback.SetText("未重新启动: " + err.Error())
		return
	}
	a.quit()
}

func (a *app) showSettings() {
	if !a.window.Visible() {
		if !a.dirty {
			a.fillSettings()
		}
		a.window.Show()
		win.ShowWindow(a.window.Handle(), win.SW_RESTORE)
		if err := CenterSettingsWindow(a.window.Handle()); err != nil {
			a.feedback.SetText(err.Error())
		}
	} else if win.IsIconic(a.window.Handle()) {
		win.ShowWindow(a.window.Handle(), win.SW_RESTORE)
	}
	win.SetForegroundWindow(a.window.Handle())
}
