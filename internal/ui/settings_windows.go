package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/tom2almighty/mitray/internal/platform"
)

func (a *app) createWindow() error {
	return (MainWindow{
		AssignTo: &a.window, Title: "MiTray 设置", Visible: false,
		MinSize: Size{650, 660}, Size: Size{710, 720},
		Font:   Font{Family: "Microsoft YaHei UI", PointSize: 9},
		Layout: VBox{Margins: Margins{Left: 24, Top: 20, Right: 24, Bottom: 20}, Spacing: 14},
		Children: []Widget{
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				Label{Text: "MiTray", Font: Font{Family: "Segoe UI", PointSize: 22, Bold: true}},
				HSpacer{}, Label{Text: "Windows · " + a.version, TextColor: walk.RGB(100, 110, 120)},
			}},
			GroupBox{Title: "当前状态", Layout: VBox{Spacing: 10}, Children: []Widget{
				Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
					Label{AssignTo: &a.statusLabel, Text: "核心  正在连接", StretchFactor: 2},
					Label{AssignTo: &a.tunLabel, Text: "TUN  未知", StretchFactor: 1},
					Label{AssignTo: &a.proxyLabel, Text: "系统代理  未知", StretchFactor: 1},
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
					HSpacer{},
				}},
				Label{AssignTo: &a.memoryLabel, Text: "TUN 状态会在成功切换后自动记忆。", TextColor: walk.RGB(85, 95, 110)},
			}},
			Composite{AssignTo: &a.settingsBody, Layout: VBox{MarginsZero: true, Spacing: 14}, Children: []Widget{
				GroupBox{Title: "核心与配置", Layout: VBox{Spacing: 10}, Children: []Widget{
					Label{Text: "mihomo 核心程序"},
					Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
						LineEdit{AssignTo: &a.coreEdit, StretchFactor: 1, CueBanner: "选择 mihomo.exe"},
						PushButton{Text: "浏览…", OnClicked: func() { a.browse(a.coreEdit, "选择 mihomo 核心", "可执行文件 (*.exe)|*.exe") }},
					}},
					Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
						Label{Text: "配置来源"}, ComboBox{AssignTo: &a.sourceCombo, Model: []string{"本地文件", "远程 URL"}, CurrentIndex: 0, OnCurrentIndexChanged: a.updateSource}, HSpacer{},
					}},
					Composite{AssignTo: &a.localBody, Layout: VBox{MarginsZero: true, Spacing: 8}, Children: []Widget{
						Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
							Label{Text: "配置名称"}, ComboBox{AssignTo: &a.profileCombo, Editable: true, StretchFactor: 1, OnCurrentIndexChanged: a.profileChanged},
							PushButton{Text: "添加配置…", OnClicked: a.addProfile},
						}},
						Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
							LineEdit{AssignTo: &a.localEdit, StretchFactor: 1, CueBanner: "选择 mihomo YAML 配置"},
							PushButton{Text: "浏览…", OnClicked: func() {
								a.browse(a.localEdit, "选择 mihomo 配置", "YAML 配置 (*.yaml;*.yml)|*.yaml;*.yml|所有文件 (*.*)|*.*")
							}},
						}},
					}},
					Composite{AssignTo: &a.remoteBody, Layout: VBox{MarginsZero: true}, Children: []Widget{
						LineEdit{AssignTo: &a.urlEdit, CueBanner: "https://example.com/config.yaml"},
						Label{Text: "下载失败时使用上次成功下载的配置。", TextColor: walk.RGB(100, 110, 120)},
					}},
				}},
				GroupBox{Title: "启动", Layout: VBox{Spacing: 10}, Children: []Widget{
					CheckBox{AssignTo: &a.autoStart, Text: "打开 MiTray 时自动启动 mihomo"},
					Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
						Label{Text: "开机自启"}, ComboBox{AssignTo: &a.startupCombo, Model: []string{"关闭", "普通权限", "管理员权限"}, CurrentIndex: 0},
						HSpacer{}, Label{Text: "登录后延迟"}, NumberEdit{AssignTo: &a.delay, Decimals: 0, MinValue: 0, MaxValue: 600, Value: 15, MinSize: Size{70, 0}, MaxSize: Size{85, 0}}, Label{Text: "秒"},
					}},
					Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
						Label{Text: "TUN 需要 mihomo 以管理员权限运行。", TextColor: walk.RGB(100, 110, 120)},
						HSpacer{}, PushButton{Text: "以管理员身份重启", Visible: !platform.IsAdmin(), OnClicked: a.elevate},
					}},
				}},
			}},
			VSpacer{},
			TextLabel{AssignTo: &a.feedback, MinSize: Size{580, 42}, Text: "修改核心或配置后，保存会重新启动正在运行的核心。", TextColor: walk.RGB(65, 90, 120)},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				Label{Text: "关闭窗口后继续在托盘运行。", TextColor: walk.RGB(110, 115, 125)}, HSpacer{},
				PushButton{Text: "收起", OnClicked: func() { a.window.Hide() }},
				PushButton{AssignTo: &a.saveButton, Text: "保存并应用", MinSize: Size{110, 32}, OnClicked: a.saveSettings},
			}},
		},
	}).Create()
}

