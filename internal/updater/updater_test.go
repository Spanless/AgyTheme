package updater

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Spanless/AgyTheme/internal/bundle"
	"github.com/Spanless/AgyTheme/internal/shared"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func makeResponse(status int, body []byte, ct string) *http.Response {
	h := make(http.Header)
	if ct != "" {
		h.Set("Content-Type", ct)
	}
	return &http.Response{
		StatusCode:    status,
		Header:        h,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}

func buildReleaseZip(t *testing.T, entries map[string]string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:])
}

func TestCompareVersion(t *testing.T) {
	if CompareVersion("v0.5.0", "0.4.0") != 1 {
		t.Fatal("expected 0.5.0 > 0.4.0")
	}
	if CompareVersion("0.4.0", "v0.4.0") != 0 {
		t.Fatal("expected equal versions")
	}
	if CompareVersion("0.5.0-rc.1", "0.5.0") != -1 {
		t.Fatal("expected prerelease < release")
	}
	if CompareVersion("0.3.9", "0.4.0") != -1 {
		t.Fatal("expected 0.3.9 < 0.4.0")
	}
}

func TestCheckHandlesNoRelease(t *testing.T) {
	u := New(Config{
		CurrentVersion: "0.4.0",
		PluginDir:      t.TempDir(),
		RootDir:        t.TempDir(),
		GOOS:           "darwin",
		GOARCH:         "arm64",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return makeResponse(http.StatusNotFound, []byte(`{"message":"Not Found"}`), "application/json"), nil
			}),
		},
	})
	res := u.Check(context.Background())
	if res.Status != "no_release" || res.UpdateAvailable || res.Error != "" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestApplyDownloadsVerifiesReplacesAndRollsBack(t *testing.T) {
	pluginDir := t.TempDir()
	rootDir := t.TempDir()
	binPath := filepath.Join(pluginDir, "bin", "agytheme")
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("old-binary-v040"), 0o755); err != nil {
		t.Fatal(err)
	}
	initState := bundle.InstallState{
		Version:  "0.4.0",
		Features: map[string]bool{"theme": true, "i18n": false, "metrics": true, "fixes": true},
	}
	if err := os.WriteFile(filepath.Join(rootDir, "state.json"), shared.MarshalPretty(initState, "  "), 0o644); err != nil {
		t.Fatal(err)
	}

	zipBytes, zipHash := buildReleaseZip(t, map[string]string{
		"agytheme/bin/agytheme-darwin-arm64": "new-binary-v050",
		"agytheme/plugin.json":               `{"name":"agytheme","version":"0.5.0"}`,
		"agytheme/SKILL.md":                  "# AgyTheme 0.5.0",
	})
	assetName := "AgyTheme-0.5.0-darwin-universal.zip"
	sumsBody := zipHash + "  " + assetName + "\n"
	relJSON, _ := json.Marshal(map[string]any{
		"tag_name": "v0.5.0",
		"body":     "## 更新内容\n- 新增设置弹窗\n- 优化更新体验",
		"html_url": "https://github.com/Spanless/AgyTheme/releases/tag/v0.5.0",
		"assets": []map[string]string{
			{"name": assetName, "browser_download_url": "https://github.com/Spanless/AgyTheme/releases/download/v0.5.0/" + assetName},
			{"name": "SHA256SUMS", "browser_download_url": "https://github.com/Spanless/AgyTheme/releases/download/v0.5.0/SHA256SUMS"},
		},
	})

	postSwapCalls := 0
	u := New(Config{
		CurrentVersion: "0.4.0",
		PluginDir:      pluginDir,
		RootDir:        rootDir,
		GOOS:           "darwin",
		GOARCH:         "arm64",
		PostSwap:       func(newBin, r string) { postSwapCalls++ },
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/releases/latest"):
					return makeResponse(http.StatusOK, relJSON, "application/json"), nil
				case strings.HasSuffix(r.URL.Path, "/"+assetName):
					return makeResponse(http.StatusOK, zipBytes, "application/zip"), nil
				case strings.HasSuffix(r.URL.Path, "/SHA256SUMS"):
					return makeResponse(http.StatusOK, []byte(sumsBody), "text/plain"), nil
				default:
					return makeResponse(http.StatusNotFound, nil, ""), nil
				}
			}),
		},
	})

	if err := u.Apply(context.Background()); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if postSwapCalls != 1 {
		t.Fatalf("expected 1 postSwap call, got %d", postSwapCalls)
	}
	gotBin, _ := os.ReadFile(binPath)
	if string(gotBin) != "new-binary-v050" {
		t.Fatalf("unexpected binary content: %q", string(gotBin))
	}
	gotPrev, _ := os.ReadFile(binPath + ".prev")
	if string(gotPrev) != "old-binary-v040" {
		t.Fatalf("unexpected backup content: %q", string(gotPrev))
	}
	var st bundle.InstallState
	if !shared.ReadJSONFile(filepath.Join(rootDir, "state.json"), &st) {
		t.Fatal("missing state.json")
	}
	if st.Version != "0.5.0" || st.PrevVersion != "0.4.0" || st.Features["i18n"] != false || !st.Features["theme"] {
		t.Fatalf("unexpected state.json after update: %+v", st)
	}
	if !shared.FileExists(filepath.Join(pluginDir, "sidecars", "agytheme", "sidecar.json")) {
		t.Fatal("sidecar.json was not written")
	}

	rb, err := u.Rollback()
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
	if rb.Version != "0.4.0" || rb.PrevVersion != "0.5.0" {
		t.Fatalf("unexpected rollback result: %+v", rb)
	}
	gotBin, _ = os.ReadFile(binPath)
	if string(gotBin) != "old-binary-v040" {
		t.Fatalf("rollback did not restore old binary: %q", string(gotBin))
	}
}

