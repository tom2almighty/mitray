package controller

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBootConnectsExistingCoreWithoutRestoring(t *testing.T) {
	for _, autoStart := range []bool{false, true} {
		c, api, core, _, store := fixture(t, true, ptr(false))
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

func TestRefreshRestoresNewCoreOnce(t *testing.T) {
	for _, tc := range []struct {
		name    string
		memory  *bool
		actual  bool
		gap     bool
		patches int
	}{
		{"remember off", ptr(false), true, false, 1},
		{"remember on after exit", ptr(true), false, true, 1},
		{"already matches", ptr(false), false, false, 0},
		{"no memory", nil, true, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, api, core, system, store := fixture(t, false, tc.memory)
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
			if tc.memory != nil {
				api.delayGets = 2
			}
			api.mu.Unlock()
			if err := c.Refresh(ctx); err != nil {
				t.Fatal(err)
			}
			want := tc.actual
			if tc.memory != nil {
				want = *tc.memory
			}
			if api.enabled != want || *c.Snapshot().TUN != want || c.Snapshot().PID != 200 || api.patches-before != tc.patches {
				t.Fatal("new core did not restore and display the expected state")
			}
			// A later WebUI toggle or same-process reload must remain effective.
			api.mu.Lock()
			api.enabled = !want
			api.mu.Unlock()
			for range 2 {
				if err := c.Refresh(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if *c.Snapshot().TUN == want || api.patches-before != tc.patches {
				t.Fatal("refresh overrode an external change")
			}
			if len(store.saved) != 0 || system.changes != 0 || core.starts != 1 {
				t.Fatal("recovery changed memory, proxy, or started another core")
			}
		})
	}
}

func TestFailedRecoveryIsNotRearmedByRefresh(t *testing.T) {
	for _, failure := range []string{"readiness", "patch"} {
		t.Run(failure, func(t *testing.T) {
			c, api, core, _, store := fixture(t, false, ptr(false))
			ctx := context.Background()
			if err := c.Boot(ctx); err != nil {
				t.Fatal(err)
			}
			c.readyTimeout = 30 * time.Millisecond
			core.pid = 200
			api.mu.Lock()
			api.enabled = true
			api.getFail = failure == "readiness"
			api.patchFail = failure == "patch"
			api.mu.Unlock()
			if err := c.Refresh(ctx); !errors.Is(err, ErrTUNRestore) {
				t.Fatalf("missing recovery failure notification: %v", err)
			}
			patches := api.patches
			api.mu.Lock()
			api.getFail, api.patchFail = false, false
			api.mu.Unlock()
			if err := c.Refresh(ctx); err != nil {
				t.Fatal(err)
			}
			if !*c.Snapshot().TUN || api.patches != patches || *c.Snapshot().Settings.TUNEnabled || len(store.saved) != 0 {
				t.Fatal("failed recovery was retried or changed the preference")
			}
			core.pid = 201
			if err := c.Refresh(ctx); err != nil {
				t.Fatal(err)
			}
			if *c.Snapshot().TUN || api.patches != patches+1 {
				t.Fatal("next core did not get its own recovery attempt")
			}
		})
	}
}

func TestAPIReconnectDoesNotRestoreTUN(t *testing.T) {
	c, api, _, _, _ := fixture(t, false, ptr(false))
	ctx := context.Background()
	if err := c.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	api.enabled, api.getFail = true, true
	api.mu.Unlock()
	if err := c.Refresh(ctx); err == nil || errors.Is(err, ErrTUNRestore) {
		t.Fatalf("API disconnect was treated as a core restart: %v", err)
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

func TestCoreReplacementStopsOldRecovery(t *testing.T) {
	c, api, core, _, _ := fixture(t, false, ptr(false))
	ctx := context.Background()
	if err := c.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	core.pid = 200
	api.mu.Lock()
	api.enabled, api.patchFail = true, true
	api.mu.Unlock()
	core.onAlive = func() {
		api.mu.Lock()
		defer api.mu.Unlock()
		if api.patches > 0 {
			core.pid = 201
		}
	}
	if err := c.Refresh(ctx); !errors.Is(err, ErrTUNRestore) {
		t.Fatalf("expected cancelled recovery: %v", err)
	}
	if api.patches != 1 || c.Snapshot().TUN != nil {
		t.Fatal("old recovery continued after the core was replaced")
	}
	core.onAlive = nil
	api.mu.Lock()
	api.patchFail = false
	api.mu.Unlock()
	if err := c.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().PID != 201 || *c.Snapshot().TUN || api.patches != 2 {
		t.Fatal("replacement core did not receive its own recovery")
	}
}

func TestForgetTUNLeavesCurrentAndFutureCoreUnchanged(t *testing.T) {
	c, api, core, _, store := fixture(t, false, ptr(false))
	ctx := context.Background()
	if err := c.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	api.enabled = true
	api.mu.Unlock()
	if err := c.ForgetTUN(); err != nil {
		t.Fatal(err)
	}
	core.pid = 200
	if err := c.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if api.patches != 0 || !*c.Snapshot().TUN || len(store.saved) != 1 || store.saved[0].TUNEnabled != nil {
		t.Fatal("cleared memory still affected the core")
	}
}