func (a *app) fillSettings() {
	if a.coreEdit == nil {
		return
	}
	a.filling = true
	defer func() { a.filling = false; a.updateSource() }()
	s := a.controller.Snapshot().Settings
	a.draftProfiles = s.Clone().Profiles
	a.coreEdit.SetText(s.CorePath)
	a.localEdit.SetText(s.ConfigPath)
	a.urlEdit.SetText(s.ConfigURL)
	a.profileCombo.SetModel(s.ProfileNames())
	a.profileCombo.SetText(s.ActiveProfile)
	if s.ConfigURL != "" {
		a.sourceCombo.SetCurrentIndex(1)
	} else {
		a.sourceCombo.SetCurrentIndex(0)
	}
	a.autoStart.SetChecked(s.AutoStartCore)
	a.delay.SetValue(float64(s.StartupDelay))
	a.fillStartup()
}

func (a *app) fillStartup() {
	idx := 0
	switch a.controller.Snapshot().Settings.StartupLevel {
	case "normal":
		idx = 1
	case "admin":
		idx = 2
	}
	a.startupCombo.SetCurrentIndex(idx)
}

func (a *app) updateSource() {
	if a.localBody == nil || a.remoteBody == nil {
		return
	}
	local := a.sourceCombo.CurrentIndex() != 1
	a.localBody.SetVisible(local)
	a.remoteBody.SetVisible(!local)
}

func (a *app) profileChanged() {
	if a.filling || a.localEdit == nil {
		return
	}
	if path, ok := a.draftProfiles[a.profileCombo.Text()]; ok {
		a.localEdit.SetText(path)
	}
}

func (a *app) browse(edit *walk.LineEdit, title, filter string) {
	if a.busy {
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

func (a *app) addProfile() {
	if a.busy {
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
	name := strings.TrimSuffix(filepath.Base(dlg.FilePath), filepath.Ext(dlg.FilePath))
	base := name
	for i := 2; a.draftProfiles[name] != ""; i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	a.draftProfiles[name] = dlg.FilePath
	names := make([]string, 0, len(a.draftProfiles))
	for n := range a.draftProfiles {
		names = append(names, n)
	}
	sort.Strings(names)
	a.filling = true
	a.profileCombo.SetModel(names)
	a.profileCombo.SetText(name)
	a.filling = false
	a.localEdit.SetText(dlg.FilePath)
	a.sourceCombo.SetCurrentIndex(0)
	a.feedback.SetText("配置已加入，点击「保存并应用」后生效。")
}

func (a *app) saveSettings() {
	if a.busy {
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
	s.Profiles = make(map[string]string, len(a.draftProfiles))
	for name, path := range a.draftProfiles {
		s.Profiles[name] = path
	}
	s.ConfigPath = strings.TrimSpace(a.localEdit.Text())
	name := strings.TrimSpace(a.profileCombo.Text())
	if name == "" {
		name = "default"
	}
	if strings.ContainsAny(name, "\r\n=[]") {
		a.feedback.SetText("配置名称不能包含换行、等号或方括号。")
		return
	}
	s.ActiveProfile = name
	if s.ConfigPath != "" {
		s.Profiles[name] = s.ConfigPath
	}
	s.ConfigURL = ""
	if a.sourceCombo.CurrentIndex() == 1 {
		s.ConfigURL = strings.TrimSpace(a.urlEdit.Text())
		if s.ConfigURL == "" {
			a.feedback.SetText("请填写远程配置 URL。")
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
