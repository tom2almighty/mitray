//go:build !windows

package i18n

import (
	"os"
	"strings"
)

func detectSystemLang() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := strings.ToLower(strings.TrimSpace(os.Getenv(key))); v != "" {
			if strings.HasPrefix(v, "zh") {
				return ZH
			}
			return EN
		}
	}
	return EN
}
