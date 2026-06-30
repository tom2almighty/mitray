;==============================================================================
; MiTray
; AutoHotkey v2.0+
; Description: Manage mihomo core with system tray interface
;==============================================================================

;@Ahk2Exe-SetMainIcon %A_ScriptName~\.[^.]+$~.ico%
;@Ahk2Exe-AddResource %A_ScriptName~\.[^.]+$~_on.ico%, 201
;@Ahk2Exe-AddResource %A_ScriptName~\.[^.]+$~_off.ico%, 202
;@Ahk2Exe-SetName %A_ScriptName~\.[^.]+$~~%
;@Ahk2Exe-SetVersion 1.0.0
;@Ahk2Exe-ExeName %A_ScriptName~\.[^.]+$~.exe%

#Requires AutoHotkey v2.0+
#SingleInstance Force
Persistent
OnError((*) => -1)  ; 捕获全局未处理异常,防止脚本意外退出

;==============================================================================
; Global Variables
;==============================================================================
; 获取脚本基础名称(不含扩展名),用于注册表和程序标识
global ScriptBaseName := RegExReplace(A_ScriptName, "\.[^.]+$", "")

global MihomoProcess := 0
global ConfigFile := A_ScriptDir "\config.ini"
global MihomoConfigFile := ""
global TempConfigFile := ""  ; 将在读取配置后设置到核心目录

; Configuration
global CorePath := ""
global CoreProcessName := ""
global ConfigPath := ""
global ConfigURL := ""
global ActiveProfile := "default"
global Profiles := Map()
global APIController := ""
global APISecret := ""
global ProxyPort := ""
global WebUIPath := ""
global WebUIName := ""
global AutoStartCore := true
global AutoStartupDelaySec := 15
global RememberTUN := true
global DesiredTUNEnabled := false
global TUNStateConfigured := false
global AutoRestoreTUN := true

; State
global IsProxyEnabled := false
global IsTUNEnabled := false
global IsAutoStartup := false
global AutoStartupLevel := ""  ; "normal" 或 "admin"
global AutoStartupMenu := 0  ; 开机自启子菜单对象
global ProfileMenu := 0  ; mihomo 配置文件子菜单对象
global StatusCheckTimer := 0
global StatusTimerCallback := 0
global TrayIconState := ""
global TrayIconOnResourceId := 201
global TrayIconOffResourceId := 202

;==============================================================================
; Initialization
;==============================================================================
LoadConfig()
SetupTrayMenu()
CheckAutoStartup()
CheckSystemProxyState()  ; Check current system proxy state

; Auto-start mihomo if configured
if (AutoStartCore) {
    if (StartMihomo()) {
        ; Wait for core to be fully ready
        Sleep(3000)

        ; Get initial TUN status from API before starting monitoring
        GetTUNStatusFromAPI()
        RestoreRememberedTUNState()

        ; Update menu to reflect current state
        UpdateMenuStates()

        ; Start status monitoring
        StartStatusMonitoring()
    }
} else {
    ; Even if not auto-starting, check if mihomo is already running
    if (CoreProcessName && ProcessExist(CoreProcessName)) {
        MihomoProcess := ProcessExist(CoreProcessName)
        ShowNotification("检测到运行", "检测到 mihomo 已在运行", 2)

        ; Get initial TUN status from API
        GetTUNStatusFromAPI()
        RestoreRememberedTUNState()

        ; Update menu to reflect current state
        UpdateMenuStates()

        StartStatusMonitoring()
    }
}

return

;==============================================================================
; Configuration Management
;==============================================================================
LoadConfig() {
    global

    ; Create default config if not exists
    if (!FileExist(ConfigFile)) {
        CreateDefaultConfig()
    }

    ReadConfigValues()

    if (!IsConfigUsable()) {
        if (!ShowSettingsGui(true)) {
            ExitApp()
        }
        ReadConfigValues()
    }
}

ReadConfigValues() {
    global

    ; Read Mihomo section
    CorePath := IniRead(ConfigFile, "Mihomo", "CorePath", "")
    ConfigPath := IniRead(ConfigFile, "Mihomo", "ConfigPath", "")
    ConfigURL := IniRead(ConfigFile, "Mihomo", "ConfigURL", "")
    ActiveProfile := IniRead(ConfigFile, "Mihomo", "ActiveProfile", "default")
    LoadProfiles()

    ; Extract process name from CorePath
    CoreProcessName := ""
    if (CorePath) {
        SplitPath(CorePath, &CoreProcessName)
    }

    ; Read Settings section
    AutoStartCore := IniRead(ConfigFile, "Settings", "AutoStartCore", "1") = "1"
    RememberTUN := IniRead(ConfigFile, "Settings", "RememberTUN", "1") = "1"
    tunValue := IniRead(ConfigFile, "Settings", "TUNEnabled", "__missing__")
    TUNStateConfigured := tunValue != "__missing__"
    DesiredTUNEnabled := TUNStateConfigured ? (tunValue = "1") : false
    AutoRestoreTUN := IniRead(ConfigFile, "Settings", "AutoRestoreTUN", "1") = "1"
    delayValue := IniRead(ConfigFile, "Settings", "AutoStartupDelaySec", "15")
    if (RegExMatch(delayValue, "^\d+$")) {
        AutoStartupDelaySec := delayValue + 0
        if (AutoStartupDelaySec > 600) {
            AutoStartupDelaySec := 600
        }
    } else {
        AutoStartupDelaySec := 15
    }

    ; Parse mihomo config file if exists to get API settings
    if (ConfigPath && FileExist(ConfigPath)) {
        ParseMihomoConfig(ConfigPath)
    }
}

LoadProfiles() {
    global Profiles, ConfigFile, ConfigPath, ConfigURL, ActiveProfile

    Profiles := Map()
    try {
        section := IniRead(ConfigFile, "Profiles")
    } catch {
        section := ""
    }
    if (section) {
        loop parse section, "`n", "`r" {
            line := Trim(A_LoopField)
            if (!line || !InStr(line, "=")) {
                continue
            }
            parts := StrSplit(line, "=", , 2)
            name := Trim(parts[1])
            path := Trim(parts[2])
            if (name && path) {
                Profiles[name] := path
            }
        }
    }

    ; Backward compatibility: migrate the legacy ConfigPath into Profiles.
    if (Profiles.Count = 0 && ConfigPath) {
        Profiles["default"] := ConfigPath
        ActiveProfile := "default"
        try {
            IniWrite("default", ConfigFile, "Mihomo", "ActiveProfile")
            IniWrite(ConfigPath, ConfigFile, "Profiles", "default")
        }
    }

    if (!ActiveProfile) {
        ActiveProfile := "default"
    }

    ; Remote URL keeps the old precedence. Local profile selection is used when ConfigURL is empty.
    if (!ConfigURL && Profiles.Has(ActiveProfile)) {
        ConfigPath := Profiles[ActiveProfile]
        try {
            IniWrite(ConfigPath, ConfigFile, "Mihomo", "ConfigPath")
        }
    }
}

