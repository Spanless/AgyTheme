package pane

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Spanless/AgyTheme/internal/bundle"
	"github.com/Spanless/AgyTheme/internal/shared"
	"github.com/Spanless/AgyTheme/internal/updater"
)

func TestSettingsAPIPreservesUnknownKeysAndHonorsAuth(t *testing.T) {
	rootDir := t.TempDir()
	pluginDir := t.TempDir()
	userDir := filepath.Join(rootDir, "user")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initialCfg := `{"enabled":true,"preset":"light","contextLimit":256000,"modules":{"theme":true,"i18n":true,"metrics":true,"deadlockHealer":true}}`
	if err := os.WriteFile(filepath.Join(userDir, "config.json"), []byte(initialCfg), 0o644); err != nil {
		t.Fatal(err)
	}
	st := bundle.InstallState{Version: "0.4.0", Features: map[string]bool{"theme": true, "i18n": false, "metrics": true, "fixes": true}}
	if err := os.WriteFile(filepath.Join(rootDir, "state.json"), shared.MarshalPretty(st, "  "), 0o644); err != nil {
		t.Fatal(err)
	}

	changed := 0
	openedDir := ""
	api := &API{
		RootDir:   rootDir,
		PluginDir: pluginDir,
		Updater:   updater.New(updater.Config{CurrentVersion: "0.4.0", PluginDir: pluginDir, RootDir: rootDir}),
		OnConfigChanged: func() {
			changed++
		},
		Doctor: func() DoctorStatus {
			return DoctorStatus{OK: true, Summary: "运行正常 · CDP 已连接", CdpReachable: true}
		},
		OpenDir: func(dir string) error {
			openedDir = dir
			return nil
		},
	}
	h := NewHandlerWithAPI(bundle.NewPanePage(bundle.EmbeddedSource()), token, api)

	// Initial GET / sets auth cookie.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("x-sidecar-token", token)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /: %d", rec.Code)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != authCookie {
		t.Fatalf("expected auth cookie, got %v", cookies)
	}

	// Cookie-authenticated POST without X-AgyTheme-UI header must be rejected.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewBufferString(`{"preset":"dark"}`))
	req.AddCookie(cookies[0])
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without UI header, got %d", rec.Code)
	}

	// Valid POST updates preset/layout/cacheMode/modules while preserving unknown fields.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewBufferString(`{"preset":"dark","telemetryLayout":"detailed","telemetryCacheMode":"step","modules":{"metrics":false}}`))
	req.AddCookie(cookies[0])
	req.Header.Set("X-AgyTheme-UI", "1")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/settings: %d %s", rec.Code, rec.Body.String())
	}
	if changed != 1 {
		t.Fatalf("expected OnConfigChanged once, got %d", changed)
	}

	var saved map[string]any
	if !shared.ReadJSONFile(filepath.Join(userDir, "config.json"), &saved) {
		t.Fatal("failed reading saved config.json")
	}
	if saved["preset"] != "dark" || saved["telemetryLayout"] != "detailed" || saved["telemetryCacheMode"] != "step" {
		t.Fatalf("unexpected saved fields: %+v", saved)
	}
	if saved["contextLimit"] != float64(256000) {
		t.Fatalf("contextLimit was lost: %+v", saved)
	}
	savedMods := saved["modules"].(map[string]any)
	if savedMods["deadlockHealer"] != true || savedMods["metrics"] != false {
		t.Fatalf("unexpected modules: %+v", savedMods)
	}

	// GET /api/settings exposes installedFeatures from state.json.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req.AddCookie(cookies[0])
	h.ServeHTTP(rec, req)
	var view settingsView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.InstalledFeatures["i18n"] != false || !view.InstalledFeatures["theme"] {
		t.Fatalf("unexpected installedFeatures: %+v", view.InstalledFeatures)
	}

	// GET /api/doctor & POST /api/open-dir
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/doctor", nil)
	req.Header.Set("x-sidecar-token", token)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("CDP")) {
		t.Fatalf("GET /api/doctor: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/open-dir", nil)
	req.Header.Set("x-sidecar-token", token)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || openedDir != userDir {
		t.Fatalf("POST /api/open-dir: %d dir=%q", rec.Code, openedDir)
	}
}
