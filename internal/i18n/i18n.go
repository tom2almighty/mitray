package i18n

import (
	"strings"
	"sync/atomic"
)

const (
	Auto = "auto"
	ZH   = "zh"
	EN   = "en"
)

// Messages holds all user-facing strings for a single locale.
type Messages struct {
	// Main / startup
	WindowTitle         string
	ErrInstanceExiting  string
	ErrReadConfigPrefix string
	ErrLoadIcon         string
	TOMLHeader          string

	// Config validation & storage
	ErrRemoteURLInvalid   string
	ErrProfileTypeInvalid string
	ErrInvalidCoreFile    string
	ErrSelectProfile      string
	ErrInvalidProfile     string
	ErrProfileNameEmpty   string
	ErrProfileNameSpaces  string
	ErrProfileNameControl string
	ErrInvalidLanguage    string
	ErrEmptyPath          string
	ErrPathIsDir          string
	ErrParseFile          string
	ErrUnknownKeys        string
	ErrMarshalFile        string
	ErrWriteConfig        string

	// Mihomo config & API
	ErrAPIHTTPStatus  string
	ErrNoController   string
	ErrConnectAPI     string
	ErrAPIAuth        string
	ErrReadAPIStatus  string
	ErrReadConnection string
	ErrControllerAddr string
	ErrControllerPort string

	// Platform core / startup / system
	ErrMultipleCores       string
	ErrIdentifyCoreAdmin   string
	ErrProcessChanged      string
	ErrStopCoreTimeout     string
	ErrTaskNameOccupied    string
	ErrReadStartupTask     string
	ErrQueryTaskScheduler  string
	ErrReadStartupAdmin    string
	ErrInvalidStartupOpt   string
	ErrInvalidStartupDelay string
	ErrCreateStartupAdmin  string
	ErrDeleteStartupAdmin  string
	ErrNoProxyPort         string
	ErrRefreshWinProxy     string

	// Source / remote profile
	ErrSelectMihomoConfig  string
	ErrReadMihomoConfig    string
	ErrInvalidLocalConfig  string
	ErrInvalidSourceType   string
	ErrRemoteContentBad    string
	ErrRemoteCacheWrite    string
	ErrRemoteNotUpdated    string
	ErrCachedConfigBad     string
	NoticeUsedCache        string
	ErrNoRemoteOrCache     string
	ErrReadRemoteCache     string
	ErrInvalidRemoteConfig string
	CacheLastSuccess       string
	CacheUpdatedAt         string
	ErrRemoteAddrInvalid   string
	ErrDownloadRemote      string
	ErrDownloadRemoteHTTP  string
	ErrDownloadInterrupted string
	ErrRemoteTooLarge      string

	// Controller lifecycle & actions
	NoticeReadStartupFailed string
	ErrStartMihomo          string
	NoticeTUNNeedsAdmin     string
	ErrCoreExited           string
	ErrNoControllerForTUN   string
	ErrWaitAPIReadyTimeout  string
	ErrStopMihomo           string
	ErrStartCoreFirst       string
	ErrCoreNotConnected     string
	ErrNoTUNStatus          string
	ErrEnableTUNNeedsAdmin  string
	ErrTUNNotConfirmed      string
	ErrTUNMissingSection    string
	ErrTUNNotReached        string
	ErrSelectLocalConfig    string
	ErrConfigFileNotFound   string
	ErrConfigPathIsDir      string
	ErrEnterRemoteURL       string
	ErrRemoteNotCached      string
	ErrReadCacheFile        string
	ErrStartupNotUpdated    string
	ErrSaveAndUndoFailed    string
	ErrSaveSettings         string
	ErrSavedApplyFailed     string
	ErrSavedStartFailed     string
	ErrProfileNotFound      string

	// Window centering errors
	ErrGetCursorPos   string
	ErrGetMonitor     string
	ErrGetMonitorWork string
	ErrGetWindowRect  string
	ErrCenterWindow   string

	// Tray menu & actions
	TraySettings        string
	TrayOpenWebUI       string
	TraySystemProxy     string
	TrayTUNMode         string
	TrayCopyProxyEnv    string
	TraySelectProfile   string
	TrayAddLocalProfile string
	TrayStartup         string
	StartupOff          string
	StartupNormal       string
	StartupAdmin        string
	TrayOpenAppDir      string
	TrayOpenCoreDir     string
	TrayOpenConfig      string
	TrayRestartAdmin    string
	TrayStartCore       string
	TrayRestartCore     string
	TrayStopCore        string
	TrayQuit            string
	TrayStopAndQuit     string

	// Status & labels
	StateUnknown           string
	StateOn                string
	StateOff               string
	CoreStopped            string
	CoreRunning            string
	ProxyTargetsCoreSuffix string
	HintDefault            string
	HintTUNNeedsAdmin      string
	HintProxyTargetsCore   string
	TooltipFormat          string
	TooltipStoppingFormat  string

	// Progress & feedback
	BusyConnecting       string
	BusyToggleProxy      string
	BusyToggleTUN        string
	BusyUpdateStartup    string
	BusyStartCore        string
	BusyRestartCore      string
	BusyStopCore         string
	BusyStopProxyAndCore string
	BusySwitchProfile    string
	BusyPrepareEnv       string
	BusySaveSettings     string
	DoneText             string
	FirstRunGuidance     string
	ErrUpdateProfileMenu string
	ErrOpenFailed        string
	ErrClipboardFailed   string
	CopiedProxyEnv       string
	ErrElevateFailed     string

	// Dialogs
	DlgStopCoreTitle       string
	DlgStopProxyConfirm    string
	DlgStopFailedTitle     string
	DlgStopFailedAskQuit   string
	DlgBrowseCoreTitle     string
	DlgBrowseCoreFilter    string
	DlgBrowseConfigTitle   string
	DlgBrowseConfigFilter  string
	DlgAddConfigTitle      string
	DlgDeleteProfileTitle  string
	DlgDeleteProfilePrompt string

	// Settings window
	GroupStatus           string
	StatusCore            string
	StatusProxy           string
	BtnStartCore          string
	BtnRestartCore        string
	BtnToggleTUN          string
	BtnToggleProxy        string
	BtnOpenConfig         string
	GroupCoreAndProfiles  string
	LabelCoreExe          string
	CueCoreExe            string
	BtnBrowse             string
	LabelProfile          string
	BtnSetActiveProfile   string
	BtnActiveProfile      string
	BtnDelete             string
	LabelProfileName      string
	LabelSource           string
	SourceLocal           string
	SourceURL             string
	LabelLocalYAML        string
	CueLocalYAML          string
	LabelRemoteURL        string
	HintRemoteDownload    string
	BtnAddLocalProfile    string
	BtnAddRemoteProfile   string
	GroupStartup          string
	CheckAutoStartCore    string
	LabelAutostart        string
	LabelLogonDelay       string
	LabelSeconds          string
	LabelLanguage         string
	LangAuto              string
	LangZH                string
	LangEN                string
	HintTUNRequiresAdmin  string
	BtnRestartAdmin       string
	FeedbackDefault       string
	FeedbackUnsaved       string
	FeedbackActivePending string
	FeedbackLocalAdded    string
	FeedbackLocalAddedAlt string
	FeedbackRemoteAdded   string
	FeedbackKeepOne       string
	FeedbackAddProfile    string
	FeedbackSaved         string
	FeedbackSavedLang     string
	ErrProfileNameExists  string
	ErrProfileInvalid     string
	HintCloseToTray       string
	BtnHide               string
	BtnSaveAndApply       string
}

