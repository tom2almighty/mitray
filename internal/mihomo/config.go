package mihomo

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Only connection information is read. All TUN parameters belong to mihomo.
type Connection struct {
	Endpoint  string
	Secret    string
	ProxyPort int
}

func ReadConnection(data []byte) (Connection, error) {
	var raw struct {
		Controller string `yaml:"external-controller"`
		Secret     string `yaml:"secret"`
		MixedPort  int    `yaml:"mixed-port"`
		Port       int    `yaml:"port"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Connection{}, fmt.Errorf("读取 API 连接信息: %w", err)
	}
	port := raw.MixedPort
	if port == 0 {
		port = raw.Port
	}
	if raw.Controller == "" {
		return Connection{Secret: raw.Secret, ProxyPort: port}, nil
	}
	endpoint := raw.Controller
	if !strings.Contains(endpoint, "://") {
		endpoint = "http://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return Connection{}, fmt.Errorf("external-controller 地址无效")
	}
	if u.Hostname() == "0.0.0.0" || u.Hostname() == "::" || u.Hostname() == "*" {
		host := "127.0.0.1"
		if u.Hostname() == "::" {
			host = "::1"
		}
		u.Host = net.JoinHostPort(host, u.Port())
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return Connection{}, fmt.Errorf("external-controller 端口无效")
		}
	}
	return Connection{Endpoint: strings.TrimRight(u.String(), "/"), Secret: raw.Secret, ProxyPort: port}, nil
}

func (c Connection) WebURL() string {
	u, err := url.Parse(c.Endpoint + "/ui/")
	if err != nil {
		return ""
	}
	if c.Secret != "" {
		q := u.Query()
		q.Set("secret", c.Secret)
		u.RawQuery = q.Encode()
	}
	return u.String()
}
