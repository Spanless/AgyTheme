package installer

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Spanless/AgyTheme/internal/shared"
)

const (
	PluginName = "agytheme"
	SidecarID  = "agytheme"
)

func DefaultPluginsDir() string {
	if d := strings.TrimSpace(os.Getenv("AGYTHEME_PLUGINS_DIR")); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini", "config", "plugins")
}

func PluginDir(pluginsDir string) string { return filepath.Join(pluginsDir, PluginName) }

func PluginBinary(pluginDir string) string { return filepath.Join(pluginDir, "bin", BinaryName()) }

func PluginPrevBinary(pluginDir string) string { return PluginBinary(pluginDir) + ".prev" }

func SidecarFile(pluginDir string) string {
	return filepath.Join(pluginDir, "sidecars", SidecarID, "sidecar.json")
}

type PluginFiles struct {
	Version string
	Skill   []byte
	Logo    []byte
}

func PluginManifest(version string) []byte {
	m := shared.NewOrderedMap()
	m.Set("name", PluginName)
	m.Set("displayName", "AgyTheme")
	m.Set("version", version)
	m.Set("description", "浅色 / 深色主题、中文界面与实时用量统计。随 Antigravity 启动和退出，不常驻后台。")
	m.Set("author", map[string]any{"name": "Spanless", "url": "https://github.com/Spanless"})
	m.Set("homepage", "https://github.com/Spanless/AgyTheme")
	m.Set("repository", "https://github.com/Spanless/AgyTheme")
	m.Set("logo", "assets/logo.svg")
	return shared.MarshalPretty(m, "  ")
}

const pluginReadme = `# AgyTheme

Light and dark themes, Chinese UI, and live usage telemetry for Google Antigravity.

- Install: follow ` + "`SKILL.md`" + ` or run ` + "`bin/agytheme-<os>-<arch> install`" + `
- Verify: ` + "`bin/agytheme doctor`" + `
- Config: ` + "`~/.gemini/antigravity/agytheme/user/config.json`" + `
- Log: ` + "`~/.gemini/antigravity/agytheme/logs/sidecar.log`" + `
- Uninstall: ` + "`bin/agytheme uninstall`" + `

https://github.com/Spanless/AgyTheme
`

func StagePluginFiles(dir string, files PluginFiles) error {
	write := func(rel string, data []byte) error {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return WriteFileAtomic(p, data, 0o644)
	}
	if err := write("plugin.json", PluginManifest(files.Version)); err != nil {
		return err
	}
	if err := write("README.md", []byte(pluginReadme)); err != nil {
		return err
	}
	if len(files.Logo) > 0 {
		if err := write("assets/logo.svg", files.Logo); err != nil {
			return err
		}
	}
	if len(files.Skill) > 0 {
		if err := write("skills/"+PluginName+"/SKILL.md", files.Skill); err != nil {
			return err
		}
	}
	return nil
}

// Atomic rename prevents Antigravity's plugin watcher from reading a partial manifest.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Changing AGYTHEME_INSTALL_ID forces Antigravity to restart the sidecar on the new binary.
func SidecarManifest(bin, dataDir, installID string) []byte {
	m := shared.NewOrderedMap()
	m.Set("command", bin)
	m.Set("args", []string{"sidecar", "--root", dataDir})
	m.Set("env", map[string]string{"AGYTHEME_INSTALL_ID": installID})
	m.Set("restart_policy", "always")
	m.Set("display_name", "AgyTheme")
	m.Set("description", "Applies the AgyTheme theme, Chinese UI and usage telemetry to Antigravity windows.")
	m.Set("has_web_ui", true)
	m.Set("ui_config", map[string]any{
		"display_name": "AgyTheme",
		"views": []map[string]string{{
			"path":       PaneViewPath,
			"entrypoint": "SIDECAR_UI_ENTRYPOINT_AUX_PANE",
			"title":      PaneViewTitle,
		}},
	})
	return shared.MarshalPretty(m, "  ")
}

const (
	PaneViewPath  = "/"
	PaneViewTitle = "用量统计"
)

func NewInstallID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func WriteSidecar(pluginDir, bin, dataDir string) (string, error) {
	path := SidecarFile(pluginDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return path, err
	}
	return path, WriteFileAtomic(path, SidecarManifest(bin, dataDir, NewInstallID()), 0o644)
}

func RemoveSidecar(pluginDir string) error {
	err := os.RemoveAll(filepath.Join(pluginDir, "sidecars"))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func FindAgy() string {
	if p, err := exec.LookPath("agy"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	name := "agy"
	if runtime.GOOS == "windows" {
		name = "agy.exe"
	}
	for _, p := range []string{
		filepath.Join(home, ".local", "bin", name),
		filepath.Join(home, ".gemini", "antigravity", "bin", name),
	} {
		if shared.FileExists(p) {
			return p
		}
	}
	return ""
}

type PluginState struct {
	Enabled bool   `json:"enabled"`
	Source  string `json:"source"`
}

func ReadPluginState(pluginsDir string) PluginState {
	if agy := FindAgy(); agy != "" {
		res := shared.RunCmd(15*time.Second, agy, "plugin", "list", "--json")
		var list []struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		}
		if res.Status == 0 && json.Unmarshal([]byte(res.Stdout), &list) == nil {
			for _, p := range list {
				if p.Name == PluginName {
					return PluginState{Enabled: p.Enabled, Source: "agy"}
				}
			}
		}
	}
	var cfg struct {
		Plugins map[string]struct {
			Enabled *bool `json:"enabled"`
		} `json:"plugins"`
	}
	cfgPath := filepath.Join(filepath.Dir(pluginsDir), "config.json")
	if shared.ReadJSONFile(cfgPath, &cfg) {
		if p, ok := cfg.Plugins[PluginName]; ok && p.Enabled != nil {
			return PluginState{Enabled: *p.Enabled, Source: "config"}
		}
	}
	return PluginState{Enabled: true, Source: "default"}
}

func EnablePlugin() error {
	agy := FindAgy()
	if agy == "" {
		return fmt.Errorf("agy CLI not found")
	}
	if res := shared.RunCmd(20*time.Second, agy, "plugin", "enable", PluginName); res.Status != 0 {
		return fmt.Errorf("agy plugin enable exited with %d: %s", res.Status, strings.TrimSpace(res.Stdout))
	}
	return nil
}
