package installer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Spanless/AgyTheme/internal/cdp"
	"github.com/Spanless/AgyTheme/internal/shared"
)

type ChannelInfo struct {
	Channel        string `json:"channel"`
	AppExists      bool   `json:"appExists"`
	PortFile       string `json:"portFile"`
	PortFileExists bool   `json:"portFileExists"`
}

type Env struct {
	Channel           string                  `json:"channel"`
	AppExists         bool                    `json:"appExists"`
	AppVersion        *string                 `json:"appVersion"`
	CdpStatus         string                  `json:"cdpStatus"`
	LauncherInstalled bool                    `json:"launcherInstalled"`
	RunningProcesses  []cdp.HostProc          `json:"runningProcesses"`
	PortFile          string                  `json:"portFile"`
	PortFileExists    bool                    `json:"portFileExists"`
	Channels          map[string]*ChannelInfo `json:"channels"`
}

func ResolveCdpStatus(running []cdp.HostProc, channels map[string]*ChannelInfo, portFileExists bool) string {
	if len(running) > 0 {
		for _, p := range running {
			if p.HasCdpFlag || (channels[p.Channel] != nil && channels[p.Channel].PortFileExists) {
				return "ready"
			}
		}
		return "missing_opt_in_flag"
	}
	if portFileExists {
		return "stale_port_file"
	}
	return "app_not_running"
}

func checkSingleChannel(ch string) *ChannelInfo {
	home, _ := os.UserHomeDir()
	isIns := ch == "insiders"
	appDirName := "Antigravity"
	dotDir := filepath.Join(home, ".antigravity")
	appPath := "/Applications/Antigravity.app"
	if isIns {
		appDirName = "Antigravity - Insiders"
		dotDir = filepath.Join(home, ".antigravity-insiders")
		appPath = "/Applications/Antigravity - Insiders.app"
	}
	portFile := defaultPortFile(appDirName)
	if runtime.GOOS != "darwin" {
		appPath = ""
	}
	pfExists := shared.FileExists(portFile)
	appExists := pfExists || shared.FileExists(filepath.Dir(portFile)) || shared.FileExists(dotDir)
	if appPath != "" {
		appExists = shared.FileExists(appPath)
	}
	return &ChannelInfo{Channel: ch, AppExists: appExists, PortFile: portFile, PortFileExists: pfExists}
}

func DetectEnvironment(channel string) Env {
	if channel == "" {
		channel = "auto"
	}
	primary := "stable"
	if channel == "insiders" {
		primary = "insiders"
	}
	stable := checkSingleChannel("stable")
	ins := checkSingleChannel("insiders")
	chInfo := stable
	if primary == "insiders" {
		chInfo = ins
	}
	channels := map[string]*ChannelInfo{"stable": stable, "insiders": ins}
	host := ReadHostVersionAndProcesses(channel)

	var pfExists bool
	switch {
	case channel == "stable":
		pfExists = stable.PortFileExists
	case channel == "insiders":
		pfExists = ins.PortFileExists
	case len(host.RunningProcesses) > 0:
		for _, p := range host.RunningProcesses {
			if c := channels[p.Channel]; c != nil && c.PortFileExists {
				pfExists = true
			}
		}
	default:
		pfExists = stable.PortFileExists || ins.PortFileExists
	}
	status := ResolveCdpStatus(host.RunningProcesses, channels, pfExists)
	activeCh := primary
	if len(host.RunningProcesses) > 0 {
		activeCh = host.RunningProcesses[0].Channel
	}
	preferred := ""
	if c := channels[activeCh]; c != nil {
		preferred = c.PortFile
	}
	if preferred == "" {
		switch {
		case chInfo.PortFileExists:
			preferred = chInfo.PortFile
		case ins.PortFileExists:
			preferred = ins.PortFile
		default:
			preferred = chInfo.PortFile
		}
	}
	procs := host.RunningProcesses
	if procs == nil {
		procs = []cdp.HostProc{}
	}
	return Env{
		Channel:           channel,
		AppExists:         chInfo.AppExists || ins.AppExists || len(procs) > 0,
		AppVersion:        host.AppVersion,
		CdpStatus:         status,
		LauncherInstalled: host.LauncherInstalled,
		RunningProcesses:  procs,
		PortFile:          preferred,
		PortFileExists:    pfExists,
		Channels:          channels,
	}
}

type ArgvResult struct {
	Path   string `json:"path"`
	Port   int    `json:"port,omitempty"`
	Action string `json:"action"`
	Error  string `json:"error,omitempty"`
}

