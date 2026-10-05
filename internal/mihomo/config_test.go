package mihomo

import "testing"

func TestConnectionFieldsAndWebURL(t *testing.T) {
	c, err := ReadConnection([]byte("external-controller: '0.0.0.0:9090'\nsecret: 'a#b&c'\nmixed-port: 7890\nexternal-ui: D:/webui\ntun:\n  enable: true\n  device: MyAdapter\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Endpoint != "http://127.0.0.1:9090" || c.ProxyPort != 7890 {
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
