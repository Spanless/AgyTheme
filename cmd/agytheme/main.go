package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	agytheme "github.com/Spanless/AgyTheme"
	"github.com/Spanless/AgyTheme/internal/bundle"
	"github.com/Spanless/AgyTheme/internal/cdp"
	"github.com/Spanless/AgyTheme/internal/daemon"
	"github.com/Spanless/AgyTheme/internal/installer"
	"github.com/Spanless/AgyTheme/internal/shared"
	"github.com/Spanless/AgyTheme/internal/updater"
)

type cliArgs []string

func (a cliArgs) has(flag string) bool {
	for _, x := range a {
		if x == flag || strings.HasPrefix(x, flag+"=") {
			return true
		}
	}
	return false
}

func (a cliArgs) get(flag, fallback string) string {
	for i, x := range a {
		if x == flag && i+1 < len(a) {
			return a[i+1]
		}
		if strings.HasPrefix(x, flag+"=") {
			return x[len(flag)+1:]
		}
	}
	return fallback
}

const usage = `AgyTheme CLI Usage (agytheme %s):
  agytheme install    [--channel auto|stable|insiders] [--preset light|dark] [--only theme,i18n,metrics,fixes] [--without <list>] [--with-launcher] [--sync-user] [--no-relaunch] [--target-dir <dir>] [--plugins-dir <dir>]
  agytheme relaunch   [--channel auto|stable|insiders] [--force] [--target-dir <dir>]
  agytheme doctor     [--channel auto|stable|insiders] [--target-dir <dir>]
  agytheme verify     [--safe-mode] [--channel auto|stable|insiders] [--target-dir <dir>]
  agytheme update     [--check] [--target-dir <dir>] [--plugins-dir <dir>]
  agytheme rollback   [--target-dir <dir>]
  agytheme uninstall  [--purge] [--target-dir <dir>]
  agytheme sidecar    [--root <dir>]   (run by Antigravity from the plugin's sidecar.json)
  agytheme daemon     [--once] [--safe-mode] [--channel stable|insiders] [--port-file <file>] [--conversations-dir <dir>] [--root <dir>] [--core-dir <dir>]
  agytheme version
`

func main() {
	args := cliArgs(os.Args[1:])
	if d := args.get("--plugins-dir", ""); d != "" {
		abs, _ := filepath.Abs(d)
		_ = os.Setenv("AGYTHEME_PLUGINS_DIR", abs)
	}
	sub := "help"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = args[0]
		args = args[1:]
	}
	switch sub {
	case "daemon":
		runDaemon(args)
	case "sidecar":
		runSidecar(args)
	case "relaunch-host":
		runRelaunchHost(args)
	case "stage-plugin":
		runStagePlugin(args)
	case "bundle":
		runBundle(args)
	case "install":
		runInstall(args)
	case "relaunch":
		runRelaunch(args)
	case "doctor":
		runDoctor(args)
	case "verify":
		runVerify(args)
	case "update":
		runUpdate(args)
	case "rollback":
		runRollback(args)
	case "uninstall":
		runUninstall(args)
	case "version", "--version", "-v":
		fmt.Println(agytheme.Version)
	default:
		fmt.Printf(usage, agytheme.Version)
	}
}

func printJSON(w io.Writer, v any) {
	w.Write(shared.MarshalPretty(v, "  "))
}

func ordered(kv ...any) *shared.OrderedMap {
	om := shared.NewOrderedMap()
	for i := 0; i+1 < len(kv); i += 2 {
		om.Set(kv[i].(string), kv[i+1])
	}
	return om
}

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}

