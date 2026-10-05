package config

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

func TestTUNMigration(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		want          *bool
	}{
		{"v1015 no preference", "[Settings]\nAutoStartCore=1\n", nil},
		{"legacy remembered on", "[Settings]\nTUNControl=runtime\nTUNEnabled=1\n", boolPtr(true)},
		{"legacy remembered off", "[Settings]\nTUNEnabled=0\n", boolPtr(false)},
		{"legacy file mode", "[Settings]\nTUNControl=file\nTUNEnabled=1\n", nil},
		{"legacy remember disabled", "[Settings]\nRememberTUN=0\nTUNEnabled=1\n", nil},
		{"new state on", "[State]\nTUNEnabled=1\n", boolPtr(true)},
		{"cleared state beats legacy", "[Settings]\nTUNEnabled=1\n[State]\nSchema=1\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := Store{Path: filepath.Join(t.TempDir(), "config.ini")}
			if err := os.WriteFile(st.Path, []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			s, err := st.Load()
			if err != nil {
				t.Fatal(err)
			}
			assertBool(t, s.TUNEnabled, tc.want)
			if err := st.Save(s); err != nil {
				t.Fatal(err)
			}
			round, err := st.Load()
			if err != nil {
				t.Fatal(err)
			}
			assertBool(t, round.TUNEnabled, tc.want)
		})
	}
}

func TestUTF16AndURLRoundTrip(t *testing.T) {
	st := Store{Path: filepath.Join(t.TempDir(), "config.ini")}
	text := "[Mihomo]\r\nCorePath=D:\\程序\\mihomo.exe\r\nConfigURL=https://example.com/config?token=abc;def#part\r\n[Settings]\r\nTUNEnabled=1\r\n"
	words := utf16.Encode([]rune(text))
	b := []byte{0xff, 0xfe}
	for _, w := range words {
		b = binary.LittleEndian.AppendUint16(b, w)
	}
	if err := os.WriteFile(st.Path, b, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if s.CorePath != "D:\\程序\\mihomo.exe" || s.ConfigURL != "https://example.com/config?token=abc;def#part" {
		t.Fatalf("unexpected settings: %+v", s)
	}
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	r, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if r.ConfigURL != s.ConfigURL || r.CorePath != s.CorePath {
		t.Fatalf("round trip changed paths or URL")
	}
	assertBool(t, r.TUNEnabled, boolPtr(true))
}

func boolPtr(b bool) *bool { return &b }
func assertBool(t *testing.T, got, want *bool) {
	t.Helper()
	if got == nil || want == nil {
		if got != want {
			t.Fatalf("got %v, want %v", got, want)
		}
		return
	}
	if *got != *want {
		t.Fatalf("got %v, want %v", *got, *want)
	}
}
