package controller

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tom2almighty/mitray/internal/config"
	"github.com/tom2almighty/mitray/internal/mihomo"
)

type Core interface {
	Find(executable string) (int, error)
	Start(executable, configPath, directory string) (int, error)
	Stop(pid int, executable string) error
	Alive(pid int, executable string) bool
	Output(pid int, executable string) string
}

type System interface {
	Proxy() (bool, string, error)
	SetProxy(enabled bool, port int) error
	Startup() (string, error)
	SetStartup(level string, delay int) error
}

type SettingsStore interface{ Save(config.Settings) error }

// ErrTUNRestore allows background recovery failures to notify the user once.
var ErrTUNRestore = errors.New("恢复 TUN 启动记忆失败，本次核心运行期间不再自动重试")

type Snapshot struct {
	Settings         config.Settings
	Running          bool
	PID              int
	TUN, Proxy       *bool
	ProxyServer      string
	ProxyTargetsCore bool
	WebURL           string
	Notice           string
	Diagnostics      string
	Revision         uint64
}

// All mutations (including lifecycle operations and recovery) share op.
// Polling only restores newly detected processes and never changes the preference.
// UI reads use a separate lock and cannot block on HTTP.
type Controller struct {
	op                          sync.Mutex
	viewMu                      sync.RWMutex
	view                        Snapshot
	base                        string
	cfg                         config.Settings
	store                       SettingsStore
	core                        Core
	system                      System
	pid                         int
	tunHandledPID               int
	activeExe                   string
	client                      *mihomo.Client
	tun, proxy                  *bool
	proxyServer                 string
	proxyPort                   int
	notice                      string
	diagnostics                 string
	revision                    uint64
	readyTimeout, retryInterval time.Duration
}

func New(base string, cfg config.Settings, store SettingsStore, core Core, system System) *Controller {
	c := &Controller{base: base, cfg: cfg.Clone(), store: store, core: core, system: system, readyTimeout: 45 * time.Second, retryInterval: 500 * time.Millisecond}
	c.publish()
	return c
}

func (c *Controller) Snapshot() Snapshot {
	c.viewMu.RLock()
	defer c.viewMu.RUnlock()
	v := c.view
	v.Settings = v.Settings.Clone()
	v.TUN, v.Proxy = copyBool(v.TUN), copyBool(v.Proxy)
	return v
}

func copyBool(p *bool) *bool {
	if p == nil {
		return nil
	}
	b := *p
	return &b
}

func proxyServerTargetsPort(server string, port int) bool {
	if port < 1 || port > 65535 {
		return false
	}
	for _, entry := range strings.Split(server, ";") {
		entry = strings.TrimSpace(entry)
		if i := strings.IndexByte(entry, '='); i >= 0 {
			entry = strings.TrimSpace(entry[i+1:])
		}
		host, value, err := net.SplitHostPort(entry)
		if err != nil {
			continue
		}
		n, err := strconv.Atoi(value)
		if err != nil || n != port {
			continue
		}
		switch strings.ToLower(strings.Trim(host, "[]")) {
		case "127.0.0.1", "localhost", "::1":
			return true
		}
	}
	return false
}

func (c *Controller) publish() {
	v := Snapshot{
		Settings:         c.cfg.Clone(),
		Running:          c.pid != 0,
		PID:              c.pid,
		TUN:              copyBool(c.tun),
		Proxy:            copyBool(c.proxy),
		ProxyServer:      c.proxyServer,
		ProxyTargetsCore: c.proxy != nil && *c.proxy && proxyServerTargetsPort(c.proxyServer, c.proxyPort),
		Notice:           c.notice,
		Diagnostics:      c.diagnostics,
	}
	if c.client != nil {
		v.WebURL = c.client.Connection.WebURL()
	}
	c.viewMu.Lock()
	old := c.view
	old.Revision, v.Revision = 0, 0
	if !reflect.DeepEqual(old, v) {
		c.revision++
	}
	v.Revision = c.revision
	c.view = v
	c.viewMu.Unlock()
}

func (c *Controller) Boot(ctx context.Context) error {
	return c.boot(ctx, false)
}

func (c *Controller) RestartOnBoot(ctx context.Context) error {
	return c.boot(ctx, true)
}

func (c *Controller) boot(ctx context.Context, restart bool) error {
	c.op.Lock()
	defer c.op.Unlock()
	defer c.publish()
	level, err := c.system.Startup()
	if err == nil {
		c.cfg.StartupLevel = level
	} else {
		c.notice = "无法读取开机自启设置: " + err.Error()
	}
	c.readProxy()
	if err := c.cfg.Validate(c.base); err != nil {
		return err
	}
	if restart {
		return c.restart(ctx)
	}
	if c.cfg.AutoStartCore {
		return c.start(ctx, nil)
	}
	exe := c.cfg.Resolve(c.base, c.cfg.CorePath)
	pid, err := c.core.Find(exe)
	if err != nil {
		return err
	}
	if pid == 0 {
		return nil
	}
	return c.start(ctx, nil)
}

