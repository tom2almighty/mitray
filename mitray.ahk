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
        ShowNotification("首次运行", "已创建默认配置文件 config.ini`n请编辑配置文件后重新运行程序", 3)
        Sleep(3000)  ; 等待通知显示
        ExitApp()
    }

    ; Read Mihomo section
    CorePath := IniRead(ConfigFile, "Mihomo", "CorePath", "")
    ConfigPath := IniRead(ConfigFile, "Mihomo", "ConfigPath", "")
    ConfigURL := IniRead(ConfigFile, "Mihomo", "ConfigURL", "")

    ; Extract process name from CorePath
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

CreateDefaultConfig() {
    global ConfigFile

    configContent := "
(
[Mihomo]
; Path to mihomo executable (required)
CorePath=

; Local config file path (optional if ConfigURL is set)
ConfigPath=

; Remote config URL (optional, takes precedence over ConfigPath)
ConfigURL=

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

ParseMihomoConfig(configPath) {
    global APIController, APISecret, ProxyPort, WebUIPath, WebUIName

    try {
        ; Reset parsed values to avoid stale state when keys are removed.
        APIController := ""
        APISecret := ""
        ProxyPort := ""
        WebUIPath := ""
        WebUIName := ""

        content := FileRead(configPath)

        ; Parse external-controller (keep as full address)
        if (RegExMatch(content, "im)^\s*external-controller\s*:\s*([^\r\n]+)$", &match)) {
            APIController := RegExReplace(Trim(match[1]), "\s+#.*$")
        }

        ; Parse secret
        if (RegExMatch(content, "im)^\s*secret\s*:\s*([^\r\n]+)$", &match)) {
            APISecret := RegExReplace(Trim(match[1]), "\s+#.*$")
        }

        ; Parse external-ui (local path)
        if (RegExMatch(content, "im)^\s*external-ui\s*:\s*([^\r\n]+)$", &match)) {
            WebUIPath := RegExReplace(Trim(match[1]), "\s+#.*$")
        }

        ; Parse external-ui-name (folder name)
        if (RegExMatch(content, "im)^\s*external-ui-name\s*:\s*([^\r\n]+)$", &match)) {
            WebUIName := RegExReplace(Trim(match[1]), "\s+#.*$")
        }

        ; Parse proxy port (try mixed-port first, then port)
        if (RegExMatch(content, "im)^\s*mixed-port\s*:\s*(?:\x22|')?(\d+)(?:\x22|')?\s*(?:#.*)?$", &match)) {
            ProxyPort := match[1]
        } else if (RegExMatch(content, "im)^\s*port\s*:\s*(?:\x22|')?(\d+)(?:\x22|')?\s*(?:#.*)?$", &match)) {
            ProxyPort := match[1]
        }
    }
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
    global AutoStartupMenu

    ; Remove default menu items
    A_TrayMenu.Delete()

    ; Add menu items
    A_TrayMenu.Add("打开 WebUI", MenuOpenWebUI)
    A_TrayMenu.Add()  ; Separator
    A_TrayMenu.Add("启用系统代理", MenuToggleProxy)
    A_TrayMenu.Add("启用 TUN 模式", MenuToggleTUN)
    A_TrayMenu.Add()  ; Separator
    A_TrayMenu.Add("刷新状态", MenuRefreshStatus)

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
