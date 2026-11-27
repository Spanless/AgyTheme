package cdp

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Spanless/AgyTheme/internal/bundle"
	"github.com/Spanless/AgyTheme/internal/shared"
)

var RelaunchGuardFile = filepath.Join(shared.RuntimeDir(), "agytheme-cdp-relaunch-guard.json")

type HostProc struct {
	Pid            int     `json:"pid"`
	Channel        string  `json:"channel"`
	ElapsedSec     float64 `json:"elapsedSec"`
	AppPath        string  `json:"appPath"`
	ExePath        string  `json:"exePath"`
	Cmd            string  `json:"cmd,omitempty"`
	HasCdpFlag     bool    `json:"hasCdpFlag"`
	HasUpdatedFlag bool    `json:"hasUpdatedFlag"`
}

type RelaunchGuard struct {
	LastRelaunchAtMs   float64   `json:"lastRelaunchAtMs"`
	LastChannel        string    `json:"lastChannel,omitempty"`
	LastOldPid         int       `json:"lastOldPid,omitempty"`
	Reason             string    `json:"reason,omitempty"`
	HandledPids        []int     `json:"handledPids"`
	RecentRelaunchesMs []float64 `json:"recentRelaunchesMs,omitempty"`
}

const (
	RelaunchWindowMs = 60000.0
	// Cap automatic relaunches per window so an unflagged restart loop halts after two attempts.
	MaxRelaunchesPerWindow = 2
)

func recentRelaunches(guard RelaunchGuard, nowMs float64) int {
	times := guard.RecentRelaunchesMs
	if len(times) == 0 && guard.LastRelaunchAtMs > 0 {
		times = []float64{guard.LastRelaunchAtMs}
	}
	n := 0
	for _, t := range times {
		if t > 0 && nowMs-t < RelaunchWindowMs {
			n++
		}
	}
	return n
}

type Decision struct {
	Action       string  `json:"action"`
	Reason       string  `json:"reason"`
	MaxWindowSec float64 `json:"maxWindowSec,omitempty"`
	DeltaMs      float64 `json:"deltaMs,omitempty"`
}

func EvaluateColdStartDecision(proc *HostProc, hasActivePort, hasActiveWal bool, guard RelaunchGuard, nowMs float64, isIgnored bool) Decision {
	return evaluateColdStart(proc, hasActivePort, hasActiveWal, guard, nowMs, isIgnored, 0)
}

func evaluateColdStart(proc *HostProc, hasActivePort, hasActiveWal bool, guard RelaunchGuard, nowMs float64, isIgnored bool, minWindowSec float64) Decision {
	if proc == nil || proc.HasCdpFlag || isIgnored || hasActivePort {
		return Decision{Action: "skip", Reason: "already-ready-or-ignored"}
	}
	maxWindow := 12.0
	if proc.HasUpdatedFlag {
		maxWindow = 25
	}
	if minWindowSec > maxWindow {
		maxWindow = minWindowSec
	}
	if math.IsNaN(proc.ElapsedSec) || math.IsInf(proc.ElapsedSec, 0) || proc.ElapsedSec > maxWindow {
		return Decision{Action: "exempt", Reason: "uptime-exceeded", MaxWindowSec: maxWindow}
	}
	if hasActiveWal {
		return Decision{Action: "exempt", Reason: "active-wal-write"}
	}
	handled := false
	for _, p := range guard.HandledPids {
		if p != 0 && p == proc.Pid {
			handled = true
		}
	}
	if handled || recentRelaunches(guard, nowMs) >= MaxRelaunchesPerWindow {
		return Decision{Action: "exempt", Reason: "cooldown-active", DeltaMs: nowMs - guard.LastRelaunchAtMs}
	}
	reason := "cold-start-unflagged"
	if proc.HasUpdatedFlag {
		reason = "cold-start-post-update"
	}
	return Decision{Action: "relaunch", Reason: reason}
}