func selfPath() string {
	p := shared.SelfExecutable()
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func resolveRoot(flag string) string {
	if flag != "" {
		abs, _ := filepath.Abs(flag)
		return abs
	}
	exeDir := filepath.Dir(selfPath())
	if filepath.Base(exeDir) == "bin" && shared.FileExists(filepath.Join(filepath.Dir(exeDir), "user")) {
		return filepath.Dir(exeDir)
	}
	if def := installer.DefaultTargetDir(); shared.FileExists(filepath.Join(def, "user")) {
		return def
	}
	cwd, _ := os.Getwd()
	return cwd
}

func resolveSource(coreDir string) bundle.Source {
	if coreDir != "" {
		abs, _ := filepath.Abs(coreDir)
		return bundle.DirSource(abs)
	}
	return bundle.EmbeddedSource()
}

func targetDirArg(args cliArgs) string {
	d := args.get("--target-dir", installer.DefaultTargetDir())
	abs, err := filepath.Abs(d)
	if err != nil {
		return d
	}
	return abs
}

func daemonCli(args cliArgs) bundle.CliOpts {
	return bundle.CliOpts{
		SafeMode:         args.has("--safe-mode"),
		RunOnce:          args.has("--once"),
		Channel:          args.get("--channel", ""),
		PortFile:         args.get("--port-file", ""),
		ConversationsDir: args.get("--conversations-dir", ""),
	}
}

func runDaemon(args cliArgs) {
	daemon.Run(daemon.Options{
		RootDir: resolveRoot(args.get("--root", "")),
		Source:  resolveSource(args.get("--core-dir", "")),
		Cli:     daemonCli(args),
	})
}

func runSidecar(args cliArgs) {
	self := selfPath()
	daemon.Run(daemon.Options{
		RootDir: resolveRoot(args.get("--root", "")),
		Source:  resolveSource(args.get("--core-dir", "")),
		Cli:     daemonCli(args),
		Sidecar: true,
		SpawnRelaunch: func(proc cdp.HostProc) error {
			_, err := shared.SpawnBreakaway(self, "relaunch-host",
				"--pid", strconv.Itoa(proc.Pid), "--app-path", proc.AppPath, "--exe-path", proc.ExePath)
			return err
		},
	})
}

func runRelaunchHost(args cliArgs) {
	pid, _ := strconv.Atoi(args.get("--pid", "0"))
	if pid <= 0 {
		fatalf("[ERROR] relaunch-host needs --pid")
	}
	proc := cdp.HostProc{Pid: pid, AppPath: args.get("--app-path", ""), ExePath: args.get("--exe-path", "")}
	if err := cdp.RelaunchHost(proc); err != nil {
		fatalf("[ERROR] %v", err)
	}
}

func runStagePlugin(args cliArgs) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fatalf("usage: agytheme stage-plugin <dir>")
	}
	if err := installer.StagePluginFiles(args[0], installer.PluginFiles{
		Version: agytheme.Version, Skill: agytheme.PluginSkill, Logo: agytheme.PluginLogo,
	}); err != nil {
		fatalf("[ERROR] %v", err)
	}
}

func runBundle(args cliArgs) {
	ctrl := bundle.NewController(bundle.Options{
		RootDir: resolveRoot(args.get("--root", "")),
		Source:  resolveSource(args.get("--core-dir", "")),
		Cli:     daemonCli(args),
	})
	ctrl.RebuildIfNeeded()
	cfg, script, patchVersion := ctrl.State()
	if args.has("--json") {
		printJSON(os.Stdout, ordered("patchVersion", patchVersion, "enabled", cfg.Enabled, "preset", cfg.Preset,
			"channel", cfg.Channel, "modules", cfg.Modules, "endpoints", cfg.Endpoints, "script", script))
		return
	}
	fmt.Print(script)
}

