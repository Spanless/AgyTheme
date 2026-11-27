package installer

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Spanless/AgyTheme/internal/cdp"
)

func TestResolveCdpStatus(t *testing.T) {
	noPort := map[string]*ChannelInfo{"stable": {PortFileExists: false}}
	withPort := map[string]*ChannelInfo{"stable": {PortFileExists: true}}
	cases := []struct {
		name    string
		running []cdp.HostProc
		ch      map[string]*ChannelInfo
		pf      bool
		want    string
	}{
		{"missing flag", []cdp.HostProc{{Pid: 43177, Channel: "stable"}}, noPort, false, "missing_opt_in_flag"},
		{"flagged", []cdp.HostProc{{Pid: 43177, Channel: "stable", HasCdpFlag: true}}, noPort, false, "ready"},
		{"argv port file", []cdp.HostProc{{Pid: 43177, Channel: "stable"}}, withPort, true, "ready"},
		{"stale", nil, withPort, true, "stale_port_file"},
		{"not running", nil, noPort, false, "app_not_running"},
	}
	for _, c := range cases {
		if got := ResolveCdpStatus(c.running, c.ch, c.pf); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestEnsureArgvRemoteDebugging(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME override not honoured by os.UserHomeDir on windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	stable := filepath.Join(home, ".antigravity")
	insiders := filepath.Join(home, ".antigravity-insiders")
	os.MkdirAll(stable, 0o755)
	os.MkdirAll(insiders, 0o755)
	orig := "// VS Code style argv\n{\n\t// comment\n\t\"enable-crash-reporter\": true\n}\n"
	os.WriteFile(filepath.Join(stable, "argv.json"), []byte(orig), 0o644)

	res := EnsureArgvRemoteDebugging("auto")
	if len(res) != 2 || res[0].Action != "patched" || res[0].Port != 9222 || res[1].Action != "created" || res[1].Port != 9223 {
		t.Fatalf("results %+v", res)
	}
	patched, _ := os.ReadFile(filepath.Join(stable, "argv.json"))
	want := "// VS Code style argv\n{\n\t\"remote-debugging-port\": 9222,\n\t// comment\n\t\"enable-crash-reporter\": true\n}\n"
	if string(patched) != want {
		t.Fatalf("patched:\n%q\nwant:\n%q", patched, want)
	}
	created, _ := os.ReadFile(filepath.Join(insiders, "argv.json"))
	if string(created) != "{\n  \"remote-debugging-port\": 9223\n}\n" {
		t.Fatalf("created %q", created)
	}
	again := EnsureArgvRemoteDebugging("auto")
	if again[0].Action != "already-present" || again[1].Action != "already-present" {
		t.Fatalf("idempotent %+v", again)
	}

	os.WriteFile(filepath.Join(stable, "argv.json"), []byte("{}"), 0o644)
	EnsureArgvRemoteDebugging("stable")
	empty, _ := os.ReadFile(filepath.Join(stable, "argv.json"))
	if string(empty) != "{\n\t\"remote-debugging-port\": 9222\n}" {
		t.Fatalf("empty object patch %q", empty)
	}
}