IsConfigUsable() {
    global CorePath, ConfigPath, ConfigURL

    if (!CorePath || !FileExist(CorePath)) {
        return false
    }

    if (ConfigURL) {
        return true
    }

    return ConfigPath && FileExist(ConfigPath)
}

SaveSettingsConfig(corePath, configPath, configURL, autoStart, rememberTun, tunEnabled, autoRestoreTun, delaySec) {
    global ConfigFile, ActiveProfile, Profiles

    if (!ActiveProfile) {
        ActiveProfile := "default"
    }

    if (configPath) {
        Profiles[ActiveProfile] := configPath
    }

    try {
        IniWrite(corePath, ConfigFile, "Mihomo", "CorePath")
        IniWrite(configPath, ConfigFile, "Mihomo", "ConfigPath")
        IniWrite(configURL, ConfigFile, "Mihomo", "ConfigURL")
        IniWrite(ActiveProfile, ConfigFile, "Mihomo", "ActiveProfile")
        if (configPath) {
            IniWrite(configPath, ConfigFile, "Profiles", ActiveProfile)
        }

        IniWrite(autoStart ? "1" : "0", ConfigFile, "Settings", "AutoStartCore")
        IniWrite(rememberTun ? "1" : "0", ConfigFile, "Settings", "RememberTUN")
        IniWrite(tunEnabled ? "1" : "0", ConfigFile, "Settings", "TUNEnabled")
        IniWrite(autoRestoreTun ? "1" : "0", ConfigFile, "Settings", "AutoRestoreTUN")
        IniWrite(delaySec, ConfigFile, "Settings", "AutoStartupDelaySec")
        return true
    } catch as err {
        MsgBox("保存配置失败: " . err.Message, "MiTray", "Iconx")
        return false
    }
}

CreateDefaultConfig() {
    global ConfigFile

    configContent := "
(
[Mihomo]
; Path to mihomo executable (required)
CorePath=

; Active local profile name. ConfigURL still takes precedence if set.
ActiveProfile=default

; Local config file path (optional if ConfigURL is set)
ConfigPath=

; Remote config URL (optional, takes precedence over ConfigPath)
ConfigURL=

[Profiles]
; default=

[Settings]
; Auto-start mihomo on script launch (1=yes, 0=no)
AutoStartCore=1

; Remember and restore TUN runtime state through mihomo API (1=yes, 0=no)
RememberTUN=1

; Desired TUN state remembered by MiTray (1=enabled, 0=disabled)
TUNEnabled=0

; Re-apply remembered TUN state after mihomo restart or WebUI config reload (1=yes, 0=no)
AutoRestoreTUN=1

; Delay (seconds) before auto-start task runs after user logon (0-600)
AutoStartupDelaySec=15
)"

    FileAppend(configContent, ConfigFile)
}

ShowSettingsGui(firstRun := false) {
    global CorePath, ConfigPath, ConfigURL, AutoStartCore, RememberTUN, DesiredTUNEnabled, AutoRestoreTUN, AutoStartupDelaySec

    state := {Done: false, Saved: false}
    title := firstRun ? "MiTray 首次设置" : "MiTray 设置"
    settingsGui := Gui("+AlwaysOnTop", title)
    settingsGui.MarginX := 14
    settingsGui.MarginY := 14
    settingsGui.SetFont("s9", "Microsoft YaHei UI")

    settingsGui.Add("Text", "x14 y18 w120", "mihomo 核心")
    coreEdit := settingsGui.Add("Edit", "x140 y15 w360", CorePath)
    browseCoreBtn := settingsGui.Add("Button", "x510 y14 w70", "浏览...")

    settingsGui.Add("Text", "x14 y58 w120", "本地配置文件")
    configEdit := settingsGui.Add("Edit", "x140 y55 w360", ConfigPath)
    browseConfigBtn := settingsGui.Add("Button", "x510 y54 w70", "浏览...")

    settingsGui.Add("Text", "x14 y98 w120", "远程配置 URL")
    urlEdit := settingsGui.Add("Edit", "x140 y95 w440", ConfigURL)

    autoStartCheck := settingsGui.Add("Checkbox", "x140 y135 w220", "启动 MiTray 时自动启动 mihomo")
    autoStartCheck.Value := AutoStartCore ? 1 : 0

    rememberTunCheck := settingsGui.Add("Checkbox", "x140 y165 w220", "记忆 TUN 状态")
    rememberTunCheck.Value := RememberTUN ? 1 : 0

    tunEnabledCheck := settingsGui.Add("Checkbox", "x370 y165 w180", "期望 TUN 开启")
    tunEnabledCheck.Value := DesiredTUNEnabled ? 1 : 0

    autoRestoreTunCheck := settingsGui.Add("Checkbox", "x140 y195 w300", "重启或 WebUI 重载后自动恢复 TUN")
    autoRestoreTunCheck.Value := AutoRestoreTUN ? 1 : 0

    settingsGui.Add("Text", "x14 y232 w120", "自启延迟秒数")
    delayEdit := settingsGui.Add("Edit", "x140 y229 w80 Number", AutoStartupDelaySec)
    settingsGui.Add("UpDown", "Range0-600", AutoStartupDelaySec)

    settingsGui.Add("Text", "x140 y258 w440 c666666", "本地配置文件和远程配置 URL 填一个即可；如果都填写，远程 URL 优先。")

    settingsGui.Add("Text", "x14 y292 w120", "解析结果")
    previewEdit := settingsGui.Add("Edit", "x140 y289 w440 h105 Multi ReadOnly -Wrap +VScroll")

    testBtn := settingsGui.Add("Button", "x140 y410 w100", "测试配置")
    saveBtn := settingsGui.Add("Button", "x390 y410 w90 Default", "保存")
    cancelBtn := settingsGui.Add("Button", "x490 y410 w90", firstRun ? "退出" : "取消")

    browseCoreBtn.OnEvent("Click", (*) => BrowseCoreFileAndPreview(coreEdit, previewEdit, configEdit, urlEdit))
    browseConfigBtn.OnEvent("Click", (*) => BrowseConfigFileAndPreview(configEdit, previewEdit, coreEdit, urlEdit))
    coreEdit.OnEvent("Change", (*) => UpdateSettingsPreview(previewEdit, coreEdit, configEdit, urlEdit))
    configEdit.OnEvent("Change", (*) => UpdateSettingsPreview(previewEdit, coreEdit, configEdit, urlEdit))
    urlEdit.OnEvent("Change", (*) => UpdateSettingsPreview(previewEdit, coreEdit, configEdit, urlEdit))
    testBtn.OnEvent("Click", (*) => UpdateSettingsPreview(previewEdit, coreEdit, configEdit, urlEdit, true))

    saveBtn.OnEvent("Click", (*) => SaveSettingsGuiValues(settingsGui, state, coreEdit, configEdit, urlEdit,
        autoStartCheck, rememberTunCheck, tunEnabledCheck, autoRestoreTunCheck, delayEdit))

    cancelBtn.OnEvent("Click", (*) => (state.Done := true))
    settingsGui.OnEvent("Close", (*) => (state.Done := true))

    UpdateSettingsPreview(previewEdit, coreEdit, configEdit, urlEdit)

    settingsGui.Show("w600 h465")
    while (!state.Done) {
        Sleep(50)
    }
    try {
        settingsGui.Destroy()
    }
    return state.Saved
}

