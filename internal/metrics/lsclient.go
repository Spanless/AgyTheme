package metrics

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Spanless/AgyTheme/internal/shared"
)

type LSClient struct {
	base  string
	token string
	hc    *http.Client
}

const lsService = "/exa.language_server_pb.LanguageServerService/"

func NewLSClient(addr, token string) *LSClient {
	addr = strings.TrimSpace(addr)
	if addr == "" || token == "" {
		return nil
	}
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || !shared.IsLoopbackHost(u.Hostname()) {
		return nil
	}
	hc := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &LSClient{base: strings.TrimRight(addr, "/"), token: token, hc: hc}
}

// Antigravity injects the local LanguageServerService ConnectRPC address and CSRF token into sidecars.
func NewLSClientFromEnv() *LSClient {
	return NewLSClient(os.Getenv("ANTIGRAVITY_LS_ADDRESS"), os.Getenv("ANTIGRAVITY_CSRF_TOKEN"))
}

type lsError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *lsError) Error() string { return "language server: " + e.Code + ": " + e.Message }

const lsUnaryTimeout = 15 * time.Second

func (c *LSClient) call(ctx context.Context, method string, req, resp any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, lsUnaryTimeout)
	defer cancel()
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+lsService+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("Connect-Protocol-Version", "1")
	hr.Header.Set("x-codeium-csrf-token", c.token)
	res, err := c.hc.Do(hr)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxEnvelopeBytes))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		e := &lsError{Code: res.Status}
		_ = json.Unmarshal(raw, e)
		return e
	}
	return json.Unmarshal(raw, resp)
}

const (
	envelopeCompressed = 1
	envelopeEndStream  = 2
	maxEnvelopeBytes   = 64 << 20
)

func (c *LSClient) stream(ctx context.Context, method string, req any, onMsg func(json.RawMessage) error) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	frame := make([]byte, 5+len(body))
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(body)))
	copy(frame[5:], body)
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+lsService+method, bytes.NewReader(frame))
	if err != nil {
		return err
	}
	hr.Header.Set("Content-Type", "application/connect+json")
	hr.Header.Set("Connect-Protocol-Version", "1")
	hr.Header.Set("x-codeium-csrf-token", c.token)
	res, err := c.hc.Do(hr)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		e := &lsError{Code: res.Status}
		_ = json.Unmarshal(raw, e)
		return e
	}
	br := bufio.NewReader(res.Body)
	var hdr [5]byte
	for {
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return io.ErrUnexpectedEOF
			}
			return err
		}
		n := binary.BigEndian.Uint32(hdr[1:])
		if n > maxEnvelopeBytes {
			return fmt.Errorf("language server: %d-byte stream message", n)
		}
		msg := make([]byte, n)
		if _, err := io.ReadFull(br, msg); err != nil {
			return err
		}
		if hdr[0]&envelopeCompressed != 0 {
			return errors.New("language server: compressed stream message")
		}
		if hdr[0]&envelopeEndStream != 0 {
			var end struct {
				Error *lsError `json:"error"`
			}
			if json.Unmarshal(msg, &end) == nil && end.Error != nil {
				return end.Error
			}
			return io.EOF
		}
		if err := onMsg(msg); err != nil {
			return err
		}
	}
}
