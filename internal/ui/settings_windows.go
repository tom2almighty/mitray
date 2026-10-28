package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unsafe"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	"github.com/tom2almighty/mitray/internal/config"
	"github.com/tom2almighty/mitray/internal/platform"
	"golang.org/x/sys/windows"
)

// CenterSettingsWindow is shared with the second-instance activation path.
func CenterSettingsWindow(hwnd win.HWND) error {
	var cursor win.POINT
	if !win.GetCursorPos(&cursor) {
		return fmt.Errorf("无法获取鼠标位置，设置窗口未居中")
	}
	point := win.RECT{Left: cursor.X, Top: cursor.Y, Right: cursor.X + 1, Bottom: cursor.Y + 1}
	monitor, _, err := windows.NewLazySystemDLL("user32.dll").NewProc("MonitorFromRect").Call(
		uintptr(unsafe.Pointer(&point)), win.MONITOR_DEFAULTTONEAREST)
	if monitor == 0 {
		return fmt.Errorf("无法获取鼠标所在显示器: %w", err)
	}
	info := win.MONITORINFO{CbSize: uint32(unsafe.Sizeof(win.MONITORINFO{}))}
	if !win.GetMonitorInfo(win.HMONITOR(monitor), &info) {
		return fmt.Errorf("无法获取显示器工作区，设置窗口未居中")
	}
	// Moving across monitors can change DPI and resize the window synchronously.
	for pass := 0; pass < 2; pass++ {
		var bounds win.RECT
		if !win.GetWindowRect(hwnd, &bounds) {
			return fmt.Errorf("无法获取设置窗口尺寸")
		}
		work := info.RcWork
		x := work.Left + max(0, (work.Right-work.Left-(bounds.Right-bounds.Left))/2)
		y := work.Top + max(0, (work.Bottom-work.Top-(bounds.Bottom-bounds.Top))/2)
		if !win.SetWindowPos(hwnd, 0, x, y, 0, 0, win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE) {
			return fmt.Errorf("无法将设置窗口居中")
		}
	}
	return nil
}

func systemMessageFont(bold bool) Font {
	var metrics win.NONCLIENTMETRICS
	metrics.CbSize = uint32(unsafe.Sizeof(metrics))
	if !win.SystemParametersInfo(win.SPI_GETNONCLIENTMETRICS, metrics.CbSize, unsafe.Pointer(&metrics), 0) {
		return Font{Bold: bold}
	}
	dpi := int32(96)
	if dc := win.GetDC(0); dc != 0 {
		if value := win.GetDeviceCaps(dc, win.LOGPIXELSY); value > 0 {
			dpi = value
		}
		win.ReleaseDC(0, dc)
	}
	pointSize := int(win.MulDiv(-metrics.LfMessageFont.LfHeight, 72, dpi))
	if pointSize < 8 || pointSize > 16 {
		pointSize = 9
	}
	return Font{
		Family:    windows.UTF16ToString(metrics.LfMessageFont.LfFaceName[:]),
		PointSize: pointSize,
		Bold:      bold,
	}
}

func systemTitleFont() Font {
	font := systemMessageFont(true)
	font.PointSize = 22
	return font
}

func systemTextColor() walk.Color {
	return walk.Color(win.GetSysColor(win.COLOR_WINDOWTEXT))
}

func systemMutedColor() walk.Color {
	return walk.Color(win.GetSysColor(win.COLOR_GRAYTEXT))
}

func systemAccentColor() walk.Color {
	return walk.Color(win.GetSysColor(win.COLOR_HOTLIGHT))
}

func statusBlock(caption string, target **walk.Label, font Font) Widget {
	return Composite{
		Layout:        VBox{MarginsZero: true, Spacing: 3},
		StretchFactor: 1,
		Children: []Widget{
			Label{Text: caption, TextColor: systemMutedColor()},
			Label{AssignTo: target, Text: "未知", Font: font, TextColor: systemTextColor()},
		},
	}
}

