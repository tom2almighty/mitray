package ui

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"github.com/tom2almighty/mitray/internal/config"
	"github.com/tom2almighty/mitray/internal/controller"
	"github.com/tom2almighty/mitray/internal/i18n"
	"github.com/tom2almighty/mitray/internal/platform"
	"github.com/tom2almighty/mitray/internal/proxyenv"
	"github.com/tom2almighty/mitray/internal/resources"
	"golang.org/x/sys/windows"
)

type app struct {
	ctx                                                          context.Context
	cancel                                                       context.CancelFunc
	opCancel                                                     context.CancelFunc
	refreshCancel                                                context.CancelFunc
	controller                                                   *controller.Controller
	base, exe, version                                           string
	window                                                       *walk.MainWindow
	tray                                                         *walk.NotifyIcon
	icons                                                        [3]*walk.Icon
	busy                                                         bool
	refreshing                                                   bool
	stopping                                                     bool
	dirty                                                        bool
	quitting                                                     bool
	start, stop, restart, tun, proxy, web, profiles              *walk.Action
	openConfig, relaunchAction, stopQuitAction                   *walk.Action
	startup                                                      map[string]*walk.Action
	mutations                                                    []*walk.Action
	profileActions                                               []*walk.Action
	profileMenu                                                  *walk.Menu
	profileSignature                                             string
	coreEdit, profileNameEdit, localEdit, urlEdit                *walk.LineEdit
	profileCombo, sourceCombo, startupCombo, languageCombo       *walk.ComboBox
	autoStart                                                    *walk.CheckBox
	delay                                                        *walk.NumberEdit
	statusLabel, tunLabel, proxyLabel, hintLabel                 *walk.Label
	feedback                                                     *walk.TextLabel
	settingsBody                                                 *walk.Composite
	saveButton, tunButton, proxyButton, startButton              *walk.PushButton
	openConfigButton, activateProfileButton, deleteProfileButton *walk.PushButton
	localBody, remoteBody                                        *walk.Composite
	draftProfiles                                                map[string]config.Profile
	draftActiveProfile                                           string
	editingProfile                                               string
	filling                                                      bool
	lastRevision                                                 uint64
}

