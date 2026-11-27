//go:build !windows

package shared

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func IsPidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func TerminatePid(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }

func KillPid(pid int) error { return syscall.Kill(pid, syscall.SIGKILL) }

func hideWindow(cmd *exec.Cmd) {}

func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func processCommandLine(pid int) string {
	res := RunCmd(1500*time.Millisecond, "ps", "-p", strconv.Itoa(pid), "-o", "args=")
	if res.Status != 0 {
		return ""
	}
	return strings.TrimSpace(res.Stdout)
}

func SpawnBreakaway(exe string, args ...string) (int, error) {
	return SpawnDetached(exe, args...)
}
