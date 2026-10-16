// Package proxyenv renders clipboard-ready commands that point shell HTTP
// clients at the local mihomo proxy port.
package proxyenv

import "strconv"

// Shell names accepted by Script.
const (
	ShellPowerShell = "pwsh"
	ShellCmd        = "cmd"
	ShellBash       = "bash"
)

// Script returns the command setting HTTP_PROXY and HTTPS_PROXY for the named
// shell. It returns an empty string for unknown shells.
func Script(shell string, port int) string {
	proxy := "http://127.0.0.1:" + strconv.Itoa(port)
	switch shell {
	case ShellPowerShell:
		return `$env:HTTP_PROXY="` + proxy + `"; $env:HTTPS_PROXY="` + proxy + `"`
	case ShellCmd:
		return `set "HTTP_PROXY=` + proxy + `" && set "HTTPS_PROXY=` + proxy + `"`
	case ShellBash:
		return `export http_proxy="` + proxy + `" https_proxy="` + proxy + `"`
	default:
		return ""
	}
}
