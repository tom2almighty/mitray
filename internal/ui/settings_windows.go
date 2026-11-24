package ui

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"sort"
	"strings"
	"unsafe"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	"github.com/tom2almighty/mitray/internal/config"
	"github.com/tom2almighty/mitray/internal/controller"
	"github.com/tom2almighty/mitray/internal/i18n"
	"github.com/tom2almighty/mitray/internal/platform"
	"golang.org/x/sys/windows"
)

// CenterSettingsWindow is shared with the second-instance activation path.
func CenterSettingsWindow(hwnd win.HWND) error {
	t := i18n.T()
	var cursor win.POINT
	if !win.GetCursorPos(&cursor) {
		return fmt.Errorf("%s", t.ErrGetCursorPos)
	}
	point := win.RECT{Left: cursor.X, Top: cursor.Y, Right: cursor.X + 1, Bottom: cursor.Y + 1}
	monitor, _, err := windows.NewLazySystemDLL("user32.dll").NewProc("MonitorFromRect").Call(
		uintptr(unsafe.Pointer(&point)), win.MONITOR_DEFAULTTONEAREST)
	if monitor == 0 {
		return fmt.Errorf(t.ErrGetMonitor, err)
	}
	info := win.MONITORINFO{CbSize: uint32(unsafe.Sizeof(win.MONITORINFO{}))}
	if !win.GetMonitorInfo(win.HMONITOR(monitor), &info) {
		return fmt.Errorf("%s", t.ErrGetMonitorWork)
	}
	// Moving across monitors can change DPI and resize the window synchronously.
	for range 2 {
		var bounds win.RECT
		if !win.GetWindowRect(hwnd, &bounds) {
			return fmt.Errorf("%s", t.ErrGetWindowRect)
		}
		work := info.RcWork
		x := work.Left + max(0, (work.Right-work.Left-(bounds.Right-bounds.Left))/2)
		y := work.Top + max(0, (work.Bottom-work.Top-(bounds.Bottom-bounds.Top))/2)
		if !win.SetWindowPos(hwnd, 0, x, y, 0, 0, win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE) {
			return fmt.Errorf("%s", t.ErrCenterWindow)
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
			Label{AssignTo: target, Text: i18n.T().StateUnknown, Font: font, TextColor: systemTextColor()},
		},
	}
}