func Run(ctx context.Context, ctl *controller.Controller, base, exe, version string, restartCore, showSettings bool) error {
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
			return fmt.Errorf(i18n.T().ErrLoadIcon, err)
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
		if firstRun || showSettings {
			a.showSettings()
		}
		a.run(i18n.T().BusyConnecting, func(ctx context.Context) error {
			if restartCore {
				return ctl.RestartOnBoot(ctx)
			}
			return ctl.Boot(ctx)
		}, firstRun, func(err error) {
			// First-run path selection is guidance, not a startup error dialog.
			if firstRun {
				a.feedback.SetText(i18n.T().FirstRunGuidance)
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
	t := i18n.T()
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
	add(nil, t.TraySettings, false, a.showSettings)
	add(&a.web, t.TrayOpenWebUI, false, func() { a.open(a.controller.Snapshot().WebURL) })
	sep()
	add(&a.proxy, t.TraySystemProxy, true, func() { a.run(i18n.T().BusyToggleProxy, a.controller.ToggleProxy, false, nil) })
	add(&a.tun, t.TrayTUNMode, true, func() { a.run(i18n.T().BusyToggleTUN, a.controller.ToggleTUN, false, nil) })
	envMenu, err := walk.NewMenu()
	if err != nil {
		return err
	}
	envAction, err := m.Actions().AddMenu(envMenu)
	if err != nil {
		return err
	}
	envAction.SetText(t.TrayCopyProxyEnv)
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
	a.profiles.SetText(t.TraySelectProfile)
	sm, err := walk.NewMenu()
	if err != nil {
		return err
	}
	sa, err := m.Actions().AddMenu(sm)
	if err != nil {
		return err
	}
	sa.SetText(t.TrayStartup)
	for _, item := range []struct{ key, text string }{
		{"off", t.StartupOff},
		{"normal", t.StartupNormal},
		{"admin", t.StartupAdmin},
	} {
		level := item.key
		x, e := a.action(sm, item.text, true, func() {
			a.run(i18n.T().BusyUpdateStartup, func(ctx context.Context) error { return a.controller.SetStartup(ctx, level, -1) }, false, func(error) { a.fillStartup() })
		})
		if e != nil {
			return e
		}
		a.startup[level] = x
		a.mutations = append(a.mutations, x)
	}
	sep()
	add(nil, t.TrayOpenAppDir, false, func() { a.open(a.base) })
	add(nil, t.TrayOpenCoreDir, false, func() {
		cfg := a.controller.Snapshot().Settings
		if cfg.CorePath != "" {
			a.open(filepath.Dir(cfg.Resolve(a.base, cfg.CorePath)))
		}
	})
	add(&a.openConfig, t.TrayOpenConfig, false, a.openActiveConfig)
	if !platform.IsAdmin() {
		add(nil, t.TrayRestartAdmin, false, a.elevate)
	}
	sep()
	add(&a.start, t.TrayStartCore, false, func() { a.run(i18n.T().BusyStartCore, a.controller.Start, false, nil) })
	add(&a.restart, t.TrayRestartCore, false, func() { a.run(i18n.T().BusyRestartCore, a.controller.Restart, false, nil) })
	add(&a.stop, t.TrayStopCore, false, func() { a.stopCore(false) })
	sep()
	add(&a.relaunchAction, t.TrayRestartApp, false, func() { a.relaunch(false) })
	add(&a.stopQuitAction, t.TrayStopAndQuit, false, func() { a.stopCore(true) })
	return err
}

func (a *app) run(label string, op func(context.Context) error, silent bool, after func(error)) {
	if a.busy || a.stopping || a.quitting {
		return
	}
	a.busy = true
	opCtx, opCancel := context.WithCancel(a.ctx)
	a.opCancel = opCancel
	if label != "" {
		a.feedback.SetText(label)
	}
	a.render()
	go func() {
		err := op(opCtx)
		a.window.Synchronize(func() {
			opCancel()
			a.opCancel = nil
			if a.quitting {
				return
			}
			a.busy = false
			if a.stopping {
				a.render()
				return
			}
			if err != nil {
				a.feedback.SetText(err.Error())
				if !silent {
					a.tray.ShowError("MiTray", err.Error())
				}
			} else {
				if label != "" {
					a.feedback.SetText(i18n.T().DoneText)
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
	if a.busy || a.refreshing || a.stopping || a.quitting {
		return
	}
	a.refreshing = true
	refreshCtx, refreshCancel := context.WithCancel(a.ctx)
	a.refreshCancel = refreshCancel
	go func() {
		err := a.controller.Refresh(refreshCtx)
		a.window.Synchronize(func() {
			refreshCancel()
			a.refreshCancel = nil
			a.refreshing = false
			if a.quitting || a.stopping {
				return
			}
			if err != nil && a.window.Visible() {
				a.feedback.SetText(err.Error())
			}
			if s := a.controller.Snapshot(); s.Revision != a.lastRevision {
				a.render()
			}
		})
	}()
}

func (a *app) stopCore(exitAfter bool) {
	if a.quitting || a.stopping {
		return
	}
	if !exitAfter && a.busy {
		return
	}
	t := i18n.T()
	s := a.controller.Snapshot()
	disableProxy := false
	if s.ProxyTargetsCore {
		message := fmt.Sprintf(t.DlgStopProxyConfirm, s.ProxyServer)
		switch walk.MsgBox(a.window, t.DlgStopCoreTitle, message, walk.MsgBoxYesNoCancel|walk.MsgBoxIconWarning|walk.MsgBoxSetForeground) {
		case walk.DlgCmdYes:
			disableProxy = true
		case walk.DlgCmdNo:
		default:
			return
		}
	}
	op := a.controller.Stop
	label := t.BusyStopCore
	if disableProxy {
		op = a.controller.StopAndDisableProxy
		label = t.BusyStopProxyAndCore
	}
	if !exitAfter {
		a.run(label, op, false, nil)
		return
	}
	a.stopping = true
	if a.opCancel != nil {
		a.opCancel()
	}
	if a.refreshCancel != nil {
		a.refreshCancel()
	}
	a.feedback.SetText(label)
	a.render()
	a.tray.SetToolTip(fmt.Sprintf(t.TooltipStoppingFormat, label))
	go func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := op(stopCtx)
		stopCancel()
		a.window.Synchronize(func() {
			if a.quitting {
				return
			}
			a.stopping = false
			if err == nil {
				a.quitNow()
				return
			}
			a.feedback.SetText(err.Error())
			a.render()
			if walk.MsgBox(a.window, t.DlgStopFailedTitle, fmt.Sprintf(t.DlgStopFailedAskQuit, err.Error()), walk.MsgBoxYesNo|walk.MsgBoxIconError|walk.MsgBoxSetForeground) == walk.DlgCmdYes {
				a.quitNow()
			}
		})
	}()
}

func stateName(value *bool) string {
	t := i18n.T()
	if value == nil {
		return t.StateUnknown
	}
	if *value {
		return t.StateOn
	}
	return t.StateOff
}

func (a *app) render() {
	if a.tray == nil {
		return
	}
	t := i18n.T()
	s := a.controller.Snapshot()
	core := t.CoreStopped
	if s.Running {
		core = t.CoreRunning
	}
	a.statusLabel.SetText(core)
	a.tunLabel.SetText(stateName(s.TUN))
	proxyState := stateName(s.Proxy)
	if s.Proxy != nil && *s.Proxy && s.ProxyTargetsCore {
		proxyState += t.ProxyTargetsCoreSuffix
	}
	a.proxyLabel.SetText(proxyState)
	hint := t.HintDefault
	accentHint := false
	if s.ConfigTUNEnabled && !platform.IsAdmin() {
		hint = t.HintTUNNeedsAdmin
		accentHint = true
	}
	if s.ProxyTargetsCore {
		hint += t.HintProxyTargetsCore
		accentHint = true
	}
	if accentHint {
		a.hintLabel.SetTextColor(systemAccentColor())
	} else {
		a.hintLabel.SetTextColor(systemMutedColor())
	}
	a.hintLabel.SetText(hint)
	blocked := a.busy || a.stopping
	_, hasActiveProfile := s.Settings.ActiveProfileConfig()
	a.tun.SetText(t.TrayTUNMode + " · " + stateName(s.TUN))
	a.tun.SetChecked(s.TUN != nil && *s.TUN)
	a.proxy.SetText(t.TraySystemProxy + " · " + stateName(s.Proxy))
	a.proxy.SetChecked(s.Proxy != nil && *s.Proxy)
	a.tun.SetEnabled(!blocked && s.Running && s.TUN != nil)
	a.proxy.SetEnabled(!blocked && s.Proxy != nil && (s.Running || *s.Proxy))
	a.start.SetEnabled(!blocked && !s.Running)
	a.restart.SetEnabled(!blocked)
	a.stop.SetEnabled(!blocked && s.Running)
	a.openConfig.SetEnabled(!a.stopping && hasActiveProfile)
	a.relaunchAction.SetEnabled(!blocked)
	a.stopQuitAction.SetEnabled(!a.stopping)
	a.web.SetEnabled(!a.stopping && s.Running && s.WebURL != "")
	for _, x := range a.mutations {
		x.SetEnabled(!blocked)
	}
	for level, x := range a.startup {
		x.SetChecked(s.StartupLevel == level)
	}
	a.settingsBody.SetEnabled(!blocked)
	a.startupCombo.SetEnabled(!blocked && s.StartupLevel != "")
	a.delay.SetEnabled(!blocked && s.StartupLevel != "")
	a.saveButton.SetEnabled(!blocked)
	a.tunButton.SetEnabled(a.tun.Enabled())
	a.proxyButton.SetEnabled(a.proxy.Enabled())
	a.openConfigButton.SetEnabled(!blocked && hasActiveProfile)
	a.startButton.SetEnabled(!blocked)
	if s.Running {
		a.startButton.SetText(t.BtnRestartCore)
	} else {
		a.startButton.SetText(t.BtnStartCore)
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
	a.tray.SetToolTip(fmt.Sprintf(t.TooltipFormat, core, stateName(s.TUN), stateName(s.Proxy)))
	a.lastRevision = s.Revision
}

func (a *app) renderProfiles(s controller.Snapshot) {
	t := i18n.T()
	names := s.Settings.ProfileNames()
	sig := strings.Join(names, "\x00")
	if sig != a.profileSignature || a.profileMenu.Actions().Len() == 0 {
		a.profileMenu.Actions().Clear()
		a.profileActions = nil
		for _, name := range names {
			x, err := a.action(a.profileMenu, name, true, func() {
				a.run(i18n.T().BusySwitchProfile, func(ctx context.Context) error { return a.controller.SelectProfile(ctx, name) }, false, nil)
			})
			if err != nil {
				a.feedback.SetText(fmt.Sprintf(t.ErrUpdateProfileMenu, err.Error()))
				continue
			}
			a.profileActions = append(a.profileActions, x)
		}
		if len(names) != 0 {
			a.profileMenu.Actions().Add(walk.NewSeparatorAction())
		}
		a.action(a.profileMenu, t.TrayAddLocalProfile, false, func() { a.showSettings(); a.addLocalProfile() })
		a.profileSignature = sig
	}
	for _, x := range a.profileActions {
		x.SetChecked(x.Text() == s.Settings.ActiveProfile)
		x.SetEnabled(!a.busy && !a.stopping)
	}
}

func (a *app) openActiveConfig() {
	path, err := a.controller.ActiveConfigPath()
	if err != nil {
		a.feedback.SetText(err.Error())
		a.tray.ShowError("MiTray", err.Error())
		return
	}
	a.open(path)
}

func (a *app) open(target string) {
	if target == "" {
		return
	}
	if err := platform.Open(target); err != nil {
		msg := fmt.Sprintf(i18n.T().ErrOpenFailed, err.Error())
		a.feedback.SetText(msg)
		a.tray.ShowError("MiTray", msg)
	}
}

func (a *app) copyEnv(shell, label string) {
	var port int
	var command string
	a.run(i18n.T().BusyPrepareEnv, func(ctx context.Context) error {
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
			msg := fmt.Sprintf(i18n.T().ErrClipboardFailed, err.Error())
			a.feedback.SetText(msg)
			a.tray.ShowError("MiTray", msg)
			return
		}
		message := fmt.Sprintf(i18n.T().CopiedProxyEnv, label, port)
		a.feedback.SetText(message)
		a.tray.ShowInfo("MiTray", message)
	})
}

func (a *app) relaunch(showSettings bool) {
	if a.busy || a.stopping || a.quitting {
		return
	}
	args := []string{"--relaunch"}
	if showSettings {
		args = append(args, "--show-settings")
	}
	cmd := exec.Command(a.exe, args...)
	cmd.Dir = a.base
	if err := cmd.Start(); err != nil {
		msg := fmt.Sprintf(i18n.T().ErrRelaunchFailed, err.Error())
		a.feedback.SetText(msg)
		a.tray.ShowError("MiTray", msg)
		return
	}
	_ = cmd.Process.Release()
	a.quitNow()
}

func (a *app) quitNow() {
	if a.quitting {
		return
	}
	a.quitting = true
	if a.opCancel != nil {
		a.opCancel()
	}
	if a.refreshCancel != nil {
		a.refreshCancel()
	}
	a.cancel()
	if a.window != nil {
		a.window.Hide()
	}
	if a.tray != nil {
		_ = a.tray.SetVisible(false)
		_ = a.tray.Dispose()
	}
	walk.App().Exit(0)
}

func (a *app) elevate() {
	if a.busy || a.stopping || a.quitting {
		return
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	exe, _ := windows.UTF16PtrFromString(a.exe)
	args, _ := windows.UTF16PtrFromString("--restart-core")
	dir, _ := windows.UTF16PtrFromString(a.base)
	if err := windows.ShellExecute(windows.Handle(a.window.Handle()), verb, exe, args, dir, windows.SW_SHOWNORMAL); err != nil {
		a.feedback.SetText(fmt.Sprintf(i18n.T().ErrElevateFailed, err.Error()))
		return
	}
	a.quitNow()
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
	a.backgroundRefresh()
}
