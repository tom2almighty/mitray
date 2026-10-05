package mihomo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Status struct {
	TUN       *bool
	ProxyPort int
}

type Client struct {
	Connection Connection
	HTTP       *http.Client
}

func NewClient(c Connection) *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil // Local control must not depend on system/environment proxies.
	return &Client{Connection: c, HTTP: &http.Client{Timeout: 3 * time.Second, Transport: tr}}
}

func (c *Client) Close() { c.HTTP.CloseIdleConnections() }

func (c *Client) request(ctx context.Context, method string, payload []byte) (*http.Response, error) {
	if c.Connection.Endpoint == "" {
		return nil, errors.New("mihomo 配置未设置 external-controller")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Connection.Endpoint+"/configs", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	if c.Connection.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.Connection.Secret)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接 mihomo API: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return nil, errors.New("mihomo API 认证失败，请检查 secret")
		}
		return nil, fmt.Errorf("mihomo API 返回 HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	resp, err := c.request(ctx, http.MethodGet, nil)
	if err != nil {
		return Status{}, err
	}
	defer resp.Body.Close()
	var raw struct {
		TUN struct {
			Enable *bool `json:"enable"`
		} `json:"tun"`
		MixedPort int `json:"mixed-port"`
		Port      int `json:"port"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&raw); err != nil {
		return Status{}, fmt.Errorf("读取 mihomo 状态: %w", err)
	}
	port := raw.MixedPort
	if port == 0 {
		port = raw.Port
	}
	return Status{TUN: raw.TUN.Enable, ProxyPort: port}, nil
}

func (c *Client) SetTUN(ctx context.Context, enabled bool) error {
	// Do not echo GET /configs back. mihomo merges omitted fields with LastTunConf.
	body := []byte(`{"tun":{"enable":false}}`)
	if enabled {
		body = []byte(`{"tun":{"enable":true}}`)
	}
	resp, err := c.request(ctx, http.MethodPatch, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return nil
}