func ReadRelaunchGuard() RelaunchGuard {
	var raw map[string]any
	g := RelaunchGuard{}
	if !shared.ReadJSONFile(RelaunchGuardFile, &raw) {
		return g
	}
	g.LastRelaunchAtMs = bundle.ToNumber(raw["lastRelaunchAtMs"])
	if math.IsNaN(g.LastRelaunchAtMs) {
		g.LastRelaunchAtMs = 0
	}
	if arr, ok := raw["handledPids"].([]any); ok {
		for _, v := range arr {
			if n := bundle.ToNumber(v); n != 0 && !math.IsNaN(n) {
				g.HandledPids = append(g.HandledPids, int(n))
			}
		}
	}
	if arr, ok := raw["recentRelaunchesMs"].([]any); ok {
		for _, v := range arr {
			if n := bundle.ToNumber(v); n > 0 && !math.IsNaN(n) {
				g.RecentRelaunchesMs = append(g.RecentRelaunchesMs, n)
			}
		}
	}
	return g
}

var (
	cdpFlagRe     = regexp.MustCompile(`--remote-debugging-port(?:=|\s|$)`)
	cdpFlagReI    = regexp.MustCompile(`(?i)--remote-debugging-port(?:=|\s|$)`)
	updatedFlagRe = regexp.MustCompile(`--updated(?:\s|$)`)
	updatedFlagRI = regexp.MustCompile(`(?i)--updated(?:\s|$)`)
	psLineRe      = regexp.MustCompile(`^(\d+)\s+(\S+)\s+(.+)$`)
	macHelperRe   = regexp.MustCompile(`(?i)Helper(?:\s|\.|/)`)
	macAppRe      = regexp.MustCompile(`^(\S.*?/Antigravity(?:\s-\sInsiders)?\.app)/Contents/MacOS/(Antigravity(?:\s-\sInsiders)?)(?:\s|$)`)
	linuxBinRe    = regexp.MustCompile(`(?i)^(/\S*/antigravity(?:-insiders)?)(?:\s|$)`)
	linuxSelfRe   = regexp.MustCompile(`(?i)agytheme`)
	insidersRe    = regexp.MustCompile(`(?i)insiders`)
	tasklistRowRe = regexp.MustCompile(`^"([^"]+)","(\d+)"`)
)

const WinHostProbeScript = `[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$ErrorActionPreference = 'SilentlyContinue'
$now = [DateTime]::UtcNow
$procs = Get-CimInstance Win32_Process -Filter "Name = 'Antigravity.exe' OR Name = 'Antigravity - Insiders.exe'"
$hostOut = @(); $nonHostPids = @()
foreach ($p in $procs) {
  $pidVal = [int]$p.ProcessId
  $cmd = [string]$p.CommandLine
  if ($cmd -match '--type=|--crashpad|--utility-sub-type=|--import=|sidecar_sdk|--ms-enable-electron-run-as-node') { $nonHostPids += $pidVal; continue }
  $age = if ($p.CreationDate) { [Math]::Max(0, [int][Math]::Round(($now - ([DateTime]$p.CreationDate).ToUniversalTime()).TotalSeconds)) } else { 999999 }
  $hostOut += @{ pid = $pidVal; name = [string]$p.Name; exe = [string]$p.ExecutablePath; cmd = $cmd; elapsedSec = $age }
}
@{ hosts = $hostOut; nonHostPids = $nonHostPids } | ConvertTo-Json -Compress -Depth 4`