func TestApplyRejectsChecksumMismatchWithoutTouchingBinary(t *testing.T) {
	pluginDir := t.TempDir()
	rootDir := t.TempDir()
	binPath := filepath.Join(pluginDir, "bin", "agytheme")
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("original"), 0o755); err != nil {
		t.Fatal(err)
	}

	zipBytes, _ := buildReleaseZip(t, map[string]string{
		"agytheme/bin/agytheme-darwin-arm64": "tampered",
	})
	assetName := "AgyTheme-0.5.0-darwin-universal.zip"
	badSums := strings.Repeat("0", 64) + "  " + assetName + "\n"
	relJSON, _ := json.Marshal(map[string]any{
		"tag_name": "v0.5.0",
		"assets": []map[string]string{
			{"name": assetName, "browser_download_url": "https://github.com/Spanless/AgyTheme/releases/download/v0.5.0/" + assetName},
			{"name": "SHA256SUMS", "browser_download_url": "https://github.com/Spanless/AgyTheme/releases/download/v0.5.0/SHA256SUMS"},
		},
	})

	u := New(Config{
		CurrentVersion: "0.4.0",
		PluginDir:      pluginDir,
		RootDir:        rootDir,
		GOOS:           "darwin",
		GOARCH:         "arm64",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/releases/latest"):
					return makeResponse(http.StatusOK, relJSON, "application/json"), nil
				case strings.HasSuffix(r.URL.Path, "/"+assetName):
					return makeResponse(http.StatusOK, zipBytes, "application/zip"), nil
				case strings.HasSuffix(r.URL.Path, "/SHA256SUMS"):
					return makeResponse(http.StatusOK, []byte(badSums), "text/plain"), nil
				default:
					return makeResponse(http.StatusNotFound, nil, ""), nil
				}
			}),
		},
	})

	if err := u.Apply(context.Background()); err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	gotBin, _ := os.ReadFile(binPath)
	if string(gotBin) != "original" {
		t.Fatalf("binary modified on failed checksum: %q", string(gotBin))
	}
}

func TestCheckRejectsInvalidSemverTag(t *testing.T) {
	u := New(Config{
		CurrentVersion: "0.4.0",
		PluginDir:      t.TempDir(),
		RootDir:        t.TempDir(),
		GOOS:           "darwin",
		GOARCH:         "arm64",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return makeResponse(http.StatusOK, []byte(`{"tag_name":"v../evil","assets":[]}`), "application/json"), nil
			}),
		},
	})
	res := u.Check(context.Background())
	if res.Status != "error" || res.UpdateAvailable {
		t.Fatalf("expected error for invalid semver tag, got %+v", res)
	}
}
