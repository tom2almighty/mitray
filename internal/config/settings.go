package config

import (
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/tom2almighty/mitray/internal/i18n"
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
	AutoStartCore bool
	Language      string
	Profiles      map[string]Profile
}

type fileSettings struct {
	Core     fileCore           `toml:"core"`
	Profiles map[string]Profile `toml:"profiles"`
}

type fileCore struct {
	Path          string `toml:"path"`
	ActiveProfile string `toml:"active_profile"`
	AutoStart     bool   `toml:"auto_start"`
	Language      string `toml:"language"`
}

func Default() Settings {
	return Settings{
		ActiveProfile: "default",
		AutoStartCore: true,
		Language:      i18n.Auto,
		Profiles: map[string]Profile{
			"default": {Type: ProfileLocal},
		},
	}
}

func (s Settings) Clone() Settings {
	profiles := make(map[string]Profile, len(s.Profiles))
	maps.Copy(profiles, s.Profiles)
	s.Profiles = profiles
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
			return fmt.Errorf("%s", i18n.T().ErrRemoteURLInvalid)
		}
		return nil
	default:
		return fmt.Errorf("%s", i18n.T().ErrProfileTypeInvalid)
	}
}

func (s Settings) Validate(base string) error {
	if err := regularFile(resolvePath(base, s.CorePath)); err != nil {
		return fmt.Errorf(i18n.T().ErrInvalidCoreFile, err)
	}
	if err := ValidateProfileName(s.ActiveProfile); err != nil {
		return err
	}
	profile, ok := s.ActiveProfileConfig()
	if !ok {
		return fmt.Errorf("%s", i18n.T().ErrSelectProfile)
	}
	if err := profile.Validate(base); err != nil {
		return fmt.Errorf(i18n.T().ErrInvalidProfile, err)
	}
	if _, ok := i18n.Normalize(s.Language); !ok {
		return fmt.Errorf("%s", i18n.T().ErrInvalidLanguage)
	}
	return nil
}

func ValidateProfileName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s", i18n.T().ErrProfileNameEmpty)
	}
	if name != strings.TrimSpace(name) {
		return fmt.Errorf("%s", i18n.T().ErrProfileNameSpaces)
	}
	if strings.ContainsAny(name, "\r\n\x00") {
		return fmt.Errorf("%s", i18n.T().ErrProfileNameControl)
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
		return fmt.Errorf("%s", i18n.T().ErrEmptyPath)
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s", i18n.T().ErrPathIsDir)
	}
	return nil
}

type Store struct{ Path string }

func (s Settings) fileSettings() fileSettings {
	profiles := make(map[string]Profile, len(s.Profiles))
	maps.Copy(profiles, s.Profiles)
	lang, ok := i18n.Normalize(s.Language)
	if !ok {
		lang = i18n.Auto
	}
	return fileSettings{
		Core:     fileCore{Path: s.CorePath, ActiveProfile: s.ActiveProfile, AutoStart: s.AutoStartCore, Language: lang},
		Profiles: profiles,
	}
}

func (f fileSettings) settings() Settings {
	profiles := make(map[string]Profile, len(f.Profiles))
	maps.Copy(profiles, f.Profiles)
	lang := f.Core.Language
	if lang == "" {
		lang = i18n.Auto
	}
	return Settings{
		CorePath:      f.Core.Path,
		ActiveProfile: f.Core.ActiveProfile,
		AutoStartCore: f.Core.AutoStart,
		Language:      lang,
		Profiles:      profiles,
	}
}

func (st Store) Load() (Settings, error) {
	data, err := os.ReadFile(st.Path)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Default(), err
	}
	file := fileSettings{
		Core:     fileCore{ActiveProfile: "default", AutoStart: true, Language: i18n.Auto},
		Profiles: map[string]Profile{},
	}
	metadata, err := toml.Decode(string(data), &file)
	if err != nil {
		return Default(), fmt.Errorf(i18n.T().ErrParseFile, filepath.Base(st.Path), err)
	}
	if keys := metadata.Undecoded(); len(keys) > 0 {
		names := make([]string, 0, len(keys))
		for _, key := range keys {
			names = append(names, key.String())
		}
		return Default(), fmt.Errorf(i18n.T().ErrUnknownKeys, filepath.Base(st.Path), strings.Join(names, ", "))
	}
	return file.settings(), nil
}

func (st Store) Save(s Settings) error {
	data, err := toml.Marshal(s.fileSettings())
	if err != nil {
		return fmt.Errorf(i18n.T().ErrMarshalFile, filepath.Base(st.Path), err)
	}
	return storage.WriteFile(st.Path, append([]byte(i18n.T().TOMLHeader), data...))
}