func (a *app) createWindow() error {
	t := i18n.T()
	font := systemMessageFont(false)
	boldFont := systemMessageFont(true)
	return (MainWindow{
		AssignTo: &a.window, Title: t.WindowTitle, Visible: false,
		MinSize: Size{Width: 680, Height: 700}, Size: Size{Width: 740, Height: 760},
		Font:   font,
		Layout: VBox{Margins: Margins{Left: 24, Top: 20, Right: 24, Bottom: 20}, Spacing: 14},
		Children: []Widget{
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				Label{Text: "MiTray", Font: systemTitleFont()},
				HSpacer{}, Label{Text: "Windows · " + a.version, TextColor: systemMutedColor()},
			}},
			GroupBox{Title: t.GroupStatus, Layout: VBox{Spacing: 12}, Children: []Widget{
				Composite{Layout: HBox{MarginsZero: true, Spacing: 14}, Children: []Widget{
					statusBlock(t.StatusCore, &a.statusLabel, boldFont),
					VSeparator{},
					statusBlock("TUN", &a.tunLabel, boldFont),
					VSeparator{},
					statusBlock(t.StatusProxy, &a.proxyLabel, boldFont),
				}},
				Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
					PushButton{AssignTo: &a.startButton, Text: t.BtnStartCore, OnClicked: func() {
						if a.controller.Snapshot().Running {
							a.run(i18n.T().BusyRestartCore, a.controller.Restart, false, nil)
						} else {
							a.run(i18n.T().BusyStartCore, a.controller.Start, false, nil)
						}
					}},
					PushButton{AssignTo: &a.tunButton, Text: t.BtnToggleTUN, OnClicked: func() { a.run(i18n.T().BusyToggleTUN, a.controller.ToggleTUN, false, nil) }},
					PushButton{AssignTo: &a.proxyButton, Text: t.BtnToggleProxy, OnClicked: func() { a.run(i18n.T().BusyToggleProxy, a.controller.ToggleProxy, false, nil) }},
					PushButton{AssignTo: &a.openConfigButton, Text: t.BtnOpenConfig, OnClicked: a.openActiveConfig},
					HSpacer{},
				}},
				Label{AssignTo: &a.hintLabel, Text: t.HintDefault, TextColor: systemMutedColor()},
			}},
			Composite{AssignTo: &a.settingsBody, Layout: VBox{MarginsZero: true, Spacing: 14}, Children: []Widget{
				GroupBox{Title: t.GroupCoreAndProfiles, Layout: VBox{Spacing: 10}, Children: []Widget{
					Label{Text: t.LabelCoreExe},
					Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
						LineEdit{AssignTo: &a.coreEdit, StretchFactor: 1, CueBanner: t.CueCoreExe, OnTextChanged: a.markDirty},
						PushButton{Text: t.BtnBrowse, OnClicked: func() { a.browse(a.coreEdit, t.DlgBrowseCoreTitle, t.DlgBrowseCoreFilter) }},
					}},
					Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
						Label{Text: t.LabelProfile},
						ComboBox{AssignTo: &a.profileCombo, StretchFactor: 1, OnCurrentIndexChanged: a.profileChanged},
						PushButton{AssignTo: &a.activateProfileButton, Text: t.BtnSetActiveProfile, OnClicked: a.activateEditingProfile},
						PushButton{AssignTo: &a.deleteProfileButton, Text: t.BtnDelete, OnClicked: a.deleteProfile},
					}},
					Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
						Label{Text: t.LabelProfileName},
						LineEdit{AssignTo: &a.profileNameEdit, StretchFactor: 1, OnTextChanged: a.markDirty},
						Label{Text: t.LabelSource},
						ComboBox{AssignTo: &a.sourceCombo, Model: []string{t.SourceLocal, t.SourceURL}, CurrentIndex: 0, OnCurrentIndexChanged: a.sourceChanged},
					}},
					Composite{AssignTo: &a.localBody, Layout: VBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
						Label{Text: t.LabelLocalYAML},
						Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
							LineEdit{AssignTo: &a.localEdit, StretchFactor: 1, CueBanner: t.CueLocalYAML, OnTextChanged: a.markDirty},
							PushButton{Text: t.BtnBrowse, OnClicked: func() {
								a.browse(a.localEdit, t.DlgBrowseConfigTitle, t.DlgBrowseConfigFilter)
							}},
						}},
					}},
					Composite{AssignTo: &a.remoteBody, Layout: VBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
						Label{Text: t.LabelRemoteURL},
						LineEdit{AssignTo: &a.urlEdit, CueBanner: "https://example.com/config.yaml", OnTextChanged: a.markDirty},
						Label{Text: t.HintRemoteDownload, TextColor: systemMutedColor()},
					}},
					Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
						PushButton{Text: t.BtnAddLocalProfile, OnClicked: a.addLocalProfile},
						PushButton{Text: t.BtnAddRemoteProfile, OnClicked: a.addRemoteProfile},
						HSpacer{},
					}},
				}},
				GroupBox{Title: t.GroupStartup, Layout: VBox{Spacing: 10}, Children: []Widget{
					Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
						CheckBox{AssignTo: &a.autoStart, Text: t.CheckAutoStartCore, OnCheckedChanged: a.markDirty},
						HSpacer{},
						Label{Text: t.LabelLanguage},
						ComboBox{AssignTo: &a.languageCombo, Model: []string{t.LangAuto, t.LangZH, t.LangEN}, CurrentIndex: 0, OnCurrentIndexChanged: a.markDirty},
					}},
					Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
						Label{Text: t.LabelAutostart},
						ComboBox{AssignTo: &a.startupCombo, Model: []string{t.StartupOff, t.StartupNormal, t.StartupAdmin}, CurrentIndex: 0, OnCurrentIndexChanged: a.markDirty},
						HSpacer{}, Label{Text: t.LabelLogonDelay},
						NumberEdit{AssignTo: &a.delay, Decimals: 0, MinValue: 0, MaxValue: 600, Value: 15, MinSize: Size{Width: 70}, MaxSize: Size{Width: 85}, OnValueChanged: a.markDirty},
						Label{Text: t.LabelSeconds},
					}},
					Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
						Label{Text: t.HintTUNRequiresAdmin, TextColor: systemMutedColor()},
						HSpacer{}, PushButton{Text: t.BtnRestartAdmin, Visible: !platform.IsAdmin(), OnClicked: a.elevate},
					}},
				}},
			}},
			VSpacer{},
			TextLabel{AssignTo: &a.feedback, MinSize: Size{Width: 600, Height: 42}, Text: t.FeedbackDefault, TextColor: systemAccentColor()},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				Label{Text: t.HintCloseToTray, TextColor: systemMutedColor()}, HSpacer{},
				PushButton{Text: t.BtnHide, OnClicked: func() { a.window.Hide() }},
				PushButton{AssignTo: &a.saveButton, Text: t.BtnSaveAndApply, MinSize: Size{Width: 110, Height: 32}, OnClicked: a.saveSettings},
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
	a.draftActiveProfile = s.ActiveProfile
	a.coreEdit.SetText(s.CorePath)
	a.autoStart.SetChecked(s.AutoStartCore)
	langIdx := 0
	switch s.Language {
	case i18n.ZH:
		langIdx = 1
	case i18n.EN:
		langIdx = 2
	}
	a.languageCombo.SetCurrentIndex(langIdx)
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
	snap := a.controller.Snapshot()
	known := snap.StartupLevel != ""
	a.startupCombo.SetEnabled(known && !a.busy && !a.stopping)
	a.delay.SetEnabled(known && !a.busy && !a.stopping)
	a.delay.SetValue(float64(snap.StartupDelay))
	idx := -1
	switch snap.StartupLevel {
	case "off":
		idx = 0
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
		a.feedback.SetText(i18n.T().FeedbackUnsaved)
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
	t := i18n.T()
	hasProfile := a.editingProfile != ""
	a.profileNameEdit.SetEnabled(hasProfile)
	a.sourceCombo.SetEnabled(hasProfile)
	a.localEdit.SetEnabled(hasProfile)
	a.urlEdit.SetEnabled(hasProfile)
	a.deleteProfileButton.SetEnabled(hasProfile && !a.busy)
	if a.activateProfileButton != nil {
		if hasProfile && a.editingProfile == a.draftActiveProfile {
			a.activateProfileButton.SetText(t.BtnActiveProfile)
			a.activateProfileButton.SetEnabled(false)
		} else {
			a.activateProfileButton.SetText(t.BtnSetActiveProfile)
			a.activateProfileButton.SetEnabled(hasProfile && !a.busy)
		}
	}
}