func EnsureArgvRemoteDebugging(channel string) []ArgvResult {
	home, _ := os.UserHomeDir()
	var dirs []string
	switch channel {
	case "insiders":
		dirs = []string{".antigravity-insiders"}
	case "stable":
		dirs = []string{".antigravity"}
	default:
		dirs = []string{".antigravity", ".antigravity-insiders"}
	}
	results := []ArgvResult{}
	for _, d := range dirs {
		port := 9222
		if d == ".antigravity-insiders" {
			port = 9223
		}
		argvDir := filepath.Join(home, d)
		argvPath := filepath.Join(argvDir, "argv.json")
		if !shared.FileExists(argvDir) {
			continue
		}
		if !shared.FileExists(argvPath) {
			if err := os.WriteFile(argvPath, []byte("{\n  \"remote-debugging-port\": "+strconv.Itoa(port)+"\n}\n"), 0o644); err != nil {
				results = append(results, ArgvResult{Path: argvPath, Action: "skipped", Error: err.Error()})
			} else {
				results = append(results, ArgvResult{Path: argvPath, Port: port, Action: "created"})
			}
			continue
		}
		rawB, err := os.ReadFile(argvPath)
		if err != nil {
			results = append(results, ArgvResult{Path: argvPath, Action: "skipped", Error: err.Error()})
			continue
		}
		raw := string(rawB)
		info := shared.InspectJsoncRootObject(raw, "remote-debugging-port")
		if info.HasRemoteDebuggingPort {
			results = append(results, ArgvResult{Path: argvPath, Action: "already-present"})
			continue
		}
		if info.OpenBraceIdx != -1 && info.CloseBraceIdx > info.OpenBraceIdx {
			afterOpen := raw[info.OpenBraceIdx+1:]
			comma := ""
			if info.HasTopProps {
				comma = ","
			}
			nl := "\n"
			if strings.HasPrefix(afterOpen, "\n") || strings.HasPrefix(afterOpen, "\r") {
				nl = ""
			}
			patched := raw[:info.OpenBraceIdx+1] + "\n\t\"remote-debugging-port\": " + strconv.Itoa(port) + comma + nl + afterOpen
			if err := os.WriteFile(argvPath, []byte(patched), 0o644); err != nil {
				results = append(results, ArgvResult{Path: argvPath, Action: "skipped", Error: err.Error()})
			} else {
				results = append(results, ArgvResult{Path: argvPath, Port: port, Action: "patched"})
			}
		} else {
			results = append(results, ArgvResult{Path: argvPath, Action: "skipped", Error: "root JSON object not found"})
		}
	}
	return results
}

type Live struct {
	Reachable bool               `json:"reachable"`
	Reason    string             `json:"reason,omitempty"`
	Port      int                `json:"port,omitempty"`
	Pages     *int               `json:"pages,omitempty"`
	TargetID  string             `json:"targetId,omitempty"`
	Title     *string            `json:"title,omitempty"`
	State     *shared.OrderedMap `json:"state,omitempty"`
}

func (l Live) StateBool(k string) bool {
	if l.State == nil {
		return false
	}
	v, _ := l.State.Get(k)
	b, _ := v.(bool)
	return b
}

func (l Live) StateValue(k string) any {
	if l.State == nil {
		return nil
	}
	v, _ := l.State.Get(k)
	return v
}

const liveStateExpr = `(() => ({
                __isHost: !!(
                  (window.location && window.location.protocol === 'vscode-file:') ||
                  (window.__APP_CONFIG__ && window.__APP_CONFIG__.productName === 'antigravity') ||
                  (window.electronNative && (
                    typeof window.electronNative.setTitleBarOverlay === 'function' ||
                    typeof window.electronNative.getZoomLevel === 'function'
                  )) ||
                  (typeof document !== 'undefined' && document.querySelector &&
                    document.querySelector('link[href*="/jetbox.css"], body[data-agy-theme], body[data-newux-theme], [id="antigravity.agentSidePanelInputBox"]'))
                ),
                version: window.__ag_patch_version || null,
                bodyReady: window.__ag_body_ready === true,
                hasStyle: !!document.getElementById('agytheme-style'),
                hasUserStyle: !!document.getElementById('ag-user-custom-style'),
                preset: document.documentElement ? document.documentElement.getAttribute('data-ag-preset') : null,
                themeMode: document.documentElement ? document.documentElement.getAttribute('data-ag-theme-mode') : null,
                pageType: document.documentElement ? document.documentElement.getAttribute('data-ag-page-type') : null,
                dictCount: window.__ag_i18n_dict ? Object.keys(window.__ag_i18n_dict).length : 0,
                protoSafe: window.__ag_i18n_dict ? Object.getPrototypeOf(window.__ag_i18n_dict) === null : false,
                modules: window.__AGYTHEME_CTX__ ? window.__AGYTHEME_CTX__.modules : null,
                pillMounted: !!document.getElementById('ag-live-metrics-pill'),
                pillText: document.getElementById('ag-live-metrics-pill')?.textContent || null
              }))()`