BrowseCoreFile(coreEdit) {
    selected := FileSelect(, coreEdit.Value, "选择 mihomo 核心", "Executable (*.exe)")
    if (selected) {
        coreEdit.Value := selected
    }
}

BrowseCoreFileAndPreview(coreEdit, previewEdit, configEdit, urlEdit) {
    BrowseCoreFile(coreEdit)
    UpdateSettingsPreview(previewEdit, coreEdit, configEdit, urlEdit)
}

BrowseConfigFile(configEdit) {
    selected := FileSelect(, configEdit.Value, "选择 mihomo 配置文件", "YAML (*.yaml; *.yml)")
    if (selected) {
        configEdit.Value := selected
    }
}

BrowseConfigFileAndPreview(configEdit, previewEdit, coreEdit, urlEdit) {
    BrowseConfigFile(configEdit)
    UpdateSettingsPreview(previewEdit, coreEdit, configEdit, urlEdit)
}

UpdateSettingsPreview(previewEdit, coreEdit, configEdit, urlEdit, testAPI := false) {
    corePath := Trim(coreEdit.Value)
    configPath := Trim(configEdit.Value)
    configURL := Trim(urlEdit.Value)

    lines := []

    if (corePath && FileExist(corePath)) {
        lines.Push("核心: OK")
    } else if (corePath) {
        lines.Push("核心: 文件不存在")
    } else {
        lines.Push("核心: 未选择")
    }

    if (configURL) {
        lines.Push("远程配置: 已填写，将优先使用")
    } else {
        lines.Push("远程配置: 未填写")
    }

    parsed := 0
    if (configPath) {
        if (!FileExist(configPath)) {
            lines.Push("本地配置: 文件不存在")
        } else {
            try {
                parsed := ReadMihomoConfig(configPath)
                lines.Push("本地配置: OK")
                lines.Push("API: " . DisplayConfigValue(parsed.Controller))
                lines.Push("代理端口: " . DisplayConfigValue(parsed.ProxyPort))
                lines.Push("WebUI: " . BuildWebUIDisplay(parsed))
                if (!parsed.Controller) {
                    lines.Push("提示: 未解析到 external-controller")
                }
                if (!parsed.ProxyPort) {
                    lines.Push("提示: 未解析到 mixed-port/port")
                }
            } catch as err {
                lines.Push("本地配置: 读取失败 - " . err.Message)
            }
        }
    } else {
        lines.Push("本地配置: 未选择")
    }

    if (testAPI) {
        if (parsed && parsed.Controller) {
            result := TestMihomoAPI(parsed.Controller, parsed.Secret)
            lines.Push("API 测试: " . result)
        } else if (configURL) {
            lines.Push("API 测试: 远程配置需要核心启动后解析")
        } else {
            lines.Push("API 测试: 缺少 external-controller")
        }
    }

    previewEdit.Value := JoinLines(lines)
}

DisplayConfigValue(value) {
    return value ? value : "未解析到"
}

BuildWebUIDisplay(parsed) {
    if (!parsed.WebUIPath && !parsed.WebUIName) {
        return "未解析到"
    }

    path := parsed.WebUIPath
    if (parsed.WebUIName) {
        path .= path ? "/" . parsed.WebUIName : parsed.WebUIName
    }
    return path
}

TestMihomoAPI(controller, secret) {
    try {
        whr := ComObject("WinHttp.WinHttpRequest.5.1")
        whr.Open("GET", "http://" . controller . "/configs", false)
        if (secret) {
            whr.SetRequestHeader("Authorization", "Bearer " . secret)
        }
        whr.SetTimeouts(1000, 1000, 2000, 2000)
        whr.Send()
        if (whr.Status = 200) {
            return "OK"
        }
        return "失败 HTTP " . whr.Status
    } catch as err {
        return "无法连接 - " . err.Message
    }
}

JoinLines(lines) {
    text := ""
    for line in lines {
        text .= (text ? "`r`n" : "") . line
    }
    return text
}

SaveSettingsGuiValues(settingsGui, state, coreEdit, configEdit, urlEdit, autoStartCheck, rememberTunCheck,
    tunEnabledCheck, autoRestoreTunCheck, delayEdit) {
    corePath := Trim(coreEdit.Value)
    configPath := Trim(configEdit.Value)
    configURL := Trim(urlEdit.Value)
    delayValue := Trim(delayEdit.Value)

    if (!corePath || !FileExist(corePath)) {
        MsgBox("请选择有效的 mihomo 核心文件。", "MiTray", "Iconx")
        return
    }

    if (!configURL && (!configPath || !FileExist(configPath))) {
        MsgBox("请在「本地配置文件」和「远程配置 URL」中至少填写一个。", "MiTray", "Iconx")
        return
    }

    if (!RegExMatch(delayValue, "^\d+$")) {
        delayValue := "15"
    }
    delaySec := delayValue + 0
    if (delaySec > 600) {
        delaySec := 600
    }

    if (SaveSettingsConfig(corePath, configPath, configURL, autoStartCheck.Value = 1, rememberTunCheck.Value = 1,
        tunEnabledCheck.Value = 1, autoRestoreTunCheck.Value = 1, delaySec)) {
        state.Saved := true
        state.Done := true
    }
}

ParseMihomoConfig(configPath) {
    global APIController, APISecret, ProxyPort, WebUIPath, WebUIName

    try {
        parsed := ReadMihomoConfig(configPath)
        APIController := parsed.Controller
        APISecret := parsed.Secret
        ProxyPort := parsed.ProxyPort
        WebUIPath := parsed.WebUIPath
        WebUIName := parsed.WebUIName
    } catch {
        APIController := ""
        APISecret := ""
        ProxyPort := ""
        WebUIPath := ""
        WebUIName := ""
    }
}

