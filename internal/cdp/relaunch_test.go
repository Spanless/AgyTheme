package cdp

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Spanless/AgyTheme/internal/bundle"
)

func TestEvaluateColdStartDecision(t *testing.T) {
	now := 1_700_000_100_000.0
	d := func(p HostProc, port, wal bool, g RelaunchGuard) Decision {
		return EvaluateColdStartDecision(&p, port, wal, g, now, false)
	}
	if got := d(HostProc{Pid: 100, ElapsedSec: 3, HasCdpFlag: true}, false, false, RelaunchGuard{}); got.Action != "skip" {
		t.Errorf("cdp flag: %+v", got)
	}
	if got := d(HostProc{Pid: 101, ElapsedSec: 5}, true, false, RelaunchGuard{}); got.Action != "skip" {
		t.Errorf("active port: %+v", got)
	}
	if got := d(HostProc{Pid: 102, ElapsedSec: 6}, false, false, RelaunchGuard{}); got != (Decision{Action: "relaunch", Reason: "cold-start-unflagged"}) {
		t.Errorf("cold start: %+v", got)
	}
	if got := d(HostProc{Pid: 103, ElapsedSec: 18}, false, false, RelaunchGuard{}); got.Action != "exempt" {
		t.Errorf("uptime: %+v", got)
	}
	if got := d(HostProc{Pid: 104, ElapsedSec: 18, HasUpdatedFlag: true}, false, false, RelaunchGuard{}); got != (Decision{Action: "relaunch", Reason: "cold-start-post-update"}) {
		t.Errorf("post update: %+v", got)
	}
	if got := d(HostProc{Pid: 105, ElapsedSec: 4}, false, true, RelaunchGuard{}); got != (Decision{Action: "defer", Reason: "active-wal-write"}) {
		t.Errorf("wal: %+v", got)
	}
	if got := d(HostProc{Pid: 105, ElapsedSec: 4}, false, true, RelaunchGuard{HandledPids: []int{105}}); got.Action != "exempt" || got.Reason != "cooldown-active" {
		t.Errorf("wal with cooldown: %+v", got)
	}
	if got := d(HostProc{Pid: 106, ElapsedSec: 4}, false, false, RelaunchGuard{LastRelaunchAtMs: now - 15000}); got.Action != "relaunch" {
		t.Errorf("single recent relaunch: %+v", got)
	}
	if got := d(HostProc{Pid: 106, ElapsedSec: 4}, false, false, RelaunchGuard{RecentRelaunchesMs: []float64{now - 40000, now - 15000}}); got.Reason != "cooldown-active" {
		t.Errorf("two relaunches within the window: %+v", got)
	}
	if got := d(HostProc{Pid: 106, ElapsedSec: 4}, false, false, RelaunchGuard{RecentRelaunchesMs: []float64{now - 90000, now - 15000}}); got.Action != "relaunch" {
		t.Errorf("an old relaunch outside the window: %+v", got)
	}
	if got := d(HostProc{Pid: 107, ElapsedSec: 4}, false, false, RelaunchGuard{HandledPids: []int{107}}); got.Reason != "cooldown-active" {
		t.Errorf("handled pid: %+v", got)
	}
	if got := EvaluateColdStartDecision(nil, false, false, RelaunchGuard{}, now, false); got.Action != "skip" {
		t.Errorf("nil proc: %+v", got)
	}
	if got := d(HostProc{Pid: 108, ElapsedSec: 4}, false, false, RelaunchGuard{}); got.Action != "relaunch" {
		t.Errorf("baseline: %+v", got)
	}
	p := HostProc{Pid: 109, ElapsedSec: 4}
	if got := EvaluateColdStartDecision(&p, false, false, RelaunchGuard{}, now, true); got.Action != "skip" {
		t.Errorf("ignored: %+v", got)
	}
}

