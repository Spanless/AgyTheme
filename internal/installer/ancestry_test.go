package installer

import (
	"os"
	"testing"
)

func TestParseParentTable(t *testing.T) {
	got := ParseParentTable("  1     0\n 412   1\r\n 9001 412\ngarbage line here\n77\n")
	want := map[int]int{1: 0, 412: 1, 9001: 412}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for pid, ppid := range want {
		if got[pid] != ppid {
			t.Fatalf("pid %d: got parent %d, want %d", pid, got[pid], ppid)
		}
	}
}

func TestDescendsFrom(t *testing.T) {
	table := map[int]int{500: 1, 600: 500, 700: 600, 800: 700, 900: 1, 950: 900}
	hosts := map[int]bool{500: true}
	if !DescendsFrom(table, 800, hosts) {
		t.Fatal("installer under Antigravity's terminal should be detected")
	}
	if DescendsFrom(table, 950, hosts) {
		t.Fatal("installer in an unrelated terminal must not be detected")
	}
	if DescendsFrom(table, 4242, hosts) {
		t.Fatal("unknown pid must not be detected")
	}
	loop := map[int]int{10: 11, 11: 10}
	if DescendsFrom(loop, 10, hosts) {
		t.Fatal("a parent cycle (pid reuse) must terminate as not found")
	}
}

func TestReadParentTableFindsOwnParent(t *testing.T) {
	if !DescendsFrom(readParentTable(), os.Getpid(), map[int]bool{os.Getppid(): true}) {
		t.Fatal("the live process table should lead from this test process to its parent")
	}
}
