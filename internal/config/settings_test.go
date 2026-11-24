package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTOMLRoundTrip(t *testing.T) {
	base := t.TempDir()
	store := Store{Path: filepath.Join(base, Filename)}
	settings := Default()
	settings.CorePath = `D:\程序\mihomo.exe`
	settings.ActiveProfile = "work.prod 北京"
	settings.Profiles = map[string]Profile{
		"work.prod 北京": {Type: ProfileLocal, Path: `D:\配置\工作.yaml`},
		"remote":       {Type: ProfileURL, URL: "https://example.com/config?token=abc&mode=rule#part"},
		"spare":        {Type: ProfileLocal, Path: "configs/spare.yaml"},
	}
	settings.AutoStartCore = false
	settings.StartupLevel = "admin"
	settings.StartupDelay = 42

	if err := store.Save(settings); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"[core]",
		`active_profile = "work.prod 北京"`,
		`[profiles."work.prod 北京"]`,
		`type = "url"`,
		"delay_seconds = 42",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("saved TOML is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "tun_enabled") || strings.Contains(text, "[state]") {
		t.Fatalf("saved TOML should not contain legacy TUN state:\n%s", text)
	}

	round, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(round, settings) {
		t.Fatalf("round trip changed settings:\ngot  %+v\nwant %+v", round, settings)
	}
}

func TestTOMLIgnoresLegacyTUNState(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), Filename)}
	content := `[core]
path = ''
active_profile = "default"

[profiles.default]
type = "local"
path = ''

[state]
tun_enabled = true
`
	if err := os.WriteFile(store.Path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := store.Load()
	if err != nil {
		t.Fatalf("legacy state section should be ignored, got error: %v", err)
	}
	if err := store.Save(settings); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "tun_enabled") || strings.Contains(string(data), "[state]") {
		t.Fatalf("resaved TOML should omit legacy state:\n%s", data)
	}
}

func TestTOMLUnknownKeyRejected(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), Filename)}
	content := `[core]
path = ''
active_profile = "default"
typo = true

[profiles.default]
type = "local"
path = ''
`
	if err := os.WriteFile(store.Path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := store.Load()
	if err == nil || !strings.Contains(err.Error(), "core.typo") {
		t.Fatalf("expected unknown key error, got %v", err)
	}
}

func TestTOMLLoadDefaultsWhenFileIsMissing(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), Filename)}
	settings, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.ActiveProfile != "default" || !settings.AutoStartCore || settings.StartupDelay != 15 {
		t.Fatalf("unexpected defaults: %+v", settings)
	}
}

func TestShippedTOMLParses(t *testing.T) {
	store := Store{Path: filepath.Join("..", "..", Filename)}
	settings, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.ActiveProfile != "default" || settings.Profiles["default"].Type != ProfileLocal {
		t.Fatalf("unexpected shipped settings: %+v", settings)
	}
}