ReadMihomoConfig(configPath) {
    content := FileRead(configPath)
    proxyPort := ReadTopLevelYamlValue(content, "mixed-port")
    if (!proxyPort) {
        proxyPort := ReadTopLevelYamlValue(content, "port")
    }

    return {
        Controller: ReadTopLevelYamlValue(content, "external-controller"),
        Secret: ReadTopLevelYamlValue(content, "secret"),
        ProxyPort: proxyPort,
        WebUIPath: ReadTopLevelYamlValue(content, "external-ui"),
        WebUIName: ReadTopLevelYamlValue(content, "external-ui-name")
    }
}

ReadTopLevelYamlValue(content, key) {
    loop parse content, "`n", "`r" {
        line := A_LoopField
        if (!line || RegExMatch(line, "^\s+#")) {
            continue
        }

        ; Only read top-level scalar keys. Nested YAML is intentionally ignored.
        if (RegExMatch(line, "^\s")) {
            continue
        }

        if (!RegExMatch(line, "^" . key . "\s*:\s*(.*)$", &match)) {
            continue
        }

        value := Trim(match[1])
        if (value = "") {
            return ""
        }

        return NormalizeYamlScalar(value)
    }

    return ""
}

NormalizeYamlScalar(value) {
    value := Trim(value)

    if (SubStr(value, 1, 1) = '"' || SubStr(value, 1, 1) = "'") {
        quote := SubStr(value, 1, 1)
        return ReadQuotedYamlScalar(value, quote)
    }

    value := StripYamlComment(value)
    return Trim(value)
}

ReadQuotedYamlScalar(value, quote) {
    result := ""
    escaped := false
    body := SubStr(value, 2)

    loop parse body {
        ch := A_LoopField
        if (quote = '"' && escaped) {
            switch ch {
                case "n":
                    result .= "`n"
                case "r":
                    result .= "`r"
                case "t":
                    result .= "`t"
                default:
                    result .= ch
            }
            escaped := false
            continue
        }

        if (quote = '"' && ch = "\") {
            escaped := true
            continue
        }

        if (ch = quote) {
            return result
        }

        result .= ch
    }

    ; If the quote is not closed, fall back to the trimmed unquoted body.
    return Trim(SubStr(value, 2))
}

StripYamlComment(value) {
    inSpace := false

    loop parse value {
        ch := A_LoopField
        if (ch = "#") {
            if (A_Index = 1 || inSpace) {
                return RTrim(SubStr(value, 1, A_Index - 1))
            }
        }
        inSpace := ch = " " || ch = "`t"
    }

    return value
}

UpdateTrayIcon() {
    global TrayIconState

    nextState := "default"
    if (!IsMihomoRunning()) {
        nextState := "off"
    } else if (IsProxyEnabled || IsTUNEnabled) {
        nextState := "on"
    }

    if (nextState = TrayIconState) {
        return
    }

    if (ApplyTrayIcon(nextState)) {
        TrayIconState := nextState
    }
}

ApplyTrayIcon(state) {
    global TrayIconOnResourceId, TrayIconOffResourceId

    if (A_IsCompiled) {
        switch state {
            case "on":
                TraySetIcon(A_ScriptFullPath, -TrayIconOnResourceId, true)
            case "off":
                TraySetIcon(A_ScriptFullPath, -TrayIconOffResourceId, true)
            default:
                TraySetIcon("*", , true)
        }
        return true
    }

    iconPath := A_ScriptDir . "\mitray.ico"
    switch state {
        case "on":
            iconPath := A_ScriptDir . "\mitray_on.ico"
        case "off":
            iconPath := A_ScriptDir . "\mitray_off.ico"
    }

    if (!FileExist(iconPath)) {
        iconPath := A_ScriptDir . "\mitray.ico"
    }

    if (!FileExist(iconPath)) {
        TraySetIcon("*", , true)
        return false
    }

    TraySetIcon(iconPath, 1, true)
    return true
}

;==============================================================================
; Tray Menu Setup
;==============================================================================
SetupTrayMenu() {
    global AutoStartupMenu, ProfileMenu

    ; Remove default menu items
    A_TrayMenu.Delete()

    ; Add menu items
    A_TrayMenu.Add("打开 WebUI", MenuOpenWebUI)
    A_TrayMenu.Add()  ; Separator
    A_TrayMenu.Add("启用系统代理", MenuToggleProxy)
    A_TrayMenu.Add("启用 TUN 模式", MenuToggleTUN)
    A_TrayMenu.Add()  ; Separator
    A_TrayMenu.Add("刷新状态", MenuRefreshStatus)

    ; mihomo 配置文件子菜单
    ProfileMenu := Menu()
    BuildProfileMenu()
    A_TrayMenu.Add("选择 mihomo 配置", ProfileMenu)
    A_TrayMenu.Add("MiTray 设置...", MenuOpenSettings)
    A_TrayMenu.Add()  ; Separator

    ; 创建开机自启子菜单
    AutoStartupMenu := Menu()
    AutoStartupMenu.Add("普通权限", MenuAutoStartupNormal)
    AutoStartupMenu.Add("管理员权限", MenuAutoStartupAdmin)
    A_TrayMenu.Add("开机自启", AutoStartupMenu)

    A_TrayMenu.Add()  ; Separator
    A_TrayMenu.Add("打开程序目录", MenuOpenScriptDir)
    A_TrayMenu.Add("打开核心目录", MenuOpenCoreDir)
    A_TrayMenu.Add()  ; Separator
    A_TrayMenu.Add("重启内核", MenuRestartCore)
    A_TrayMenu.Add("停止内核", MenuStopCore)
    A_TrayMenu.Add("退出程序", MenuExitProgram)

    ; Update menu states
    UpdateMenuStates()
}

BuildProfileMenu() {
    global ProfileMenu, Profiles

    if (!ProfileMenu) {
        return
    }

    for name, path in Profiles {
        ProfileMenu.Add(name, MenuSelectProfile)
    }

    if (Profiles.Count > 0) {
        ProfileMenu.Add()
    }
    ProfileMenu.Add("添加配置文件...", MenuAddProfile)
}

UpdateMenuStates() {
    UpdateTrayIcon()

    ; Update proxy checkbox
    if (IsProxyEnabled) {
        A_TrayMenu.Check("启用系统代理")
    } else {
        A_TrayMenu.Uncheck("启用系统代理")
    }

    ; Update TUN checkbox
    if (IsTUNEnabled) {
        A_TrayMenu.Check("启用 TUN 模式")
    } else {
        A_TrayMenu.Uncheck("启用 TUN 模式")
    }

    ; Update auto-startup checkboxes
    if (AutoStartupMenu) {
        if (AutoStartupLevel = "normal") {
            AutoStartupMenu.Check("普通权限")
            AutoStartupMenu.Uncheck("管理员权限")
        } else if (AutoStartupLevel = "admin") {
            AutoStartupMenu.Uncheck("普通权限")
            AutoStartupMenu.Check("管理员权限")
        } else {
            AutoStartupMenu.Uncheck("普通权限")
            AutoStartupMenu.Uncheck("管理员权限")
        }
    }

    ; Update active profile checkbox
    if (ProfileMenu) {
        for name, path in Profiles {
            if (name = ActiveProfile) {
                ProfileMenu.Check(name)
            } else {
                ProfileMenu.Uncheck(name)
            }
        }
    }
}