var (
	current atomic.Pointer[Messages]
	langVal atomic.Value
)

func init() {
	Set(ZH)
}

// T returns the active locale message table.
func T() *Messages {
	return current.Load()
}

// Code returns the resolved active language code ("zh" or "en").
func Code() string {
	if v, ok := langVal.Load().(string); ok && v != "" {
		return v
	}
	return ZH
}

// Normalize validates and normalizes a language preference string.
func Normalize(pref string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(pref)) {
	case "", Auto:
		return Auto, true
	case ZH:
		return ZH, true
	case EN:
		return EN, true
	default:
		return "", false
	}
}

// Resolve maps a preference ("auto", "zh", "en") to a concrete locale ("zh" or "en").
func Resolve(pref string) string {
	switch strings.ToLower(strings.TrimSpace(pref)) {
	case ZH:
		return ZH
	case EN:
		return EN
	default:
		return detectSystemLang()
	}
}

// Init sets the active locale from a preference ("auto", "zh", "en").
func Init(pref string) string {
	code := Resolve(pref)
	Set(code)
	return code
}

// Set activates a concrete locale ("zh" or "en").
func Set(lang string) {
	if strings.EqualFold(strings.TrimSpace(lang), EN) {
		current.Store(&enMessages)
		langVal.Store(EN)
		return
	}
	current.Store(&zhMessages)
	langVal.Store(ZH)
}

// AllSettingsTitles returns every locale's settings window title for single-instance activation.
func AllSettingsTitles() []string {
	return []string{zhMessages.WindowTitle, enMessages.WindowTitle}
}