func RunPowerShell(timeout time.Duration, script string) shared.CmdResult {
	return shared.RunCmd(timeout, "powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
}

func asList(raw json.RawMessage) []json.RawMessage {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if raw[0] == '[' {
		var arr []json.RawMessage
		_ = json.Unmarshal(raw, &arr)
		return arr
	}
	return []json.RawMessage{raw}
}

func ParseWinHostProbe(stdout string) (hosts []HostProc, nonHostPids []int, ok bool) {
	var payload struct {
		Hosts       json.RawMessage `json:"hosts"`
		NonHostPids json.RawMessage `json:"nonHostPids"`
	}
	if json.Unmarshal([]byte(shared.StripUtf8Bom(strings.TrimSpace(stdout))), &payload) != nil {
		return nil, nil, false
	}
	for _, r := range asList(payload.NonHostPids) {
		var n float64
		if json.Unmarshal(r, &n) == nil && n > 0 {
			nonHostPids = append(nonHostPids, int(n))
		}
	}
	for _, r := range asList(payload.Hosts) {
		var item struct {
			Pid        any    `json:"pid"`
			Name       string `json:"name"`
			Exe        string `json:"exe"`
			Cmd        string `json:"cmd"`
			ElapsedSec any    `json:"elapsedSec"`
		}
		if json.Unmarshal(r, &item) != nil {
			continue
		}
		ch := "stable"
		nameOrExe := item.Name
		if nameOrExe == "" {
			nameOrExe = item.Exe
		}
		if insidersRe.MatchString(nameOrExe) {
			ch = "insiders"
		}
		el := bundle.ToNumber(item.ElapsedSec)
		if item.ElapsedSec == nil || math.IsNaN(el) {
			el = math.Inf(1)
		}
		hosts = append(hosts, HostProc{
			Pid:            int(bundle.ToNumber(item.Pid)),
			Channel:        ch,
			ElapsedSec:     el,
			ExePath:        item.Exe,
			Cmd:            item.Cmd,
			HasCdpFlag:     cdpFlagReI.MatchString(item.Cmd),
			HasUpdatedFlag: updatedFlagRI.MatchString(item.Cmd),
		})
	}
	return hosts, nonHostPids, true
}

func ParsePosixHostProcs(goos, stdout string, allowStable, allowInsiders bool) []HostProc {
	var found []HostProc
	for _, line := range strings.Split(stdout, "\n") {
		m := psLineRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		pid, _ := strconv.Atoi(m[1])
		cmd := m[3]
		if pid == 0 || shared.NonHostCmdRe.MatchString(cmd) {
			continue
		}
		var hp HostProc
		if goos == "darwin" {
			if macHelperRe.MatchString(cmd) {
				continue
			}
			am := macAppRe.FindStringSubmatch(cmd)
			if am == nil {
				continue
			}
			hp.AppPath = am[1]
			hp.ExePath = am[1] + "/Contents/MacOS/" + am[2]
			hp.Channel = "stable"
			if insidersRe.MatchString(am[2]) {
				hp.Channel = "insiders"
			}
		} else {
			if linuxSelfRe.MatchString(cmd) {
				continue
			}
			bm := linuxBinRe.FindStringSubmatch(cmd)
			if bm == nil {
				continue
			}
			hp.ExePath = bm[1]
			hp.Channel = "stable"
			if insidersRe.MatchString(bm[1]) {
				hp.Channel = "insiders"
			}
		}
		if (hp.Channel == "stable" && !allowStable) || (hp.Channel == "insiders" && !allowInsiders) {
			continue
		}
		hp.Pid = pid
		hp.ElapsedSec = shared.ParsePosixEtimeSeconds(m[2])
		hp.Cmd = cmd
		hp.HasCdpFlag = cdpFlagRe.MatchString(cmd)
		hp.HasUpdatedFlag = updatedFlagRe.MatchString(cmd)
		found = append(found, hp)
	}
	return found
}

func HasActiveConversationWalWrite(conversationsDir string, window time.Duration) bool {
	if conversationsDir == "" {
		return false
	}
	entries, err := os.ReadDir(conversationsDir)
	if err != nil {
		return false
	}
	now := time.Now()
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".db-wal") {
			continue
		}
		fi, err := os.Stat(filepath.Join(conversationsDir, e.Name()))
		if err == nil && fi.Size() > 4096 && now.Sub(fi.ModTime()) < window {
			return true
		}
	}
	return false
}

