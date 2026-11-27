package pane

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	agytheme "github.com/Spanless/AgyTheme"
	"github.com/Spanless/AgyTheme/internal/bundle"
	"github.com/Spanless/AgyTheme/internal/installer"
	"github.com/Spanless/AgyTheme/internal/shared"
	"github.com/Spanless/AgyTheme/internal/updater"
)

type DoctorStatus struct {
	OK           bool   `json:"ok"`
	Summary      string `json:"summary"`
	CdpReachable bool   `json:"cdpReachable"`
}

type API struct {
	RootDir         string
	PluginDir       string
	Updater         *updater.Updater
	OnConfigChanged func()
	Doctor          func() DoctorStatus
	OpenDir         func(dir string) error
}

type settingsPatch struct {
	Preset             *string         `json:"preset"`
	TelemetryLayout    *string         `json:"telemetryLayout"`
	TelemetryCacheMode *string         `json:"telemetryCacheMode"`
	Modules            map[string]bool `json:"modules"`
}

type settingsView struct {
	Version           string          `json:"version"`
	PrevVersion       string          `json:"prevVersion,omitempty"`
	RollbackAvailable bool            `json:"rollbackAvailable"`
	Config            settingsValues  `json:"config"`
	InstalledFeatures map[string]bool `json:"installedFeatures"`
	Update            updater.Status  `json:"update"`
}

type settingsValues struct {
	Preset             string          `json:"preset"`
	TelemetryLayout    string          `json:"telemetryLayout"`
	TelemetryCacheMode string          `json:"telemetryCacheMode"`
	Modules            map[string]bool `json:"modules"`
}

func (a *API) handle(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	switch r.URL.Path {
	case "/api/settings":
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, a.readSettings())
		case http.MethodPost:
			a.handleSaveSettings(w, r)
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return true
	case "/api/update/check":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return true
		}
		if a.Updater == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "更新服务未启用"})
			return true
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		a.Updater.Check(ctx)
		writeJSON(w, http.StatusOK, a.Updater.Status())
		return true
	case "/api/update/apply":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return true
		}
		if a.Updater == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "更新服务未启用"})
			return true
		}
		writeJSON(w, http.StatusOK, a.Updater.StartApply())
		return true
	case "/api/update/status":
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return true
		}
		if a.Updater == nil {
			writeJSON(w, http.StatusOK, updater.Status{Phase: "idle", CurrentVersion: agytheme.Version})
			return true
		}
		writeJSON(w, http.StatusOK, a.Updater.Status())
		return true
	case "/api/rollback":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return true
		}
		if a.Updater == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "更新服务未启用"})
			return true
		}
		res, err := a.Updater.Rollback()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return true
		}
		writeJSON(w, http.StatusOK, res)
		return true
	case "/api/doctor":
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return true
		}
		st := DoctorStatus{OK: true, Summary: "运行正常"}
		if a.Doctor != nil {
			st = a.Doctor()
		}
		writeJSON(w, http.StatusOK, st)
		return true
	case "/api/open-dir":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return true
		}
		userDir := filepath.Join(a.RootDir, "user")
		_ = os.MkdirAll(userDir, 0o755)
		opener := a.OpenDir
		if opener == nil {
			opener = openFolderInOS
		}
		if err := opener(userDir); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": userDir})
		return true
	default:
		return false
	}
}

func (a *API) readSettings() settingsView {
	cfgPath := filepath.Join(a.RootDir, "user", "config.json")
	raw := map[string]any{}
	shared.ReadJSONFile(cfgPath, &raw)

	preset := "light"
	if s, ok := raw["preset"].(string); ok && (s == "light" || s == "dark") {
		preset = s
	}
	layout := "compact"
	if s, ok := raw["telemetryLayout"].(string); ok && (s == "compact" || s == "detailed") {
		layout = s
	}
	cacheMode := "session"
	if s, ok := raw["telemetryCacheMode"].(string); ok && (s == "session" || s == "step") {
		cacheMode = s
	}

	mods := map[string]bool{"theme": true, "i18n": true, "metrics": true}
	if m, ok := raw["modules"].(map[string]any); ok {
		for _, k := range []string{"theme", "i18n", "metrics"} {
			if v, exists := m[k]; exists {
				mods[k] = bundle.Truthy(v)
			}
		}
	}

	features := map[string]bool{"theme": true, "i18n": true, "metrics": true, "fixes": true}
	var st bundle.InstallState
	if shared.ReadJSONFile(filepath.Join(a.RootDir, "state.json"), &st) && len(st.Features) > 0 {
		for _, k := range []string{"theme", "i18n", "metrics", "fixes"} {
			if v, ok := st.Features[k]; ok {
				features[k] = v
			}
		}
	}

	ver := agytheme.Version
	var prevVer string
	var rollbackAvail bool
	updStatus := updater.Status{Phase: "idle", CurrentVersion: ver}
	if a.Updater != nil {
		updStatus = a.Updater.Status()
		if updStatus.CurrentVersion != "" {
			ver = updStatus.CurrentVersion
		}
		prevVer = a.Updater.PrevVersion()
		rollbackAvail = a.Updater.RollbackAvailable()
	}

	return settingsView{
		Version:           ver,
		PrevVersion:       prevVer,
		RollbackAvailable: rollbackAvail,
		Config: settingsValues{
			Preset:             preset,
			TelemetryLayout:    layout,
			TelemetryCacheMode: cacheMode,
			Modules:            mods,
		},
		InstalledFeatures: features,
		Update:            updStatus,
	}
}

func (a *API) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var patch settingsPatch
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32*1024)).Decode(&patch); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的请求格式"})
		return
	}
	if patch.Preset != nil && *patch.Preset != "light" && *patch.Preset != "dark" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的 preset 值"})
		return
	}
	if patch.TelemetryLayout != nil && *patch.TelemetryLayout != "compact" && *patch.TelemetryLayout != "detailed" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的 telemetryLayout 值"})
		return
	}
	if patch.TelemetryCacheMode != nil && *patch.TelemetryCacheMode != "session" && *patch.TelemetryCacheMode != "step" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的 telemetryCacheMode 值"})
		return
	}

	cfgPath := filepath.Join(a.RootDir, "user", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	doc := shared.NewOrderedMap()
	if raw, err := os.ReadFile(cfgPath); err == nil {
		_ = json.Unmarshal([]byte(shared.StripUtf8Bom(string(raw))), doc)
	}
	if patch.Preset != nil {
		doc.Set("preset", *patch.Preset)
	}
	if patch.TelemetryLayout != nil {
		doc.Set("telemetryLayout", *patch.TelemetryLayout)
	}
	if patch.TelemetryCacheMode != nil {
		doc.Set("telemetryCacheMode", *patch.TelemetryCacheMode)
	}
	if len(patch.Modules) > 0 {
		mods := shared.NewOrderedMap()
		if existing, ok := doc.Get("modules"); ok {
			if rawMods, err := json.Marshal(existing); err == nil {
				_ = json.Unmarshal(rawMods, mods)
			}
		}
		for _, k := range []string{"theme", "i18n", "metrics"} {
			if v, ok := patch.Modules[k]; ok {
				mods.Set(k, v)
			}
		}
		doc.Set("modules", mods)
	}

	if err := installer.WriteFileAtomic(cfgPath, shared.MarshalPretty(doc, "  "), 0o644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if a.OnConfigChanged != nil {
		a.OnConfigChanged()
	}
	writeJSON(w, http.StatusOK, a.readSettings())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func openFolderInOS(dir string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", dir)
	case "windows":
		cmd = exec.Command("explorer", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
