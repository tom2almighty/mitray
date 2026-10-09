package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	changes int
	level   string
}

func (f *fakeSystem) Proxy() (bool, error)             { return f.proxy, nil }
func (f *fakeSystem) SetProxy(b bool, p int) error     { f.proxy = b; f.changes++; return nil }
func (f *fakeSystem) Startup() (string, error)         { return "off", nil }
func (f *fakeSystem) SetStartup(l string, d int) error { f.level = l; return nil }

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
	if f.getFail || f.delayGets > 0 {
		f.delayGets--
		http.Error(w, "not ready", 503)
		return
	}
	fmt.Fprintf(w, `{"tun":{"device":"custom-adapter","stack":"system","dns-hijack":["any:53"],"enable":%t},"mixed-port":7890}`, f.enabled)
}

func fixture(t *testing.T, initial bool, remembered *bool) (*Controller, *apiFixture, *fakeCore, *fakeSystem, *fakeStore) {
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
	cfg.ConfigPath = path
	cfg.TUNEnabled = remembered
	core, system, store := &fakeCore{}, &fakeSystem{}, &fakeStore{}
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

func TestBootTUNMatrix(t *testing.T) {
	for _, yamlState := range []bool{false, true} {
		for _, memory := range []*bool{nil, ptr(false), ptr(true)} {
			name := fmt.Sprintf("yaml=%t remembered=%v", yamlState, memory)
			t.Run(name, func(t *testing.T) {
				c, api, _, system, store := fixture(t, yamlState, memory)
				if err := c.Boot(context.Background()); err != nil {
					t.Fatal(err)
				}
				want := yamlState
				if memory != nil {
					want = *memory
				}
				if api.enabled != want {
					t.Fatalf("got %t want %t", api.enabled, want)
				}
				patches := 0
				if want != yamlState {
					patches = 1
				}
				if api.patches != patches {
					t.Fatalf("patches %d want %d", api.patches, patches)
				}
				if len(store.saved) != 0 {
					t.Fatal("boot must not learn observed state")
				}
				if system.changes != 0 {
					t.Fatal("TUN must not change system proxy")
				}
			})
		}
	}
}

func TestToggleRememberAndRefreshIsReadOnly(t *testing.T) {
	c, api, _, system, store := fixture(t, false, nil)
	ctx := context.Background()
	if err := c.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.ToggleTUN(ctx); err != nil {
		t.Fatal(err)
	}
	if len(store.saved) != 1 || store.saved[0].TUNEnabled == nil || !*store.saved[0].TUNEnabled {
		t.Fatal("successful toggle not remembered")
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
	if !*c.Snapshot().Settings.TUNEnabled || *c.Snapshot().TUN {
		t.Fatal("actual and remembered state conflated")
	}
	if system.changes != 0 {
		t.Fatal("TUN changed proxy")
	}
}

func TestFailuresKeepPreferenceAndUnknownState(t *testing.T) {
	c, api, _, _, store := fixture(t, false, ptr(false))
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
	if len(store.saved) != 0 || *c.Snapshot().Settings.TUNEnabled {
		t.Fatal("failure changed memory")
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
	if *c.Snapshot().Settings.TUNEnabled {
		t.Fatal("refresh failure changed preference")
	}
}

func TestPersistenceFailureIsReported(t *testing.T) {
	c, api, _, _, store := fixture(t, false, ptr(false))
	ctx := context.Background()
	if err := c.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	store.err = errors.New("disk full")
	if err := c.ToggleTUN(ctx); err == nil {
		t.Fatal("must report disk failure")
	}
	if !api.enabled || !*c.Snapshot().TUN {
		t.Fatal("must display the actual applied state")
	}
	if *c.Snapshot().Settings.TUNEnabled {
		t.Fatal("failed disk write claimed persistence")
	}
}

func TestDelayedReadinessAndRestartOrder(t *testing.T) {
	c, api, core, _, _ := fixture(t, false, ptr(true))
	api.delayGets = 3
	ctx := context.Background()
	if err := c.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	api.enabled = false
	api.mu.Unlock()
	if err := c.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(core.events) != "[start stop start]" {
		t.Fatalf("wrong lifecycle order: %v", core.events)
	}
	if !api.enabled {
		t.Fatal("restart did not restore preference")
	}
}

func TestStaleSettingsCannotOverwriteTUN(t *testing.T) {
	c, _, _, _, _ := fixture(t, false, nil)
	ctx := context.Background()
	if err := c.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	draft := c.Snapshot().Settings
	if err := c.ToggleTUN(ctx); err != nil {
		t.Fatal(err)
	}
	draft.StartupDelay = 25
	if err := c.SaveSettings(ctx, draft); err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().Settings.TUNEnabled == nil || !*c.Snapshot().Settings.TUNEnabled {
		t.Fatal("open settings form overwrote latest TUN preference")
	}
}

func TestConcurrentTogglesAreSerialized(t *testing.T) {
	c, api, _, _, store := fixture(t, false, nil)
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
	if api.enabled || len(store.saved) != 10 {
		t.Fatalf("lost toggles: state=%t saves=%d", api.enabled, len(store.saved))
	}
}

func ptr(b bool) *bool { return &b }
