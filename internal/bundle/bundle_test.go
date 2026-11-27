package bundle

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	agytheme "github.com/Spanless/AgyTheme"
)

func TestParseFeatureSelection(t *testing.T) {
	only, err := ParseFeatureSelection("i18n,metrics", "")
	if err != nil {
		t.Fatal(err)
	}
	if !only.Explicit || !reflect.DeepEqual(only.Selected, map[string]bool{"theme": false, "i18n": true, "metrics": true, "fixes": false}) {
		t.Errorf("only: %+v", only)
	}
	wantPatch := map[string]bool{"theme": false, "i18n": true, "metrics": true, "deadlockHealer": false, "zoomShortcuts": false}
	if !reflect.DeepEqual(only.ModulesPatch, wantPatch) {
		t.Errorf("patch: %+v", only.ModulesPatch)
	}
	without, err := ParseFeatureSelection("", "metrics,fixes")
	if err != nil {
		t.Fatal(err)
	}
	if !without.Explicit || !reflect.DeepEqual(without.Selected, map[string]bool{"theme": true, "i18n": true, "metrics": false, "fixes": false}) {
		t.Errorf("without: %+v", without)
	}
	alias, _ := ParseFeatureSelection("ZH  zoom", "")
	if !alias.Selected["i18n"] || !alias.Selected["fixes"] || alias.Selected["theme"] {
		t.Errorf("alias: %+v", alias)
	}
	all, _ := ParseFeatureSelection("all", "theme")
	if all.Selected["theme"] || !all.Selected["metrics"] {
		t.Errorf("all minus theme: %+v", all)
	}
	none, _ := ParseFeatureSelection("", "")
	if none.Explicit {
		t.Errorf("default should not be explicit")
	}
	if _, err := ParseFeatureSelection("nonexistent", ""); err == nil || !strings.Contains(err.Error(), "Unknown feature") {
		t.Errorf("unknown: %v", err)
	}
	if _, err := ParseFeatureSelection(" , ", ""); err == nil || !strings.Contains(err.Error(), "Empty feature list") {
		t.Errorf("empty: %v", err)
	}
}

func TestFeatureCoreFilesExist(t *testing.T) {
	src := EmbeddedSource()
	for _, g := range FeatureGroups {
		for _, f := range g.CoreFiles {
			if !src.exists(f) {
				t.Errorf("feature %s lists missing core file %s", g.ID, f)
			}
		}
	}
}

func bootOf(t *testing.T, script string) BootPayload {
	t.Helper()
	i := strings.Index(script, `})({"version"`)
	if i < 0 {
		t.Fatal("boot payload not found in script")
	}
	var boot BootPayload
	if err := json.NewDecoder(strings.NewReader(script[i+3:])).Decode(&boot); err != nil {
		t.Fatal(err)
	}
	return boot
}

func newRoot(t *testing.T, state, config string) string {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "user"), 0o755)
	if state != "" {
		os.WriteFile(filepath.Join(root, "state.json"), []byte(state), 0o644)
	}
	if config != "" {
		os.WriteFile(filepath.Join(root, "user", "config.json"), []byte(config), 0o644)
	}
	return root
}

func TestNormalizePresetFallsBackToLight(t *testing.T) {
	if NormalizePreset(" dark ") != PresetDark {
		t.Error("dark should stay dark")
	}
	if NormalizePreset("onyx-dark") != PresetLight || NormalizePreset("") != PresetLight {
		t.Error("anything else should normalize to light")
	}
}

func TestBootTellsRuntimeWhetherTheSidePaneIsServed(t *testing.T) {
	served := NewController(Options{RootDir: newRoot(t, "", ""), Source: EmbeddedSource(), SidePane: true})
	served.RebuildIfNeeded()
	_, script, _ := served.State()
	if !bootOf(t, script).SidePane {
		t.Error("sidePane should be true when the side pane is served")
	}

	plain := NewController(Options{RootDir: newRoot(t, "", ""), Source: EmbeddedSource()})
	plain.RebuildIfNeeded()
	_, script, _ = plain.State()
	if bootOf(t, script).SidePane {
		t.Error("sidePane should be false without the side pane")
	}
}