func (a *app) createWindow() error {
	font := systemMessageFont(false)
	boldFont := systemMessageFont(true)
	return (MainWindow{
		AssignTo: &a.window, Title: "MiTray 设置", Visible: false,
		MinSize: Size{Width: 680, Height: 680}, Size: Size{Width: 740, Height: 740},
		Font:   font,
		Layout: VBox{Margins: Margins{Left: 24, Top: 20, Right: 24, Bottom: 20}, Spacing: 14},
		Children: []Widget{
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				Label{Text: "MiTray", Font: systemTitleFont()},
				HSpacer{}, Label{Text: "Windows · " + a.version, TextColor: systemMutedColor()},
			}},
			GroupBox{Title: "当前状态", Layout: VBox{Spacing: 12}, Children: []Widget{
				Composite{Layout: HBox{MarginsZero: true, Spacing: 14}, Children: []Widget{
					statusBlock("核心", &a.statusLabel, boldFont),
					VSeparator{},
					statusBlock("TUN", &a.tunLabel, boldFont),
					VSeparator{},
					statusBlock("系统代理", &a.proxyLabel, boldFont),
				}},
				Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
					PushButton{AssignTo: &a.startButton, Text: "启动核心", OnClicked: func() {
						if a.controller.Snapshot().Running {
							a.run("正在重启核心…", a.controller.Restart, false, nil)
						} else {
							a.run("正在启动核心…", a.controller.Start, false, nil)
						}
					}},
					PushButton{AssignTo: &a.tunButton, Text: "切换 TUN", OnClicked: func() { a.run("正在切换 TUN…", a.controller.ToggleTUN, false, nil) }},
					PushButton{AssignTo: &a.proxyButton, Text: "切换系统代理", OnClicked: func() { a.run("正在切换系统代理…", a.controller.ToggleProxy, false, nil) }},
					PushButton{AssignTo: &a.diagnosticsButton, Text: "核心诊断…", OnClicked: a.showDiagnostics},
					HSpacer{},
				}},
				Label{AssignTo: &a.memoryLabel, Text: "TUN 状态会在成功切换后自动记忆。", TextColor: systemMutedColor()},
			}},
			Composite{AssignTo: &a.settingsBody, Layout: VBox{MarginsZero: true, Spacing: 14}, Children: []Widget{
				GroupBox{Title: "核心与配置", Layout: VBox{Spacing: 10}, Children: []Widget{
					Label{Text: "mihomo 核心程序"},
					Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
						LineEdit{AssignTo: &a.coreEdit, StretchFactor: 1, CueBanner: "选择 mihomo.exe", OnTextChanged: a.markDirty},
						PushButton{Text: "浏览…", OnClicked: func() { a.browse(a.coreEdit, "选择 mihomo 核心", "可执行文件 (*.exe)|*.exe") }},
					}},
					Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
						Label{Text: "配置档案"},
						ComboBox{AssignTo: &a.profileCombo, StretchFactor: 1, OnCurrentIndexChanged: a.profileChanged},
						PushButton{AssignTo: &a.deleteProfileButton, Text: "删除…", OnClicked: a.deleteProfile},
					}},
					Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
						Label{Text: "配置名称"},
						LineEdit{AssignTo: &a.profileNameEdit, StretchFactor: 1, OnTextChanged: a.markDirty},
						Label{Text: "来源"},
						ComboBox{AssignTo: &a.sourceCombo, Model: []string{"本地文件", "远程 URL"}, CurrentIndex: 0, OnCurrentIndexChanged: a.sourceChanged},
					}},
					Composite{AssignTo: &a.localBody, Layout: VBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
						Label{Text: "本地 YAML 文件"},
						Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
							LineEdit{AssignTo: &a.localEdit, StretchFactor: 1, CueBanner: "选择 mihomo YAML 配置", OnTextChanged: a.markDirty},
							PushButton{Text: "浏览…", OnClicked: func() {
								a.browse(a.localEdit, "选择 mihomo 配置", "YAML 配置 (*.yaml;*.yml)|*.yaml;*.yml|所有文件 (*.*)|*.*")
							}},
						}},
					}},
					Composite{AssignTo: &a.remoteBody, Layout: VBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
						Label{Text: "远程 URL"},
						LineEdit{AssignTo: &a.urlEdit, CueBanner: "https://example.com/config.yaml", OnTextChanged: a.markDirty},
						Label{Text: "远程配置始终直连下载；下载或内容校验失败时使用上次成功缓存。", TextColor: systemMutedColor()},
					}},
					Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
						PushButton{Text: "添加本地配置…", OnClicked: a.addLocalProfile},
						PushButton{Text: "添加远程配置…", OnClicked: a.addRemoteProfile},
						HSpacer{},
					}},
				}},
				GroupBox{Title: "启动", Layout: VBox{Spacing: 10}, Children: []Widget{
					CheckBox{AssignTo: &a.autoStart, Text: "打开 MiTray 时自动启动 mihomo", OnCheckedChanged: a.markDirty},
					Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
						Label{Text: "开机自启"},
						ComboBox{AssignTo: &a.startupCombo, Model: []string{"关闭", "普通权限", "管理员权限"}, CurrentIndex: 0, OnCurrentIndexChanged: a.markDirty},
						HSpacer{}, Label{Text: "登录后延迟"},
						NumberEdit{AssignTo: &a.delay, Decimals: 0, MinValue: 0, MaxValue: 600, Value: 15, MinSize: Size{Width: 70}, MaxSize: Size{Width: 85}, OnValueChanged: a.markDirty},
						Label{Text: "秒"},
					}},
					Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
						Label{Text: "TUN 需要 mihomo 以管理员权限运行。", TextColor: systemMutedColor()},
						HSpacer{}, PushButton{Text: "以管理员身份重启", Visible: !platform.IsAdmin(), OnClicked: a.elevate},
					}},
				}},
			}},
			VSpacer{},
			TextLabel{AssignTo: &a.feedback, MinSize: Size{Width: 600, Height: 42}, Text: "修改核心或配置档案后，保存会重新启动正在运行的核心。", TextColor: systemAccentColor()},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				Label{Text: "关闭窗口后继续在托盘运行，未保存的修改会保留。", TextColor: systemMutedColor()}, HSpacer{},
				PushButton{Text: "收起", OnClicked: func() { a.window.Hide() }},
				PushButton{AssignTo: &a.saveButton, Text: "保存并应用", MinSize: Size{Width: 110, Height: 32}, OnClicked: a.saveSettings},
			}},
		},
	}).Create()
}

func (a *app) fillSettings() {
	if a.coreEdit == nil {
		return
	}
	a.filling = true
	defer func() {
		a.filling = false
		a.dirty = false
		a.updateSource()
	}()
	s := a.controller.Snapshot().Settings
	a.draftProfiles = s.Clone().Profiles
	a.coreEdit.SetText(s.CorePath)
	a.autoStart.SetChecked(s.AutoStartCore)
	a.delay.SetValue(float64(s.StartupDelay))
	a.fillStartup()
	a.refreshProfileModel(s.ActiveProfile)
	if profile, ok := a.draftProfiles[s.ActiveProfile]; ok {
		a.loadProfile(s.ActiveProfile, profile)
	} else {
		a.editingProfile = ""
		a.profileNameEdit.SetText("")
		a.localEdit.SetText("")
		a.urlEdit.SetText("")
		a.updateProfileControls()
	}
}

func (a *app) fillStartup() {
	wasFilling := a.filling
	a.filling = true
	defer func() { a.filling = wasFilling }()
	idx := 0
	switch a.controller.Snapshot().Settings.StartupLevel {
	case "normal":
		idx = 1
	case "admin":
		idx = 2
	}
	a.startupCombo.SetCurrentIndex(idx)
}

func (a *app) markDirty() {
	if a.filling {
		return
	}
	if !a.dirty {
		a.feedback.SetText("有未保存的修改。")
	}
	a.dirty = true
}

func (a *app) sourceChanged() {
	a.updateSource()
	a.markDirty()
}

func (a *app) updateSource() {
	if a.localBody == nil || a.remoteBody == nil {
		return
	}
	local := a.sourceCombo.CurrentIndex() != 1
	a.localBody.SetVisible(local)
	a.remoteBody.SetVisible(!local)
}

func (a *app) updateProfileControls() {
	if a.profileNameEdit == nil {
		return
	}
	hasProfile := a.editingProfile != ""
	a.profileNameEdit.SetEnabled(hasProfile)
	a.sourceCombo.SetEnabled(hasProfile)
	a.localEdit.SetEnabled(hasProfile)
	a.urlEdit.SetEnabled(hasProfile)
	a.deleteProfileButton.SetEnabled(hasProfile && !a.busy)
}

