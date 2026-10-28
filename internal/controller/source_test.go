package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tom2almighty/mitray/internal/config"
)

func remoteCachePath(base, address string) string {
	hash := sha256.Sum256([]byte(address))
	return filepath.Join(base, "cache", fmt.Sprintf("%x.yaml", hash[:16]))
}

func TestRemoteCorruptDownloadFallsBackToValidCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("mixed-port: ["))
	}))
	defer srv.Close()

	base := t.TempDir()
	cache := remoteCachePath(base, srv.URL)
	if err := os.MkdirAll(filepath.Dir(cache), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, []byte("mixed-port: 7891\n"), 0600); err != nil {
		t.Fatal(err)
	}

	src, err := prepareProfileSource(context.Background(), base, config.Profile{Type: config.ProfileURL, URL: srv.URL}, false)
	if err != nil {
		t.Fatal(err)
	}
	if src.connection.ProxyPort != 7891 {
		t.Fatalf("got port %d, want cached 7891", src.connection.ProxyPort)
	}
	if !strings.Contains(src.warning, "远程配置内容无效") || !strings.Contains(src.warning, "缓存（更新于") {
		t.Fatalf("unexpected fallback warning: %s", src.warning)
	}
}

func TestRemoteCacheWriteFailureStillUsesFreshConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("mixed-port: 7892\n"))
	}))
	defer srv.Close()

	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "cache"), []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	src, err := prepareProfileSource(context.Background(), base, config.Profile{Type: config.ProfileURL, URL: srv.URL}, false)
	if err != nil {
		t.Fatal(err)
	}
	if src.connection.ProxyPort != 7892 {
		t.Fatalf("fresh config was discarded: %+v", src)
	}
	if !strings.Contains(src.warning, "缓存写入失败") {
		t.Fatalf("missing cache warning: %s", src.warning)
	}
}

func TestRemoteCorruptDownloadWithoutCacheReportsCause(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("mixed-port: ["))
	}))
	defer srv.Close()

	_, err := prepareProfileSource(context.Background(), t.TempDir(), config.Profile{Type: config.ProfileURL, URL: srv.URL}, false)
	if err == nil {
		t.Fatal("expected failure")
	}
	message := err.Error()
	if !strings.Contains(message, "远程配置内容无效") || !strings.Contains(message, "读取缓存") {
		t.Fatalf("missing download/cache context: %v", err)
	}
}
