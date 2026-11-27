package shared

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var DefaultAllowedOrigins = []string{
	"http://127.0.0.1",
	"https://127.0.0.1",
	"http://localhost",
	"https://localhost",
	"vscode-file://",
	"plugin://swarm--",
}

var NonHostCmdRe = regexp.MustCompile(`(?i)--type=|--crashpad|--utility-sub-type=|--import=|sidecar_sdk|--ms-enable-electron-run-as-node`)

var (
	CdpFlagRe     = regexp.MustCompile(`(?i)--remote-debugging-port(?:=|\s|$)`)
	UpdatedFlagRe = regexp.MustCompile(`(?i)--updated(?:\s|$)`)
)

func Platform() string {
	if runtime.GOOS == "windows" {
		return "win32"
	}
	return runtime.GOOS
}

func StripUtf8Bom(raw string) string {
	return strings.TrimPrefix(raw, "\uFEFF")
}

func ReadJSONFile(path string, v any) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal([]byte(StripUtf8Bom(string(raw))), v) == nil
}

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func MtimeMs(fi os.FileInfo) float64 {
	return float64(fi.ModTime().UnixNano()) / 1e6
}

func FormatJSNumber(f float64) string {
	if math.IsInf(f, 1) {
		return "Infinity"
	}
	if math.IsInf(f, -1) {
		return "-Infinity"
	}
	if math.IsNaN(f) {
		return "NaN"
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func ParsePosixEtimeSeconds(raw string) float64 {
	s := strings.TrimSpace(raw)
	if s == "" {
		return math.Inf(1)
	}
	days := 0
	rest := s
	if idx := strings.Index(s, "-"); idx != -1 {
		d, ok := parseLeadingInt(s[:idx])
		if !ok {
			return math.Inf(1)
		}
		days = d
		rest = s[idx+1:]
	}
	parts := strings.Split(rest, ":")
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, ok := parseLeadingInt(p)
		if !ok {
			return math.Inf(1)
		}
		nums[i] = n
	}
	base := float64(days * 86400)
	switch len(nums) {
	case 3:
		return base + float64(nums[0]*3600+nums[1]*60+nums[2])
	case 2:
		return base + float64(nums[0]*60+nums[1])
	case 1:
		return base + float64(nums[0])
	}
	return math.Inf(1)
}

func parseLeadingInt(s string) (int, bool) {
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	end := 0
	if end < len(s) && (s[end] == '-' || s[end] == '+') {
		end++
	}
	start := end
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == start {
		return 0, false
	}
	n, err := strconv.Atoi(s[:end])
	return n, err == nil
}

func IsLoopbackHost(host string) bool {
	h := strings.TrimSpace(strings.ToLower(host))
	h = strings.Trim(h, "[]")
	if h == "" {
		return false
	}
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// RuntimeDir returns a per-user directory for PID and guard files rather than shared /tmp.
func RuntimeDir() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); xdg != "" {
		return filepath.Join(xdg, "agytheme")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".gemini", "antigravity", "agytheme", "run")
	}
	if uid := os.Getuid(); uid >= 0 {
		return filepath.Join(os.TempDir(), fmt.Sprintf("agytheme-%d", uid))
	}
	return filepath.Join(os.TempDir(), "agytheme")
}

