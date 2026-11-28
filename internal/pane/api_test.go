package pane

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Spanless/AgyTheme/internal/bundle"
	"github.com/Spanless/AgyTheme/internal/metrics"
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

	// Valid POST updates layout/cacheMode/modules while preserving unknown fields including preset.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewBufferString(`{"telemetryLayout":"detailed","telemetryCacheMode":"step","modules":{"metrics":false}}`))
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
	if saved["preset"] != "light" || saved["telemetryLayout"] != "detailed" || saved["telemetryCacheMode"] != "step" {
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

func TestDirectMetricsAndSSEEndpoints(t *testing.T) {
	watched := ""
	sample := &metrics.Payload{Cid: "cid-1", CascadeID: "cid-1", HasData: true, Sig: "sig-1"}
	api := &API{
		RootDir:   t.TempDir(),
		PluginDir: t.TempDir(),
		WatchCid:  func(cid string) { watched = cid },
		Metrics: func(cid string) (*metrics.Payload, bool) {
			if cid == "cid-1" {
				return sample, true
			}
			return nil, false
		},
		Subscribe: func() (<-chan string, func()) {
			ch := make(chan string, 1)
			return ch, func() {}
		},
	}
	h := NewHandlerWithAPI(bundle.NewPanePage(bundle.EmbeddedSource()), token, api)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/metrics?cid=cid-1", nil)
	req.Header.Set("x-sidecar-token", token)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || watched != "cid-1" || !strings.Contains(rec.Body.String(), `"sig-1"`) {
		t.Fatalf("GET /api/metrics: %d watched=%q body=%s", rec.Code, watched, rec.Body.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/metrics/stream?cid=cid-1&token="+token, nil).WithContext(ctx)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/event-stream") ||
		!strings.Contains(rec.Body.String(), "event: metrics") {
		t.Fatalf("GET /api/metrics/stream: %d ct=%q body=%s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
}

func TestRelaunchEndpointBusyAndForce(t *testing.T) {
	calls := []bool{}
	api := &API{
		RootDir:   t.TempDir(),
		PluginDir: t.TempDir(),
		Relaunch: func(force bool) (bool, error) {
			calls = append(calls, force)
			if !force {
				return true, errTestBusy
			}
			return false, nil
		},
	}
	h := NewHandlerWithAPI(bundle.NewPanePage(bundle.EmbeddedSource()), token, api)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/relaunch", nil)
	req.Header.Set("x-sidecar-token", token)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"busy":true`) {
		t.Fatalf("busy POST /api/relaunch: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/relaunch", bytes.NewBufferString(`{"force":true}`))
	req.Header.Set("x-sidecar-token", token)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"relaunching":true`) {
		t.Fatalf("force POST /api/relaunch: %d %s", rec.Code, rec.Body.String())
	}
	if len(calls) != 2 || calls[0] != false || calls[1] != true {
		t.Fatalf("unexpected calls: %v", calls)
	}
}

var errTestBusy = &testErr{"active conversation"}

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }
