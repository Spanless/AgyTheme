package daemon

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	agytheme "github.com/Spanless/AgyTheme"
	"github.com/Spanless/AgyTheme/internal/bundle"
	"github.com/Spanless/AgyTheme/internal/cdp"
	"github.com/Spanless/AgyTheme/internal/installer"
	"github.com/Spanless/AgyTheme/internal/metrics"
	"github.com/Spanless/AgyTheme/internal/pane"
	"github.com/Spanless/AgyTheme/internal/shared"
	"github.com/Spanless/AgyTheme/internal/updater"
)

const SyncInterval = 2500 * time.Millisecond

func PidFile(channel string) string {
	if channel == "" {
		channel = "default"
	}
	return filepath.Join(os.TempDir(), "agytheme-daemon-"+channel+".pid")
}

func AcquirePidLock(channel string) func() {
	pidFile := PidFile(channel)
	if old := shared.ReadPidFile(pidFile); old > 0 && old != os.Getpid() &&
		shared.IsConfirmedDaemonPid(old) {
		_ = shared.TerminatePid(old)
	}
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o644)
	return func() {
		raw, err := os.ReadFile(pidFile)
		if err == nil && strings.TrimSpace(string(raw)) == strconv.Itoa(os.Getpid()) {
			_ = os.Remove(pidFile)
		}
	}
}

func TrimLogFileIfOversized(path string, maxBytes, keepBytes int64) bool {
	if maxBytes <= 0 {
		maxBytes = 256 * 1024
	}
	if keepBytes <= 0 {
		keepBytes = 64 * 1024
	}
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() <= maxBytes {
		return false
	}
	readLen := keepBytes
	if fi.Size() < readLen {
		readLen = fi.Size()
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	buf := make([]byte, readLen)
	_, err = f.ReadAt(buf, fi.Size()-readLen)
	f.Close()
	if err != nil {
		return false
	}
	tail := buf
	if nl := bytes.IndexByte(buf, '\n'); nl != -1 && nl+1 < len(buf) {
		tail = buf[nl+1:]
	}
	header := fmt.Sprintf("[Daemon] Log trimmed at %s (kept %dB)\n", time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), len(tail))
	return os.WriteFile(path, append([]byte(header), tail...), 0o644) == nil
}

// Antigravity never rotates captured sidecar stdout, so logs are redirected here for trimming.
func SidecarLogFile(rootDir string) string {
	return filepath.Join(rootDir, "logs", "sidecar.log")
}

func redirectSidecarOutput(path string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	fmt.Printf("[Daemon] AgyTheme sidecar logs to %s\n", path)
	os.Stdout = f
	os.Stderr = f
}

// Sidecars start seconds after Antigravity; allow a wider cold-start relaunch window.
const SidecarColdStartWindowSec = 40.0

const PendingActivationName = "pending-activation.json"

type Options struct {
	RootDir string
	Source  bundle.Source
	Cli     bundle.CliOpts
	// Sidecar mode waits on an existing PID lock rather than exiting to avoid Antigravity restart loops.
	Sidecar       bool
	SpawnRelaunch func(proc cdp.HostProc) error
}

func WaitForPidLock(channel string, stop <-chan os.Signal) func() {
	pidFile := PidFile(channel)
	announced := false
	for {
		old := shared.ReadPidFile(pidFile)
		if old <= 0 || old == os.Getpid() || !shared.IsConfirmedDaemonPid(old) {
			break
		}
		if !announced {
			fmt.Printf("[Daemon] Another AgyTheme daemon (PID %d) is running; waiting for it to exit.\n", old)
			announced = true
		}
		select {
		case <-stop:
			os.Exit(0)
		case <-time.After(5 * time.Second):
		}
	}
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o644)
	return func() {
		raw, err := os.ReadFile(pidFile)
		if err == nil && strings.TrimSpace(string(raw)) == strconv.Itoa(os.Getpid()) {
			_ = os.Remove(pidFile)
		}
	}
}