func (c *Controller) Start(ctx context.Context) error {
	c.op.Lock()
	defer c.op.Unlock()
	defer c.publish()
	return c.start(ctx, nil)
}

func (c *Controller) start(ctx context.Context, prepared *source) error {
	c.diagnostics = ""
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.cfg.Validate(c.base); err != nil {
		return err
	}
	exe := c.cfg.Resolve(c.base, c.cfg.CorePath)
	pid, err := c.core.Find(exe)
	if err != nil {
		return err
	}
	newProcess := pid == 0
	if pid != 0 {
		c.pid, c.activeExe = pid, exe
		c.tunHandledPID = pid // Connecting to a running core must preserve its state.
	}
	var src source
	if prepared != nil {
		src = *prepared
	} else {
		src, err = prepareSource(ctx, c.base, c.cfg, pid != 0)
		if err != nil {
			return err
		}
	}
	c.setClient(src.connection)
	c.notice = src.warning
	c.activeExe = exe
	if pid == 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		pid, err = c.core.Start(exe, src.path, filepath.Dir(exe))
		if err != nil {
			return fmt.Errorf("启动 mihomo: %w", err)
		}
	}
	c.pid, c.tun = pid, nil
	c.tunHandledPID = pid
	c.publish()
	if newProcess && c.cfg.TUNEnabled != nil {
		return c.restoreTUN(ctx)
	}
	return c.waitReady(ctx)
}

func (c *Controller) restoreTUN(ctx context.Context) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("%w: %w", ErrTUNRestore, err)
		}
	}()
	if err := c.waitReady(ctx); err != nil {
		return err
	}
	return c.applyTUN(ctx, *c.cfg.TUNEnabled)
}

func (c *Controller) checkCore() error {
	if !c.core.Alive(c.pid, c.activeExe) {
		output := c.coreOutput()
		c.pid, c.tun = 0, nil
		c.diagnostics = output
		err := errors.New("核心进程已退出或变化，已取消当前操作")
		if output != "" {
			return fmt.Errorf("%w\n\n最近核心输出:\n%s", err, tailText(output, 1600))
		}
		return err
	}
	return nil
}

func (c *Controller) coreOutput() string {
	if c.pid == 0 {
		return ""
	}
	return strings.TrimSpace(c.core.Output(c.pid, c.activeExe))
}

func tailText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return "…\n" + text[len(text)-limit:]
}

func (c *Controller) setClient(conn mihomo.Connection) {
	if c.client != nil {
		c.client.Close()
	}
	c.client = mihomo.NewClient(conn)
	c.proxyPort = conn.ProxyPort
}

func (c *Controller) waitReady(ctx context.Context) error {
	if c.client.Connection.Endpoint == "" {
		return errors.New("核心已启动，但配置缺少 external-controller，无法控制 TUN")
	}
	ctx, cancel := context.WithTimeout(ctx, c.readyTimeout)
	defer cancel()
	var last error
	for {
		if err := c.checkCore(); err != nil {
			return err
		}
		status, err := c.client.Status(ctx)
		if err == nil {
			c.acceptStatus(status)
			return nil
		}
		if mihomo.IsPermanent(err) {
			return err
		}
		last = err
		if err := pause(ctx, c.retryInterval); err != nil {
			output := c.coreOutput()
			c.diagnostics = output
			err = fmt.Errorf("等待核心 API 就绪超时，可重启核心重试: %w", last)
			if output != "" {
				return fmt.Errorf("%w\n\n最近核心输出:\n%s", err, tailText(output, 1600))
			}
			return err
		}
	}
}

func (c *Controller) Stop(ctx context.Context) error {
	c.op.Lock()
	defer c.op.Unlock()
	defer c.publish()
	return c.stop()
}

func (c *Controller) stop() error {
	if c.pid == 0 {
		exe := c.cfg.Resolve(c.base, c.cfg.CorePath)
		pid, err := c.core.Find(exe)
		if err != nil {
			return err
		}
		c.pid, c.activeExe = pid, exe
	}
	if c.pid != 0 {
		if err := c.core.Stop(c.pid, c.activeExe); err != nil {
			return fmt.Errorf("停止 mihomo: %w", err)
		}
	}
	c.pid, c.tun, c.tunHandledPID = 0, nil, 0
	c.notice = ""
	c.diagnostics = ""
	return nil
}

func (c *Controller) Restart(ctx context.Context) error {
	c.op.Lock()
	defer c.op.Unlock()
	defer c.publish()
	return c.restart(ctx)
}

