package config

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/tom2almighty/mitray/internal/storage"
	"gopkg.in/ini.v1"
)

type Settings struct {
	CorePath, ConfigPath, ConfigURL, ActiveProfile string
	Profiles                                       map[string]string
	AutoStartCore                                  bool
	StartupLevel                                   string // off, normal, admin; empty means discover an existing task
	StartupDelay                                   int
	TUNEnabled                                     *bool // nil means the user has not overridden the YAML setting
}

func Default() Settings {
	return Settings{Profiles: map[string]string{}, ActiveProfile: "default", AutoStartCore: true, StartupDelay: 15}
}

func (s Settings) Clone() Settings {
	p := make(map[string]string, len(s.Profiles))
	for k, v := range s.Profiles {
		p[k] = v
	}
	s.Profiles = p
	if s.TUNEnabled != nil {
		b := *s.TUNEnabled
		s.TUNEnabled = &b
	}
	return s
}

func (s Settings) ProfileNames() []string {
	names := make([]string, 0, len(s.Profiles))
	for name := range s.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s Settings) Resolve(base, path string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	return filepath.Clean(path)
}

func (s Settings) Validate(base string) error {
	if err := regularFile(s.Resolve(base, s.CorePath)); err != nil {
		return fmt.Errorf("请选择有效的 mihomo 核心文件: %w", err)
	}
	if s.ConfigURL != "" {
		u, err := url.Parse(s.ConfigURL)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
			return fmt.Errorf("远程配置地址必须是完整的 HTTP 或 HTTPS URL")
		}
	} else if err := regularFile(s.Resolve(base, s.ConfigPath)); err != nil {
		return fmt.Errorf("请选择有效的 mihomo 配置文件: %w", err)
	}
	if s.StartupDelay < 0 || s.StartupDelay > 600 {
		return fmt.Errorf("自启延迟应为 0–600 秒")
	}
	switch s.StartupLevel {
	case "", "off", "normal", "admin":
	default:
		return fmt.Errorf("无效的开机自启选项")
	}
	return nil
}

func regularFile(path string) error {
	if path == "" {
		return fmt.Errorf("路径为空")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("所选路径是文件夹")
	}
	return nil
}

type Store struct{ Path string }

func readINI(path string) (*ini.File, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ini.Empty(), nil
	}
	if err != nil {
		return nil, err
	}
	// AHK IniWrite can produce UTF-16LE files.
	if len(b) >= 2 && b[0] == 0xff && b[1] == 0xfe {
		b = decodeUTF16(b[2:])
	}
	return ini.LoadSources(ini.LoadOptions{IgnoreInlineComment: true, PreserveSurroundedQuote: true}, b)
}

func (st Store) Load() (Settings, error) {
	s := Default()
	f, err := readINI(st.Path)
	if err != nil {
		return s, err
	}
	m, opts := f.Section("Mihomo"), f.Section("Settings")
	s.CorePath = m.Key("CorePath").Value()
	s.ConfigPath = m.Key("ConfigPath").Value()
	s.ConfigURL = m.Key("ConfigURL").Value()
	if v := m.Key("ActiveProfile").Value(); v != "" {
		s.ActiveProfile = v
	}
	for _, k := range f.Section("Profiles").Keys() {
		if k.Value() != "" {
			s.Profiles[k.Name()] = k.Value()
		}
	}
	if len(s.Profiles) == 0 && s.ConfigPath != "" {
		s.Profiles[s.ActiveProfile] = s.ConfigPath
	}
	if path := s.Profiles[s.ActiveProfile]; s.ConfigURL == "" && path != "" {
		s.ConfigPath = path
	}
	s.AutoStartCore = opts.Key("AutoStartCore").MustBool(true)
	s.StartupDelay = opts.Key("AutoStartupDelaySec").MustInt(15)
	if s.StartupDelay < 0 || s.StartupDelay > 600 {
		s.StartupDelay = 15
	}
	s.StartupLevel = opts.Key("AutoStartupLevel").Value()
	hasState := f.HasSection("State")
	state := f.Section("State")
	// Once State exists, a missing key is intentional ("follow YAML"). Do not
	// resurrect an old Settings/TUNEnabled after the user clears the override.
	if hasState {
		if state.HasKey("TUNEnabled") {
			s.TUNEnabled, err = parseBool(state.Key("TUNEnabled").Value())
		}
	} else if opts.Key("TUNControl").Value() != "file" && opts.Key("RememberTUN").MustBool(true) && opts.HasKey("TUNEnabled") {
		s.TUNEnabled, err = parseBool(opts.Key("TUNEnabled").Value())
	}
	return s, err
}

func parseBool(v string) (*bool, error) {
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return nil, fmt.Errorf("保存的 TUN 状态无效，请将 TUNEnabled 设为 0 或 1")
	}
	return &b, nil
}

func (st Store) Save(s Settings) error {
	f, err := readINI(st.Path)
	if err != nil {
		return err
	}
	m, opts := f.Section("Mihomo"), f.Section("Settings")
	for k, v := range map[string]string{"CorePath": s.CorePath, "ConfigPath": s.ConfigPath, "ConfigURL": s.ConfigURL, "ActiveProfile": s.ActiveProfile} {
		m.Key(k).SetValue(v)
	}
	f.DeleteSection("Profiles")
	for _, name := range s.ProfileNames() {
		f.Section("Profiles").Key(name).SetValue(s.Profiles[name])
	}
	opts.Key("AutoStartCore").SetValue(boolINI(s.AutoStartCore))
	opts.Key("AutoStartupDelaySec").SetValue(strconv.Itoa(s.StartupDelay))
	opts.Key("AutoStartupLevel").SetValue(s.StartupLevel)
	for _, k := range []string{"TUNControl", "TUNEnabled", "RememberTUN", "AutoRestoreTUN"} {
		opts.DeleteKey(k)
	}
	state := f.Section("State")
	state.Key("Schema").SetValue("1")
	state.DeleteKey("TUNEnabled")
	if s.TUNEnabled != nil {
		state.Key("TUNEnabled").SetValue(boolINI(*s.TUNEnabled))
	}
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		return err
	}
	return storage.WriteFile(st.Path, buf.Bytes())
}

func boolINI(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