func Run(opts Options) {
	paneNote := ""
	sidePane := false
	syncNow := make(chan struct{}, 1)
	var mgr *cdp.Manager
	var upd *updater.Updater
	if opts.Sidecar && !opts.Cli.RunOnce {
		pluginDir := installer.PluginDir(installer.DefaultPluginsDir())
		upd = updater.New(updater.Config{
			CurrentVersion: agytheme.Version,
			PluginDir:      pluginDir,
			RootDir:        opts.RootDir,
		})
		api := &pane.API{
			RootDir:   opts.RootDir,
			PluginDir: pluginDir,
			Updater:   upd,
			OnConfigChanged: func() {
				select {
				case syncNow <- struct{}{}:
				default:
				}
			},
			Doctor: func() pane.DoctorStatus {
				attached := 0
				if mgr != nil {
					attached = mgr.AttachedCount()
				}
				if attached > 0 {
					return pane.DoctorStatus{OK: true, Summary: "运行正常 · CDP 已连接", CdpReachable: true}
				}
				return pane.DoctorStatus{OK: false, Summary: "未连接到 Antigravity 调试端口", CdpReachable: false}
			},
			Metrics: func(cid string) (*metrics.Payload, bool) {
				if mgr == nil {
					return nil, false
				}
				return mgr.MetricsPayload(cid)
			},
			WatchCid: func(cid string) {
				if mgr != nil {
					mgr.WatchPaneCid(cid)
				}
			},
			Subscribe: func() (<-chan string, func()) {
				if mgr == nil {
					return nil, func() {}
				}
				return mgr.SubscribeMetrics()
			},
		}
		if addr, stop, err := pane.ListenFromEnvWithAPI(bundle.NewPanePage(opts.Source), api); err == nil {
			defer stop()
			sidePane = true
			paneNote = "Side-pane panel on " + addr
		} else {
			paneNote = "Side-pane panel unavailable: " + err.Error()
		}
	}
	ctrl := bundle.NewController(bundle.Options{RootDir: opts.RootDir, Source: opts.Source, Cli: opts.Cli, SidePane: sidePane})
	ls := metrics.NewLSClientFromEnv()
	mgr = cdp.NewManager(ctrl, ls, opts.Cli.RunOnce, opts.Cli.SafeMode)
	if opts.Sidecar && !opts.Cli.RunOnce {
		logPath := SidecarLogFile(opts.RootDir)
		redirectSidecarOutput(logPath)
		mgr.LogTrim = func() { TrimLogFileIfOversized(logPath, 0, 0) }
	}

	coldStartWindow := 0.0
	if opts.Sidecar {
		coldStartWindow = SidecarColdStartWindowSec
	}
	mgr.ConfigureRelaunch(opts.SpawnRelaunch, filepath.Join(opts.RootDir, PendingActivationName), coldStartWindow)

	var release func()
	if !opts.Cli.RunOnce {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
		if opts.Sidecar {
			release = WaitForPidLock(opts.Cli.Channel, sigs)
		} else {
			release = AcquirePidLock(opts.Cli.Channel)
		}
		go func() {
			<-sigs
			mgr.CloseAll()
			release()
			os.Exit(0)
		}()
	}
	if mgr.LogTrim != nil {
		mgr.LogTrim()
	}
	ctrl.RebuildIfNeeded()
	cfg, _, patchVersion := ctrl.State()
	mode := "daemon"
	if opts.Sidecar {
		mode = "sidecar"
	}
	fmt.Printf("[agytheme] Started (%s) | mode=%s | preset=%s | channel=%s | metrics=%t | safeMode=%t\n",
		patchVersion, mode, cfg.Preset, cfg.Channel, ls != nil, opts.Cli.SafeMode)
	if paneNote != "" {
		fmt.Printf("[agytheme] %s\n", paneNote)
	}

	mgr.Sync()
	if opts.Cli.RunOnce {
		mgr.CloseAll()
		return
	}
	if upd != nil {
		go func() {
			time.Sleep(5 * time.Second)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			upd.Check(ctx)
		}()
	}
	ticker := time.NewTicker(SyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			mgr.Sync()
		case <-syncNow:
			mgr.Sync()
		}
	}
}
