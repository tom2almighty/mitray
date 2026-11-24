package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tom2almighty/mitray/internal/config"
)

type fakeCore struct {
	pid           int
	starts, stops int
	events        []string
	onAlive       func()
}

func (f *fakeCore) Find(string) (int, error) { return f.pid, nil }
func (f *fakeCore) Start(exe, path, dir string) (int, error) {
	f.pid = 100 + f.starts
	f.starts++
	f.events = append(f.events, "start")
	return f.pid, nil
}
func (f *fakeCore) Stop(pid int, exe string) error {
	f.pid = 0
	f.stops++
	f.events = append(f.events, "stop")
	return nil
}
func (f *fakeCore) Alive(pid int, exe string) bool {
	if f.onAlive != nil {
		f.onAlive()
	}
	return pid != 0 && pid == f.pid
}

type fakeSystem struct {
	proxy   bool
	server  string
	changes int
	level   string
	admin   bool
}

func (f *fakeSystem) Proxy() (bool, string, error) { return f.proxy, f.server, nil }
func (f *fakeSystem) SetProxy(b bool, p int) error {
	f.proxy = b
	if b {
		f.server = fmt.Sprintf("127.0.0.1:%d", p)
	}
	f.changes++
	return nil
}
func (f *fakeSystem) Startup() (string, error)         { return "off", nil }
func (f *fakeSystem) SetStartup(l string, d int) error { f.level = l; return nil }
func (f *fakeSystem) IsAdmin() bool                    { return f.admin }

type fakeStore struct {
	saved []config.Settings
	err   error
}

func (f *fakeStore) Save(s config.Settings) error {
	if f.err != nil {
		return f.err
	}
	f.saved = append(f.saved, s.Clone())
	return nil
}

type apiFixture struct {
	mu        sync.Mutex
	enabled   bool
	patches   int
	getFail   bool
	patchFail bool
	delayGets int
	gets      int
	authFail  bool
}

func (f *apiFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path != "/configs" {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodPatch {
		f.patches++
		var payload map[string]map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "bad JSON", 400)
			return
		}
		if len(payload) != 1 || len(payload["tun"]) != 1 || payload["tun"]["enable"] == nil {
			http.Error(w, "must only PATCH enable", 400)
			return
		}
		if f.patchFail {
			http.Error(w, "failure", 500)
			return
		}
		if err := json.Unmarshal(payload["tun"]["enable"], &f.enabled); err != nil {
			http.Error(w, "invalid", 400)
			return
		}
		w.WriteHeader(204)
		return
	}
	f.gets++
	if f.authFail {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if f.getFail || f.delayGets > 0 {
		f.delayGets--
		http.Error(w, "not ready", 503)
		return
	}
	fmt.Fprintf(w, `{"tun":{"device":"custom-adapter","stack":"system","dns-hijack":["any:53"],"enable":%t},"mixed-port":7890}`, f.enabled)
}

func TestAuthenticationFailureDoesNotRetry(t *testing.T) {
	c, api, _, _, _ := fixture(t, false)
	api.authFail = true
	if err := c.Boot(context.Background()); err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("expected secret error, got %v", err)
	}
	if api.gets != 1 {
		t.Fatalf("authentication failure was retried %d times", api.gets)
	}
}

