package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrimLogFileIfOversized(t *testing.T) {
	p := filepath.Join(t.TempDir(), "d.log")
	var sb strings.Builder
	for i := 0; sb.Len() < 4000; i++ {
		sb.WriteString("line-")
		sb.WriteString(strings.Repeat("x", i%7))
		sb.WriteString("\n")
	}
	sb.WriteString("LAST\n")
	os.WriteFile(p, []byte(sb.String()), 0o644)
	if TrimLogFileIfOversized(p, 10_000, 100) {
		t.Fatal("under limit should not trim")
	}
	if !TrimLogFileIfOversized(p, 1000, 100) {
		t.Fatal("over limit should trim")
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	lines := strings.Split(s, "\n")
	if !strings.HasPrefix(lines[0], "[Daemon] Log trimmed at ") || !strings.HasSuffix(s, "LAST\n") || len(b) > 200 {
		t.Fatalf("trimmed content %q", s)
	}
	for _, l := range lines[1 : len(lines)-1] {
		if !strings.HasPrefix(l, "line-") && l != "LAST" {
			t.Fatalf("partial line kept: %q", l)
		}
	}
	if TrimLogFileIfOversized(filepath.Join(t.TempDir(), "missing"), 1, 1) {
		t.Fatal("missing file should be no-op")
	}
}

func TestPidFile(t *testing.T) {
	if !strings.HasSuffix(PidFile(""), "agytheme-daemon-default.pid") || !strings.HasSuffix(PidFile("insiders"), "agytheme-daemon-insiders.pid") {
		t.Fatal(PidFile(""), PidFile("insiders"))
	}
}
