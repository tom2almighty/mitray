package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tom2almighty/mitray/internal/config"
	"github.com/tom2almighty/mitray/internal/i18n"
	"github.com/tom2almighty/mitray/internal/mihomo"
	"github.com/tom2almighty/mitray/internal/storage"
)

const defaultMihomoUA = "clash.meta"

type source struct {
	path       string
	connection mihomo.Connection
	warning    string
}

func prepareSource(ctx context.Context, base string, cfg config.Settings, cachedOnly bool) (source, error) {
	profile, ok := cfg.ActiveProfileConfig()
	if !ok {
		return source{}, fmt.Errorf("%s", i18n.T().ErrSelectMihomoConfig)
	}
	return prepareProfileSource(ctx, base, profile, cachedOnly)
}

func prepareProfileSource(ctx context.Context, base string, profile config.Profile, cachedOnly bool) (source, error) {
	if err := ctx.Err(); err != nil {
		return source{}, err
	}
	switch profile.Type {
	case config.ProfileLocal:
		path := profile.Resolve(base)
		data, err := os.ReadFile(path)
		if err != nil {
			return source{}, fmt.Errorf(i18n.T().ErrReadMihomoConfig, err)
		}
		conn, err := mihomo.ReadConnection(data)
		if err != nil {
			return source{}, fmt.Errorf(i18n.T().ErrInvalidLocalConfig, err)
		}
		return source{path: path, connection: conn}, nil
	case config.ProfileURL:
		return prepareRemoteSource(ctx, base, profile, cachedOnly)
	default:
		return source{}, fmt.Errorf("%s", i18n.T().ErrInvalidSourceType)
	}
}

func remoteCacheName(address string) string {
	hash := sha256.Sum256([]byte(address))
	return fmt.Sprintf("%x.yaml", hash[:16])
}

func isRemoteCacheFilename(name string) bool {
	if !strings.HasSuffix(name, ".yaml") || len(name) != 32+len(".yaml") {
		return false
	}
	for _, c := range name[:32] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func pruneRemoteCache(base string, profiles map[string]config.Profile) {
	dir := filepath.Join(base, "cache")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	keep := make(map[string]struct{}, len(profiles))
	for _, p := range profiles {
		if p.Type == config.ProfileURL && p.URL != "" {
			keep[remoteCacheName(p.URL)] = struct{}{}
		}
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !isRemoteCacheFilename(name) {
			continue
		}
		if _, ok := keep[name]; !ok {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

func resolveDownloadUA(cachePath string) string {
	if cached, err := os.ReadFile(cachePath); err == nil {
		if conn, err := mihomo.ReadConnection(cached); err == nil && conn.GlobalUA != "" {
			return conn.GlobalUA
		}
	}
	return defaultMihomoUA
}

func prepareRemoteSource(ctx context.Context, base string, profile config.Profile, cachedOnly bool) (source, error) {
	path := filepath.Join(base, "cache", remoteCacheName(profile.URL))
	var data []byte
	var freshErr error
	warning := ""

	if !cachedOnly {
		ua := resolveDownloadUA(path)
		data, freshErr = download(ctx, profile.URL, ua)
		if freshErr == nil {
			if _, parseErr := mihomo.ReadConnection(data); parseErr != nil {
				data, freshErr = nil, fmt.Errorf(i18n.T().ErrRemoteContentBad, parseErr)
			}
		}
		if freshErr == nil {
			if writeErr := storage.WriteFile(path, data); writeErr != nil {
				data, freshErr = nil, fmt.Errorf(i18n.T().ErrRemoteCacheWrite, writeErr)
			}
		}
		if freshErr != nil {
			warning = fmt.Sprintf(i18n.T().ErrRemoteNotUpdated, freshErr.Error())
		}
	}

	if cachedOnly || freshErr != nil {
		cached, cacheErr := os.ReadFile(path)
		if cacheErr == nil {
			if _, parseErr := mihomo.ReadConnection(cached); parseErr != nil {
				cacheErr = fmt.Errorf(i18n.T().ErrCachedConfigBad, parseErr)
			} else {
				data = cached
				if warning != "" {
					warning += fmt.Sprintf(i18n.T().NoticeUsedCache, cacheDescription(path))
				}
			}
		}
		if cacheErr != nil {
			if freshErr != nil {
				return source{}, fmt.Errorf(i18n.T().ErrNoRemoteOrCache, freshErr, cacheErr)
			}
			return source{}, fmt.Errorf(i18n.T().ErrReadRemoteCache, cacheErr)
		}
	}

	if err := ctx.Err(); err != nil {
		return source{}, err
	}
	conn, err := mihomo.ReadConnection(data)
	if err != nil {
		return source{}, fmt.Errorf(i18n.T().ErrInvalidRemoteConfig, err)
	}
	return source{path: path, connection: conn, warning: warning}, nil
}

func cacheDescription(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return i18n.T().CacheLastSuccess
	}
	return fmt.Sprintf(i18n.T().CacheUpdatedAt, info.ModTime().Format("2006-01-02 15:04"))
}

func download(ctx context.Context, address, userAgent string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	client := &http.Client{Transport: tr}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("%s", i18n.T().ErrRemoteAddrInvalid)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf(i18n.T().ErrDownloadRemote, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(i18n.T().ErrDownloadRemoteHTTP, resp.StatusCode)
	}
	const maxSize = 16 << 20
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return nil, fmt.Errorf(i18n.T().ErrDownloadInterrupted, err)
	}
	if len(b) > maxSize {
		return nil, fmt.Errorf("%s", i18n.T().ErrRemoteTooLarge)
	}
	return b, nil
}
