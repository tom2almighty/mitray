package mihomo

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/tom2almighty/mitray/internal/i18n"
	"gopkg.in/yaml.v3"
)

// Only connection information is read. All TUN parameters belong to mihomo.
type Connection struct {
	Endpoint   string
	Secret     string
	ProxyPort  int
	HasTUN     bool
	TUNEnabled bool
	GlobalUA   string
}

type rawListener struct {
	Type   string `yaml:"type"`
	Port   int    `yaml:"port"`
	Listen string `yaml:"listen"`
}

func validPort(p int) bool {
	return p >= 1 && p <= 65535
}

func isLocalListen(listen string) bool {
	host := strings.ToLower(strings.Trim(strings.TrimSpace(listen), "[]"))
	switch host {
	case "", "0.0.0.0", "::", "*", "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}

func pickProxyPort(mixedPort, port int, listeners []rawListener) int {
	if validPort(mixedPort) {
		return mixedPort
	}
	if validPort(port) {
		return port
	}
	var httpPort int
	for _, l := range listeners {
		if !validPort(l.Port) || !isLocalListen(l.Listen) {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(l.Type)) {
		case "mixed":
			return l.Port
		case "http":
			if httpPort == 0 {
				httpPort = l.Port
			}
		}
	}
	return httpPort
}

func ReadConnection(data []byte) (Connection, error) {
	var raw struct {
		Controller string        `yaml:"external-controller"`
		Secret     string        `yaml:"secret"`
		MixedPort  int           `yaml:"mixed-port"`
		Port       int           `yaml:"port"`
		Listeners  []rawListener `yaml:"listeners"`
		GlobalUA   string        `yaml:"global-ua"`
		TUN        yaml.Node     `yaml:"tun"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Connection{}, fmt.Errorf(i18n.T().ErrReadConnection, err)
	}
	port := pickProxyPort(raw.MixedPort, raw.Port, raw.Listeners)
	hasTUN := raw.TUN.Kind != 0 && raw.TUN.Tag != "!!null"
	tunEnabled := false
	if hasTUN {
		var tun struct {
			Enable bool `yaml:"enable"`
		}
		if err := raw.TUN.Decode(&tun); err == nil {
			tunEnabled = tun.Enable
		}
	}
	ua := strings.TrimSpace(raw.GlobalUA)
	if raw.Controller == "" {
		return Connection{Secret: raw.Secret, ProxyPort: port, HasTUN: hasTUN, TUNEnabled: tunEnabled, GlobalUA: ua}, nil
	}
	endpoint := raw.Controller
	if !strings.Contains(endpoint, "://") {
		endpoint = "http://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return Connection{}, fmt.Errorf("%s", i18n.T().ErrControllerAddr)
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
			return Connection{}, fmt.Errorf("%s", i18n.T().ErrControllerPort)
		}
	}
	return Connection{
		Endpoint:   strings.TrimRight(u.String(), "/"),
		Secret:     raw.Secret,
		ProxyPort:  port,
		HasTUN:     hasTUN,
		TUNEnabled: tunEnabled,
		GlobalUA:   ua,
	}, nil
}

func (c Connection) WebURL() string {
	if c.Endpoint == "" {
		return ""
	}
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