func runDaemonOnce(targetDir string, extra []string, inherit bool) {
	bin := daemonBinary()
	cmdArgs := append([]string{"daemon", "--once", "--root", targetDir}, extra...)
	cmd := exec.Command(bin, cmdArgs...)
	if inherit {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	_ = cmd.Run()
}

func pluginDir() string { return installer.PluginDir(installer.DefaultPluginsDir()) }

func daemonBinary() string {
	if bin := installer.PluginBinary(pluginDir()); shared.FileExists(bin) {
		return bin
	}
	return selfPath()
}

func resolveLauncherMode(env installer.Env) string {
	switch runtime.GOOS {
	case "darwin":
		if env.LauncherInstalled {
			return "optional-launcher-app"
		}
		return "single-icon-m2-relay"
	case "windows":
		return "in-place-lnk"
	}
	return "desktop-entry"
}

func remediationFor(targetDir string, live installer.Live, cdpStatus string) any {
	if !live.Reachable && cdpStatus == "missing_opt_in_flag" {
		q, _ := json.Marshal(daemonBinary())
		return string(q) + " relaunch"
	}
	return nil
}

func effectiveCdpStatus(env installer.Env, live installer.Live) string {
	if live.Reachable {
		return "ready"
	}
	return env.CdpStatus
}

func readState(targetDir string) bundle.InstallState {
	var st bundle.InstallState
	shared.ReadJSONFile(filepath.Join(targetDir, "state.json"), &st)
	return st
}

func runDoctor(args cliArgs) {
	channel := args.get("--channel", "auto")
	targetDir := targetDirArg(args)
	env := installer.DetectEnvironment(channel)
	live := installer.InspectLiveWindow(&env, "", 0)
	cdpMissing := !live.Reachable && env.CdpStatus == "missing_opt_in_flag"
	plugin := pluginDir()
	bin := installer.PluginBinary(plugin)
	sidecarFile := installer.SidecarFile(plugin)
	var sidecarPid any
	if pid := shared.ReadPidFile(daemon.PidFile("")); pid > 0 && shared.IsPidAlive(pid) {
		sidecarPid = pid
	}
	state := readState(targetDir)
	var installedVersion any
	if state.Version != "" {
		installedVersion = state.Version
	}
	var features any
	if state.Features != nil {
		features = state.Features
	}
	report := ordered(
		"ok", !cdpMissing,
		"runtime", ordered(
			"version", agytheme.Version,
			"go", runtime.Version(),
			"executable", selfPath(),
			"platform", shared.Platform(),
			"arch", runtime.GOARCH,
		),
		"antigravity", ordered(
			"channel", env.Channel,
			"appExists", env.AppExists,
			"appVersion", env.AppVersion,
			"cdpStatus", effectiveCdpStatus(env, live),
			"launcherMode", resolveLauncherMode(env),
			"launcherInstalled", env.LauncherInstalled,
			"runningProcesses", env.RunningProcesses,
			"remediation", remediationFor(targetDir, live, env.CdpStatus),
			"portFile", env.PortFile,
			"portFileExists", env.PortFileExists,
			"channels", env.Channels,
		),
		"install", ordered(
			"targetDir", targetDir,
			"pluginDir", plugin,
			"binary", bin,
			"binaryInstalled", shared.FileExists(bin),
			"sidecarFile", sidecarFile,
			"sidecarConfigured", shared.FileExists(sidecarFile),
			"sidecarPid", sidecarPid,
			"plugin", installer.ReadPluginState(installer.DefaultPluginsDir()),
			"pendingActivation", shared.FileExists(filepath.Join(targetDir, daemon.PendingActivationName)),
			"log", daemon.SidecarLogFile(targetDir),
			"installedVersion", installedVersion,
			"features", features,
			"rollbackAvailable", shared.FileExists(installer.PluginPrevBinary(plugin)),
		),
		"liveWindow", live,
	)
	printJSON(os.Stdout, report)
}

func fileSHA(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func copyFileAtomic(src, dst string, perm os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	_ = os.Chmod(tmp, perm)
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func installBinary(dst, prev string) (any, error) {
	self := selfPath()
	if resolved, err := filepath.EvalSymlinks(dst); err == nil && resolved == self {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, err
	}
	var backup any
	if shared.FileExists(dst) {
		if fileSHA(dst) == fileSHA(self) {
			return nil, nil
		}
		if err := os.Remove(prev); err != nil && !os.IsNotExist(err) {
			_ = os.Rename(prev, fmt.Sprintf("%s.%d.old", prev, time.Now().Unix()))
		}
		if err := os.Rename(dst, prev); err != nil {
			return nil, err
		}
		backup = prev
	}
	return backup, copyFileAtomic(self, dst, 0o755)
}

func cleanupPluginBin(binDir string) []string {
	removed := []string{}
	entries, err := os.ReadDir(binDir)
	if err != nil {
		return removed
	}
	keep := map[string]bool{installer.BinaryName(): true, installer.BinaryName() + ".prev": true}
	for _, e := range entries {
		name := e.Name()
		if keep[name] || e.IsDir() || !strings.HasPrefix(name, "agytheme") {
			continue
		}
		if strings.HasPrefix(name, "agytheme-") || strings.HasSuffix(name, ".old") || strings.HasSuffix(name, ".tmp") {
			if os.Remove(filepath.Join(binDir, name)) == nil {
				removed = append(removed, name)
			}
		}
	}
	return removed
}

func asOrdered(v any) *shared.OrderedMap {
	if om, ok := v.(*shared.OrderedMap); ok {
		return om
	}
	return nil
}

func isTruthyString(v any) bool {
	s, ok := v.(string)
	return ok && s != ""
}

func runInstall(args cliArgs) {
	cliChannel := args.get("--channel", "")
	channel := cliChannel
	if channel == "" {
		channel = "auto"
	}
	presetArg := args.get("--preset", "")
	onlyArg := args.get("--only", args.get("--features", ""))
	withoutArg := args.get("--without", args.get("--skip", ""))
	withLauncher := args.has("--with-launcher")
	syncUser := args.has("--sync-user")
	targetDir := targetDirArg(args)

	sel, err := bundle.ParseFeatureSelection(onlyArg, withoutArg)
	if err != nil {
		fatalf("[ERROR] %v", err)
	}
	plugin := pluginDir()
	bin := installer.PluginBinary(plugin)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		fatalf("[ERROR] Cannot create %s: %v", targetDir, err)
	}
	backupCreated, err := installBinary(bin, installer.PluginPrevBinary(plugin))
	if err != nil {
		fatalf("[ERROR] Failed to install binary to %s: %v", bin, err)
	}
	binCleaned := cleanupPluginBin(filepath.Dir(bin))

	installer.StopRunningDaemonProcesses()
	if err := installer.StagePluginFiles(plugin, installer.PluginFiles{
		Version: agytheme.Version, Skill: agytheme.PluginSkill, Logo: agytheme.PluginLogo,
	}); err != nil {
		fatalf("[ERROR] Cannot write the plugin into %s: %v", plugin, err)
	}

	userDir := filepath.Join(targetDir, "user")
	_ = os.MkdirAll(userDir, 0o755)
	_ = fs.WalkDir(agytheme.UserFS, "user", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		dst := filepath.Join(userDir, filepath.Base(p))
		if syncUser || !shared.FileExists(dst) {
			if data, err := agytheme.UserFS.ReadFile(p); err == nil {
				_ = os.WriteFile(dst, data, 0o644)
			}
		}
		return nil
	})

	installed := map[string]bool{}
	for _, id := range bundle.FeatureIDs() {
		installed[id] = !sel.Explicit || sel.Selected[id]
	}

	cfgPath := filepath.Join(userDir, "config.json")
	cfg := shared.NewOrderedMap()
	if raw, err := os.ReadFile(cfgPath); err == nil {
		if err := json.Unmarshal([]byte(shared.StripUtf8Bom(string(raw))), cfg); err != nil {
			fatalf("[ERROR] %s is not valid JSON: %v", cfgPath, err)
		}
	}
	if v, _ := cfg.Get("enabled"); func() bool { _, ok := v.(bool); return !ok }() {
		cfg.Set("enabled", true)
	}
	if cliChannel != "" {
		cfg.Set("channel", cliChannel)
	} else if v, _ := cfg.Get("channel"); !isTruthyString(v) {
		cfg.Set("channel", "auto")
	}
	if presetArg != "" {
		cfg.Set("preset", bundle.NormalizePreset(presetArg))
	} else {
		curPreset, _ := cfg.Get("preset")
		s, _ := curPreset.(string)
		cfg.Set("preset", bundle.NormalizePreset(s))
	}
	if v, _ := cfg.Get("telemetryLayout"); !isTruthyString(v) {
		cfg.Set("telemetryLayout", "compact")
	}
	if v, _ := cfg.Get("telemetryCacheMode"); !isTruthyString(v) {
		cfg.Set("telemetryCacheMode", "session")
	}
	modsV, _ := cfg.Get("modules")
	mods := asOrdered(modsV)
	if mods == nil {
		mods = shared.NewOrderedMap()
		cfg.Set("modules", mods)
	}
	allowed := map[string]bool{}
	for _, k := range bundle.ModuleKeys {
		allowed[k] = true
	}
	for _, k := range mods.Keys() {
		if !allowed[k] {
			mods.Delete(k)
		}
	}
	if sel.Explicit {
		for _, g := range bundle.FeatureGroups {
			for _, k := range g.ConfigKeys {
				mods.Set(k, sel.ModulesPatch[k])
			}
		}
	}
	isBool := func(k string) bool { v, _ := mods.Get(k); _, ok := v.(bool); return ok }
	if !isBool("cdpAutoRelaunch") {
		mods.Set("cdpAutoRelaunch", true)
	}
	if !installed["theme"] {
		mods.Set("theme", false)
	} else if !isBool("theme") {
		mods.Set("theme", true)
	}
	if !installed["i18n"] {
		mods.Set("i18n", false)
	}
	if !installed["metrics"] {
		mods.Set("metrics", false)
	}
	if !installed["fixes"] {
		mods.Set("deadlockHealer", false)
		mods.Set("zoomShortcuts", false)
	}
	if err := os.WriteFile(cfgPath, shared.MarshalPretty(cfg, "  "), 0o644); err != nil {
		fatalf("[ERROR] Cannot write %s: %v", cfgPath, err)
	}

	prevState := readState(targetDir)
	state := bundle.InstallState{Version: agytheme.Version}
	if prevState.Version != "" && prevState.Version != agytheme.Version {
		state.PrevVersion = prevState.Version
	} else if prevState.PrevVersion != "" {
		state.PrevVersion = prevState.PrevVersion
	}
	if sel.Explicit {
		state.Features = installed
	}
	_ = os.WriteFile(filepath.Join(targetDir, "state.json"), shared.MarshalPretty(state, "  "), 0o644)

	cfgChannel, _ := cfg.Get("channel")
	chStr, _ := cfgChannel.(string)
	argvStatus := installer.EnsureArgvRemoteDebugging(chStr)
	launcherInfo := installer.EnsureCdpLaunchers(chStr, withLauncher)

	sidecarFile, err := installer.WriteSidecar(plugin, bin, targetDir)
	if err != nil {
		fatalf("[ERROR] Cannot write %s: %v", sidecarFile, err)
	}
	pluginState := installer.ReadPluginState(installer.DefaultPluginsDir())
	var enableErr any
	if !pluginState.Enabled {
		if err := installer.EnablePlugin(); err != nil {
			enableErr = err.Error()
		} else {
			pluginState = installer.ReadPluginState(installer.DefaultPluginsDir())
		}
	}

	runDaemonOnce(targetDir, nil, true)

	postEnv := installer.DetectEnvironment(chStr)
	live := installer.InspectLiveWindow(&postEnv, "", 0)
	cdpStatus := effectiveCdpStatus(postEnv, live)
	activation := activateAfterInstall(args, targetDir, chStr, postEnv, cdpStatus)
	if activation.Relaunched {
		postEnv = installer.DetectEnvironment(chStr)
		live = installer.InspectLiveWindow(&postEnv, "", 0)
		cdpStatus = effectiveCdpStatus(postEnv, live)
	}
	var features any
	if state.Features != nil {
		features = state.Features
	}
	report := ordered(
		"ok", true,
		"version", agytheme.Version,
		"targetDir", targetDir,
		"pluginDir", plugin,
		"binary", bin,
		"sidecarFile", sidecarFile,
		"plugin", ordered("enabled", pluginState.Enabled, "source", pluginState.Source, "enableError", enableErr),
		"features", features,
		"appVersion", postEnv.AppVersion,
		"cdpStatus", cdpStatus,
		"launcherMode", resolveLauncherMode(postEnv),
		"launcherInfo", launcherInfo,
		"activation", activation,
		"remediation", remediationFor(targetDir, live, cdpStatus),
		"userSynced", syncUser,
		"backupCreated", backupCreated,
		"argvStatus", argvStatus,
		"binCleaned", binCleaned,
		"config", cfg,
		"liveWindow", live,
	)
	var buf bytes.Buffer
	buf.WriteString("\n[Install Complete] ")
	buf.Write(shared.MarshalPretty(report, "  "))
	os.Stdout.Write(buf.Bytes())
	fmt.Fprintln(os.Stderr, "\n"+installSummary(activation, cdpStatus, live, pluginState.Enabled))
}

type installActivation struct {
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	Relaunched bool   `json:"relaunched"`
	Detail     any    `json:"detail,omitempty"`
}

const activeTurnWindow = 15 * time.Second

func activateAfterInstall(args cliArgs, targetDir, channel string, env installer.Env, cdpStatus string) installActivation {
	if cdpStatus != "missing_opt_in_flag" {
		return installActivation{Status: "not_needed", Reason: cdpStatus}
	}
	if args.has("--no-relaunch") {
		return installActivation{Status: "disabled", Reason: "no_relaunch_flag"}
	}
	if channel == "" {
		channel = "auto"
	}
	reason := ""
	if installer.InsideHostProcess(env.RunningProcesses) {
		reason = "installer_inside_antigravity"
	} else if len(installer.HasRecentActiveConversationWal(channel, activeTurnWindow)) > 0 {
		reason = "active_conversation"
	}
	if reason != "" {
		marker := filepath.Join(targetDir, daemon.PendingActivationName)
		if err := cdp.WritePendingActivation(marker, reason); err != nil {
			return installActivation{Status: "relaunch_failed", Reason: reason, Detail: err.Error()}
		}
		return installActivation{Status: "scheduled", Reason: reason, Detail: marker}
	}
	fmt.Fprintln(os.Stderr, "[agytheme] 正在重启 Antigravity 以启用 AgyTheme，窗口和工作区会自动恢复…")
	res := installer.RelaunchAntigravityWithCdp(channel, true, func() {
		runDaemonOnce(targetDir, nil, false)
	})
	if res.ExitCode != 0 {
		return installActivation{Status: "relaunch_failed", Detail: res.Payload}
	}
	return installActivation{Status: "relaunched", Relaunched: true, Detail: res.Payload}
}

func installSummary(a installActivation, cdpStatus string, live installer.Live, pluginEnabled bool) string {
	switch {
	case !pluginEnabled:
		return "[agytheme] 文件已装好，但 AgyTheme 插件在 Antigravity 里处于停用状态：请在 Antigravity 的插件设置里启用它（或运行 agy plugin enable agytheme）。"
	case live.Reachable && live.StateBool("hasStyle"):
		return "[agytheme] 安装完成，已在 Antigravity 中生效。"
	case cdpStatus == "ready" || a.Status == "relaunched":
		return "[agytheme] 安装完成，Antigravity 窗口加载完后即生效。"
	case a.Status == "scheduled":
		return "[agytheme] 安装完成。当前对话结束、Antigravity 空闲约 30 秒后，它会自动重启一次来启用 AgyTheme，窗口和对话都会恢复，无需手动操作。"
	case a.Status == "relaunch_failed" || a.Status == "disabled":
		return "[agytheme] 安装完成。请退出并重新打开 Antigravity 一次以生效。"
	default:
		return "[agytheme] 安装完成，下次打开 Antigravity 时自动生效。"
	}
}

func runRelaunch(args cliArgs) {
	channel := args.get("--channel", "auto")
	targetDir := targetDirArg(args)
	res := installer.RelaunchAntigravityWithCdp(channel, args.has("--force"), func() {
		runDaemonOnce(targetDir, nil, false)
	})
	if res.ExitCode == 0 {
		printJSON(os.Stdout, res.Payload)
		return
	}
	printJSON(os.Stderr, res.Payload)
	os.Exit(res.ExitCode)
}

func runVerify(args cliArgs) {
	safeMode := args.has("--safe-mode")
	channel := args.get("--channel", "auto")
	targetDir := targetDirArg(args)
	var extra []string
	if safeMode {
		extra = append(extra, "--safe-mode")
	}
	if channel != "" && channel != "auto" {
		extra = append(extra, "--channel", channel)
	}
	runDaemonOnce(targetDir, extra, true)
	env := installer.DetectEnvironment(channel)
	live := installer.InspectLiveWindow(&env, "", 0)
	cdpStatus := effectiveCdpStatus(env, live)
	ok := live.Reachable && (live.StateBool("hasStyle") || (live.Pages != nil && *live.Pages == 0))
	printJSON(os.Stdout, ordered(
		"ok", ok,
		"version", live.StateValue("version"),
		"themeStyle", live.StateBool("hasStyle"),
		"cdpStatus", cdpStatus,
		"remediation", remediationFor(targetDir, live, cdpStatus),
		"safeMode", safeMode,
		"channel", channel,
		"live", live,
	))
	if !ok {
		os.Exit(1)
	}
}

func runUpdate(args cliArgs) {
	targetDir := targetDirArg(args)
	u := updater.New(updater.Config{
		CurrentVersion: agytheme.Version,
		PluginDir:      pluginDir(),
		RootDir:        targetDir,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if args.has("--check") {
		res := u.Check(ctx)
		printJSON(os.Stdout, res)
		if res.Status == "error" {
			os.Exit(1)
		}
		return
	}
	if err := u.Apply(ctx); err != nil {
		printJSON(os.Stderr, u.Status())
		fatalf("[ERROR] %v", err)
	}
	printJSON(os.Stdout, u.Status())
}

func runRollback(args cliArgs) {
	targetDir := targetDirArg(args)
	plugin := pluginDir()
	bin := installer.PluginBinary(plugin)
	prev := installer.PluginPrevBinary(plugin)
	if !shared.FileExists(prev) {
		fatalf("[ERROR] No backup found at %s", prev)
	}
	tmp := bin + ".rollback"
	_ = os.Remove(tmp)
	if shared.FileExists(bin) {
		if err := os.Rename(bin, tmp); err != nil {
			fatalf("[ERROR] Cannot move %s: %v", bin, err)
		}
	}
	if err := os.Rename(prev, bin); err != nil {
		_ = os.Rename(tmp, bin)
		fatalf("[ERROR] Cannot restore %s: %v", prev, err)
	}
	if shared.FileExists(tmp) {
		_ = os.Rename(tmp, prev)
	}
	if _, err := installer.WriteSidecar(plugin, bin, targetDir); err != nil {
		fatalf("[ERROR] Cannot rewrite the sidecar manifest: %v", err)
	}
	runDaemonOnce(targetDir, nil, true)
	env := installer.DetectEnvironment("auto")
	live := installer.InspectLiveWindow(&env, "", 0)
	printJSON(os.Stdout, ordered("ok", true, "restoredFrom", prev, "restoredTo", bin, "live", live))
}

func waitForSidecarExit(timeout time.Duration) bool {
	pidFile := daemon.PidFile("")
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pid := shared.ReadPidFile(pidFile)
		if pid <= 0 || !shared.IsPidAlive(pid) {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

func removePluginDir(dir string) string {
	if !shared.FileExists(dir) {
		return "absent"
	}
	if agy := installer.FindAgy(); agy != "" {
		shared.RunCmd(20*time.Second, agy, "plugin", "uninstall", installer.PluginName)
	}
	if err := os.RemoveAll(dir); err == nil || !shared.FileExists(dir) {
		return "removed"
	}
	if runtime.GOOS == "windows" {
		script := "Start-Sleep -Seconds 3; Remove-Item -LiteralPath '" + strings.ReplaceAll(dir, "'", "''") + "' -Recurse -Force -ErrorAction SilentlyContinue"
		if _, err := shared.SpawnDetachedHidden("powershell", "-NoProfile", "-NonInteractive", "-Command", script); err == nil {
			return "scheduled"
		}
	}
	return "failed"
}

func runUninstall(args cliArgs) {
	targetDir := targetDirArg(args)
	plugin := pluginDir()
	cfgPath := filepath.Join(targetDir, "user", "config.json")

	_ = installer.RemoveSidecar(plugin)
	sidecarStopped := waitForSidecarExit(8 * time.Second)
	installer.StopRunningDaemonProcesses()
	installer.RemoveCdpLaunchers()
	_ = os.Remove(filepath.Join(targetDir, daemon.PendingActivationName))
	uiRestored := false
	if raw, err := os.ReadFile(cfgPath); err == nil {
		cfg := shared.NewOrderedMap()
		if json.Unmarshal([]byte(shared.StripUtf8Bom(string(raw))), cfg) == nil {
			cfg.Set("enabled", false)
			if os.WriteFile(cfgPath, shared.MarshalPretty(cfg, "  "), 0o644) == nil {
				runDaemonOnce(targetDir, nil, false)
				_ = os.WriteFile(cfgPath, raw, 0o644)
				uiRestored = true
			}
		}
	}
	pluginRemoved := removePluginDir(plugin)
	dataKept := any(filepath.Join(targetDir, "user"))
	if args.has("--purge") {
		if os.RemoveAll(targetDir) == nil {
			dataKept = nil
		}
	}
	printJSON(os.Stdout, ordered(
		"ok", pluginRemoved != "failed",
		"pluginDir", plugin,
		"pluginRemoved", pluginRemoved,
		"sidecarStopped", sidecarStopped,
		"uiRestored", uiRestored,
		"userConfigKept", dataKept,
	))
	if pluginRemoved == "failed" {
		fmt.Fprintf(os.Stderr, "[agytheme] 插件文件夹没能删除，请关闭 Antigravity 后手动删除：%s\n", plugin)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "[agytheme] 已卸载：Antigravity 恢复原样，AgyTheme 不会再运行。")
}