func IsAllowedOrigin(urlStr string, allowed []string) bool {
	if urlStr == "" {
		return false
	}
	u, err := url.Parse(urlStr)
	if err != nil || u.Scheme == "" || u.User != nil {
		return false
	}
	if allowed == nil {
		allowed = DefaultAllowedOrigins
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	rawHost := strings.ToLower(u.Host)
	for _, raw := range allowed {
		rule := strings.TrimSpace(raw)
		if rule == "" {
			continue
		}
		if strings.HasSuffix(rule, "://") {
			if scheme == strings.ToLower(strings.TrimSuffix(rule, "://")) {
				return true
			}
			continue
		}
		if strings.HasPrefix(strings.ToLower(rule), "plugin://") && strings.HasSuffix(rule, "--") {
			wantPrefix := strings.ToLower(strings.TrimPrefix(rule, "plugin://"))
			if scheme == "plugin" && strings.HasPrefix(rawHost, wantPrefix) {
				return true
			}
			continue
		}
		ru, err := url.Parse(rule)
		if err != nil || ru.Scheme == "" || ru.Host == "" || ru.User != nil {
			continue
		}
		if scheme != strings.ToLower(ru.Scheme) || host != strings.ToLower(ru.Hostname()) {
			continue
		}
		if ru.Port() != "" && u.Port() != ru.Port() {
			continue
		}
		if ru.Path != "" && ru.Path != "/" && !strings.HasPrefix(u.Path, ru.Path) {
			continue
		}
		return true
	}
	return false
}

func ReadPortFromFile(portFile string) int {
	if portFile == "" {
		return 0
	}
	raw, err := os.ReadFile(portFile)
	if err != nil {
		return 0
	}
	line := strings.TrimSpace(string(raw))
	if i := strings.IndexAny(line, "\r\n"); i != -1 {
		line = line[:i]
	}
	n, ok := parseLeadingInt(line)
	if !ok || n <= 0 {
		return 0
	}
	return n
}

type CdpTarget struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	Title                string `json:"title"`
	URL                  string `json:"url"`
	ParentID             string `json:"parentId,omitempty"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func FetchTargetsFromPort(port int, timeout time.Duration) []CdpTarget {
	if port <= 0 {
		return nil
	}
	if timeout <= 0 {
		timeout = 450 * time.Millisecond
	}
	body, err := localHTTPGet(port, "/json/list", timeout)
	if err != nil {
		return nil
	}
	var out []CdpTarget
	if err := json.Unmarshal(body, &out); err != nil {
		return nil
	}
	return out
}

func localHTTPGet(port int, path string, timeout time.Duration) ([]byte, error) {
	nc, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), timeout)
	if err != nil {
		return nil, err
	}
	defer nc.Close()
	_ = nc.SetDeadline(time.Now().Add(timeout))
	host := "127.0.0.1:" + strconv.Itoa(port)
	if _, err := io.WriteString(nc, "GET "+path+" HTTP/1.1\r\nHost: "+host+"\r\nConnection: close\r\n\r\n"); err != nil {
		return nil, err
	}
	br := bufio.NewReader(nc)
	status, err := br.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if f := strings.Fields(status); len(f) < 2 || f[1] != "200" {
		return nil, fmt.Errorf("GET %s: %s", path, strings.TrimSpace(status))
	}
	length := int64(-1)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Content-Length") {
			if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				length = n
			}
		}
	}
	const maxLocalHTTPBytes = 16 << 20
	if length < 0 {
		return io.ReadAll(io.LimitReader(br, maxLocalHTTPBytes))
	}
	if length > maxLocalHTTPBytes {
		return nil, fmt.Errorf("GET %s: response too large (%d bytes)", path, length)
	}
	body := make([]byte, length)
	_, err = io.ReadFull(br, body)
	return body, err
}

type JsoncRootInfo struct {
	OpenBraceIdx           int
	CloseBraceIdx          int
	HasTopProps            bool
	HasRemoteDebuggingPort bool
	TargetIntValue         int
}

func skipJsoncTrivia(raw string, k int) int {
	n := len(raw)
	for k < n {
		switch {
		case raw[k] == ' ' || raw[k] == '\t' || raw[k] == '\n' || raw[k] == '\r' || raw[k] == '\f' || raw[k] == '\v':
			k++
		case raw[k] == '/' && k+1 < n && raw[k+1] == '/':
			k += 2
			for k < n && raw[k] != '\n' {
				k++
			}
		case raw[k] == '/' && k+1 < n && raw[k+1] == '*':
			k += 2
			for k+1 < n && !(raw[k] == '*' && raw[k+1] == '/') {
				k++
			}
			k += 2
		default:
			return k
		}
	}
	return k
}

func isJSWhitespaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v'
}

func InspectJsoncRootObject(raw, targetProp string) JsoncRootInfo {
	info := JsoncRootInfo{OpenBraceIdx: -1, CloseBraceIdx: -1}
	n := len(raw)
	depth := 0
	i := 0
	for i < n {
		ch := raw[i]
		var next byte
		if i+1 < n {
			next = raw[i+1]
		}
		if ch == '/' && next == '/' {
			i += 2
			for i < n && raw[i] != '\n' {
				i++
			}
			continue
		}
		if ch == '/' && next == '*' {
			i += 2
			for i+1 < n && !(raw[i] == '*' && raw[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}
		if ch == '"' {
			i++
			var sb strings.Builder
			for i < n {
				if raw[i] == '\\' {
					end := i + 2
					if end > n {
						end = n
					}
					sb.WriteString(raw[i:end])
					i += 2
					continue
				}
				if raw[i] == '"' {
					i++
					break
				}
				sb.WriteByte(raw[i])
				i++
			}
			if depth == 1 && info.OpenBraceIdx != -1 && info.CloseBraceIdx == -1 {
				info.HasTopProps = true
				if sb.String() == targetProp {
					k := skipJsoncTrivia(raw, i)
					if k < n && raw[k] == ':' {
						info.HasRemoteDebuggingPort = true
						vStart := skipJsoncTrivia(raw, k+1)
						end := vStart
						for end < n && raw[end] >= '0' && raw[end] <= '9' {
							end++
						}
						if end > vStart {
							if v, err := strconv.Atoi(raw[vStart:end]); err == nil && v > 0 {
								info.TargetIntValue = v
							}
						}
					}
				}
			}
			continue
		}
		switch {
		case ch == '{':
			if depth == 0 && info.OpenBraceIdx == -1 {
				info.OpenBraceIdx = i
			}
			depth++
		case ch == '}':
			depth--
			if depth == 0 && info.OpenBraceIdx != -1 && info.CloseBraceIdx == -1 {
				info.CloseBraceIdx = i
			}
		case depth == 1 && info.OpenBraceIdx != -1 && info.CloseBraceIdx == -1 && !isJSWhitespaceByte(ch):
			info.HasTopProps = true
		}
		i++
	}
	return info
}

func ExtractJsoncNumberProperty(raw, prop string) int {
	if raw == "" {
		return 0
	}
	return InspectJsoncRootObject(raw, prop).TargetIntValue
}

func SleepMs(ms int) {
	if ms > 0 {
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
}

func TruncRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}
