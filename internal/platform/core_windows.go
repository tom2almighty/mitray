package platform

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type Core struct {
	runs sync.Map
}

type coreRun struct {
	executable string
	output     *outputBuffer
}

func NewCore() *Core { return &Core{} }

func imagePath(h windows.Handle) (string, error) {
	b := make([]uint16, 32768)
	n := uint32(len(b))
	if err := windows.QueryFullProcessImageName(h, 0, &b[0], &n); err != nil {
		return "", err
	}
	return windows.UTF16ToString(b[:n]), nil
}

func samePath(a, b string) bool {
	if p, err := filepath.EvalSymlinks(a); err == nil {
		a = p
	}
	if p, err := filepath.EvalSymlinks(b); err == nil {
		b = p
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func (*Core) Find(executable string) (int, error) {
	if executable == "" {
		return 0, nil
	}
	h, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(h)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	var found int
	var accessErr error
	for err = windows.Process32First(h, &entry); err == nil; err = windows.Process32Next(h, &entry) {
		if !strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), filepath.Base(executable)) {
			continue
		}
		p, openErr := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, entry.ProcessID)
		if openErr != nil {
			if accessErr == nil {
				accessErr = openErr
			}
			continue
		}
		path, pathErr := imagePath(p)
		windows.CloseHandle(p)
		if pathErr != nil {
			if accessErr == nil {
				accessErr = pathErr
			}
			continue
		}
		if samePath(path, executable) {
			if found != 0 {
				return 0, errors.New("所选核心有多个运行实例，请先关闭多余实例")
			}
			found = int(entry.ProcessID)
		}
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return 0, err
	}
	if found == 0 && accessErr != nil {
		return 0, fmt.Errorf("无法识别同名核心进程，请以管理员身份运行 MiTray: %w", accessErr)
	}
	return found, nil
}

func (c *Core) Start(executable, configPath, directory string) (int, error) {
	cmd := exec.Command(executable, "-d", directory, "-f", configPath)
	cmd.Dir = directory
	output := newOutputBuffer(32 << 10)
	cmd.Stdout = output
	cmd.Stderr = output
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	c.runs.Range(func(key, _ any) bool {
		c.runs.Delete(key)
		return true
	})
	c.runs.Store(pid, coreRun{executable: executable, output: output})
	go func() {
		waitErr := cmd.Wait()
		message := "\n[MiTray] mihomo 已退出"
		if cmd.ProcessState != nil {
			message += fmt.Sprintf("，退出码 %d", cmd.ProcessState.ExitCode())
		} else if waitErr != nil {
			message += ": " + waitErr.Error()
		}
		_, _ = output.Write([]byte(message + "\n"))
	}()
	return pid, nil
}

func (*Core) Alive(pid int, executable string) bool {
	if pid == 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	result, err := windows.WaitForSingleObject(h, 0)
	if err != nil || result != uint32(windows.WAIT_TIMEOUT) {
		return false
	}
	path, err := imagePath(h)
	return err == nil && samePath(path, executable)
}

func (*Core) Stop(pid int, executable string) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil
	}
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	if result, err := windows.WaitForSingleObject(h, 0); err == nil && result == windows.WAIT_OBJECT_0 {
		return nil
	}
	path, err := imagePath(h)
	if err != nil {
		return err
	}
	if !samePath(path, executable) {
		return errors.New("进程已变化，取消停止以避免关闭其他程序")
	}
	if err := windows.TerminateProcess(h, 0); err != nil {
		return err
	}
	result, err := windows.WaitForSingleObject(h, 5000)
	if err != nil {
		return err
	}
	if result == uint32(windows.WAIT_TIMEOUT) {
		return errors.New("等待旧核心退出超时")
	}
	return nil
}

func (c *Core) Output(pid int, executable string) string {
	value, ok := c.runs.Load(pid)
	if !ok {
		return ""
	}
	run, ok := value.(coreRun)
	if !ok {
		return ""
	}
	if !samePath(run.executable, executable) {
		return ""
	}
	return run.output.String()
}
