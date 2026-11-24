package controller

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
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
}

type System interface {
	Proxy() (bool, string, error)
	SetProxy(enabled bool, port int) error
	Startup() (string, int, error)
	SetStartup(level string, delay int) error
	IsAdmin() bool
}

type SettingsStore interface{ Save(config.Settings) error }

type StartupSpec struct {
	Level string
	Delay int
}

const defaultStartupDelay = 15

type Snapshot struct {
	Settings         config.Settings
	Running          bool
	PID              int
	TUN, Proxy       *bool
	ConfigTUNEnabled bool
	StartupLevel     string // "", "off", "normal", "admin"; "" means unknown/unreadable
	StartupDelay     int
	ProxyServer      string
	ProxyTargetsCore bool
	WebURL           string
	Notice           string
	Revision         uint64
}

// All mutations (including lifecycle operations) share op.
// Polling is read-only and never overrides the running core's TUN state.
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
	ownedPID                    int
	activeExe                   string
	client                      *mihomo.Client
	hasTUNConfig                bool
	configTUNEnabled            bool
	startupLevel                string
	startupDelay                int
	tun, proxy                  *bool
	proxyServer                 string
	proxyPort                   int
	notice                      string
	revision                    uint64
	readyTimeout, retryInterval time.Duration
}

func New(base string, cfg config.Settings, store SettingsStore, core Core, system System) *Controller {
	c := &Controller{base: base, cfg: cfg.Clone(), store: store, core: core, system: system, startupDelay: defaultStartupDelay, readyTimeout: 45 * time.Second, retryInterval: 500 * time.Millisecond}
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
	for entry := range strings.SplitSeq(server, ";") {
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
		ConfigTUNEnabled: c.configTUNEnabled,
		StartupLevel:     c.startupLevel,
		StartupDelay:     c.startupDelay,
		Proxy:            copyBool(c.proxy),
		ProxyServer:      c.proxyServer,
		ProxyTargetsCore: c.proxy != nil && *c.proxy && proxyServerTargetsPort(c.proxyServer, c.proxyPort),
		Notice:           c.notice,
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
	level, delay, err := c.system.Startup()
	if err == nil {
		c.startupLevel, c.startupDelay = level, delay
	} else {
		c.startupLevel = ""
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
		c.ownedPID = 0 // Connecting to a running core must preserve its state.
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
		c.ownedPID = pid
	}
	c.pid, c.tun = pid, nil
	c.publish()
	if err := c.waitReady(ctx); err != nil {
		return err
	}
	if newProcess && c.configTUNEnabled && !c.system.IsAdmin() && (c.tun == nil || !*c.tun) {
		hint := "当前配置文件启用了 TUN，但 MiTray 未以管理员身份运行，请以管理员身份重新启动。"
		if c.notice == "" {
			c.notice = hint
		} else {
			c.notice += "；" + hint
		}
	}
	return nil
}

func (c *Controller) checkCore() error {
	if !c.core.Alive(c.pid, c.activeExe) {
		c.pid, c.ownedPID, c.tun = 0, 0, nil
		return errors.New("核心进程已退出或变化，已取消当前操作")
	}
	return nil
}

func (c *Controller) setClient(conn mihomo.Connection) {
	if c.client != nil {
		c.client.Close()
	}
	c.client = mihomo.NewClient(conn)
	c.proxyPort = conn.ProxyPort
	c.hasTUNConfig = conn.HasTUN
	c.configTUNEnabled = conn.TUNEnabled
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
			return fmt.Errorf("等待核心 API 就绪超时，可重启核心重试: %w", last)
		}
	}
}

func (c *Controller) Stop(ctx context.Context) error {
	c.op.Lock()
	defer c.op.Unlock()
	defer c.publish()
	return c.stop()
}