func TestParsePosixHostProcsDarwin(t *testing.T) {
	ps := `  501   00:07 /Applications/Antigravity.app/Contents/MacOS/Antigravity
  502   01:02:03 /Applications/Antigravity - Insiders.app/Contents/MacOS/Antigravity - Insiders --remote-debugging-port=0 --updated
  503   00:05 /Applications/Antigravity.app/Contents/Frameworks/Antigravity Helper (Renderer).app/Contents/MacOS/Antigravity Helper (Renderer) --type=renderer
  504   00:05 /usr/bin/vim
`
	got := ParsePosixHostProcs("darwin", ps, true, true)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Pid != 501 || got[0].Channel != "stable" || got[0].ElapsedSec != 7 || got[0].HasCdpFlag ||
		got[0].AppPath != "/Applications/Antigravity.app" || got[0].ExePath != "/Applications/Antigravity.app/Contents/MacOS/Antigravity" {
		t.Errorf("stable: %+v", got[0])
	}
	if got[1].Pid != 502 || got[1].Channel != "insiders" || got[1].ElapsedSec != 3723 || !got[1].HasCdpFlag || !got[1].HasUpdatedFlag {
		t.Errorf("insiders: %+v", got[1])
	}
	if only := ParsePosixHostProcs("darwin", ps, true, false); len(only) != 1 || only[0].Pid != 501 {
		t.Errorf("stable only: %+v", only)
	}
}

func TestParsePosixHostProcsLinux(t *testing.T) {
	ps := `  10   00:03 /usr/share/antigravity/antigravity --no-sandbox
  11   00:03 /usr/share/antigravity/antigravity --type=zygote
  12   00:03 /home/u/.gemini/antigravity/agytheme/bin/agytheme daemon
  13   00:03 /opt/antigravity-insiders/antigravity-insiders
`
	got := ParsePosixHostProcs("linux", ps, true, true)
	if len(got) != 2 || got[0].Pid != 10 || got[0].Channel != "stable" || got[1].Pid != 13 || got[1].Channel != "insiders" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseCascadeIDList(t *testing.T) {
	a := "0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0"
	b := "11111111-2222-3333-4444-555555555555"
	if got := ParseCascadeIDList(a + ", bad " + b + " " + a); len(got) != 2 || got[0] != a || got[1] != b {
		t.Errorf("csv: %v", got)
	}
	if got := ParseCascadeIDList(`["` + b + `","x"]`); len(got) != 1 || got[0] != b {
		t.Errorf("json str: %v", got)
	}
	if got := ParseCascadeIDList([]any{a, 3, nil}); len(got) != 1 {
		t.Errorf("array: %v", got)
	}
	var many []any
	for i := 0; i < 9; i++ {
		many = append(many, "0000000"+string(rune('0'+i))+"-0000-0000-0000-000000000000")
	}
	if got := ParseCascadeIDList(many); len(got) != 6 {
		t.Errorf("cap: %d", len(got))
	}
	if got := ParseCascadeIDList(nil); got == nil || len(got) != 0 {
		t.Errorf("nil: %v", got)
	}
}

func TestTargetOriginKey(t *testing.T) {
	if got := TargetOriginKey("vscode-file://vscode-app/Applications/x/workbench.html"); got != "vscode-file://vscode-app" {
		t.Errorf("got %q", got)
	}
	if got := TargetOriginKey("http://127.0.0.1:9222/a?b"); got != "http://127.0.0.1:9222" {
		t.Errorf("got %q", got)
	}
}

func TestPendingActivation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pending-activation.json")
	now := time.Now()
	if _, ok := ReadPendingActivation(path, now); ok {
		t.Fatal("missing marker must not read as pending")
	}
	if err := WritePendingActivation(path, "installer_inside_antigravity"); err != nil {
		t.Fatal(err)
	}
	pa, ok := ReadPendingActivation(path, now)
	if !ok || pa.Reason != "installer_inside_antigravity" {
		t.Fatalf("fresh marker: %+v %v", pa, ok)
	}
	if _, ok := ReadPendingActivation(path, now.Add(PendingActivationTTL+time.Minute)); ok {
		t.Fatal("expired marker must not read as pending")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expired marker must be deleted")
	}
}

func TestSidecarColdStartWindow(t *testing.T) {
	proc := &HostProc{Pid: 7, ElapsedSec: 30}
	if d := EvaluateColdStartDecision(proc, false, false, RelaunchGuard{}, 1e12, false); d.Action != "exempt" {
		t.Fatalf("daemon window: got %+v, want exempt", d)
	}
	if d := evaluateColdStart(proc, false, false, RelaunchGuard{}, 1e12, false, 40); d.Action != "relaunch" {
		t.Fatalf("sidecar window: got %+v, want relaunch", d)
	}
	proc.ElapsedSec = 90
	if d := evaluateColdStart(proc, false, false, RelaunchGuard{}, 1e12, false, 40); d.Action != "exempt" {
		t.Fatalf("past sidecar window: got %+v, want exempt", d)
	}
}

