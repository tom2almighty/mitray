package platform

import (
	"encoding/xml"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskNameIsStableAndPathScoped(t *testing.T) {
	first := taskName(`C:\Tools\MiTray\mitray.exe`)
	if first != taskName(`c:\tools\mitray\MITRAY.EXE`) {
		t.Fatal("task name is not stable across path casing")
	}
	if first == taskName(`D:\Tools\MiTray\mitray.exe`) {
		t.Fatal("different executable paths share a task name")
	}
	if !strings.HasPrefix(first, "mitray-") {
		t.Fatalf("unexpected task name: %s", first)
	}
}

func TestTaskNameCapsLongExecutableNames(t *testing.T) {
	name := taskName(filepath.Join(`C:\Tools`, strings.Repeat("a", 200)+".exe"))
	if len([]rune(name)) != 89 {
		t.Fatalf("got %d runes, want 89", len([]rune(name)))
	}
}

func TestTaskXMLContainsEscapedValuesAndSettings(t *testing.T) {
	executable := `C:\Tools & Tests\mitray.exe`
	content := taskXML(`S-1-5-21 & DOMAIN`, executable, "admin", 42)
	var task struct {
		Triggers struct {
			Logon struct {
				UserID string `xml:"UserId"`
				Delay  string `xml:"Delay"`
			} `xml:"LogonTrigger"`
		} `xml:"Triggers"`
		Principals struct {
			Principal struct {
				RunLevel string `xml:"RunLevel"`
			} `xml:"Principal"`
		} `xml:"Principals"`
		Actions struct {
			Exec struct {
				Command          string `xml:"Command"`
				WorkingDirectory string `xml:"WorkingDirectory"`
			} `xml:"Exec"`
		} `xml:"Actions"`
	}
	if err := xml.Unmarshal(decodeOutput([]byte(content)), &task); err != nil {
		t.Fatal(err)
	}
	if task.Triggers.Logon.UserID != `S-1-5-21 & DOMAIN` || task.Triggers.Logon.Delay != "PT42S" {
		t.Fatalf("unexpected trigger: %+v", task.Triggers)
	}
	if task.Principals.Principal.RunLevel != "HighestAvailable" {
		t.Fatalf("unexpected run level: %s", task.Principals.Principal.RunLevel)
	}
	if task.Actions.Exec.Command != executable || task.Actions.Exec.WorkingDirectory != filepath.Dir(executable) {
		t.Fatalf("unexpected action: %+v", task.Actions.Exec)
	}
}

func TestSamePathUsesWindowsCaseRules(t *testing.T) {
	if !samePath(`C:\Tools\MiTray\mitray.exe`, `c:\tools\mitray\MITRAY.EXE`) {
		t.Fatal("equivalent Windows paths did not match")
	}
	if samePath(`C:\Tools\mitray.exe`, `D:\Tools\mitray.exe`) {
		t.Fatal("different Windows paths matched")
	}
}

func TestCoreOutputIsScopedToExecutableIdentity(t *testing.T) {
	core := NewCore()
	output := newOutputBuffer(1024)
	_, _ = output.Write([]byte("core output"))
	core.runs.Store(123, coreRun{executable: `C:\Tools\MiTray\mihomo.exe`, output: output})

	if got := core.Output(123, `c:\tools\mitray\MIHOMO.EXE`); got != "core output" {
		t.Fatalf("same executable did not return output: %q", got)
	}
	if got := core.Output(123, `C:\Other\mihomo.exe`); got != "" {
		t.Fatalf("different executable reused PID output: %q", got)
	}
}
