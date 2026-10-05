package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/tom2almighty/mitray/internal/config"
	"github.com/tom2almighty/mitray/internal/mihomo"
	"github.com/tom2almighty/mitray/internal/storage"
)

type source struct {
	path       string
	connection mihomo.Connection
	warning    string
}

func prepareSource(ctx context.Context, base string, cfg config.Settings, cachedOnly bool) (source, error) {
	if err := ctx.Err(); err != nil {
		return source{}, err
	}
	path := cfg.Resolve(base, cfg.ConfigPath)
	var data []byte
	var err error
	warning := ""
	if cfg.ConfigURL == "" {
		data, err = os.ReadFile(path)
	} else {
		hash := sha256.Sum256([]byte(cfg.ConfigURL))
		path = filepath.Join(base, "cache", fmt.Sprintf("%x.yaml", hash[:16]))
		if !cachedOnly {
			data, err = download(ctx, cfg.ConfigURL)
			if err == nil {
				if _, parseErr := mihomo.ReadConnection(data); parseErr != nil {
					err = parseErr
				}
			}
			if err == nil {
				err = storage.WriteFile(path, data)
			}
			if err != nil {
				warning = "远程配置暂时不可用，已使用上次下载的配置"
			}
		}
		if cachedOnly || err != nil {
			data, err = os.ReadFile(path)
		}
	}
	if ctx.Err() != nil {
		return source{}, ctx.Err()
	}
	if err != nil {
		if cfg.ConfigURL != "" {
			return source{}, fmt.Errorf("无法取得远程配置，且没有可用缓存；联网后点击启动核心重试")
		}
		return source{}, fmt.Errorf("读取 mihomo 配置: %w", err)
	}
	conn, err := mihomo.ReadConnection(data)
	if err != nil {
		return source{}, err
	}
	return source{path: path, connection: conn, warning: warning}, nil
}

func download(ctx context.Context, address string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	client := &http.Client{Transport: tr}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("远程配置地址无效")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载远程配置失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载远程配置: HTTP %d", resp.StatusCode)
	}
	const maxSize = 16 << 20
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return nil, fmt.Errorf("远程配置下载中断")
	}
	if len(b) > maxSize {
		return nil, fmt.Errorf("远程配置超过 16 MiB")
	}
	return b, nil
}