func (c *Controller) restart(ctx context.Context) error {
	// Download/read first, then wait for the old PID to exit before starting a
	// new process. /restart's early HTTP response is not a readiness signal.
	if err := c.cfg.Validate(c.base); err != nil {
		return err
	}
	src, err := prepareSource(ctx, c.base, c.cfg, false)
	if err != nil {
		return err
	}
	if err := c.stop(); err != nil {
		return err
	}
	return c.start(ctx, &src)
}

func (c *Controller) acceptStatus(s mihomo.Status) {
	c.tun = copyBool(s.TUN)
	if s.ProxyPort > 0 {
		c.proxyPort = s.ProxyPort
	}
}

func (c *Controller) readProxy() {
	b, server, err := c.system.Proxy()
	if err != nil {
		c.proxy = nil
		c.proxyServer = ""
	} else {
		c.proxy = &b
		c.proxyServer = server
	}
}

func (c *Controller) Refresh(ctx context.Context) error {
	c.op.Lock()
	defer c.op.Unlock()
	defer c.publish()
	c.readProxy()
	restore := false
	if c.pid != 0 && !c.core.Alive(c.pid, c.activeExe) {
		c.diagnostics = c.coreOutput()
		c.pid, c.tun = 0, nil
	}
	if c.pid == 0 {
		exe := c.cfg.Resolve(c.base, c.cfg.CorePath)
		if exe == "" {
			return nil
		}
		pid, err := c.core.Find(exe)
		if err != nil {
			return err
		}
		if pid == 0 {
			return nil
		}
		restore = c.tunHandledPID != pid && c.cfg.TUNEnabled != nil
		c.pid, c.activeExe, c.tun = pid, exe, nil
		// Consume recovery before any I/O so a failure cannot rearm it on refresh.
		c.tunHandledPID = pid
		c.publish()
		if c.client != nil {
			c.client.Close()
			c.client = nil
		}
	}
	if c.client == nil {
		src, err := prepareSource(ctx, c.base, c.cfg, true)
		if err != nil {
			if restore {
				return fmt.Errorf("%w: %w", ErrTUNRestore, err)
			}
			return err
		}
		c.setClient(src.connection)
	}
	if restore {
		return c.restoreTUN(ctx)
	}
	s, err := c.client.Status(ctx)
	if err != nil {
		c.tun = nil
		return err
	}
	c.acceptStatus(s)
	return nil
}

func (c *Controller) ToggleTUN(ctx context.Context) error {
	c.op.Lock()
	defer c.op.Unlock()
	defer c.publish()
	if c.pid == 0 || !c.core.Alive(c.pid, c.activeExe) {
		return errors.New("请先启动 mihomo 核心")
	}
	if c.client == nil {
		return errors.New("尚未连接核心，请刷新状态或重启核心")
	}
	s, err := c.client.Status(ctx)
	if err != nil {
		c.tun = nil
		return err
	}
	c.acceptStatus(s)
	if s.TUN == nil {
		return errors.New("mihomo 未返回 TUN 状态，请在 WebUI 查看日志")
	}
	target := !*s.TUN
	if err := c.applyTUN(ctx, target); err != nil {
		return err
	}
	next := c.cfg.Clone()
	next.TUNEnabled = &target
	if err := c.store.Save(next); err != nil {
		return fmt.Errorf("TUN 已切换，但记忆保存失败，下次启动仍使用原记录: %w", err)
	}
	c.cfg = next
	return nil
}

func (c *Controller) applyTUN(ctx context.Context, target bool) error {
	if err := c.checkCore(); err != nil {
		return err
	}
	if c.tun != nil && *c.tun == target {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if err := c.checkCore(); err != nil {
			return err
		}
		last = c.client.SetTUN(ctx, target)
		if mihomo.IsPermanent(last) {
			return last
		}
		if last == nil {
			// An accepted PATCH is not proof that the adapter was created.
			for check := 0; check < 3; check++ {
				if err := c.checkCore(); err != nil {
					return err
				}
				s, err := c.client.Status(ctx)
				if err == nil {
					if err := c.checkCore(); err != nil {
						return err
					}
					c.acceptStatus(s)
					if s.TUN != nil && *s.TUN == target {
						return nil
					}
				} else {
					c.tun = nil
					last = err
					if mihomo.IsPermanent(err) {
						return err
					}
				}
				if pause(ctx, c.retryInterval) != nil {
					break
				}
			}
		}
		if pause(ctx, c.retryInterval) != nil {
			break
		}
	}
	if last != nil {
		return fmt.Errorf("TUN 切换未确认，原记忆已保留: %w", last)
	}
	return errors.New("TUN 未达到目标状态，原记忆已保留；请在 WebUI 查看日志，并确认 mihomo 具有管理员权限")
}

