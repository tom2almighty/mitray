package mihomo

import "testing"

func TestConnectionFieldsAndWebURL(t *testing.T) {
	c, err := ReadConnection([]byte("external-controller: '0.0.0.0:9090'\nsecret: 'a#b&c'\nmixed-port: 7890\nexternal-ui: D:/webui\ntun:\n  enable: true\n  device: MyAdapter\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Endpoint != "http://127.0.0.1:9090" || c.ProxyPort != 7890 || !c.HasTUN || !c.TUNEnabled {
		t.Fatalf("unexpected connection: %+v", c)
	}
	if c.WebURL() != "http://127.0.0.1:9090/ui/?secret=a%23b%26c" {
		t.Fatal(c.WebURL())
	}
}

func TestYAMLAliases(t *testing.T) {
	c, err := ReadConnection([]byte("api: &api 127.0.0.1:9090\nexternal-controller: *api\nsecret: \"a#b\" # comment\nport: 7891\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Endpoint != "http://127.0.0.1:9090" || c.Secret != "a#b" || c.ProxyPort != 7891 {
		t.Fatalf("unexpected connection: %+v", c)
	}
}

func TestWebURLEmptyWithoutEndpoint(t *testing.T) {
	c, err := ReadConnection([]byte("mixed-port: 7890\nsecret: abc\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.WebURL() != "" {
		t.Fatalf("expected empty WebURL without external-controller, got %q", c.WebURL())
	}
	if c.HasTUN || c.TUNEnabled {
		t.Fatal("expected HasTUN=false and TUNEnabled=false when tun block is absent")
	}

	cNull, err := ReadConnection([]byte("mixed-port: 7890\ntun: null\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cNull.HasTUN || cNull.TUNEnabled {
		t.Fatalf("expected HasTUN=false and TUNEnabled=false for tun: null, got %+v", cNull)
	}

	cDisabled, err := ReadConnection([]byte("mixed-port: 7890\ntun:\n  enable: false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cDisabled.HasTUN || cDisabled.TUNEnabled {
		t.Fatalf("expected HasTUN=true and TUNEnabled=false, got %+v", cDisabled)
	}
}

func TestListenersPortCompatibilityAndGlobalUA(t *testing.T) {
	t.Run("prefers mixed over http and skips remote or socks listeners", func(t *testing.T) {
		yamlData := `
global-ua: clash.meta/1.19
listeners:
  - name: remote-mixed
    type: mixed
    port: 7880
    listen: 192.168.1.5
  - name: socks-in
    type: socks
    port: 7881
    listen: 127.0.0.1
  - name: http-in
    type: http
    port: 7882
    listen: 127.0.0.1
  - name: mixed-in
    type: mixed
    port: 7883
    listen: 0.0.0.0
`
		c, err := ReadConnection([]byte(yamlData))
		if err != nil {
			t.Fatal(err)
		}
		if c.ProxyPort != 7883 {
			t.Fatalf("got port %d, want 7883", c.ProxyPort)
		}
		if c.GlobalUA != "clash.meta/1.19" {
			t.Fatalf("got GlobalUA %q, want clash.meta/1.19", c.GlobalUA)
		}
	})

	t.Run("falls back to http listener when no mixed listener exists", func(t *testing.T) {
		yamlData := `
listeners:
  - name: http-in
    type: http
    port: 7884
`
		c, err := ReadConnection([]byte(yamlData))
		if err != nil {
			t.Fatal(err)
		}
		if c.ProxyPort != 7884 {
			t.Fatalf("got port %d, want 7884", c.ProxyPort)
		}
	})

	t.Run("mixed-port takes priority over listeners", func(t *testing.T) {
		yamlData := `
mixed-port: 7890
listeners:
  - name: mixed-in
    type: mixed
    port: 7885
`
		c, err := ReadConnection([]byte(yamlData))
		if err != nil {
			t.Fatal(err)
		}
		if c.ProxyPort != 7890 {
			t.Fatalf("got port %d, want 7890", c.ProxyPort)
		}
	})
}
