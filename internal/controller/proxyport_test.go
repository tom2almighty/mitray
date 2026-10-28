package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/tom2almighty/mitray/internal/config"
)

func TestProxyPortReadsSelectedConfigWithoutCore(t *testing.T) {
	ctx := context.Background()
	c, _, core, _, _ := fixture(t, false, nil)
	c.cfg.AutoStartCore = false
	if err := c.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	if core.starts != 0 {
		t.Fatal("fixture started a core")
	}
	if port, err := c.ProxyPort(ctx); err != nil || port != 7890 {
		t.Fatalf("mixed-port: got %d err %v", port, err)
	}
	path := c.cfg.Profiles["default"].Path
	if err := os.WriteFile(path, []byte("port: 7891\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if port, err := c.ProxyPort(ctx); err != nil || port != 7891 {
		t.Fatalf("port fallback: got %d err %v", port, err)
	}
	if err := os.WriteFile(path, []byte("tun:\n  enable: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ProxyPort(ctx); err == nil {
		t.Fatal("missing proxy port must be reported")
	}
}

func TestProxyPortPrefersLiveCoreState(t *testing.T) {
	ctx := context.Background()
	c, _, _, _, _ := fixture(t, false, nil)
	if err := c.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	if !c.Snapshot().Running {
		t.Fatal("fixture core did not start")
	}
	path := c.cfg.Profiles["default"].Path
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if port, err := c.ProxyPort(ctx); err != nil || port != 7890 {
		t.Fatalf("live port not preferred over disk: got %d err %v", port, err)
	}
	if err := os.WriteFile(path, []byte("mixed-port: 9999\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if port, err := c.ProxyPort(ctx); err != nil || port != 9999 {
		t.Fatalf("stopped core must fall back to the config: got %d err %v", port, err)
	}
}

func TestProxyPortUsesRemoteCacheOnly(t *testing.T) {
	ctx := context.Background()
	c, _, _, _, _ := fixture(t, false, nil)
	c.cfg.AutoStartCore = false
	c.cfg.Profiles["default"] = config.Profile{Type: config.ProfileURL, URL: "https://example.com/mihomo.yaml"}
	if err := c.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ProxyPort(ctx); err == nil {
		t.Fatal("missing cache must be reported")
	}
	hash := sha256.Sum256([]byte(c.cfg.Profiles["default"].URL))
	cache := filepath.Join(c.base, "cache", fmt.Sprintf("%x.yaml", hash[:16]))
	if err := os.MkdirAll(filepath.Dir(cache), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, []byte("mixed-port: 7892\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if port, err := c.ProxyPort(ctx); err != nil || port != 7892 {
		t.Fatalf("cached config ignored: got %d err %v", port, err)
	}
}
