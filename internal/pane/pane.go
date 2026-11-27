package pane

import (
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Spanless/AgyTheme/internal/bundle"
)

// Self-contained side-pane page; host runtime pushes state via postMessage and settings talk to same-origin /api/*.
const (
	csp        = "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src data:; connect-src 'self'; base-uri 'none'; form-action 'none'"
	authCookie = "agy_pane_token"
)

func NewHandler(page *bundle.PanePage, token string) http.Handler {
	return NewHandlerWithAPI(page, token, nil)
}

func NewHandlerWithAPI(page *bundle.PanePage, token string, api *API) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, token) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if api != nil && api.handle(w, r) {
			return
		}
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Persist auth token in a strict same-origin cookie so iframe fetch() calls stay authenticated when Antigravity proxies only the initial load.
		http.SetCookie(w, &http.Cookie{
			Name:     authCookie,
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", csp)
		if r.Method == http.MethodGet {
			_, _ = w.Write(page.Bytes())
		}
	})
}

func authorized(r *http.Request, token string) bool {
	if token == "" {
		return false
	}
	got := r.Header.Get("x-sidecar-token")
	if got == "" {
		got = r.URL.Query().Get("token")
	}
	if got != "" {
		return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
	}
	c, err := r.Cookie(authCookie)
	if err != nil || c.Value == "" {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-AgyTheme-UI") != "1" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(token)) == 1
}

var ErrNoWebPort = errors.New("no sidecar web port")

// Antigravity assigns the sidecar web port and x-sidecar-token via env vars.
func ListenFromEnv(page *bundle.PanePage) (addr string, stop func(), err error) {
	return ListenFromEnvWithAPI(page, nil)
}

func ListenFromEnvWithAPI(page *bundle.PanePage, api *API) (addr string, stop func(), err error) {
	port := strings.TrimSpace(os.Getenv("ANTIGRAVITY_SIDECAR_WEB_PORT"))
	token := os.Getenv("ANTIGRAVITY_SIDECAR_UI_TOKEN")
	if port == "" || port == "0" || token == "" {
		return "", nil, ErrNoWebPort
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		return "", nil, err
	}
	srv := &http.Server{Handler: NewHandlerWithAPI(page, token, api), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return ln.Addr().String(), func() { _ = srv.Close() }, nil
}
