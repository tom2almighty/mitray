package platform

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

func (s System) taskName() string {
	return strings.TrimSuffix(filepath.Base(s.Executable), filepath.Ext(s.Executable))
}

func schtasks(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "schtasks.exe", args...)
	cmd.SysProcAttr = HiddenCommand(nil)
	return cmd.CombinedOutput()
}

func decodeOutput(b []byte) []byte {
	if len(b) >= 2 && ((b[0] == 0xff && b[1] == 0xfe) || b[1] == 0) {
		if b[0] == 0xff {
			b = b[2:]
		}
		u := make([]uint16, len(b)/2)
		for i := range u {
			u[i] = binary.LittleEndian.Uint16(b[i*2:])
		}
		b = []byte(string(utf16.Decode(u)))
	}
	return bytes.ReplaceAll(b, []byte(`encoding="UTF-16"`), []byte(`encoding="UTF-8"`))
}

func (s System) Startup() (string, error) {
	output, err := schtasks("/Query", "/TN", s.taskName(), "/XML")
	if err != nil {
		// Check whether the task exists using a successful enumeration. A denied
		// query must not silently turn an existing administrator task into "off".
		all, listErr := schtasks("/Query", "/FO", "CSV", "/NH")
		if listErr != nil {
			return "", fmt.Errorf("无法查询任务计划程序")
		}
		if !strings.Contains(strings.ToLower(string(decodeOutput(all))), strings.ToLower(`\`+s.taskName())+`"`) {
			return "off", nil
		}
		return "", fmt.Errorf("无法读取已有自启任务，请以管理员身份运行 MiTray")
	}
	var task struct {
		Principals struct {
			Principal struct {
				RunLevel string `xml:"RunLevel"`
			} `xml:"Principal"`
		} `xml:"Principals"`
		Settings struct {
			Enabled string `xml:"Enabled"`
		} `xml:"Settings"`
	}
	if err := xml.Unmarshal(decodeOutput(output), &task); err != nil {
		return "", fmt.Errorf("读取自启任务: %w", err)
	}
	if task.Settings.Enabled == "false" {
		return "off", nil
	}
	if task.Principals.Principal.RunLevel == "HighestAvailable" {
		return "admin", nil
	}
	return "normal", nil
}

func xmlText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func (s System) SetStartup(level string, delay int) error {
	if level == "" || level == "off" {
		current, err := s.Startup()
		if err != nil {
			return err
		}
		if current == "off" {
			return nil
		}
		if _, err := schtasks("/Delete", "/TN", s.taskName(), "/F"); err != nil {
			return fmt.Errorf("删除自启任务失败，请以管理员身份运行 MiTray")
		}
		return nil
	}
	if level != "admin" && level != "normal" {
		return fmt.Errorf("无效的自启选项")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	userID := user.User.Sid.String()
	runLevel := "LeastPrivilege"
	if level == "admin" {
		runLevel = "HighestAvailable"
	}
	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <Triggers><LogonTrigger><Enabled>true</Enabled><UserId>%s</UserId><Delay>PT%dS</Delay></LogonTrigger></Triggers>
  <Principals><Principal id="Author"><UserId>%s</UserId><LogonType>InteractiveToken</LogonType><RunLevel>%s</RunLevel></Principal></Principals>
  <Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><StartWhenAvailable>true</StartWhenAvailable><RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable><AllowStartOnDemand>true</AllowStartOnDemand><Enabled>true</Enabled><ExecutionTimeLimit>PT0S</ExecutionTimeLimit></Settings>
  <Actions Context="Author"><Exec><Command>%s</Command><Arguments>--autostart</Arguments><WorkingDirectory>%s</WorkingDirectory></Exec></Actions>
</Task>`, xmlText(userID), delay, xmlText(userID), runLevel, xmlText(s.Executable), xmlText(filepath.Dir(s.Executable)))
	f, err := os.CreateTemp("", "mitray-task-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	words := utf16.Encode([]rune(content))
	b := make([]byte, 2+2*len(words))
	b[0], b[1] = 0xff, 0xfe
	for i, w := range words {
		binary.LittleEndian.PutUint16(b[2+i*2:], w)
	}
	_, writeErr := f.Write(b)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if _, err := schtasks("/Create", "/TN", s.taskName(), "/XML", f.Name(), "/F"); err != nil {
		return fmt.Errorf("创建自启任务失败；管理员自启需要以管理员身份运行 MiTray")
	}
	return nil
}