func (a *app) refreshProfileModel(selected string) {
	names := make([]string, 0, len(a.draftProfiles))
	for name := range a.draftProfiles {
		names = append(names, name)
	}
	sort.Strings(names)
	index := -1
	for i, name := range names {
		if name == selected {
			index = i
			break
		}
	}
	wasFilling := a.filling
	a.filling = true
	a.profileCombo.SetModel(names)
	if index >= 0 {
		a.profileCombo.SetCurrentIndex(index)
	} else {
		a.profileCombo.SetCurrentIndex(-1)
	}
	a.filling = wasFilling
}

func (a *app) loadProfile(name string, profile config.Profile) {
	wasFilling := a.filling
	a.filling = true
	a.editingProfile = name
	a.profileNameEdit.SetText(name)
	if profile.Type == config.ProfileURL {
		a.sourceCombo.SetCurrentIndex(1)
	} else {
		a.sourceCombo.SetCurrentIndex(0)
	}
	a.localEdit.SetText(profile.Path)
	a.urlEdit.SetText(profile.URL)
	a.filling = wasFilling
	a.updateSource()
	a.updateProfileControls()
	a.refreshProfileModel(name)
}

func (a *app) commitCurrentProfile() error {
	if a.editingProfile == "" {
		return nil
	}
	name := strings.TrimSpace(a.profileNameEdit.Text())
	if err := config.ValidateProfileName(name); err != nil {
		return err
	}
	if name != a.editingProfile {
		if _, exists := a.draftProfiles[name]; exists {
			return fmt.Errorf("配置名称 %q 已存在", name)
		}
		delete(a.draftProfiles, a.editingProfile)
	}
	profile := config.Profile{Type: config.ProfileLocal, Path: strings.TrimSpace(a.localEdit.Text())}
	if a.sourceCombo.CurrentIndex() == 1 {
		profile = config.Profile{Type: config.ProfileURL, URL: strings.TrimSpace(a.urlEdit.Text())}
	}
	a.draftProfiles[name] = profile
	a.editingProfile = name
	a.refreshProfileModel(name)
	return nil
}

func (a *app) profileChanged() {
	if a.filling || a.localEdit == nil {
		return
	}
	name := a.profileCombo.Text()
	if name == "" || name == a.editingProfile {
		return
	}
	if err := a.commitCurrentProfile(); err != nil {
		a.refreshProfileModel(a.editingProfile)
		a.feedback.SetText(err.Error())
		return
	}
	profile, ok := a.draftProfiles[name]
	if !ok {
		return
	}
	a.loadProfile(name, profile)
	a.markDirty()
}