func evaluatePageState(wsURL string) *shared.OrderedMap {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := cdp.Dial(ctx, wsURL)
	if err != nil {
		return nil
	}
	defer conn.Close()
	res := conn.Send("Runtime.evaluate", map[string]any{"expression": liveStateExpr, "returnByValue": true})
	if res == nil || len(res.Result) == 0 {
		return nil
	}
	var r struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
	}
	if json.Unmarshal(res.Result, &r) != nil || len(r.Result.Value) == 0 || r.Result.Value[0] != '{' {
		return nil
	}
	om := shared.NewOrderedMap()
	if json.Unmarshal(r.Result.Value, om) != nil {
		return nil
	}
	return om
}

func InspectLiveWindow(env *Env, portFile string, fallbackPort int) Live {
	var files []string
	var ports []int
	switch {
	case env != nil:
		if env.PortFile != "" {
			files = append(files, env.PortFile)
		}
		if s := env.Channels["stable"]; s != nil {
			files = append(files, s.PortFile)
		}
		if i := env.Channels["insiders"]; i != nil {
			files = append(files, i.PortFile)
		}
		if s := env.Channels["stable"]; s == nil || s.AppExists {
			ports = append(ports, 9222)
		}
		if i := env.Channels["insiders"]; i != nil && i.AppExists {
			ports = append(ports, 9223)
		}
	case portFile != "":
		files = append(files, portFile)
		if fallbackPort > 0 {
			ports = append(ports, fallbackPort)
		}
	case fallbackPort > 0:
		ports = append(ports, fallbackPort)
	}
	port := 0
	var targets []shared.CdpTarget
	tried := map[int]bool{}
	for _, pf := range files {
		p := shared.ReadPortFromFile(pf)
		if p > 0 && !tried[p] {
			tried[p] = true
			if res := shared.FetchTargetsFromPort(p, 0); len(res) > 0 {
				port, targets = p, res
				break
			}
		}
	}
	if len(targets) == 0 {
		for _, fb := range ports {
			if fb > 0 && !tried[fb] {
				tried[fb] = true
				if res := shared.FetchTargetsFromPort(fb, 0); len(res) > 0 {
					port, targets = fb, res
					break
				}
			}
		}
	}
	if port == 0 || len(targets) == 0 {
		return Live{Reachable: false, Reason: "No active CDP port responded (Antigravity 2.33.0+ requires --remote-debugging-port=0 opt-in launch)"}
	}
	zero := 0
	var pages []shared.CdpTarget
	for _, t := range targets {
		if t.Type == "page" && t.WebSocketDebuggerURL != "" && !strings.HasPrefix(t.URL, "devtools://") &&
			shared.IsAllowedOrigin(t.URL, nil) && !strings.Contains(t.Title, "Launchpad") {
			pages = append(pages, t)
		}
	}
	if len(pages) == 0 {
		return Live{Reachable: true, Port: port, Pages: &zero, Reason: "No active workbench page target"}
	}
	type cand struct {
		page  shared.CdpTarget
		state *shared.OrderedMap
	}
	var selected, firstHost, firstNull *cand
	for _, p := range pages {
		st := evaluatePageState(p.WebSocketDebuggerURL)
		if st == nil {
			if firstNull == nil {
				firstNull = &cand{p, nil}
			}
			continue
		}
		isHostV, _ := st.Get("__isHost")
		st.Delete("__isHost")
		if b, _ := isHostV.(bool); b {
			if hs, _ := st.Get("hasStyle"); hs == true {
				selected = &cand{p, st}
				break
			}
			if firstHost == nil {
				firstHost = &cand{p, st}
			}
		}
	}
	if selected == nil {
		selected = firstHost
	}
	if selected == nil {
		selected = firstNull
	}
	if selected == nil {
		return Live{Reachable: true, Port: port, Pages: &zero, Reason: "No active workbench page target"}
	}
	title := selected.page.Title
	return Live{Reachable: true, Port: port, TargetID: selected.page.ID, Title: &title, State: selected.state}
}