func (c *Controller) StopAndDisableProxy(ctx context.Context) error {
	c.op.Lock()
	defer c.op.Unlock()
	defer c.publish()
	if err := c.system.SetProxy(false, 0); err != nil {
		c.readProxy()
		return err
	}
	c.readProxy()
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
	c.pid, c.ownedPID, c.tun = 0, 0, nil
	c.notice = ""
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
	if c.pid != 0 && !c.core.Alive(c.pid, c.activeExe) {
		c.pid, c.ownedPID, c.tun = 0, 0, nil
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
		// Externally started cores are observed only; never override their TUN state.
		c.pid, c.ownedPID, c.activeExe, c.tun = pid, 0, exe, nil
		c.publish()
		if c.client != nil {
			c.client.Close()
			c.client = nil
		}
	}
	if c.client == nil {
		src, err := prepareSource(ctx, c.base, c.cfg, true)
		if err != nil {
			return err
		}
		c.setClient(src.connection)
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
		return errors.New("尚未连接核心，请稍候或重启核心")
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
	return c.applyTUN(ctx, !*s.TUN)
}

func (c *Controller) applyTUN(ctx context.Context, target bool) error {
	if err := c.checkCore(); err != nil {
		return err
	}
	if c.tun != nil && *c.tun == target {
		return nil
	}
	if target && c.ownedPID != 0 && c.ownedPID == c.pid && !c.system.IsAdmin() {
		return errors.New("当前 MiTray 未以管理员身份运行，无法启用 TUN；请使用「以管理员身份重新启动」")
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	var last error
	for range 3 {
		if err := c.checkCore(); err != nil {
			return err
		}
		last = c.client.SetTUN(ctx, target)
		if mihomo.IsPermanent(last) {
			return last
		}
		if last == nil {
			// An accepted PATCH is not proof that the adapter was created.
			for range 3 {
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
		return fmt.Errorf("TUN 切换未确认: %w", last)
	}
	if target && !c.hasTUNConfig {
		return errors.New("TUN 未达到目标状态；当前配置文件未包含 tun 段落，请检查 YAML 中的 tun/dns 配置并确认 mihomo 具有管理员权限")
	}
	return errors.New("TUN 未达到目标状态；请在 WebUI 查看日志，并确认 mihomo 具有管理员权限")
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
			return errors.New("尚未连接核心，请稍候或重启核心")
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

// ActiveConfigPath returns the local YAML path or cached remote YAML path for
// the currently active profile without mutating core state or downloading.
func (c *Controller) ActiveConfigPath() (string, error) {
	s := c.Snapshot().Settings
	profile, ok := s.ActiveProfileConfig()
	if !ok {
		return "", errors.New("请先在设置中选择 mihomo 配置文件")
	}
	switch profile.Type {
	case config.ProfileLocal:
		path := profile.Resolve(c.base)
		if path == "" {
			return "", errors.New("请先在设置中选择本地 mihomo 配置文件")
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("无法找到配置文件: %w", err)
		}
		if info.IsDir() {
			return "", errors.New("当前配置路径是文件夹")
		}
		return path, nil
	case config.ProfileURL:
		if strings.TrimSpace(profile.URL) == "" {
			return "", errors.New("请先填写远程配置 URL")
		}
		path := filepath.Join(c.base, "cache", remoteCacheName(profile.URL))
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return "", errors.New("远程配置尚未缓存，请先启动或重启核心下载")
			}
			return "", fmt.Errorf("无法读取远程配置缓存: %w", err)
		}
		return path, nil
	default:
		return "", errors.New("配置来源类型无效")
	}
}

func validateStartup(spec StartupSpec) error {
	switch spec.Level {
	case "off", "normal", "admin":
	default:
		return errors.New("无效的开机自启选项")
	}
	if spec.Delay < 0 || spec.Delay > 600 {
		return errors.New("自启延迟应为 0–600 秒")
	}
	return nil
}

func (c *Controller) SaveSettings(ctx context.Context, next config.Settings) error {
	c.op.Lock()
	defer c.op.Unlock()
	defer c.publish()
	return c.saveSettings(ctx, next, nil)
}

func (c *Controller) SaveSettingsWithStartup(ctx context.Context, next config.Settings, startup *StartupSpec) error {
	c.op.Lock()
	defer c.op.Unlock()
	defer c.publish()
	return c.saveSettings(ctx, next, startup)
}

func (c *Controller) saveSettings(ctx context.Context, next config.Settings, startup *StartupSpec) error {
	if err := next.Validate(c.base); err != nil {
		return err
	}
	if startup != nil {
		if err := validateStartup(*startup); err != nil {
			return err
		}
	}
	next = next.Clone()
	old := c.cfg
	oldLevel, oldDelay := c.startupLevel, c.startupDelay
	taskChanged := startup != nil && (startup.Level != oldLevel || (startup.Level != "off" && startup.Delay != oldDelay))
	if taskChanged {
		if err := c.system.SetStartup(startup.Level, startup.Delay); err != nil {
			return fmt.Errorf("开机自启未更新，设置尚未保存: %w", err)
		}
	}
	if err := c.store.Save(next); err != nil {
		if taskChanged && oldLevel != "" {
			if undoErr := c.system.SetStartup(oldLevel, oldDelay); undoErr != nil {
				return fmt.Errorf("保存失败: %v；恢复原自启设置也失败: %v", err, undoErr)
			}
		}
		return fmt.Errorf("保存设置失败: %w", err)
	}
	c.cfg = next
	if taskChanged {
		c.startupLevel, c.startupDelay = startup.Level, startup.Delay
	}
	oldProfile, _ := old.ActiveProfileConfig()
	nextProfile, _ := next.ActiveProfileConfig()
	changed := old.CorePath != next.CorePath || !reflect.DeepEqual(oldProfile, nextProfile)
	if changed {
		if c.pid != 0 {
			if err := c.restart(ctx); err != nil {
				pruneRemoteCache(c.base, next.Profiles)
				return fmt.Errorf("设置已保存，但应用失败: %w", err)
			}
			pruneRemoteCache(c.base, next.Profiles)
			return nil
		}
		if next.AutoStartCore {
			if err := c.start(ctx, nil); err != nil {
				pruneRemoteCache(c.base, next.Profiles)
				return fmt.Errorf("设置已保存，但启动失败: %w", err)
			}
		}
	}
	pruneRemoteCache(c.base, next.Profiles)
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
	return c.saveSettings(ctx, next, nil)
}

func (c *Controller) SetStartup(ctx context.Context, level string, delay int) error {
	c.op.Lock()
	defer c.op.Unlock()
	defer c.publish()
	if delay < 0 {
		delay = c.startupDelay
	}
	spec := StartupSpec{Level: level, Delay: delay}
	if err := validateStartup(spec); err != nil {
		return err
	}
	if err := c.system.SetStartup(spec.Level, spec.Delay); err != nil {
		return err
	}
	c.startupLevel = spec.Level
	if spec.Level == "off" {
		c.startupDelay = defaultStartupDelay
	} else {
		c.startupDelay = spec.Delay
	}
	return nil
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