type RelaunchController struct {
	mu             sync.Mutex
	ignored        map[int]bool
	exemptByChan   map[string]int
	knownNonHost   map[int]bool
	relaunching    bool
	SelfExecutable string
	// Quitting Antigravity terminates its sidecar, so sidecar relaunches must run in a detached helper.
	Spawn                 func(proc HostProc) error
	PendingFile           string
	MinColdStartWindowSec float64
}

// Post-install activation waits for conversation idle when relaunching mid-turn is unsafe.
type PendingActivation struct {
	CreatedAtMs float64 `json:"createdAtMs"`
	Reason      string  `json:"reason,omitempty"`
}

const (
	PendingActivationTTL = 6 * time.Hour
	PendingIdleWindow    = 30 * time.Second
	pendingMinAge        = 20 * time.Second
)

func WritePendingActivation(path, reason string) error {
	pa := PendingActivation{CreatedAtMs: float64(time.Now().UnixMilli()), Reason: reason}
	data, err := json.MarshalIndent(pa, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func ReadPendingActivation(path string, now time.Time) (PendingActivation, bool) {
	var pa PendingActivation
	if path == "" || !shared.ReadJSONFile(path, &pa) {
		return pa, false
	}
	if pa.CreatedAtMs <= 0 || now.Sub(time.UnixMilli(int64(pa.CreatedAtMs))) > PendingActivationTTL {
		_ = os.Remove(path)
		return pa, false
	}
	return pa, true
}

func (r *RelaunchController) ClearPending() {
	if r.PendingFile != "" && shared.FileExists(r.PendingFile) {
		_ = os.Remove(r.PendingFile)
	}
}

func RelaunchHost(proc HostProc) error {
	StopHostProcess(proc.Pid)
	if runtime.GOOS == "darwin" && proc.AppPath != "" {
		if res := shared.RunCmd(5*time.Second, "/usr/bin/open", "-a", proc.AppPath, "--args", "--remote-debugging-port=0"); res.Status != 0 {
			return fmt.Errorf("open -a %s exited with %d", proc.AppPath, res.Status)
		}
		return nil
	}
	if proc.ExePath == "" {
		return fmt.Errorf("no executable path for PID %d", proc.Pid)
	}
	_, err := shared.SpawnDetached(proc.ExePath, "--remote-debugging-port=0")
	return err
}

func NewRelaunchController() *RelaunchController {
	return &RelaunchController{ignored: map[int]bool{}, exemptByChan: map[string]int{}, knownNonHost: map[int]bool{}}
}

func (r *RelaunchController) prune() {
	for pid := range r.ignored {
		if !shared.IsPidAlive(pid) {
			delete(r.ignored, pid)
		}
	}
	for ch, pid := range r.exemptByChan {
		if !shared.IsPidAlive(pid) {
			delete(r.exemptByChan, ch)
		}
	}
	for pid := range r.knownNonHost {
		if !shared.IsPidAlive(pid) {
			delete(r.knownNonHost, pid)
		}
	}
}

func findEndpoint(eps []bundle.Endpoint, ch string) *bundle.Endpoint {
	for i := range eps {
		if eps[i].Channel == ch {
			return &eps[i]
		}
	}
	return nil
}

func (r *RelaunchController) probe(channelMode string, eps []bundle.Endpoint) []HostProc {
	allowStable := channelMode != "insiders"
	allowInsiders := channelMode != "stable"
	switch runtime.GOOS {
	case "darwin", "linux":
		res := shared.RunCmd(2500*time.Millisecond, "ps", "-axo", "pid=,etime=,command=")
		if res.Status != 0 || res.Stdout == "" {
			return nil
		}
		return ParsePosixHostProcs(runtime.GOOS, res.Stdout, allowStable, allowInsiders)
	case "windows":
		type spec struct{ ch, img string }
		var specs []spec
		stableEp := findEndpoint(eps, "stable")
		insEp := findEndpoint(eps, "insiders")
		sPid, iPid := r.exemptByChan["stable"], r.exemptByChan["insiders"]
		if allowStable && !(stableEp != nil && shared.ReadPortFromFile(stableEp.PortFile) > 0) && !(sPid != 0 && shared.IsPidAlive(sPid)) {
			specs = append(specs, spec{"stable", "Antigravity.exe"})
		}
		if allowInsiders && insEp != nil && insEp.ChannelInstalled && shared.ReadPortFromFile(insEp.PortFile) == 0 && !(iPid != 0 && shared.IsPidAlive(iPid)) {
			specs = append(specs, spec{"insiders", "Antigravity - Insiders.exe"})
		}
		if len(specs) == 0 {
			return nil
		}
		candidates := 0
		for _, s := range specs {
			tl := shared.RunCmd(2500*time.Millisecond, "tasklist", "/FI", "IMAGENAME eq "+s.img, "/FO", "CSV", "/NH")
			if tl.Status != 0 {
				continue
			}
			for _, row := range strings.Split(tl.Stdout, "\n") {
				cm := tasklistRowRe.FindStringSubmatch(strings.TrimSpace(row))
				if cm != nil && strings.EqualFold(cm[1], s.img) {
					p, _ := strconv.Atoi(cm[2])
					if p > 0 && !r.ignored[p] && !r.knownNonHost[p] {
						candidates++
					}
				}
			}
		}
		if candidates == 0 {
			return nil
		}
		cim := RunPowerShell(5*time.Second, WinHostProbeScript)
		if cim.Status != 0 || strings.TrimSpace(cim.Stdout) == "" {
			return nil
		}
		hosts, nonHost, ok := ParseWinHostProbe(cim.Stdout)
		if !ok {
			return nil
		}
		for _, p := range nonHost {
			r.knownNonHost[p] = true
		}
		var out []HostProc
		for _, h := range hosts {
			if h.HasCdpFlag && h.Pid > 0 {
				r.knownNonHost[h.Pid] = true
			}
			if h.Pid > 0 && h.ExePath != "" && ((h.Channel == "stable" && allowStable) || (h.Channel == "insiders" && allowInsiders)) {
				out = append(out, h)
			}
		}
		return out
	}
	return nil
}

func StopHostProcess(pid int) {
	if runtime.GOOS == "windows" {
		ps := fmt.Sprintf("$gp = Get-Process -Id %d -ErrorAction SilentlyContinue; if ($gp) { [void]$gp.CloseMainWindow(); if (-not $gp.WaitForExit(1800)) { Stop-Process -Id %d -Force -ErrorAction SilentlyContinue } }", pid, pid)
		RunPowerShell(4500*time.Millisecond, ps)
	} else {
		_ = shared.TerminatePid(pid)
	}
	for i := 0; i < 55 && shared.IsPidAlive(pid); i++ {
		shared.SleepMs(80)
	}
	if shared.IsPidAlive(pid) && runtime.GOOS != "windows" {
		_ = shared.KillPid(pid)
		for i := 0; i < 15 && shared.IsPidAlive(pid); i++ {
			shared.SleepMs(60)
		}
	}
	shared.SleepMs(180)
}

func (r *RelaunchController) MaybeAutoRelaunch(cfg bundle.ActiveConfig, safeMode, runOnce bool) bool {
	if safeMode || runOnce || !cfg.Enabled {
		return false
	}
	if v, ok := cfg.Modules["cdpAutoRelaunch"]; ok && v == false {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.relaunching {
		return false
	}
	r.prune()
	procs := r.probe(cfg.Channel, cfg.Endpoints)
	for i := range procs {
		proc := procs[i]
		ep := findEndpoint(cfg.Endpoints, proc.Channel)
		if ep == nil && len(cfg.Endpoints) > 0 {
			ep = &cfg.Endpoints[0]
		}
		hasPort := ep != nil && shared.ReadPortFromFile(ep.PortFile) > 0
		convDir := ""
		if ep != nil {
			convDir = ep.ConversationsDir
		}
		guard := ReadRelaunchGuard()
		now := time.Now()

		if !hasPort && !proc.HasCdpFlag {
			if pa, ok := ReadPendingActivation(r.PendingFile, now); ok {
				if now.Sub(time.UnixMilli(int64(pa.CreatedAtMs))) < pendingMinAge || HasActiveConversationWalWrite(convDir, PendingIdleWindow) {
					continue
				}
				_ = os.Remove(r.PendingFile)
				if r.relaunchProc(proc, "post-install-activation", guard, now) {
					return true
				}
				continue
			}
		}

		hasWal := false
		if !hasPort {
			hasWal = HasActiveConversationWalWrite(convDir, 15*time.Second)
		}
		d := evaluateColdStart(&proc, hasPort, hasWal, guard, float64(now.UnixMilli()), r.ignored[proc.Pid], r.MinColdStartWindowSec)
		if d.Action == "skip" {
			continue
		}
		if d.Action == "exempt" {
			r.ignored[proc.Pid] = true
			r.exemptByChan[proc.Channel] = proc.Pid
			switch d.Reason {
			case "uptime-exceeded":
				hint := "agytheme"
				if r.SelfExecutable != "" {
					hint = r.SelfExecutable
				}
				fmt.Printf("[Daemon] Detected running Antigravity (%s, PID %d, uptime %ss) without --remote-debugging-port; skipping auto-relaunch to protect active session. Run `%s relaunch` when idle.\n",
					proc.Channel, proc.Pid, shared.FormatJSNumber(proc.ElapsedSec), hint)
			case "active-wal-write":
				fmt.Printf("[Daemon] Skipped cold-start auto-relaunch for PID %d: active conversation WAL write within 15s.\n", proc.Pid)
			case "cooldown-active":
				fmt.Printf("[Daemon] Skipped cold-start auto-relaunch for PID %d: %d relaunches in the last %ds.\n", proc.Pid, MaxRelaunchesPerWindow, int(RelaunchWindowMs/1000))
			}
			continue
		}
		if r.relaunchProc(proc, d.Reason, guard, now) {
			return true
		}
	}
	return false
}

func (r *RelaunchController) relaunchProc(proc HostProc, reason string, guard RelaunchGuard, now time.Time) bool {
	r.relaunching = true
	defer func() { r.relaunching = false }()
	r.ignored[proc.Pid] = true
	nowMs := float64(now.UnixMilli())
	handled := guard.HandledPids
	if len(handled) > 15 {
		handled = handled[len(handled)-15:]
	}
	next := RelaunchGuard{
		LastRelaunchAtMs: nowMs,
		LastChannel:      proc.Channel,
		LastOldPid:       proc.Pid,
		Reason:           reason,
	}
	seen := map[int]bool{}
	for _, p := range append(append([]int{}, handled...), proc.Pid) {
		if !seen[p] {
			seen[p] = true
			next.HandledPids = append(next.HandledPids, p)
		}
	}
	for _, t := range guard.RecentRelaunchesMs {
		if nowMs-t < RelaunchWindowMs {
			next.RecentRelaunchesMs = append(next.RecentRelaunchesMs, t)
		}
	}
	next.RecentRelaunchesMs = append(next.RecentRelaunchesMs, nowMs)
	if data, err := json.MarshalIndent(next, "", "  "); err == nil {
		_ = os.MkdirAll(filepath.Dir(RelaunchGuardFile), 0o700)
		_ = os.WriteFile(RelaunchGuardFile, append(data, '\n'), 0o600)
	}
	var err error
	if r.Spawn != nil {
		err = r.Spawn(proc)
	} else {
		err = RelaunchHost(proc)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "[Daemon] Auto-relaunch error for PID %d (%s): %v\n", proc.Pid, reason, err)
		return false
	}
	fmt.Printf("[Daemon] Auto-relaunched Antigravity (%s, old PID %d, age %ss, %s) with --remote-debugging-port=0.\n",
		proc.Channel, proc.Pid, shared.FormatJSNumber(proc.ElapsedSec), reason)
	return true
}