;==============================================================================
; Menu Handlers
;==============================================================================
MenuOpenWebUI(*) {
    global APIController, APISecret, WebUIPath, WebUIName

    if (!APIController) {
        ShowNotification("错误", "API 配置未设置", 3)
        return
    }

    ; Check if mihomo is running
    if (!IsMihomoRunning()) {
        ShowNotification("错误", "mihomo 未运行", 3)
        return
    }

    ; Construct WebUI URL
    url := "http://" . APIController . "/" . WebUIPath

    ; Add external-ui-name if configured
    if (WebUIName) {
        url .= "/" . WebUIName
    }

    ; Add secret parameter
    if (APISecret) {
        url .= "?secret=" . APISecret
    }

    Run(url)
    ShowNotification("WebUI", "已在浏览器中打开 WebUI", 2)
}

MenuToggleProxy(*) {
    if (IsProxyEnabled) {
        DisableSystemProxy()
    } else {
        EnableSystemProxy()
    }
}

MenuToggleTUN(*) {
    if (IsTUNEnabled) {
        DisableTUNMode()
    } else {
        EnableTUNMode()
    }
}

MenuRefreshStatus(*) {
    RefreshAllStatus()
    ShowNotification("状态刷新", "已刷新系统代理和 TUN 状态", 2)
}

MenuOpenSettings(*) {
    wasRunning := IsMihomoRunning()
    if (ShowSettingsGui(false)) {
        LoadConfig()
        SetupTrayMenu()
        CheckAutoStartup()
        CheckSystemProxyState()
        if (wasRunning) {
            ShowNotification("配置已保存", "配置已保存，重启内核后生效", 3)
        } else {
            ShowNotification("配置已保存", "配置已保存", 2)
        }
    }
}

MenuSelectProfile(itemName, *) {
    SwitchMihomoProfile(itemName)
}

MenuAddProfile(*) {
    global ConfigFile, Profiles, ActiveProfile, ConfigPath

    selected := FileSelect(, ConfigPath, "选择 mihomo 配置文件", "YAML (*.yaml; *.yml)")
    if (!selected) {
        return
    }

    SplitPath(selected, , , , &baseName)
    result := InputBox("请输入配置名称：", "添加 mihomo 配置", , baseName ? baseName : "default")
    if (result.Result != "OK") {
        return
    }

    profileName := Trim(result.Value)
    profileName := RegExReplace(profileName, "[=\r\n]", "_")
    if (!profileName) {
        ShowNotification("错误", "配置名称不能为空", 2)
        return
    }

    if (Profiles.Has(profileName)) {
        ShowNotification("错误", "配置名称已存在: " . profileName, 3)
        return
    }

    Profiles[profileName] := selected
    try {
        IniWrite(selected, ConfigFile, "Profiles", profileName)
        SwitchMihomoProfile(profileName)
    } catch as err {
        ShowNotification("错误", "添加配置失败: " . err.Message, 3)
    }
}

SwitchMihomoProfile(profileName) {
    global ConfigFile, Profiles, ActiveProfile, ConfigPath, ConfigURL

    if (!Profiles.Has(profileName)) {
        ShowNotification("错误", "配置不存在: " . profileName, 2)
        return false
    }

    if (profileName = ActiveProfile && ConfigPath = Profiles[profileName] && !ConfigURL) {
        UpdateMenuStates()
        return true
    }

    wasRunning := IsMihomoRunning()

    ActiveProfile := profileName
    ConfigPath := Profiles[profileName]
    ConfigURL := ""

    try {
        IniWrite(ActiveProfile, ConfigFile, "Mihomo", "ActiveProfile")
        IniWrite(ConfigPath, ConfigFile, "Mihomo", "ConfigPath")
        IniWrite("", ConfigFile, "Mihomo", "ConfigURL")
    } catch as err {
        ShowNotification("错误", "保存配置选择失败: " . err.Message, 3)
        return false
    }

    ParseMihomoConfig(ConfigPath)
    SetupTrayMenu()

    if (wasRunning) {
        StopMihomo()
        Sleep(1000)
        if (StartMihomo()) {
            Sleep(3000)
            StartStatusMonitoring()
            RefreshAllStatus()
        }
    } else {
        UpdateMenuStates()
    }

    ShowNotification("配置切换", "已切换到: " . ActiveProfile, 2)
    return true
}

MenuAutoStartupNormal(*) {
    if (AutoStartupLevel = "normal") {
        DisableAutoStartup()
    } else {
        EnableAutoStartup("normal")
    }
}

MenuAutoStartupAdmin(*) {
    if (AutoStartupLevel = "admin") {
        DisableAutoStartup()
    } else {
        EnableAutoStartup("admin")
    }
}

MenuOpenScriptDir(*) {
    Run('explorer.exe "' . A_ScriptDir . '"')
}

MenuOpenCoreDir(*) {
    global CorePath

    if (!CorePath || !FileExist(CorePath)) {
        ShowNotification("错误", "核心路径未配置或文件不存在", 3)
        return
    }

    ; 获取核心所在目录
    SplitPath(CorePath, , &coreDir)
    Run('explorer.exe "' . coreDir . '"')
}

MenuRestartCore(*) {
    ShowNotification("重启内核", "正在重启 mihomo 内核...", 2)

    ; Try API restart first
    if (RestartCoreViaAPI()) {
        Sleep(3000)
        RefreshAllStatus()
        ShowNotification("重启成功", "mihomo 内核已通过 API 重启", 2)
        return
    }

    ; Fallback to process restart
    StopMihomo()
    Sleep(1000)
    if (StartMihomo()) {
        Sleep(3000)
        StartStatusMonitoring()
        RefreshAllStatus()
    }
}

MenuStopCore(*) {
    StopMihomo()
    StopStatusMonitoring()
}

MenuExitProgram(*) {
    ; Just exit the program, don't stop mihomo or change proxy settings
    ; This allows mihomo to continue running in background
    StopStatusMonitoring()
    ExitApp()
}

;==============================================================================
; Status Monitoring
;==============================================================================
StartStatusMonitoring() {
    global StatusCheckTimer, StatusTimerCallback

    ; Stop existing timer if any
    StopStatusMonitoring()

    ; Refresh status immediately
    RefreshAllStatus()

    ; Reuse the same callback object so SetTimer can always stop it reliably
    if (!StatusTimerCallback) {
        StatusTimerCallback := RefreshAllStatus
    }

    ; Set up periodic status check (every 30 seconds)
    SetTimer(StatusTimerCallback, 30000)
    StatusCheckTimer := 1
}

