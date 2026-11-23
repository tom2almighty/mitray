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

func TestRemoteCacheWriteFailureWithoutCacheReportsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("mixed-port: 7892\n"))
	}))
	defer srv.Close()

	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "cache"), []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := prepareProfileSource(context.Background(), base, config.Profile{Type: config.ProfileURL, URL: srv.URL}, false)
	if err == nil {
		t.Fatal("expected error when cache write fails and no fallback cache exists")
	}
	if !strings.Contains(err.Error(), "远程配置缓存写入失败") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoteDownloadUsesMihomoUAAndCachedGlobalUA(t *testing.T) {
	var seenUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("mixed-port: 7890\nglobal-ua: clash.meta/custom\n"))
	}))
	defer srv.Close()

	base := t.TempDir()
	profile := config.Profile{Type: config.ProfileURL, URL: srv.URL}
	if _, err := prepareProfileSource(context.Background(), base, profile, false); err != nil {
		t.Fatal(err)
	}
	if seenUA != defaultMihomoUA {
		t.Fatalf("first download UA = %q, want %q", seenUA, defaultMihomoUA)
	}
	if _, err := prepareProfileSource(context.Background(), base, profile, false); err != nil {
		t.Fatal(err)
	}
	if seenUA != "clash.meta/custom" {
		t.Fatalf("second download UA = %q, want clash.meta/custom", seenUA)
	}
}

func TestPruneRemoteCacheRemovesOnlyOrphans(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "cache")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	activeURL := "https://example.com/active.yaml"
	orphanURL := "https://example.com/orphan.yaml"
	activeFile := remoteCachePath(base, activeURL)
	orphanFile := remoteCachePath(base, orphanURL)
	otherFile := filepath.Join(dir, "readme.txt")
	for _, f := range []string{activeFile, orphanFile, otherFile} {
		if err := os.WriteFile(f, []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	pruneRemoteCache(base, map[string]config.Profile{
		"a": {Type: config.ProfileURL, URL: activeURL},
		"b": {Type: config.ProfileLocal, Path: "local.yaml"},
	})

	if _, err := os.Stat(activeFile); err != nil {
		t.Fatalf("active cache removed: %v", err)
	}
	if _, err := os.Stat(orphanFile); !os.IsNotExist(err) {
		t.Fatalf("orphan cache was not removed: %v", err)
	}
	if _, err := os.Stat(otherFile); err != nil {
		t.Fatalf("unrelated file removed: %v", err)
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