func (a *app) uniqueProfileName(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "config"
	}
	name := base
	for i := 2; ; i++ {
		if _, exists := a.draftProfiles[name]; !exists {
			return name
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
}

func (a *app) addLocalProfile() {
	if a.busy {
		return
	}
	if err := a.commitCurrentProfile(); err != nil {
		a.feedback.SetText(err.Error())
		return
	}
	dlg := walk.FileDialog{Title: "添加 mihomo 配置", Filter: "YAML 配置 (*.yaml;*.yml)|*.yaml;*.yml|所有文件 (*.*)|*.*"}
	ok, err := dlg.ShowOpen(a.window)
	if err != nil {
		a.feedback.SetText(err.Error())
		return
	}
	if !ok {
		return
	}
	base := strings.TrimSuffix(filepath.Base(dlg.FilePath), filepath.Ext(dlg.FilePath))
	name := a.uniqueProfileName(base)
	profile := config.Profile{Type: config.ProfileLocal, Path: dlg.FilePath}
	a.draftProfiles[name] = profile
	a.loadProfile(name, profile)
	a.markDirty()
	a.feedback.SetText("已添加本地配置，点击「保存并应用」后生效。")
}

func (a *app) addRemoteProfile() {
	if a.busy {
		return
	}
	if err := a.commitCurrentProfile(); err != nil {
		a.feedback.SetText(err.Error())
		return
	}
	name := a.uniqueProfileName("remote")
	profile := config.Profile{Type: config.ProfileURL}
	a.draftProfiles[name] = profile
	a.loadProfile(name, profile)
	a.markDirty()
	a.feedback.SetText("已添加远程配置，请填写 URL 后保存。")
}

func (a *app) deleteProfile() {
	if a.busy || a.editingProfile == "" {
		return
	}
	if err := a.commitCurrentProfile(); err != nil {
		a.feedback.SetText(err.Error())
		return
	}
	if len(a.draftProfiles) <= 1 {
		a.feedback.SetText("至少保留一个配置档案。")
		return
	}
	name := a.editingProfile
	if walk.MsgBox(a.window, "删除配置档案", fmt.Sprintf("删除配置档案 %q？", name),
		walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
		return
	}
	delete(a.draftProfiles, name)
	names := make([]string, 0, len(a.draftProfiles))
	for next := range a.draftProfiles {
		names = append(names, next)
	}
	sort.Strings(names)
	next := names[0]
	a.loadProfile(next, a.draftProfiles[next])
	a.markDirty()
}

func (a *app) browse(edit *walk.LineEdit, title, filter string) {
	if a.busy {
		return
	}
	if edit != a.coreEdit && a.editingProfile == "" {
		a.feedback.SetText("请先添加一个配置档案。")
		return
	}
	dlg := walk.FileDialog{Title: title, Filter: filter, FilePath: edit.Text()}
	ok, err := dlg.ShowOpen(a.window)
	if err != nil {
		a.feedback.SetText(err.Error())
		return
	}
	if ok {
		edit.SetText(dlg.FilePath)
	}
}

func (a *app) saveSettings() {
	if a.busy {
		return
	}
	if err := a.commitCurrentProfile(); err != nil {
		a.feedback.SetText(err.Error())
		return
	}
	if a.editingProfile == "" {
		a.feedback.SetText("请先添加一个配置档案。")
		return
	}
	s := a.controller.Snapshot().Settings
	s.CorePath = strings.TrimSpace(a.coreEdit.Text())
	s.AutoStartCore = a.autoStart.Checked()
	s.StartupDelay = int(a.delay.Value())
	levels := []string{"off", "normal", "admin"}
	idx := a.startupCombo.CurrentIndex()
	if idx < 0 {
		idx = 0
	}
	s.StartupLevel = levels[idx]
	s.Profiles = make(map[string]config.Profile, len(a.draftProfiles))
	for name, profile := range a.draftProfiles {
		s.Profiles[name] = profile
	}
	s.ActiveProfile = a.editingProfile
	for name, profile := range s.Profiles {
		if err := profile.Validate(a.base); err != nil {
			a.feedback.SetText(fmt.Sprintf("配置档案 %q 无效: %v", name, err))
			return
		}
	}
	if err := s.Validate(a.base); err != nil {
		a.feedback.SetText(err.Error())
		return
	}
	a.run("正在保存并应用…", func(ctx context.Context) error { return a.controller.SaveSettings(ctx, s) }, false, func(err error) {
		if err == nil {
			a.fillSettings()
			a.feedback.SetText("设置已保存。关闭此窗口后，MiTray 会继续在托盘运行。")
		}
	})
}

func (a *app) showDiagnostics() {
	text := a.controller.Snapshot().Diagnostics
	if text == "" {
		walk.MsgBox(a.window, "核心诊断", "当前没有可用的核心输出。", walk.MsgBoxOK|walk.MsgBoxIconInformation)
		return
	}
	var dlg *walk.Dialog
	_, err := (Dialog{
		AssignTo: &dlg, Title: "mihomo 核心诊断", Size: Size{Width: 760, Height: 500},
		Font:   systemMessageFont(false),
		Layout: VBox{Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16}, Spacing: 10},
		Children: []Widget{
			TextLabel{Text: "输出仅保存在内存中，不写入文件；退出 MiTray 后清除。"},
			TextEdit{Text: text, ReadOnly: true, VScroll: true, HScroll: true, StretchFactor: 1},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				HSpacer{}, PushButton{Text: "关闭", OnClicked: func() { dlg.Close(walk.DlgCmdOK) }},
			}},
		},
	}).Run(a.window)
	if err != nil {
		a.feedback.SetText("打开核心诊断失败: " + err.Error())
	}
}
