package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStagePluginAndSidecar(t *testing.T) {
	dir := filepath.Join(t.TempDir(), PluginName)
	if err := StagePluginFiles(dir, PluginFiles{Version: "9.9.9", Skill: []byte("# skill\n"), Logo: []byte("<svg/>")}); err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	raw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil || json.Unmarshal(raw, &manifest) != nil {
		t.Fatalf("plugin.json unreadable: %v %s", err, raw)
	}
	if manifest["name"] != PluginName || manifest["version"] != "9.9.9" || manifest["logo"] != "assets/logo.svg" {
		t.Fatalf("manifest %v", manifest)
	}
	for _, rel := range []string{"README.md", "assets/logo.svg", "skills/agytheme/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
	}

	bin := PluginBinary(dir)
	path, err := WriteSidecar(dir, bin, "/data/agytheme")
	if err != nil || path != SidecarFile(dir) {
		t.Fatalf("WriteSidecar: %v %s", err, path)
	}
	first, _ := os.ReadFile(path)
	var sc struct {
		Command       string            `json:"command"`
		Args          []string          `json:"args"`
		Env           map[string]string `json:"env"`
		RestartPolicy string            `json:"restart_policy"`
		HasWebUI      bool              `json:"has_web_ui"`
		UIConfig      struct {
			Views []struct {
				Path, Entrypoint, Title string
			} `json:"views"`
		} `json:"ui_config"`
	}
	if err := json.Unmarshal(first, &sc); err != nil {
		t.Fatal(err)
	}
	if sc.Command != bin || len(sc.Args) != 3 || sc.Args[0] != "sidecar" || sc.Args[2] != "/data/agytheme" || sc.RestartPolicy != "always" || sc.Env["AGYTHEME_INSTALL_ID"] == "" {
		t.Fatalf("sidecar.json %s", first)
	}
	if !sc.HasWebUI || len(sc.UIConfig.Views) != 1 || sc.UIConfig.Views[0].Path != "/" ||
		sc.UIConfig.Views[0].Entrypoint != "SIDECAR_UI_ENTRYPOINT_AUX_PANE" || sc.UIConfig.Views[0].Title == "" {
		t.Fatalf("side-pane view in sidecar.json %s", first)
	}
	if _, err := WriteSidecar(dir, bin, "/data/agytheme"); err != nil {
		t.Fatal(err)
	}
	if second, _ := os.ReadFile(path); string(second) == string(first) {
		t.Fatal("a rewrite should carry a new install id")
	}
	if err := RemoveSidecar(dir); err != nil || fileExists(path) {
		t.Fatalf("RemoveSidecar: %v", err)
	}
	if err := RemoveSidecar(dir); err != nil {
		t.Fatalf("RemoveSidecar on a missing dir: %v", err)
	}
}

func TestReadPluginStateFromConfig(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	root := t.TempDir()
	plugins := filepath.Join(root, "plugins")
	if st := ReadPluginState(plugins); !st.Enabled || st.Source != "default" {
		t.Fatalf("no config: %+v", st)
	}
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(`{"plugins":{"agytheme":{"enabled":false}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if st := ReadPluginState(plugins); st.Enabled || st.Source != "config" {
		t.Fatalf("disabled in config: %+v", st)
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