func fixture(t *testing.T, initial bool) (*Controller, *apiFixture, *fakeCore, *fakeSystem, *fakeStore) {
	t.Helper()
	api := &apiFixture{enabled: initial}
	srv := httptest.NewServer(http.HandlerFunc(api.serve))
	t.Cleanup(srv.Close)
	base := t.TempDir()
	exe := filepath.Join(base, "mihomo.exe")
	path := filepath.Join(base, "config.yaml")
	if err := os.WriteFile(exe, []byte("fake"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(fmt.Sprintf("external-controller: %s\nmixed-port: 7890\ntun:\n  enable: %t\n  device: custom-adapter\n", srv.URL, initial)), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.CorePath = exe
	cfg.Profiles["default"] = config.Profile{Type: config.ProfileLocal, Path: path}
	core, system, store := &fakeCore{}, &fakeSystem{admin: true}, &fakeStore{}
	c := New(base, cfg, store, core, system)
	c.readyTimeout = time.Second
	c.retryInterval = time.Millisecond
	t.Cleanup(func() {
		if c.client != nil {
			c.client.Close()
		}
	})
	return c, api, core, system, store
}

func TestBootFollowsYAMLWithoutPatching(t *testing.T) {
	for _, yamlState := range []bool{false, true} {
		t.Run(fmt.Sprintf("yaml=%t", yamlState), func(t *testing.T) {
			c, api, _, system, store := fixture(t, yamlState)
			if err := c.Boot(context.Background()); err != nil {
				t.Fatal(err)
			}
			if api.enabled != yamlState {
				t.Fatalf("got %t want %t", api.enabled, yamlState)
			}
			if api.patches != 0 {
				t.Fatalf("boot sent %d PATCH requests, want 0", api.patches)
			}
			if len(store.saved) != 0 {
				t.Fatal("boot must not persist TUN state")
			}
			if system.changes != 0 {
				t.Fatal("TUN must not change system proxy")
			}
			if c.Snapshot().ConfigTUNEnabled != yamlState {
				t.Fatalf("ConfigTUNEnabled = %t, want %t", c.Snapshot().ConfigTUNEnabled, yamlState)
			}
		})
	}
}

func TestToggleTUNIsEphemeralAndRefreshIsReadOnly(t *testing.T) {
	c, api, _, system, store := fixture(t, false)
	ctx := context.Background()
	if err := c.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.ToggleTUN(ctx); err != nil {
		t.Fatal(err)
	}
	if !api.enabled || !*c.Snapshot().TUN {
		t.Fatal("manual toggle did not enable TUN")
	}
	if len(store.saved) != 0 {
		t.Fatal("manual toggle must not persist TUN state")
	}
	api.mu.Lock()
	api.enabled = false
	api.mu.Unlock() // External WebUI change.
	if err := c.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if api.patches != 1 {
		t.Fatal("refresh fought an external change")
	}
	if *c.Snapshot().TUN {
		t.Fatal("refresh did not observe external TUN state")
	}
	if system.changes != 0 {
		t.Fatal("TUN changed proxy")
	}
}

func TestFailuresLeaveTUNUnknownWithoutSaving(t *testing.T) {
	c, api, _, _, store := fixture(t, false)
	ctx := context.Background()
	if err := c.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	api.patchFail = true
	api.mu.Unlock()
	if err := c.ToggleTUN(ctx); err == nil {
		t.Fatal("expected failed PATCH")
	}
	if len(store.saved) != 0 {
		t.Fatal("failure wrote settings")
	}
	api.mu.Lock()
	api.getFail = true
	api.mu.Unlock()
	if err := c.Refresh(ctx); err == nil {
		t.Fatal("expected API failure")
	}
	if c.Snapshot().TUN != nil {
		t.Fatal("unreachable API must show unknown")
	}
}

func TestDelayedReadinessAndRestartFollowsYAML(t *testing.T) {
	c, api, core, _, store := fixture(t, false)
	api.delayGets = 3
	ctx := context.Background()
	if err := c.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.ToggleTUN(ctx); err != nil {
		t.Fatal(err)
	}
	// Simulate the restarted core booting from YAML (tun.enable: false).
	api.mu.Lock()
	api.enabled = false
	api.mu.Unlock()
	if err := c.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(core.events) != "[start stop start]" {
		t.Fatalf("wrong lifecycle order: %v", core.events)
	}
	if api.enabled || api.patches != 1 || len(store.saved) != 0 {
		t.Fatalf("restart must follow YAML without re-applying prior toggle: enabled=%t patches=%d saves=%d", api.enabled, api.patches, len(store.saved))
	}
}

func TestConcurrentTogglesAreSerialized(t *testing.T) {
	c, api, _, _, store := fixture(t, false)
	ctx := context.Background()
	if err := c.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.ToggleTUN(ctx); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if api.enabled || len(store.saved) != 0 {
		t.Fatalf("unexpected concurrent toggle state: enabled=%t saves=%d", api.enabled, len(store.saved))
	}
}

func TestNonAdminTUNWarningsAndFastFail(t *testing.T) {
	t.Run("boot hints when YAML enables TUN without admin", func(t *testing.T) {
		c, api, _, system, _ := fixture(t, true)
		api.enabled = false // Core could not create TUN without admin.
		system.admin = false
		if err := c.Boot(context.Background()); err != nil {
			t.Fatalf("unexpected boot error: %v", err)
		}
		if api.patches != 0 {
			t.Fatalf("boot sent %d PATCH requests, want 0", api.patches)
		}
		if !strings.Contains(c.Snapshot().Notice, "管理员") {
			t.Fatalf("expected admin notice, got %q", c.Snapshot().Notice)
		}
	})

	t.Run("manual toggle fast-fails without admin", func(t *testing.T) {
		c, api, _, system, store := fixture(t, false)
		system.admin = false
		ctx := context.Background()
		if err := c.Boot(ctx); err != nil {
			t.Fatal(err)
		}
		err := c.ToggleTUN(ctx)
		if err == nil || !strings.Contains(err.Error(), "管理员") {
			t.Fatalf("expected non-admin toggle error, got %v", err)
		}
		if api.patches != 0 || len(store.saved) != 0 {
			t.Fatalf("non-admin toggle sent %d patches or saved %d times", api.patches, len(store.saved))
		}
	})
}

func TestMissingTUNSectionHintWhenPATCHDoesNotEnable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			w.WriteHeader(204)
			return
		}
		_, _ = w.Write([]byte(`{"tun":{"enable":false},"mixed-port":7890}`))
	}))
	defer srv.Close()

	base := t.TempDir()
	exe := filepath.Join(base, "mihomo.exe")
	path := filepath.Join(base, "config.yaml")
	if err := os.WriteFile(exe, []byte("fake"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(fmt.Sprintf("external-controller: %s\nmixed-port: 7890\n", srv.URL)), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.CorePath = exe
	cfg.Profiles["default"] = config.Profile{Type: config.ProfileLocal, Path: path}
	c := New(base, cfg, &fakeStore{}, &fakeCore{}, &fakeSystem{admin: true})
	c.readyTimeout = time.Second
	c.retryInterval = time.Millisecond
	defer func() {
		if c.client != nil {
			c.client.Close()
		}
	}()
	ctx := context.Background()
	if err := c.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	err := c.ToggleTUN(ctx)
	if err == nil || !strings.Contains(err.Error(), "未包含 tun 段落") {
		t.Fatalf("expected missing tun section hint, got %v", err)
	}
}

func TestStopTerminatesCoreDirectlyWithoutPatchingTUN(t *testing.T) {
	c, api, core, _, store := fixture(t, true)
	ctx := context.Background()
	if err := c.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if api.patches != 0 {
		t.Fatalf("Stop sent %d PATCH requests, want 0", api.patches)
	}
	if core.stops != 1 {
		t.Fatalf("expected 1 core stop, got %d", core.stops)
	}
	if len(store.saved) != 0 {
		t.Fatal("Stop must not modify settings")
	}
}

func TestActiveConfigPath(t *testing.T) {
	t.Run("local file", func(t *testing.T) {
		c, _, _, _, _ := fixture(t, false)
		got, err := c.ActiveConfigPath()
		if err != nil {
			t.Fatal(err)
		}
		if got != c.cfg.Profiles["default"].Path {
			t.Fatalf("got %q want %q", got, c.cfg.Profiles["default"].Path)
		}
	})

	t.Run("remote cache", func(t *testing.T) {
		c, _, _, _, _ := fixture(t, false)
		url := "https://example.com/sub.yaml"
		c.cfg.Profiles["default"] = config.Profile{Type: config.ProfileURL, URL: url}
		c.publish()
		if _, err := c.ActiveConfigPath(); err == nil || !strings.Contains(err.Error(), "尚未缓存") {
			t.Fatalf("expected uncached remote error, got %v", err)
		}
		cachePath := filepath.Join(c.base, "cache", remoteCacheName(url))
		if err := os.MkdirAll(filepath.Dir(cachePath), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cachePath, []byte("mixed-port: 7890\n"), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := c.ActiveConfigPath()
		if err != nil {
			t.Fatal(err)
		}
		if got != cachePath {
			t.Fatalf("got %q want %q", got, cachePath)
		}
	})
}
