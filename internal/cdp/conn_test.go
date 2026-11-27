package cdp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func fakeDevTools(t *testing.T, bigEvent []byte) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		ws.SetReadLimit(64 << 20)
		ctx := r.Context()
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				_, data, err := ws.Read(ctx)
				if err != nil {
					return
				}
				var req struct {
					ID     int    `json:"id"`
					Method string `json:"method"`
				}
				_ = json.Unmarshal(data, &req)
				resp, _ := json.Marshal(map[string]any{"id": req.ID, "result": map[string]any{"echo": req.Method}})
				if err := ws.Write(ctx, websocket.MessageText, resp); err != nil {
					return
				}
			}
		}()
		if err := ws.Ping(ctx); err != nil {
			return
		}
		wr, err := ws.Writer(ctx, websocket.MessageText)
		if err != nil {
			return
		}
		half := len(bigEvent) / 2
		_, _ = wr.Write(bigEvent[:half])
		_, _ = wr.Write(bigEvent[half:])
		_ = wr.Close()
		<-done
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/devtools/page/1"
}

func TestConnRoundTripsCommandsAndEvents(t *testing.T) {
	bigEvent, _ := json.Marshal(map[string]any{"method": "Big.event", "params": map[string]any{"blob": strings.Repeat("x", 3<<20)}})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := Dial(ctx, fakeDevTools(t, bigEvent))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ev := <-c.Events()
	if ev.Method != "Big.event" || len(ev.Params) < 3<<20 {
		t.Fatalf("event %q with %d bytes of params", ev.Method, len(ev.Params))
	}
	largeParams := map[string]any{"expression": strings.Repeat("y", 200<<10)}
	got := c.Send("Runtime.evaluate", largeParams)
	if got == nil || !bytes.Contains(got.Result, []byte(`"echo":"Runtime.evaluate"`)) {
		t.Fatalf("Send = %+v", got)
	}
	small := c.Send("Page.enable", nil)
	if small == nil || !bytes.Contains(small.Result, []byte(`"echo":"Page.enable"`)) {
		t.Fatalf("Send = %+v", small)
	}
}

func TestConnDoneClosesWhenServerHangsUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		_ = ws.Close(websocket.StatusGoingAway, "bye")
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("Done not closed after the server closed the socket")
	}
	if c.Send("Page.enable", nil) != nil {
		t.Fatal("Send on a closed socket should return nil")
	}
}

func TestDialRejectsNonWebSocketEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")); err == nil {
		t.Fatal("Dial should fail when the server does not upgrade")
	}
}