func TestColdStartActiveWalDefersAndRelaunchesWhenIdle(t *testing.T) {
	tmp := t.TempDir()
	origGuard := RelaunchGuardFile
	RelaunchGuardFile = filepath.Join(tmp, "guard.json")
	t.Cleanup(func() { RelaunchGuardFile = origGuard })

	convDir := filepath.Join(tmp, "conversations")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	walPath := filepath.Join(convDir, "turn.db-wal")
	if err := os.WriteFile(walPath, make([]byte, 8192), 0o644); err != nil {
		t.Fatal(err)
	}

	pendingPath := filepath.Join(tmp, "pending-activation.json")
	spawned := 0
	rc := NewRelaunchController()
	rc.PendingFile = pendingPath
	rc.MinColdStartWindowSec = 40
	rc.Probe = func(string, []bundle.Endpoint) []HostProc {
		return []HostProc{{Pid: 7323, Channel: "stable", ElapsedSec: 5, AppPath: "/Applications/Antigravity.app"}}
	}
	rc.Spawn = func(HostProc) error {
		spawned++
		return nil
	}
	cfg := bundle.ActiveConfig{
		Enabled: true,
		Channel: "stable",
		Endpoints: []bundle.Endpoint{{
			Channel:          "stable",
			PortFile:         filepath.Join(tmp, "DevToolsActivePort"),
			ConversationsDir: convDir,
		}},
	}

	// Active WAL during cold start defers instead of permanently exempting the PID.
	if rc.MaybeAutoRelaunch(cfg, false, false) {
		t.Fatal("must not relaunch while WAL is active")
	}
	if spawned != 0 {
		t.Fatalf("spawned=%d while WAL active, want 0", spawned)
	}
	if !rc.HasPending() {
		t.Fatal("expected pending activation marker after active-wal deferral")
	}

	// Still active: remains deferred without relaunching.
	if rc.MaybeAutoRelaunch(cfg, false, false) {
		t.Fatal("must stay deferred while WAL is still fresh")
	}
	if spawned != 0 {
		t.Fatalf("spawned=%d while still busy, want 0", spawned)
	}

	// Age the WAL past the idle window: deferred cold-start now relaunches the host.
	stale := time.Now().Add(-(PendingIdleWindow + 5*time.Second))
	if err := os.Chtimes(walPath, stale, stale); err != nil {
		t.Fatal(err)
	}
	if !rc.MaybeAutoRelaunch(cfg, false, false) {
		t.Fatal("expected deferred relaunch to fire once WAL became idle")
	}
	if spawned != 1 {
		t.Fatalf("spawned=%d after idle, want 1", spawned)
	}
	if rc.HasPending() {
		t.Fatal("pending marker must be cleared after relaunch")
	}
}

func TestRelaunchNowChecksBusyAndHonorsForce(t *testing.T) {
	tmp := t.TempDir()
	origGuard := RelaunchGuardFile
	RelaunchGuardFile = filepath.Join(tmp, "guard.json")
	t.Cleanup(func() { RelaunchGuardFile = origGuard })

	convDir := filepath.Join(tmp, "conversations")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	walPath := filepath.Join(convDir, "turn.db-wal")
	if err := os.WriteFile(walPath, make([]byte, 8192), 0o644); err != nil {
		t.Fatal(err)
	}

	spawned := 0
	rc := NewRelaunchController()
	rc.PendingFile = filepath.Join(tmp, "pending-activation.json")
	rc.Probe = func(string, []bundle.Endpoint) []HostProc {
		return []HostProc{{Pid: 8001, Channel: "stable", ElapsedSec: 120, AppPath: "/Applications/Antigravity.app"}}
	}
	rc.Spawn = func(HostProc) error {
		spawned++
		return nil
	}
	cfg := bundle.ActiveConfig{
		Enabled: true,
		Channel: "stable",
		Endpoints: []bundle.Endpoint{{
			Channel:          "stable",
			PortFile:         filepath.Join(tmp, "DevToolsActivePort"),
			ConversationsDir: convDir,
		}},
	}

	busy, err := rc.RelaunchNow(cfg, false)
	if !busy || err == nil {
		t.Fatalf("expected busy=true and error when WAL active, got busy=%v err=%v", busy, err)
	}
	if spawned != 0 {
		t.Fatalf("spawned=%d on busy refusal, want 0", spawned)
	}

	busy, err = rc.RelaunchNow(cfg, true)
	if busy || err != nil {
		t.Fatalf("expected forced relaunch to succeed, got busy=%v err=%v", busy, err)
	}
	if spawned != 1 {
		t.Fatalf("spawned=%d after force relaunch, want 1", spawned)
	}
}
