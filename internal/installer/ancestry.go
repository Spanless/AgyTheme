package installer

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Spanless/AgyTheme/internal/cdp"
	"github.com/Spanless/AgyTheme/internal/shared"
)

func ParseParentTable(stdout string) map[int]int {
	table := map[int]int{}
	for _, line := range strings.Split(stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		table[pid] = ppid
	}
	return table
}

func readParentTable() map[int]int {
	if runtime.GOOS == "windows" {
		res := cdp.RunPowerShell(8*time.Second, `Get-CimInstance Win32_Process | ForEach-Object { "$($_.ProcessId) $($_.ParentProcessId)" }`)
		return ParseParentTable(res.Stdout)
	}
	return ParseParentTable(shared.RunCmd(5*time.Second, "ps", "-A", "-o", "pid=,ppid=").Stdout)
}

func DescendsFrom(table map[int]int, pid int, hosts map[int]bool) bool {
	seen := map[int]bool{}
	for pid > 1 && !seen[pid] {
		if hosts[pid] {
			return true
		}
		seen[pid] = true
		next, ok := table[pid]
		if !ok {
			return false
		}
		pid = next
	}
	return false
}

func InsideHostProcess(procs []cdp.HostProc) bool {
	if len(procs) == 0 {
		return false
	}
	hosts := map[int]bool{}
	for _, p := range procs {
		hosts[p.Pid] = true
	}
	return DescendsFrom(readParentTable(), os.Getpid(), hosts)
}