StopStatusMonitoring() {
    global StatusCheckTimer, StatusTimerCallback

    if (StatusCheckTimer && StatusTimerCallback) {
        SetTimer(StatusTimerCallback, 0)
        StatusCheckTimer := 0
    }
}

RefreshAllStatus() {
    try {
        ; Check if mihomo is still running
        if (!IsMihomoRunning()) {
            StopStatusMonitoring()
            return
        }

        ; Refresh system proxy state
        CheckSystemProxyState()

        ; Refresh TUN state from API
        if (GetTUNStatusFromAPI()) {
            RestoreRememberedTUNState()
        }

        ; Update menu
        UpdateMenuStates()
    } catch as err {
        ; 防止定时器回调中的异常导致脚本崩溃
    }
}

;==============================================================================
; Mihomo Process Management
;==============================================================================
IsMihomoRunning() {
    global MihomoProcess, CoreProcessName

    if (!CoreProcessName) {
        return false
    }

    if (ProcessExist(CoreProcessName)) {
        ; Update PID if needed
        if (!MihomoProcess || !ProcessExist(MihomoProcess)) {
            MihomoProcess := ProcessExist(CoreProcessName)
        }
        return true
    }

    MihomoProcess := 0
    return false
}

StartMihomo() {
    global MihomoProcess, CorePath, CoreProcessName, ConfigPath, ConfigURL, MihomoConfigFile, TempConfigFile

    ; Check if mihomo process is already running
    if (IsMihomoRunning()) {
        ShowNotification("提示", "mihomo 已在运行中", 2)
        return true
    }

    ; Validate core path
    if (!CorePath || !FileExist(CorePath)) {
        ShowNotification("错误", "mihomo 核心路径未配置或文件不存在`n请编辑 config.ini", 3)
        return false
    }

    ; 设置临时配置文件路径到核心目录
    if (CorePath) {
        SplitPath(CorePath, , &coreDir)
        TempConfigFile := coreDir . "\config-downloaded.yaml"
    }

    ; Determine which config to use
    if (ConfigURL) {
        ; Download config from URL
        ShowNotification("下载配置", "正在从 URL 下载配置文件...", 2)
        if (!DownloadConfig(ConfigURL, TempConfigFile)) {
            ShowNotification("错误", "下载配置文件失败", 2)
            return false
        }
        MihomoConfigFile := TempConfigFile
    } else if (ConfigPath) {
        MihomoConfigFile := ConfigPath
    } else {
        ShowNotification("错误", "未配置本地配置文件或远程 URL`n请编辑 config.ini", 2)
        return false
    }

    ; Validate config file exists
    if (!FileExist(MihomoConfigFile)) {
        ShowNotification("错误", "配置文件不存在: " . MihomoConfigFile, 3)
        return false
    }

    ; Parse config to get settings
    ParseMihomoConfig(MihomoConfigFile)

    ; Get core directory for working directory
    SplitPath(CorePath, , &coreDir)

    ; Start mihomo with working directory set to core directory
    try {
        MihomoProcess := Run('"' . CorePath . '" -d ".\\" -f "' . MihomoConfigFile . '"', coreDir, "Hide")
        Sleep(2000)  ; Wait for startup

        ; Check if process started successfully
        if (IsMihomoRunning()) {
            ShowNotification("启动成功", "mihomo 内核已启动", 2)
            return true
        } else {
            ShowNotification("错误", "mihomo 启动失败", 2)
            return false
        }
    } catch as err {
        ShowNotification("错误", "启动 mihomo 失败: " . err.Message, 2)
        return false
    }
}

StopMihomo() {
    global MihomoProcess, CoreProcessName, IsTUNEnabled

    if (!IsMihomoRunning()) {
        ShowNotification("提示", "mihomo 未在运行", 2)
        return
    }

    ; 尝试关闭进程(先用进程名,更可靠)
    if (CoreProcessName && ProcessExist(CoreProcessName)) {
        ProcessClose(CoreProcessName)

        ; 等待进程退出(最多等待3秒)
        waitCount := 0
        while (ProcessExist(CoreProcessName) && waitCount < 30) {
            Sleep(100)
            waitCount++
        }
    }

    ; 如果进程名关闭失败,尝试用 PID 关闭
    if (MihomoProcess && ProcessExist(MihomoProcess)) {
        ProcessClose(MihomoProcess)

        ; 再次等待
        waitCount := 0
        while (ProcessExist(MihomoProcess) && waitCount < 30) {
            Sleep(100)
            waitCount++
        }
    }

    ; 验证是否成功关闭
    if (IsMihomoRunning()) {
        ShowNotification("错误", "无法停止 mihomo 内核,请手动结束进程", 3)
        return
    }

    ; 成功关闭,重置状态
    MihomoProcess := 0
    IsTUNEnabled := false

    ; Refresh proxy state from system instead of forcing a local flag
    CheckSystemProxyState()
    UpdateMenuStates()

    ShowNotification("停止", "mihomo 内核已停止", 2)
}

RestartCoreViaAPI() {
    global APIController, APISecret

    if (!IsMihomoRunning()) {
        return false
    }

    try {
        whr := ComObject("WinHttp.WinHttpRequest.5.1")
        whr.Open("POST", "http://" . APIController . "/restart", false)
        whr.SetRequestHeader("Content-Type", "application/json")

        if (APISecret) {
            whr.SetRequestHeader("Authorization", "Bearer " . APISecret)
        }

        whr.Send('{}')

        ; Check response status
        if (whr.Status = 204 || whr.Status = 200) {
            return true
        }

        return false
    } catch as err {
        return false
    }
}

DownloadConfig(url, destPath) {
    try {
        ; Delete old temp file if exists
        if (FileExist(destPath)) {
            FileDelete(destPath)
        }

        Download(url, destPath)
        return FileExist(destPath)
    } catch {
        return false
    }
}

;==============================================================================
; System Proxy Control
;==============================================================================
CheckSystemProxyState() {
    global IsProxyEnabled

    try {
        proxyEnable := RegRead("HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings", "ProxyEnable")
        IsProxyEnabled := (proxyEnable = 1)
    } catch {
        IsProxyEnabled := false
    }

    UpdateMenuStates()
}

