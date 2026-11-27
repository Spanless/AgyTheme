package pane

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Spanless/AgyTheme/internal/bundle"
)

const token = "ui-token"

func serve(t *testing.T, src bundle.Source) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(NewHandler(bundle.NewPanePage(src), token))
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, method, url string, header map[string]string) (int, http.Header, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(body)
}

func TestServesThePanelToTheHost(t *testing.T) {
	srv := serve(t, bundle.EmbeddedSource())

	status, h, body := get(t, http.MethodGet, srv.URL+"/?conversationId=abc", map[string]string{"x-sidecar-token": token})
	if status != http.StatusOK || !strings.HasPrefix(h.Get("Content-Type"), "text/html") {
		t.Fatalf("status %d, content type %q", status, h.Get("Content-Type"))
	}
	if h.Get("Content-Security-Policy") == "" || h.Get("Cache-Control") != "no-store" {
		t.Fatalf("headers %v", h)
	}
	for _, part := range []string{"#ag-telemetry-dock", "function renderDockBody", "agytheme:ready"} {
		if !strings.Contains(body, part) {
			t.Errorf("page lacks %q", part)
		}
	}

	status, _, _ = get(t, http.MethodGet, srv.URL+"/?token="+token, nil)
	if status != http.StatusOK {
		t.Fatalf("query token: status %d", status)
	}
}

func TestRefusesRequestsWithoutTheToken(t *testing.T) {
	srv := serve(t, bundle.EmbeddedSource())

	if status, _, _ := get(t, http.MethodGet, srv.URL+"/", nil); status != http.StatusForbidden {
		t.Errorf("no token: status %d", status)
	}
	if status, _, _ := get(t, http.MethodGet, srv.URL+"/", map[string]string{"x-sidecar-token": "wrong"}); status != http.StatusForbidden {
		t.Errorf("wrong token: status %d", status)
	}
	empty := httptest.NewServer(NewHandler(bundle.NewPanePage(bundle.EmbeddedSource()), ""))
	defer empty.Close()
	if status, _, _ := get(t, http.MethodGet, empty.URL+"/?token=", map[string]string{"x-sidecar-token": ""}); status != http.StatusForbidden {
		t.Errorf("empty server token: status %d", status)
	}
}

func TestServesNothingElse(t *testing.T) {
	srv := serve(t, bundle.EmbeddedSource())
	auth := map[string]string{"x-sidecar-token": token}

	if status, _, _ := get(t, http.MethodGet, srv.URL+"/preload.js", auth); status != http.StatusNotFound {
		t.Errorf("other path: status %d", status)
	}
	if status, _, _ := get(t, http.MethodPost, srv.URL+"/", auth); status != http.StatusMethodNotAllowed {
		t.Errorf("POST: status %d", status)
	}
}

func TestInlinedSourcesCannotCloseTheirElement(t *testing.T) {
	dir := sourceDir(t, map[string]string{
		"runtime/45-telemetry-render.js": "var a = '</script><b>x</b>';",
		"styles/30-telemetry.css":        "/* </style> */",
	})
	srv := serve(t, bundle.DirSource(dir))

	_, _, body := get(t, http.MethodGet, srv.URL+"/", map[string]string{"x-sidecar-token": token})
	if strings.Count(body, "</script>") != 1 || strings.Count(body, "</style>") != 1 {
		t.Fatalf("inlined sources leaked closing tags:\n%s", body)
	}
}

func TestPageFollowsSourceEdits(t *testing.T) {
	dir := sourceDir(t, map[string]string{"pane/pane.js": "var v = 1;"})
	srv := serve(t, bundle.DirSource(dir))
	auth := map[string]string{"x-sidecar-token": token}

	if _, _, body := get(t, http.MethodGet, srv.URL+"/", auth); !strings.Contains(body, "var v = 1;") {
		t.Fatalf("first page:\n%s", body)
	}
	sourceFiles(t, dir, map[string]string{"pane/pane.js": "var v = 22;"})
	if _, _, body := get(t, http.MethodGet, srv.URL+"/", auth); !strings.Contains(body, "var v = 22;") {
		t.Fatalf("edited page:\n%s", body)
	}
}

func TestListenFromEnv(t *testing.T) {
	page := bundle.NewPanePage(bundle.EmbeddedSource())
	t.Setenv("ANTIGRAVITY_SIDECAR_WEB_PORT", "")
	t.Setenv("ANTIGRAVITY_SIDECAR_UI_TOKEN", token)
	if _, _, err := ListenFromEnv(page); err != ErrNoWebPort {
		t.Fatalf("no port: %v", err)
	}

	t.Setenv("ANTIGRAVITY_SIDECAR_WEB_PORT", freePort(t))
	addr, stop, err := ListenFromEnv(page)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if status, _, _ := get(t, http.MethodGet, "http://"+addr+"/", map[string]string{"x-sidecar-token": token}); status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
}

func sourceDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	all := map[string]string{
		"runtime/45-telemetry-render.js": "", "pane/pane.js": "",
		"styles/30-telemetry.css": "", "pane/pane.css": "",
	}
	for k, v := range files {
		all[k] = v
	}
	sourceFiles(t, dir, all)
	return dir
}

func sourceFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return port
}
