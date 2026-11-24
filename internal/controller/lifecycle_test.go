package controller

import (
	"context"
	"testing"
)

func TestBootConnectsExistingCoreWithoutPatching(t *testing.T) {
	for _, autoStart := range []bool{false, true} {
		c, api, core, _, store := fixture(t, true)
		c.cfg.AutoStartCore = autoStart
		core.pid = 200
		ctx := context.Background()
		if err := c.Boot(ctx); err != nil {
			t.Fatal(err)
		}
		if err := c.Start(ctx); err != nil {
			t.Fatal(err)
		}
		if err := c.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
		if core.starts != 0 || api.patches != 0 || !*c.Snapshot().TUN || len(store.saved) != 0 {
			t.Fatalf("autoStart=%t: connecting changed the running core", autoStart)
		}
	}
}

func TestRefreshObservesExternalNewCoreWithoutOverridingTUN(t *testing.T) {
	for _, tc := range []struct {
		name   string
		actual bool
		gap    bool
	}{
		{"actual on", true, false},
		{"actual off after exit", false, true},
		{"actual off", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, api, core, system, store := fixture(t, false)
			ctx := context.Background()
			if err := c.Boot(ctx); err != nil {
				t.Fatal(err)
			}
			before := api.patches
			if tc.gap {
				core.pid = 0
				if err := c.Refresh(ctx); err != nil {
					t.Fatal(err)
				}
				if c.Snapshot().Running || c.Snapshot().TUN != nil {
					t.Fatal("exited core still displayed as running")
				}
			}
			core.pid = 200
			api.mu.Lock()
			api.enabled = tc.actual
			api.mu.Unlock()
			if err := c.Refresh(ctx); err != nil {
				t.Fatal(err)
			}
			if api.enabled != tc.actual || *c.Snapshot().TUN != tc.actual || c.Snapshot().PID != 200 || api.patches != before {
				t.Fatalf("refresh modified external core: enabled=%t want=%t patches=%d->%d", api.enabled, tc.actual, before, api.patches)
			}
			if len(store.saved) != 0 || system.changes != 0 || core.starts != 1 {
				t.Fatal("refresh changed settings, proxy, or started another core")
			}
		})
	}
}

func TestAPIReconnectDoesNotPatchTUN(t *testing.T) {
	c, api, _, _, _ := fixture(t, false)
	ctx := context.Background()
	if err := c.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	api.enabled, api.getFail = true, true
	api.mu.Unlock()
	if err := c.Refresh(ctx); err == nil {
		t.Fatal("expected API failure")
	}
	if c.Snapshot().TUN != nil {
		t.Fatal("unreachable API must display unknown")
	}
	api.mu.Lock()
	api.getFail = false
	api.mu.Unlock()
	if err := c.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if api.patches != 0 || !*c.Snapshot().TUN {
		t.Fatal("API reconnect overwrote the external state")
	}
}

func TestRenamingActiveProfileDoesNotRestartCore(t *testing.T) {
	c, _, core, _, _ := fixture(t, false)
	ctx := context.Background()
	if err := c.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	next := c.Snapshot().Settings
	profile := next.Profiles["default"]
	delete(next.Profiles, "default")
	next.Profiles["renamed"] = profile
	next.ActiveProfile = "renamed"
	if err := c.SaveSettings(ctx, next); err != nil {
		t.Fatal(err)
	}
	if core.starts != 1 || core.stops != 0 {
		t.Fatalf("profile rename restarted the core: %+v", core.events)
	}
}