EnableSystemProxy() {
    global IsProxyEnabled, ProxyPort

    if (!IsValidPort(ProxyPort)) {
        ShowNotification("错误", "代理端口无效，请检查 mihomo 配置中的 mixed-port/port", 3)
        return
    }

    try {
        ; Set registry values
        RegWrite(1, "REG_DWORD", "HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings", "ProxyEnable")
        RegWrite("127.0.0.1:" . ProxyPort, "REG_SZ", "HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings",
            "ProxyServer")
        RegWrite(
            "localhost;127.*;10.*;172.16.*;172.17.*;172.18.*;172.19.*;172.20.*;172.21.*;172.22.*;172.23.*;172.24.*;172.25.*;172.26.*;172.27.*;172.28.*;172.29.*;172.30.*;172.31.*;192.168.*;<local>",
            "REG_SZ", "HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings", "ProxyOverride")

        ; Apply settings immediately
        DllCall("wininet\InternetSetOptionA", "UInt", 0, "UInt", 39, "UInt", 0, "UInt", 0)
        DllCall("wininet\InternetSetOptionA", "UInt", 0, "UInt", 37, "UInt", 0, "UInt", 0)

        IsProxyEnabled := true
        UpdateMenuStates()
        ShowNotification("系统代理", "系统代理已启用 (端口: " . ProxyPort . ")", 2)
    } catch as err {
        ShowNotification("错误", "启用系统代理失败: " . err.Message, 2)
    }
}

DisableSystemProxy() {
    global IsProxyEnabled

    try {
        ; Clear registry values
        RegWrite(0, "REG_DWORD", "HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings", "ProxyEnable")
        RegWrite("", "REG_SZ", "HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings", "ProxyServer")

        ; Apply settings immediately
        DllCall("wininet\InternetSetOptionA", "UInt", 0, "UInt", 39, "UInt", 0, "UInt", 0)
        DllCall("wininet\InternetSetOptionA", "UInt", 0, "UInt", 37, "UInt", 0, "UInt", 0)

        IsProxyEnabled := false
        UpdateMenuStates()
        ShowNotification("系统代理", "系统代理已禁用", 2)
    } catch as err {
        ShowNotification("错误", "禁用系统代理失败: " . err.Message, 2)
    }
}

;==============================================================================
; TUN Mode Control
;==============================================================================
SaveDesiredTUNState(enabled) {
    global DesiredTUNEnabled, TUNStateConfigured, ConfigFile

    DesiredTUNEnabled := enabled
    TUNStateConfigured := true
    try {
        IniWrite(enabled ? "1" : "0", ConfigFile, "Settings", "TUNEnabled")
    }
}

RestoreRememberedTUNState() {
    global RememberTUN, AutoRestoreTUN, DesiredTUNEnabled, IsTUNEnabled

    if (!RememberTUN || !AutoRestoreTUN) {
        return false
    }

    if (!IsMihomoRunning()) {
        return false
    }

    if (IsTUNEnabled = DesiredTUNEnabled) {
        return true
    }

    return SetTUNMode(DesiredTUNEnabled, false, false)
}

GetTUNStatusFromAPI() {
    global IsTUNEnabled, APIController, APISecret, RememberTUN, TUNStateConfigured

    if (!IsMihomoRunning()) {
        return false
    }

    try {
        whr := ComObject("WinHttp.WinHttpRequest.5.1")
        whr.Open("GET", "http://" . APIController . "/configs", false)

        if (APISecret) {
            whr.SetRequestHeader("Authorization", "Bearer " . APISecret)
        }

        ; Set timeout (in milliseconds)
        whr.SetTimeouts(1000, 1000, 2000, 2000)

        whr.Send()

        ; Check response status
        if (whr.Status != 200) {
            return false
        }

        response := whr.ResponseText

        ; Parse JSON response to get TUN status
        ; Simple regex parsing (for production, consider using a JSON library)
        if (RegExMatch(response, '"tun":\s*\{[^}]*"enable":\s*(true|false)', &match)) {
            IsTUNEnabled := (match[1] = "true")
            if (RememberTUN && !TUNStateConfigured) {
                SaveDesiredTUNState(IsTUNEnabled)
            }
            return true
        }

        return false
    } catch {
        return false
    }
}

EnableTUNMode() {
    if (SetTUNMode(true, true, true)) {
        return true
    }
    return false
}

DisableTUNMode() {
    if (SetTUNMode(false, true, true)) {
        return true
    }
    return false
}

SetTUNMode(enabled, remember := true, notify := true) {
    global IsTUNEnabled, APIController, APISecret, RememberTUN

    ; Ensure mihomo is running
    if (!IsMihomoRunning()) {
        if (notify) {
            ShowNotification("错误", "mihomo 未运行", 2)
        }
        return false
    }

    ; Try multiple times in case API is not ready
    retryCount := 3
    loop retryCount {
        try {
            whr := ComObject("WinHttp.WinHttpRequest.5.1")
            whr.Open("PATCH", "http://" . APIController . "/configs", false)
            whr.SetRequestHeader("Content-Type", "application/json")

            if (APISecret) {
                whr.SetRequestHeader("Authorization", "Bearer " . APISecret)
            }

            ; Set timeout
            whr.SetTimeouts(1000, 1000, 3000, 3000)

            whr.Send('{"tun": {"enable": ' . (enabled ? 'true' : 'false') . '}}')

            ; Check response status
            if (whr.Status = 204 || whr.Status = 200) {
                ; Wait a moment for change to take effect
                Sleep(500)

                ; Verify the change
                if (GetTUNStatusFromAPI() && IsTUNEnabled = enabled) {
                    if (remember && RememberTUN) {
                        SaveDesiredTUNState(enabled)
                    }
                    UpdateMenuStates()
                    if (notify) {
                        ShowNotification("TUN 模式", enabled ? "TUN 模式已启用" : "TUN 模式已禁用", 2)
                    }
                    return true
                }
            }
        } catch {
            ; Retry on error
        }

        ; Wait before retry
        if (A_Index < retryCount) {
            Sleep(1000)
        }
    }

    if (!A_IsAdmin && enabled) {
        if (notify) {
            ShowNotification("权限不足", "TUN 模式需要管理员权限`n请退出程序后选择「以管理员身份运行」", 3)
        }
    } else if (notify) {
        ShowNotification("错误", (enabled ? "启用" : "禁用") . " TUN 模式失败，请检查 mihomo API 是否正常", 2)
    }
    return false
}

;==============================================================================
; Auto-startup Management (使用任务计划程序 + XML)
;==============================================================================
CheckAutoStartup() {
    global IsAutoStartup, AutoStartupLevel, ScriptBaseName

    try {
        cmd := 'schtasks /Query /TN "' . ScriptBaseName . '" 2>nul'
        result := RunWaitOne(cmd)

        if (InStr(result, ScriptBaseName)) {
            IsAutoStartup := true

            cmd := 'schtasks /Query /TN "' . ScriptBaseName . '" /XML'
            xmlResult := RunWaitOne(cmd)

            if (InStr(xmlResult, "<RunLevel>HighestAvailable</RunLevel>")) {
                AutoStartupLevel := "admin"
            } else {
                AutoStartupLevel := "normal"
            }
        } else {
            IsAutoStartup := false
            AutoStartupLevel := ""
        }
    } catch {
        IsAutoStartup := false
        AutoStartupLevel := ""
    }

    UpdateMenuStates()
}

