package installer

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Spanless/AgyTheme/internal/shared"
)

const (
	InstallDirName  = "agytheme"
	binaryBaseName  = "agytheme"
	daemonPidPrefix = "agytheme-daemon-"
)

func DefaultTargetDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini", "antigravity", InstallDirName)
}

func BinaryName() string {
	if runtime.GOOS == "windows" {
		return binaryBaseName + ".exe"
	}
	return binaryBaseName
}

func StopRunningDaemonProcesses() []int {
	stopped := []int{}
	tmp := os.TempDir()
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return stopped
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, daemonPidPrefix) || !strings.HasSuffix(name, ".pid") {
			continue
		}
		pidPath := filepath.Join(tmp, name)
		pid := shared.ReadPidFile(pidPath)
		if shared.IsConfirmedDaemonPid(pid) && shared.TerminatePid(pid) == nil {
			stopped = append(stopped, pid)
		}
		_ = os.Remove(pidPath)
	}
	for _, pid := range stopped {
		for i := 0; i < 20 && shared.IsPidAlive(pid); i++ {
			shared.SleepMs(100)
		}
	}
	return stopped
}
