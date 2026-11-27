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
	dirs := []string{shared.RuntimeDir()}
	if tmp := os.TempDir(); tmp != "" && tmp != dirs[0] {
		dirs = append(dirs, tmp)
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, daemonPidPrefix) || !strings.HasSuffix(name, ".pid") {
				continue
			}
			pidPath := filepath.Join(dir, name)
			pid := shared.ReadPidFile(pidPath)
			if shared.IsConfirmedDaemonPid(pid) && shared.TerminatePid(pid) == nil {
				stopped = append(stopped, pid)
			}
			_ = os.Remove(pidPath)
		}
	}
	for _, pid := range stopped {
		for i := 0; i < 20 && shared.IsPidAlive(pid); i++ {
			shared.SleepMs(100)
		}
	}
	return stopped
}
