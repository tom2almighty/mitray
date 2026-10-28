package controller

import (
	"context"
	"testing"
)

func TestProxyServerTargetsPort(t *testing.T) {
	for _, tc := range []struct {
		name   string
		server string
		port   int
		want   bool
	}{
		{"plain loopback", "127.0.0.1:7890", 7890, true},
		{"localhost", "localhost:7890", 7890, true},
		{"ipv6", "[::1]:7890", 7890, true},
		{"protocol map", "http=127.0.0.1:7890;https=127.0.0.1:7890", 7890, true},
		{"different port", "127.0.0.1:7891", 7890, false},
		{"remote host", "192.168.1.2:7890", 7890, false},
		{"invalid port", "127.0.0.1:7890", 0, false},
		{"multiple entries", "http=127.0.0.1:8080;https=127.0.0.1:7890", 7890, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := proxyServerTargetsPort(tc.server, tc.port); got != tc.want {
				t.Fatalf("got %t, want %t", got, tc.want)
			}
		})
	}
}

func TestProxyTargetsCoreRequiresEnabledProxy(t *testing.T) {
	c, _, core, system, _ := fixture(t, false, nil)
	ctx := context.Background()
	if err := c.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	system.proxy, system.server = true, "127.0.0.1:7890"
	if err := c.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if !c.Snapshot().ProxyTargetsCore {
		t.Fatal("enabled proxy should be recognized as targeting the core")
	}
	core.pid = 0
	if err := c.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if !c.Snapshot().ProxyTargetsCore {
		t.Fatal("stopped core must still be identified while the proxy points at it")
	}
	system.proxy = false
	if err := c.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().ProxyTargetsCore {
		t.Fatal("disabled proxy with a stale server must not target the core")
	}
}

func TestToggleProxyRequiresLiveCoreOnlyWhenEnabling(t *testing.T) {
	ctx := context.Background()

	t.Run("stale controller PID", func(t *testing.T) {
		c, _, core, system, _ := fixture(t, false, nil)
		if err := c.Boot(ctx); err != nil {
			t.Fatal(err)
		}
		core.pid = 200
		if err := c.ToggleProxy(ctx); err == nil {
			t.Fatal("proxy was enabled without a live known core")
		}
		if system.changes != 0 {
			t.Fatal("stale core changed the system proxy")
		}
	})

	t.Run("API failure", func(t *testing.T) {
		c, api, _, system, _ := fixture(t, false, nil)
		if err := c.Boot(ctx); err != nil {
			t.Fatal(err)
		}
		api.mu.Lock()
		api.getFail = true
		api.mu.Unlock()
		if err := c.ToggleProxy(ctx); err == nil {
			t.Fatal("proxy was enabled after an API failure")
		}
		if system.changes != 0 {
			t.Fatal("API failure changed the system proxy")
		}
	})

	t.Run("disable after core exit", func(t *testing.T) {
		c, _, core, system, _ := fixture(t, false, nil)
		if err := c.Boot(ctx); err != nil {
			t.Fatal(err)
		}
		system.proxy, system.server = true, "127.0.0.1:7890"
		core.pid = 0
		if err := c.ToggleProxy(ctx); err != nil {
			t.Fatal(err)
		}
		if system.proxy || system.changes != 1 {
			t.Fatal("disabled proxy should not require a running core")
		}
	})
}

func TestSnapshotRevisionOnlyChangesWithContentView(t *testing.T) {
	c, _, _, system, _ := fixture(t, false, nil)
	ctx := context.Background()
	if err := c.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	revision := c.Snapshot().Revision
	if err := c.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if got := c.Snapshot().Revision; got != revision {
		t.Fatalf("unchanged refresh changed revision from %d to %d", revision, got)
	}
	system.proxy = true
	if err := c.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if got := c.Snapshot().Revision; got <= revision {
		t.Fatalf("changed snapshot did not increase revision: %d -> %d", revision, got)
	}
}