func TestRebuildFullBundle(t *testing.T) {
	root := newRoot(t, "", "\uFEFF{\"preset\":\"dark\",\"channel\":\"stable\"}")
	c := NewController(Options{RootDir: root, Source: EmbeddedSource()})
	if !c.RebuildIfNeeded() {
		t.Fatal("first build should rebuild")
	}
	if c.RebuildIfNeeded() {
		t.Fatal("unchanged inputs should not rebuild")
	}
	cfg, script, pv := c.State()
	if cfg.Preset != PresetDark || cfg.Channel != "stable" {
		t.Errorf("cfg %+v", cfg)
	}
	if !strings.HasPrefix(pv, agytheme.Version+"-stable-") || len(pv) != len(agytheme.Version)+len("-stable-")+8 {
		t.Errorf("patch version %q", pv)
	}
	if strings.Contains(script, "__AG_BOOT_PAYLOAD__);") {
		t.Error("placeholder not replaced")
	}
	boot := bootOf(t, script)
	if boot.Version != pv || boot.Preset != PresetDark || len(boot.I18nDict) < 800 || !strings.Contains(boot.CSSText, ".ag-lm-ring-arc") {
		t.Errorf("boot: version=%s preset=%s dict=%d", boot.Version, boot.Preset, len(boot.I18nDict))
	}
	for _, m := range []string{"theme", "i18n", "metrics", "deadlockHealer", "zoomShortcuts"} {
		if boot.Modules[m] != true {
			t.Errorf("module %s should be on: %v", m, boot.Modules[m])
		}
	}

	os.WriteFile(filepath.Join(root, "user", "i18n.override.json"), []byte(`{"Settings":"偏好"}`), 0o644)
	if !c.RebuildIfNeeded() {
		t.Fatal("override change should rebuild")
	}
	_, script2, pv2 := c.State()
	if pv2 == pv || bootOf(t, script2).I18nDict["Settings"] != "偏好" {
		t.Errorf("override not applied: %s", pv2)
	}
}

func TestRebuildRespectsInstalledFeatures(t *testing.T) {
	root := newRoot(t, `{"version":"0.1.0","features":{"theme":false,"i18n":true,"metrics":false,"fixes":false}}`, `{"modules":{"theme":true,"metrics":true}}`)
	c := NewController(Options{RootDir: root, Source: EmbeddedSource()})
	c.RebuildIfNeeded()
	cfg, script, _ := c.State()
	boot := bootOf(t, script)
	for _, m := range []string{"theme", "metrics", "deadlockHealer", "zoomShortcuts"} {
		if boot.Modules[m] != false || cfg.ModuleOn(m) {
			t.Errorf("uninstalled module %s must be forced off", m)
		}
	}
	if boot.Modules["i18n"] != true || len(boot.I18nDict) < 800 {
		t.Errorf("i18n should stay on")
	}
	if boot.CSSText != "" {
		t.Errorf("css should be empty, got %d bytes", len(boot.CSSText))
	}
	src := EmbeddedSource()
	for _, f := range []string{"runtime/20-theme-sync.js", "runtime/50-telemetry-ui.js", "runtime/10-guards.js"} {
		body := strings.TrimSpace(src.read(f))
		if len(body) > 200 && strings.Contains(script, body[:200]) {
			t.Errorf("runtime part %s of an uninstalled feature leaked into script", f)
		}
	}
	for _, f := range []string{"runtime/00-head.js", "runtime/60-i18n-engine.js"} {
		body := strings.TrimSpace(src.read(f))
		if !strings.Contains(script, body[:200]) {
			t.Errorf("runtime part %s missing", f)
		}
	}
}

func TestSafeModeIgnoresUserFiles(t *testing.T) {
	root := newRoot(t, "", `{"enabled":false,"preset":"dark","themeVars":{"x":1}}`)
	os.WriteFile(filepath.Join(root, "user", "custom.css"), []byte("body{color:red}"), 0o644)
	c := NewController(Options{RootDir: root, Source: EmbeddedSource(), Cli: CliOpts{SafeMode: true}})
	c.RebuildIfNeeded()
	_, script, _ := c.State()
	boot := bootOf(t, script)
	if !boot.Enabled || boot.Preset != PresetLight || boot.CustomCSS != "" || len(boot.ThemeVars) != 0 {
		t.Errorf("safe mode leaked user config: %+v", boot.UserConfig)
	}
}

func TestI18nDictionary(t *testing.T) {
	raw, err := fs.ReadFile(agytheme.CoreFS, "core/i18n.json")
	if err != nil {
		t.Fatal(err)
	}
	var dict map[string]any
	if err := json.Unmarshal(raw, &dict); err != nil {
		t.Fatal(err)
	}
	if len(dict) < 800 {
		t.Errorf("expected >= 800 entries, got %d", len(dict))
	}
	if _, ok := dict["Settings"].(string); !ok {
		t.Error("Settings should be translated")
	}
	for _, k := range []string{"Minimal", "Low", "Medium", "High", "Extra", "Extra High", "XHigh", "Max", "Orchestrator", "Orchestrator (Carbon)", "Orchestrator (Argon)"} {
		if _, ok := dict[k]; ok {
			t.Errorf("model/effort label %q should stay untranslated", k)
		}
	}
}
