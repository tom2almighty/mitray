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
	enabled := false
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
	settings.TUNEnabled = &enabled

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
		"tun_enabled = false",
		"delay_seconds = 42",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("saved TOML is missing %q:\n%s", want, text)
		}
	}

	round, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(round, settings) {
		t.Fatalf("round trip changed settings:\ngot  %+v\nwant %+v", round, settings)
	}
}

func TestTOMLTUNOverridePresence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value *bool
		key   bool
	}{
		{"follow YAML", nil, false},
		{"disabled", boolPtr(false), true},
		{"enabled", boolPtr(true), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := Store{Path: filepath.Join(t.TempDir(), Filename)}
			settings := Default()
			settings.TUNEnabled = tc.value
			if err := store.Save(settings); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(store.Path)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(string(data), "tun_enabled"); got != tc.key {
				t.Fatalf("tun_enabled presence = %t, want %t:\n%s", got, tc.key, data)
			}
			round, err := store.Load()
			if err != nil {
				t.Fatal(err)
			}
			assertBool(t, round.TUNEnabled, tc.value)
		})
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
	if settings.ActiveProfile != "default" || !settings.AutoStartCore || settings.StartupDelay != 15 || settings.TUNEnabled != nil {
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

func boolPtr(b bool) *bool { return &b }

func assertBool(t *testing.T, got, want *bool) {
	t.Helper()
	if got == nil || want == nil {
		if got != want {
			t.Fatalf("got %v, want %v", got, want)
		}
		return
	}
	if *got != *want {
		t.Fatalf("got %v, want %v", *got, *want)
	}
}