func (a *app) activateEditingProfile() {
	if a.busy || a.editingProfile == "" {
		return
	}
	if err := a.commitCurrentProfile(); err != nil {
		a.feedback.SetText(err.Error())
		return
	}
	if a.draftActiveProfile == a.editingProfile {
		return
	}
	a.draftActiveProfile = a.editingProfile
	a.updateProfileControls()
	a.markDirty()
	a.feedback.SetText(i18n.T().FeedbackActivePending)
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
			return fmt.Errorf(i18n.T().ErrProfileNameExists, name)
		}
		delete(a.draftProfiles, a.editingProfile)
		if a.draftActiveProfile == a.editingProfile {
			a.draftActiveProfile = name
		}
	}
	profile := config.Profile{Type: config.ProfileLocal, Path: strings.TrimSpace(a.localEdit.Text())}
	if a.sourceCombo.CurrentIndex() == 1 {
		profile = config.Profile{Type: config.ProfileURL, URL: strings.TrimSpace(a.urlEdit.Text())}
	}
	a.draftProfiles[name] = profile
	a.editingProfile = name
	a.refreshProfileModel(name)
	a.updateProfileControls()
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
	t := i18n.T()
	dlg := walk.FileDialog{Title: t.DlgAddConfigTitle, Filter: t.DlgBrowseConfigFilter}
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
	if _, ok := a.draftProfiles[a.draftActiveProfile]; !ok {
		a.draftActiveProfile = name
	}
	a.loadProfile(name, profile)
	a.markDirty()
	if a.draftActiveProfile == name {
		a.feedback.SetText(t.FeedbackLocalAdded)
	} else {
		a.feedback.SetText(t.FeedbackLocalAddedAlt)
	}
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
	if _, ok := a.draftProfiles[a.draftActiveProfile]; !ok {
		a.draftActiveProfile = name
	}
	a.loadProfile(name, profile)
	a.markDirty()
	a.feedback.SetText(i18n.T().FeedbackRemoteAdded)
}