EnableAutoStartup(level := "normal") {
    global IsAutoStartup, AutoStartupLevel, ScriptBaseName, AutoStartupDelaySec

    try {
        ; 先删除已存在的任务（如果有）
        DisableAutoStartup()

        ; 获取可执行文件路径
        exePath := A_IsCompiled ? A_ScriptFullPath : A_ScriptFullPath

        ; 根据权限级别设置 RunLevel
        runLevel := (level = "admin") ? "HighestAvailable" : "LeastPrivilege"
        levelText := (level = "admin") ? "管理员权限" : "普通权限"
        delayIso := "PT" . AutoStartupDelaySec . "S"

        ; 生成 XML 内容（路径需要 XML 转义）
        exePathEscaped := XmlEscape(exePath)

        xmlContent := '<?xml version="1.0" encoding="UTF-16"?>'
            . '`r`n<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">'
            . '`r`n  <RegistrationInfo>'
            . '`r`n    <URI>\' . ScriptBaseName . '</URI>'
            . '`r`n  </RegistrationInfo>'
            . '`r`n  <Triggers>'
            . '`r`n    <LogonTrigger>'
            . '`r`n      <Enabled>true</Enabled>'
            . '`r`n      <Delay>' . delayIso . '</Delay>'
            . '`r`n    </LogonTrigger>'
            . '`r`n  </Triggers>'
            . '`r`n  <Principals>'
            . '`r`n    <Principal id="Author">'
            . '`r`n      <LogonType>InteractiveToken</LogonType>'
            . '`r`n      <RunLevel>' . runLevel . '</RunLevel>'
            . '`r`n    </Principal>'
            . '`r`n  </Principals>'
            . '`r`n  <Settings>'
            . '`r`n    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>'
            . '`r`n    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>'
            . '`r`n    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>'
            . '`r`n    <AllowHardTerminate>false</AllowHardTerminate>'
            . '`r`n    <StartWhenAvailable>false</StartWhenAvailable>'
            . '`r`n    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>'
            . '`r`n    <IdleSettings>'
            . '`r`n      <StopOnIdleEnd>false</StopOnIdleEnd>'
            . '`r`n      <RestartOnIdle>false</RestartOnIdle>'
            . '`r`n    </IdleSettings>'
            . '`r`n    <AllowStartOnDemand>true</AllowStartOnDemand>'
            . '`r`n    <Enabled>true</Enabled>'
            . '`r`n    <Hidden>false</Hidden>'
            . '`r`n    <RunOnlyIfIdle>false</RunOnlyIfIdle>'
            . '`r`n    <WakeToRun>false</WakeToRun>'
            . '`r`n    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>'
            . '`r`n    <Priority>7</Priority>'
            . '`r`n  </Settings>'
            . '`r`n  <Actions Context="Author">'
            . '`r`n    <Exec>'
            . '`r`n      <Command>' . exePathEscaped . '</Command>'
            . '`r`n    </Exec>'
            . '`r`n  </Actions>'
            . '`r`n</Task>'

        ; 创建临时 XML 文件
        tempXmlPath := A_Temp . '\schtask_' . ScriptBaseName . '_' . A_TickCount . '.xml'

        ; 使用 FileAppend（自动处理 UTF-16 BOM）
        try {
            FileDelete(tempXmlPath)  ; 确保文件不存在
        }

        ; 写入文件，使用 UTF-16 编码
        FileAppend(xmlContent, tempXmlPath, "UTF-16")

        ; 验证文件是否创建成功
        if (!FileExist(tempXmlPath)) {
            ShowNotification("错误", "无法创建临时 XML 文件", 2)
            return
        }

        ; 使用 XML 文件创建任务
        cmd := 'schtasks /Create /TN "' . ScriptBaseName . '" '
            . '/XML "' . tempXmlPath . '" '
            . '/F'

        ; 执行命令并获取输出
        result := RunWaitOne(cmd)

        ; 删除临时文件
        try {
            FileDelete(tempXmlPath)
        } catch {
            ; 忽略删除失败
        }

        ; 检查是否成功
        if (InStr(result, "SUCCESS") || InStr(result, "成功") || InStr(result, "已成功")) {
            IsAutoStartup := true
            AutoStartupLevel := level
            UpdateMenuStates()
            ShowNotification("开机自启", "已启用开机自启动 (" . levelText . ")", 2)
        } else {
            ; 显示详细错误信息
            ShowNotification("错误", "启用开机自启失败`n`n" . result, 5)
        }
    } catch as err {
        ; 确保删除临时文件
        try {
            if (FileExist(tempXmlPath))
                FileDelete(tempXmlPath)
        }
        ShowNotification("错误", "启用开机自启失败: " . err.Message, 3)
    }
}

DisableAutoStartup() {
    global IsAutoStartup, AutoStartupLevel, ScriptBaseName

    try {
        ; 删除任务计划程序中的任务
        cmd := 'schtasks /Delete /TN "' . ScriptBaseName . '" /F'
        result := RunWaitOne(cmd)

        IsAutoStartup := false
        AutoStartupLevel := ""
        UpdateMenuStates()

        ; 只有在任务存在时才显示成功通知
        if (InStr(result, "SUCCESS") || InStr(result, "成功")) {
            ShowNotification("开机自启", "已禁用开机自启动", 2)
        }
    } catch as err {
        ; 忽略删除不存在任务的错误
        IsAutoStartup := false
        AutoStartupLevel := ""
        UpdateMenuStates()
    }
}

;==============================================================================
; Utility Functions
;==============================================================================
ShowNotification(title, message, duration := 2) {
    TrayTip(message, title, 0x1)
    SetTimer(() => TrayTip(), -duration * 1000)
}

RunWaitOne(command) {
    shell := ComObject("WScript.Shell")
    exec := shell.Exec(A_ComSpec " /C " . command)

    ; 等待命令完成并读取输出
    output := exec.StdOut.ReadAll()

    return output
}

; XML 转义函数
XmlEscape(str) {
    str := StrReplace(str, "&", "&amp;")
    str := StrReplace(str, "<", "&lt;")
    str := StrReplace(str, ">", "&gt;")
    str := StrReplace(str, '"', "&quot;")
    str := StrReplace(str, "'", "&apos;")
    return str
}

IsValidPort(port) {
    if (!RegExMatch(port, "^\d+$")) {
        return false
    }

    portNum := port + 0
    return portNum >= 1 && portNum <= 65535
}
