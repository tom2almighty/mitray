package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/tom2almighty/mitray/internal/storage"
)

const (
	Filename     = "mitray.toml"
	ProfileLocal = "local"
	ProfileURL   = "url"
)

type Profile struct {
	Type string `toml:"type"`
	Path string `toml:"path,omitempty"`
	URL  string `toml:"url,omitempty"`
}

type Settings struct {
	CorePath      string
	ActiveProfile string
	Profiles      map[string]Profile
	AutoStartCore bool
	StartupLevel  string // off, normal, admin; empty means discover an existing task
	StartupDelay  int
	TUNEnabled    *bool // nil means the user has not overridden the YAML setting
}

type fileSettings struct {
	Core     fileCore           `toml:"core"`
	Profiles map[string]Profile `toml:"profiles"`
	Startup  fileStartup        `toml:"startup"`
	State    fileState          `toml:"state"`
}

type fileCore struct {
	Path          string `toml:"path"`
	ActiveProfile string `toml:"active_profile"`
}

type fileStartup struct {
	LaunchCore   bool   `toml:"launch_core"`
	TaskLevel    string `toml:"task_level"`
	DelaySeconds int    `toml:"delay_seconds"`
}

type fileState struct {
	TUNEnabled *bool `toml:"tun_enabled,omitempty"`
}

func Default() Settings {
	return Settings{Profiles: map[string]Profile{}, ActiveProfile: "default", AutoStartCore: true, StartupDelay: 15}
}

func (s Settings) Clone() Settings {
	profiles := make(map[string]Profile, len(s.Profiles))
	for name, profile := range s.Profiles {
		profiles[name] = profile
	}
	s.Profiles = profiles
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

func (s Settings) ActiveProfileConfig() (Profile, bool) {
	profile, ok := s.Profiles[s.ActiveProfile]
	return profile, ok
}

func (s Settings) Resolve(base, path string) string {
	return resolvePath(base, path)
}

func (p Profile) Resolve(base string) string {
	return resolvePath(base, p.Path)
}

func (p Profile) Validate(base string) error {
	switch p.Type {
	case ProfileLocal:
		return regularFile(p.Resolve(base))
	case ProfileURL:
		u, err := url.Parse(p.URL)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
			return fmt.Errorf("远程配置地址必须是完整的 HTTP 或 HTTPS URL")
		}
		return nil
	default:
		return fmt.Errorf("配置来源类型必须是 local 或 url")
	}
}

func (s Settings) Validate(base string) error {
	if err := regularFile(resolvePath(base, s.CorePath)); err != nil {
		return fmt.Errorf("请选择有效的 mihomo 核心文件: %w", err)
	}
	if err := ValidateProfileName(s.ActiveProfile); err != nil {
		return err
	}
	profile, ok := s.ActiveProfileConfig()
	if !ok {
		return fmt.Errorf("请选择有效的 mihomo 配置档案")
	}
	if err := profile.Validate(base); err != nil {
		return fmt.Errorf("请选择有效的 mihomo 配置档案: %w", err)
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

func ValidateProfileName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("配置名称不能为空")
	}
	if name != strings.TrimSpace(name) {
		return fmt.Errorf("配置名称首尾不能包含空白")
	}
	if strings.ContainsAny(name, "\r\n\x00") {
		return fmt.Errorf("配置名称不能包含换行或空字符")
	}
	return nil
}

func resolvePath(base, path string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	return filepath.Clean(path)
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

func (s Settings) fileSettings() fileSettings {
	profiles := make(map[string]Profile, len(s.Profiles))
	for name, profile := range s.Profiles {
		profiles[name] = profile
	}
	return fileSettings{
		Core:     fileCore{Path: s.CorePath, ActiveProfile: s.ActiveProfile},
		Profiles: profiles,
		Startup:  fileStartup{LaunchCore: s.AutoStartCore, TaskLevel: s.StartupLevel, DelaySeconds: s.StartupDelay},
		State:    fileState{TUNEnabled: s.TUNEnabled},
	}
}

func (f fileSettings) settings() Settings {
	profiles := make(map[string]Profile, len(f.Profiles))
	for name, profile := range f.Profiles {
		profiles[name] = profile
	}
	s := Settings{
		CorePath:      f.Core.Path,
		ActiveProfile: f.Core.ActiveProfile,
		Profiles:      profiles,
		AutoStartCore: f.Startup.LaunchCore,
		StartupLevel:  f.Startup.TaskLevel,
		StartupDelay:  f.Startup.DelaySeconds,
	}
	if f.State.TUNEnabled != nil {
		b := *f.State.TUNEnabled
		s.TUNEnabled = &b
	}
	return s
}

func (st Store) Load() (Settings, error) {
	data, err := os.ReadFile(st.Path)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Default(), err
	}
	file := Default().fileSettings()
	metadata, err := toml.Decode(string(data), &file)
	if err != nil {
		return Default(), fmt.Errorf("解析 %s: %w", filepath.Base(st.Path), err)
	}
	if keys := metadata.Undecoded(); len(keys) > 0 {
		names := make([]string, 0, len(keys))
		for _, key := range keys {
			names = append(names, key.String())
		}
		return Default(), fmt.Errorf("%s 包含未知配置项: %s", filepath.Base(st.Path), strings.Join(names, ", "))
	}
	return file.settings(), nil
}

func (st Store) Save(s Settings) error {
	data, err := toml.Marshal(s.fileSettings())
	if err != nil {
		return fmt.Errorf("生成 %s: %w", filepath.Base(st.Path), err)
	}
	const header = "# MiTray 配置。相对路径以 MiTray 所在目录为准。\n" +
		"# type = \"local\" 使用 path；type = \"url\" 使用 url，远程配置缓存到 cache/。\n\n"
	return storage.WriteFile(st.Path, append([]byte(header), data...))
}
