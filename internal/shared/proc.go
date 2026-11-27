package shared

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type CmdResult struct {
	Stdout string
	Status int
}

func RunCmd(timeout time.Duration, name string, args ...string) CmdResult {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	hideWindow(cmd)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	status := 0
	if err != nil {
		status = -1
		if ee, ok := err.(*exec.ExitError); ok && ctx.Err() == nil {
			status = ee.ExitCode()
		}
	}
	return CmdResult{Stdout: out.String(), Status: status}
}

func RunShell(script string) int {
	return RunCmd(15*time.Second, "/bin/sh", "-c", script).Status
}

func SpawnDetached(exe string, args ...string) (int, error) {
	cmd := exec.Command(exe, args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	detachProcess(cmd)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return pid, nil
}

func SpawnDetachedHidden(exe string, args ...string) (int, error) {
	cmd := exec.Command(exe, args...)
	detachProcess(cmd)
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return pid, nil
}

func SelfExecutable() string {
	p, err := os.Executable()
	if err != nil {
		return os.Args[0]
	}
	return p
}

func IsConfirmedDaemonPid(pid int) bool {
	if pid <= 0 || pid == os.Getpid() || !IsPidAlive(pid) {
		return false
	}
	args := processCommandLine(pid)
	if args == "" {
		return false
	}
	return strings.Contains(strings.ToLower(args), "agytheme")
}

func ReadPidFile(path string) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0
	}
	return n
}
