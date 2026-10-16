package proxyenv

import "testing"

func TestScript(t *testing.T) {
	for _, tc := range []struct {
		name, shell, want string
	}{
		{"powershell", ShellPowerShell, `$env:HTTP_PROXY="http://127.0.0.1:7890"; $env:HTTPS_PROXY="http://127.0.0.1:7890"`},
		{"cmd", ShellCmd, `set "HTTP_PROXY=http://127.0.0.1:7890" && set "HTTPS_PROXY=http://127.0.0.1:7890"`},
		{"bash", ShellBash, `export http_proxy="http://127.0.0.1:7890" https_proxy="http://127.0.0.1:7890"`},
		{"unknown", "fish", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Script(tc.shell, 7890); got != tc.want {
				t.Fatalf("Script(%q) = %q, want %q", tc.shell, got, tc.want)
			}
		})
	}
}
