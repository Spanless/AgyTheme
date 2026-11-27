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
