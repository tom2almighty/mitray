package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

const DefaultStartupDelay = 15

func (s System) taskName() string {
	return taskName(s.Executable)
}

func taskName(executable string) string {
	canonical := filepath.Clean(executable)
	if resolved, err := filepath.EvalSymlinks(canonical); err == nil {
		canonical = resolved
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(canonical), filepath.Ext(canonical)))
	if runes := []rune(base); len(runes) > 80 {
		base = string(runes[:80])
	}
	hash := sha256.Sum256([]byte(strings.ToLower(canonical)))
	return fmt.Sprintf("%s-%x", base, hash[:4])
}

func legacyTaskName(executable string) string {
	return strings.TrimSuffix(filepath.Base(executable), filepath.Ext(executable))
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

type taskInfo struct {
	level   string
	delay   int
	command string
}

func (s System) Startup() (string, int, error) {
	info, exists, err := queryTask(s.taskName())
	if err != nil {
		return "", DefaultStartupDelay, err
	}
	if exists {
		if !samePath(info.command, s.Executable) {
			return "", DefaultStartupDelay, fmt.Errorf("自启任务名已被其他程序占用，未修改现有任务")
		}
		return info.level, info.delay, nil
	}
	legacy := legacyTaskName(s.Executable)
	if legacy == s.taskName() {
		return "off", DefaultStartupDelay, nil
	}
	info, exists, err = queryTask(legacy)
	if err != nil {
		return "", DefaultStartupDelay, err
	}
	if !exists || !samePath(info.command, s.Executable) {
		return "off", DefaultStartupDelay, nil
	}
	return info.level, info.delay, nil
}

func parseTaskDelay(raw string) int {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "PT") || len(s) <= 2 {
		return DefaultStartupDelay
	}
	s = s[2:]
	total := 0
	numStart := 0
	parsedAny := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			continue
		}
		if i == numStart {
			return DefaultStartupDelay
		}
		n, err := strconv.Atoi(s[numStart:i])
		if err != nil || n < 0 {
			return DefaultStartupDelay
		}
		switch c {
		case 'H':
			total += n * 3600
		case 'M':
			total += n * 60
		case 'S':
			total += n
		default:
			return DefaultStartupDelay
		}
		parsedAny = true
		numStart = i + 1
	}
	if !parsedAny || numStart != len(s) {
		return DefaultStartupDelay
	}
	if total > 600 {
		return 600
	}
	return total
}

func parseTaskXML(data []byte) (taskInfo, error) {
	var task struct {
		Triggers struct {
			LogonTrigger struct {
				Delay string `xml:"Delay"`
			} `xml:"LogonTrigger"`
		} `xml:"Triggers"`
		Principals struct {
			Principal struct {
				RunLevel string `xml:"RunLevel"`
			} `xml:"Principal"`
		} `xml:"Principals"`
		Settings struct {
			Enabled string `xml:"Enabled"`
		} `xml:"Settings"`
		Actions struct {
			Exec struct {
				Command string `xml:"Command"`
			} `xml:"Exec"`
		} `xml:"Actions"`
	}
	if err := xml.Unmarshal(decodeOutput(data), &task); err != nil {
		return taskInfo{}, fmt.Errorf("读取自启任务: %w", err)
	}
	level := "normal"
	if task.Settings.Enabled == "false" {
		level = "off"
	} else if task.Principals.Principal.RunLevel == "HighestAvailable" {
		level = "admin"
	}
	return taskInfo{
		level:   level,
		delay:   parseTaskDelay(task.Triggers.LogonTrigger.Delay),
		command: task.Actions.Exec.Command,
	}, nil
}

func queryTask(name string) (info taskInfo, exists bool, err error) {
	output, err := schtasks("/Query", "/TN", name, "/XML")
	if err != nil {
		// Check whether the task exists using a successful enumeration. A denied
		// query must not silently turn an existing administrator task into "off".
		all, listErr := schtasks("/Query", "/FO", "CSV", "/NH")
		if listErr != nil {
			return taskInfo{}, false, fmt.Errorf("无法查询任务计划程序")
		}
		if !taskListed(all, name) {
			return taskInfo{}, false, nil
		}
		return taskInfo{}, true, fmt.Errorf("无法读取已有自启任务，请以管理员身份运行 MiTray")
	}
	info, err = parseTaskXML(output)
	if err != nil {
		return taskInfo{}, true, err
	}
	return info, true, nil
}

func taskListed(output []byte, name string) bool {
	return strings.Contains(strings.ToLower(string(decodeOutput(output))), strings.ToLower(`\`+name)+`"`)
}

func xmlText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func taskXML(userID, executable, level string, delay int) string {
	runLevel := "LeastPrivilege"
	if level == "admin" {
		runLevel = "HighestAvailable"
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <Triggers><LogonTrigger><Enabled>true</Enabled><UserId>%s</UserId><Delay>PT%dS</Delay></LogonTrigger></Triggers>
  <Principals><Principal id="Author"><UserId>%s</UserId><LogonType>InteractiveToken</LogonType><RunLevel>%s</RunLevel></Principal></Principals>
  <Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><StartWhenAvailable>true</StartWhenAvailable><RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable><AllowStartOnDemand>true</AllowStartOnDemand><Enabled>true</Enabled><ExecutionTimeLimit>PT0S</ExecutionTimeLimit></Settings>
  <Actions Context="Author"><Exec><Command>%s</Command><WorkingDirectory>%s</WorkingDirectory></Exec></Actions>
</Task>`, xmlText(userID), delay, xmlText(userID), runLevel, xmlText(executable), xmlText(filepath.Dir(executable)))
}

func (s System) SetStartup(level string, delay int) error {
	currentName := s.taskName()
	currentInfo, currentExists, err := queryTask(currentName)
	if err != nil {
		return err
	}
	if currentExists && !samePath(currentInfo.command, s.Executable) {
		return fmt.Errorf("自启任务名已被其他程序占用，未修改现有任务")
	}
	ownedLegacy, err := s.ownedLegacyTask()
	if err != nil {
		return err
	}
	if level == "" || level == "off" {
		if currentExists {
			if err := deleteTask(currentName); err != nil {
				return err
			}
		}
		if ownedLegacy {
			return deleteTask(legacyTaskName(s.Executable))
		}
		return nil
	}
	if level != "admin" && level != "normal" {
		return fmt.Errorf("无效的自启选项")
	}
	if delay < 0 || delay > 600 {
		return fmt.Errorf("自启延迟应为 0–600 秒")
	}
	if ownedLegacy {
		if err := deleteTask(legacyTaskName(s.Executable)); err != nil {
			return err
		}
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	userID := user.User.Sid.String()
	content := taskXML(userID, s.Executable, level, delay)
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

func (s System) ownedLegacyTask() (bool, error) {
	legacy := legacyTaskName(s.Executable)
	if legacy == s.taskName() {
		return false, nil
	}
	info, exists, err := queryTask(legacy)
	if err != nil || !exists {
		return false, err
	}
	return samePath(info.command, s.Executable), nil
}

func deleteTask(name string) error {
	if _, err := schtasks("/Delete", "/TN", name, "/F"); err != nil {
		return fmt.Errorf("删除自启任务失败，请以管理员身份运行 MiTray: %w", err)
	}
	return nil
}