func (c *Controller) ToggleProxy(ctx context.Context) error {
	c.op.Lock()
	defer c.op.Unlock()
	defer c.publish()
	enabled, _, err := c.system.Proxy()
	if err != nil {
		return err
	}
	if !enabled {
		if c.pid == 0 || !c.core.Alive(c.pid, c.activeExe) {
			c.pid, c.tun = 0, nil
			c.readProxy()
			return errors.New("请先启动 mihomo 核心")
		}
		if c.client == nil {
			c.readProxy()
			return errors.New("尚未连接核心，请刷新状态或重启核心")
		}
		s, err := c.client.Status(ctx)
		if err != nil {
			c.tun = nil
			c.readProxy()
			return err
		}
		c.acceptStatus(s)
		if c.proxyPort < 1 || c.proxyPort > 65535 {
			return errors.New("mihomo 没有可用的 mixed-port 或 port")
		}
	}
	if err := c.system.SetProxy(!enabled, c.proxyPort); err != nil {
		c.readProxy()
		return err
	}
	c.readProxy()
	return nil
}

// ProxyPort resolves the local HTTP proxy port used by clipboard commands.
// The live port of a running core wins; otherwise the selected configuration
// (local file or cached remote copy) is read without starting or touching the
// core. A remote configuration is never downloaded here.
func (c *Controller) ProxyPort(ctx context.Context) (int, error) {
	c.op.Lock()
	defer c.op.Unlock()
	if c.pid != 0 && c.proxyPort > 0 && c.core.Alive(c.pid, c.activeExe) {
		return c.proxyPort, nil
	}
	if _, ok := c.cfg.ActiveProfileConfig(); !ok {
		return 0, errors.New("请先在设置中选择 mihomo 配置文件")
	}
	src, err := prepareSource(ctx, c.base, c.cfg, true)
	if err != nil {
		return 0, err
	}
	if src.connection.ProxyPort == 0 {
		return 0, errors.New("mihomo 没有可用的 mixed-port 或 port")
	}
	return src.connection.ProxyPort, nil
}

func (c *Controller) ForgetTUN() error {
	c.op.Lock()
	defer c.op.Unlock()
	defer c.publish()
	next := c.cfg.Clone()
	next.TUNEnabled = nil
	if err := c.store.Save(next); err != nil {
		return err
	}
	c.cfg = next
	return nil // Follow YAML on the next core start; do not force a reload now.
}

func (c *Controller) SaveSettings(ctx context.Context, next config.Settings) error {
	c.op.Lock()
	defer c.op.Unlock()
	defer c.publish()
	return c.saveSettings(ctx, next)
}

func (c *Controller) saveSettings(ctx context.Context, next config.Settings) error {
	if err := next.Validate(c.base); err != nil {
		return err
	}
	next = next.Clone()
	next.TUNEnabled = copyBool(c.cfg.TUNEnabled) // An open settings window may be stale.
	old := c.cfg
	taskChanged := next.StartupLevel != old.StartupLevel || (next.StartupLevel != "off" && next.StartupDelay != old.StartupDelay)
	if taskChanged {
		if err := c.system.SetStartup(next.StartupLevel, next.StartupDelay); err != nil {
			return fmt.Errorf("开机自启未更新，设置尚未保存: %w", err)
		}
	}
	if err := c.store.Save(next); err != nil {
		if taskChanged {
			if undoErr := c.system.SetStartup(old.StartupLevel, old.StartupDelay); undoErr != nil {
				return fmt.Errorf("保存失败: %v；恢复原自启设置也失败: %v", err, undoErr)
			}
		}
		return fmt.Errorf("保存设置失败: %w", err)
	}
	c.cfg = next
	oldProfile, _ := old.ActiveProfileConfig()
	nextProfile, _ := next.ActiveProfileConfig()
	changed := old.CorePath != next.CorePath || !reflect.DeepEqual(oldProfile, nextProfile)
	if changed {
		if c.pid != 0 {
			if err := c.restart(ctx); err != nil {
				return fmt.Errorf("设置已保存，但应用失败: %w", err)
			}
			return nil
		}
		if next.AutoStartCore {
			if err := c.start(ctx, nil); err != nil {
				return fmt.Errorf("设置已保存，但启动失败: %w", err)
			}
		}
	}
	return nil
}

func (c *Controller) SelectProfile(ctx context.Context, name string) error {
	c.op.Lock()
	defer c.op.Unlock()
	defer c.publish()
	_, ok := c.cfg.Profiles[name]
	if !ok {
		return errors.New("配置不存在")
	}
	next := c.cfg.Clone()
	next.ActiveProfile = name
	return c.saveSettings(ctx, next)
}

func (c *Controller) SetStartup(ctx context.Context, level string) error {
	c.op.Lock()
	defer c.op.Unlock()
	defer c.publish()
	next := c.cfg.Clone()
	next.StartupLevel = level
	return c.saveSettings(ctx, next)
}

func pause(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
