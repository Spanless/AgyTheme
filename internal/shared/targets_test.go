package shared

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func serverPort(t *testing.T, h http.Handler) int {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	return port
}

func TestFetchTargetsFromPortReadsJSONList(t *testing.T) {
	port := serverPort(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/list" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		_, _ = w.Write([]byte(`[{"id":"A","type":"page","title":"Antigravity","url":"vscode-file://x","webSocketDebuggerUrl":"ws://127.0.0.1/devtools/page/A"}]`))
	}))
	got := FetchTargetsFromPort(port, time.Second)
	if len(got) != 1 || got[0].ID != "A" || got[0].WebSocketDebuggerURL != "ws://127.0.0.1/devtools/page/A" {
		t.Fatalf("targets = %+v", got)
	}
}

func TestFetchTargetsFromPortFailsQuietly(t *testing.T) {
	port := serverPort(t, http.NotFoundHandler())
	if got := FetchTargetsFromPort(port, time.Second); got != nil {
		t.Fatalf("404 should give no targets, got %+v", got)
	}
}

func TestFetchTargetsFromPortRejectsOversizedContentLength(t *testing.T) {
	port := serverPort(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 99999999999\r\n\r\n")
		_ = buf.Flush()
	}))
	if got := FetchTargetsFromPort(port, time.Second); got != nil {
		t.Fatalf("oversized Content-Length should be rejected, got %+v", got)
	}
}
