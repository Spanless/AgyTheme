package shared

import (
	"encoding/json"
	"math"
	"testing"
)

func TestParsePosixEtimeSeconds(t *testing.T) {
	cases := map[string]float64{"09": 9, "01:25": 85, "02:03:04": 7384, "1-00:01:05": 86465}
	for in, want := range cases {
		if got := ParsePosixEtimeSeconds(in); got != want {
			t.Errorf("%q: got %v want %v", in, got, want)
		}
	}
	for _, bad := range []string{"", "bad"} {
		if got := ParsePosixEtimeSeconds(bad); !math.IsInf(got, 1) {
			t.Errorf("%q: got %v want +Inf", bad, got)
		}
	}
}

func TestNonHostCmdRe(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		{"/Applications/Antigravity.app/Contents/MacOS/Antigravity", false},
		{"/Applications/Antigravity.app/Contents/MacOS/Antigravity --remote-debugging-port=0", false},
		{"/Applications/Antigravity.app/Contents/Frameworks/Antigravity Helper.app/Contents/MacOS/Antigravity Helper --import=file:///Users/demo/.gemini/antigravity/lib/node_modules/sidecar_sdk/register_loader.js main.mjs", true},
		{`Antigravity.exe --type=renderer --user-data-dir=C:\Users\demo`, true},
		{"Antigravity.exe --ms-enable-electron-run-as-node main.js", true},
	}
	for _, c := range cases {
		if got := NonHostCmdRe.MatchString(c.cmd); got != c.want {
			t.Errorf("%q: got %v want %v", c.cmd, got, c.want)
		}
	}
}

func TestStripUtf8Bom(t *testing.T) {
	var v struct {
		Enabled bool   `json:"enabled"`
		Preset  string `json:"preset"`
	}
	if err := json.Unmarshal([]byte(StripUtf8Bom("\uFEFF{\"enabled\":true,\"preset\":\"light\"}")), &v); err != nil {
		t.Fatal(err)
	}
	if !v.Enabled || v.Preset != "light" {
		t.Fatalf("unexpected %+v", v)
	}
	if StripUtf8Bom(`{"ok":1}`) != `{"ok":1}` {
		t.Fatal("altered plain string")
	}
}

func TestOrderedMapRoundTrip(t *testing.T) {
	src := `{"z":1,"a":{"y":"<b>&","b":[1,{"k":true,"c":null}]},"m":2.5}`
	m := NewOrderedMap()
	if err := json.Unmarshal([]byte(src), m); err != nil {
		t.Fatal(err)
	}
	out, err := MarshalNoEscape(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != src {
		t.Fatalf("round trip mismatch:\n%s\n%s", out, src)
	}
	m.Set("a2", "x")
	m.Delete("z")
	m.Set("m", 3.0)
	if got := m.Keys(); len(got) != 3 || got[0] != "a" || got[1] != "m" || got[2] != "a2" {
		t.Fatalf("keys %v", got)
	}
	pretty := string(MarshalPretty(map[string]any{"a": []any{}, "b": map[string]any{}}, "  "))
	if pretty != "{\n  \"a\": [],\n  \"b\": {}\n}\n" {
		t.Fatalf("pretty %q", pretty)
	}
}

func TestInspectJsoncRootObject(t *testing.T) {
	raw := "// comment\n{\n  /* \"remote-debugging-port\": 1 */\n  \"enable-crash-reporter\": true\n}\n"
	info := InspectJsoncRootObject(raw, "remote-debugging-port")
	if info.HasRemoteDebuggingPort || !info.HasTopProps || info.OpenBraceIdx < 0 || info.CloseBraceIdx <= info.OpenBraceIdx {
		t.Fatalf("unexpected %+v", info)
	}
	info2 := InspectJsoncRootObject("{\"remote-debugging-port\": 9222}", "remote-debugging-port")
	if !info2.HasRemoteDebuggingPort {
		t.Fatalf("expected property detected: %+v", info2)
	}
}

func TestIsAllowedOriginEnforcesHostBoundaries(t *testing.T) {
	if !IsAllowedOrigin("http://127.0.0.1:51234/index.html", nil) {
		t.Error("expected loopback ipv4 origin allowed")
	}
	if !IsAllowedOrigin("https://localhost:8443/c/abc", nil) {
		t.Error("expected localhost origin allowed")
	}
	if !IsAllowedOrigin("vscode-file://vscode-app/index.html", nil) {
		t.Error("expected vscode-file origin allowed")
	}
	if !IsAllowedOrigin("plugin://swarm--abc/index.html", nil) {
		t.Error("expected swarm plugin origin allowed")
	}
	if IsAllowedOrigin("https://localhost.evil.example/", nil) {
		t.Error("prefix bypass localhost.evil.example must be rejected")
	}
	if IsAllowedOrigin("http://127.0.0.1.nip.io/", nil) {
		t.Error("prefix bypass 127.0.0.1.nip.io must be rejected")
	}
	if IsAllowedOrigin("https://localhost@evil.example/", nil) {
		t.Error("userinfo bypass localhost@evil.example must be rejected")
	}
	if IsAllowedOrigin("vscode-file-evil://x", nil) {
		t.Error("scheme prefix bypass vscode-file-evil must be rejected")
	}
	if IsAllowedOrigin("http://127.0.0.1:9000/app", []string{"http://127.0.0.1:8080"}) {
		t.Error("mismatched explicit port must be rejected")
	}
	if !IsAllowedOrigin("http://127.0.0.1:8080/app", []string{"http://127.0.0.1:8080"}) {
		t.Error("matching explicit port must be allowed")
	}
}

func TestIsLoopbackHost(t *testing.T) {
	if !IsLoopbackHost("127.0.0.1") || !IsLoopbackHost("localhost") || !IsLoopbackHost("::1") || !IsLoopbackHost("[::1]") {
		t.Error("expected loopback hosts accepted")
	}
	if IsLoopbackHost("localhost.evil.example") || IsLoopbackHost("192.168.1.1") || IsLoopbackHost("example.com") || IsLoopbackHost("") {
		t.Error("expected non-loopback hosts rejected")
	}
}