func (a *app) deleteProfile() {
	if a.busy || a.editingProfile == "" {
		return
	}
	if err := a.commitCurrentProfile(); err != nil {
		a.feedback.SetText(err.Error())
		return
	}
	t := i18n.T()
	if len(a.draftProfiles) <= 1 {
		a.feedback.SetText(t.FeedbackKeepOne)
		return
	}
	name := a.editingProfile
	if walk.MsgBox(a.window, t.DlgDeleteProfileTitle, fmt.Sprintf(t.DlgDeleteProfilePrompt, name),
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
	if a.draftActiveProfile == name {
		a.draftActiveProfile = next
	}
	a.loadProfile(next, a.draftProfiles[next])
	a.markDirty()
}

func (a *app) browse(edit *walk.LineEdit, title, filter string) {
	if a.busy {
		return
	}
	if edit != a.coreEdit && a.editingProfile == "" {
		a.feedback.SetText(i18n.T().FeedbackAddProfile)
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
	t := i18n.T()
	if a.editingProfile == "" {
		a.feedback.SetText(t.FeedbackAddProfile)
		return
	}
	snap := a.controller.Snapshot()
	s := snap.Settings
	s.CorePath = strings.TrimSpace(a.coreEdit.Text())
	s.AutoStartCore = a.autoStart.Checked()
	langs := []string{i18n.Auto, i18n.ZH, i18n.EN}
	langIdx := a.languageCombo.CurrentIndex()
	if langIdx < 0 || langIdx >= len(langs) {
		langIdx = 0
	}
	s.Language = langs[langIdx]
	langChanged := i18n.Resolve(s.Language) != i18n.Code()
	var startup *controller.StartupSpec
	if snap.StartupLevel != "" {
		levels := []string{"off", "normal", "admin"}
		idx := a.startupCombo.CurrentIndex()
		if idx >= 0 && idx < len(levels) {
			startup = &controller.StartupSpec{
				Level: levels[idx],
				Delay: int(a.delay.Value()),
			}
		}
	}
	s.Profiles = make(map[string]config.Profile, len(a.draftProfiles))
	maps.Copy(s.Profiles, a.draftProfiles)
	s.ActiveProfile = a.draftActiveProfile
	if _, ok := s.Profiles[s.ActiveProfile]; !ok {
		s.ActiveProfile = a.editingProfile
	}
	for name, profile := range s.Profiles {
		if err := profile.Validate(a.base); err != nil {
			a.feedback.SetText(fmt.Sprintf(t.ErrProfileInvalid, name, err))
			return
		}
	}
	if err := s.Validate(a.base); err != nil {
		a.feedback.SetText(err.Error())
		return
	}
	a.run(t.BusySaveSettings, func(ctx context.Context) error { return a.controller.SaveSettingsWithStartup(ctx, s, startup) }, false, func(err error) {
		if err == nil {
			a.fillSettings()
			if langChanged {
				a.feedback.SetText(t.FeedbackSavedLang)
				if walk.MsgBox(a.window, t.DlgRestartLangTitle, t.DlgRestartLangPrompt, walk.MsgBoxYesNo|walk.MsgBoxIconQuestion|walk.MsgBoxSetForeground) == walk.DlgCmdYes {
					a.relaunch(true)
				}
			} else {
				a.feedback.SetText(t.FeedbackSaved)
			}
		}
	})
}
