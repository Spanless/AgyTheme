package installer

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	agytheme "github.com/Spanless/AgyTheme"
	"github.com/Spanless/AgyTheme/internal/bundle"
	"github.com/Spanless/AgyTheme/internal/cdp"
	"github.com/Spanless/AgyTheme/internal/shared"
)

const (
	macLauncherBundlePrefix = "com.agytheme.launcher"
	linuxDesktopMarker      = "# X-AgyTheme=1"
	lsregister              = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
)

func defaultPortFile(appDirName string) string { return bundle.DefaultPortFile(appDirName) }

type HostRuntime struct {
	AppVersion        *string
	RunningProcesses  []cdp.HostProc
	LauncherInstalled bool
}

var plistVersionRe = regexp.MustCompile(`<key>CFBundleShortVersionString</key>\s*<string>([^<]+)</string>`)

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func psBool(b bool) string {
	if b {
		return "$true"
	}
	return "$false"
}

const winLnkDirsExpr = `if ($env:AG_WIN32_LNK_DIRS) { $env:AG_WIN32_LNK_DIRS -split ';' } else { @(
  [Environment]::GetFolderPath('Desktop'),
  [Environment]::GetFolderPath('CommonDesktopDirectory'),
  (Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs'),
  (Join-Path $env:ProgramData 'Microsoft\Windows\Start Menu\Programs'),
  (Join-Path $env:APPDATA 'Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar')
) }`

func winExeCandidates(channel string, includeInsidersFallback bool) []string {
	home, _ := os.UserHomeDir()
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		local = filepath.Join(home, "AppData", "Local")
	}
	prog := os.Getenv("ProgramFiles")
	if prog == "" {
		prog = `C:\Program Files`
	}
	if channel == "insiders" {
		return []string{
			filepath.Join(local, "Programs", "Antigravity - Insiders", "Antigravity - Insiders.exe"),
			filepath.Join(prog, "Antigravity - Insiders", "Antigravity - Insiders.exe"),
		}
	}
	c := []string{
		filepath.Join(local, "Programs", "Antigravity", "Antigravity.exe"),
		filepath.Join(prog, "Antigravity", "Antigravity.exe"),
	}
	if includeInsidersFallback {
		c = append(c, filepath.Join(local, "Programs", "Antigravity - Insiders", "Antigravity - Insiders.exe"))
	}
	return c
}

func firstExisting(paths []string) string {
	for _, p := range paths {
		if shared.FileExists(p) {
			return p
		}
	}
	return ""
}

func ReadHostVersionAndProcesses(channel string) HostRuntime {
	home, _ := os.UserHomeDir()
	allowStable := channel != "insiders"
	allowInsiders := channel != "stable"
	var out HostRuntime

	switch runtime.GOOS {
	case "darwin":
		apps := []string{"/Applications/Antigravity.app", filepath.Join(home, "Applications", "Antigravity.app"),
			"/Applications/Antigravity - Insiders.app", filepath.Join(home, "Applications", "Antigravity - Insiders.app")}
		if channel == "insiders" {
			apps = apps[2:]
		}
		for _, a := range apps {
			raw, err := os.ReadFile(filepath.Join(a, "Contents", "Info.plist"))
			if err != nil {
				continue
			}
			if m := plistVersionRe.FindStringSubmatch(string(raw)); m != nil && strings.TrimSpace(m[1]) != "" {
				v := strings.TrimSpace(m[1])
				out.AppVersion = &v
				break
			}
		}
		launcher := filepath.Join(home, "Applications", "Antigravity (AgyTheme).app")
		if channel == "insiders" {
			launcher = filepath.Join(home, "Applications", "Antigravity - Insiders (AgyTheme).app")
		}
		out.LauncherInstalled = shared.FileExists(filepath.Join(launcher, "Contents", "MacOS", "launcher"))
		if ps := shared.RunCmd(2500*time.Millisecond, "ps", "-axo", "pid=,etime=,command="); ps.Status == 0 {
			out.RunningProcesses = cdp.ParsePosixHostProcs("darwin", ps.Stdout, allowStable, allowInsiders)
		}
	case "windows":
		existing := firstExisting(winExeCandidates(channel, true))
		lines := []string{
			`[Console]::OutputEncoding = [System.Text.Encoding]::UTF8`,
			`$ErrorActionPreference = 'SilentlyContinue'`,
			`$allowStable = ` + psBool(allowStable),
			`$allowInsiders = ` + psBool(allowInsiders),
			`$now = [DateTime]::UtcNow`,
			`$ver = $null`,
		}
		if existing != "" {
			lines = append(lines, `$ver = (Get-Item -LiteralPath `+psQuote(existing)+`).VersionInfo.ProductVersion`)
		}
		lines = append(lines,
			`$procs = @(Get-CimInstance Win32_Process -Filter "Name = 'Antigravity.exe' OR Name = 'Antigravity - Insiders.exe'" | Where-Object { $_.CommandLine -notmatch '--type=|--crashpad|--utility-sub-type=|--import=|sidecar_sdk|--ms-enable-electron-run-as-node' } | ForEach-Object { $age = if ($_.CreationDate) { [Math]::Max(0, [int][Math]::Round(($now - ([DateTime]$_.CreationDate).ToUniversalTime()).TotalSeconds)) } else { 999999 }; @{ pid = [int]$_.ProcessId; name = [string]$_.Name; exe = [string]$_.ExecutablePath; cmd = [string]$_.CommandLine; elapsedSec = $age } })`,
			`$lnkOk = $false`,
			`$wsh = New-Object -ComObject WScript.Shell`,
			`$lnkDirs = `+winLnkDirsExpr,
			`foreach ($d in $lnkDirs) { if ($d -and (Test-Path -LiteralPath $d)) { Get-ChildItem -LiteralPath $d -Filter '*.lnk' -Recurse -ErrorAction SilentlyContinue | ForEach-Object { try { $s = $wsh.CreateShortcut($_.FullName); $leaf = [IO.Path]::GetFileName([string]$s.TargetPath); $matchTarget = (($leaf -ieq 'Antigravity.exe' -and $allowStable) -or ($leaf -ieq 'Antigravity - Insiders.exe' -and $allowInsiders)); if ($matchTarget -and ($s.Arguments -match '--remote-debugging-port')) { $lnkOk = $true } } catch {} } } }`,
			`@{ appVersion = $ver; launcherInstalled = $lnkOk; procs = $procs } | ConvertTo-Json -Compress -Depth 4`,
		)
		r := cdp.RunPowerShell(6*time.Second, strings.Join(lines, "\n"))
		if r.Status == 0 && strings.TrimSpace(r.Stdout) != "" {
			var info struct {
				AppVersion        any             `json:"appVersion"`
				LauncherInstalled bool            `json:"launcherInstalled"`
				Procs             json.RawMessage `json:"procs"`
			}
			if json.Unmarshal([]byte(shared.StripUtf8Bom(strings.TrimSpace(r.Stdout))), &info) == nil {
				if s, ok := info.AppVersion.(string); ok && strings.TrimSpace(s) != "" {
					v := strings.TrimSpace(s)
					out.AppVersion = &v
				}
				out.LauncherInstalled = info.LauncherInstalled
				wrapped, _ := json.Marshal(map[string]json.RawMessage{"hosts": info.Procs})
				hosts, _, _ := cdp.ParseWinHostProbe(string(wrapped))
				for _, h := range hosts {
					if h.ExePath == "" {
						h.ExePath = existing
					}
					h.Cmd = ""
					if h.Pid > 0 && ((h.Channel == "stable" && allowStable) || (h.Channel == "insiders" && allowInsiders)) {
						out.RunningProcesses = append(out.RunningProcesses, h)
					}
				}
			}
		}
	default:
		desktop := filepath.Join(home, ".local", "share", "applications", "antigravity.desktop")
		if raw, err := os.ReadFile(desktop); err == nil {
			out.LauncherInstalled = strings.Contains(string(raw), "--remote-debugging-port")
		}
		for _, pkg := range []string{"/usr/share/antigravity/resources/app/package.json", "/opt/Antigravity/resources/app/package.json"} {
			var p struct {
				Version any `json:"version"`
			}
			if shared.ReadJSONFile(pkg, &p) && p.Version != nil && fmt.Sprint(p.Version) != "" {
				v := fmt.Sprint(p.Version)
				out.AppVersion = &v
				break
			}
		}
		if ps := shared.RunCmd(2500*time.Millisecond, "ps", "-axo", "pid=,etime=,command="); ps.Status == 0 {
			out.RunningProcesses = cdp.ParsePosixHostProcs("linux", ps.Stdout, allowStable, allowInsiders)
		}
	}
	for i := range out.RunningProcesses {
		out.RunningProcesses[i].Cmd = ""
	}
	return out
}

func EnsureCdpLaunchers(channel string, withLauncher bool) map[string]any {
	home, _ := os.UserHomeDir()
	allowStable := channel != "insiders"
	allowInsiders := channel != "stable"
	switch runtime.GOOS {
	case "darwin":
		if !withLauncher {
			cleaned := RemoveCdpLaunchers()
			return map[string]any{"platform": "darwin", "mode": "single-icon-m2-relay", "launchers": []string{}, "removedLaunchers": cleaned}
		}
		appsDir := filepath.Join(home, "Applications")
		type spec struct{ srcApp, launcherName, bundleID, displayName string }
		var specs []spec
		if allowStable {
			if src := firstExisting([]string{"/Applications/Antigravity.app", filepath.Join(appsDir, "Antigravity.app")}); src != "" {
				specs = append(specs, spec{src, "Antigravity (AgyTheme).app", macLauncherBundlePrefix, "Antigravity (AgyTheme)"})
			}
		}
		if allowInsiders {
			if src := firstExisting([]string{"/Applications/Antigravity - Insiders.app", filepath.Join(appsDir, "Antigravity - Insiders.app")}); src != "" {
				specs = append(specs, spec{src, "Antigravity - Insiders (AgyTheme).app", macLauncherBundlePrefix + ".insiders", "Antigravity - Insiders (AgyTheme)"})
			}
		}
		created := []string{}
		for _, s := range specs {
			dest := filepath.Join(appsDir, s.launcherName)
			contents := filepath.Join(dest, "Contents")
			macosDir := filepath.Join(contents, "MacOS")
			resDir := filepath.Join(contents, "Resources")
			if os.MkdirAll(macosDir, 0o755) != nil || os.MkdirAll(resDir, 0o755) != nil {
				continue
			}
			iconBase := ""
			srcRes := filepath.Join(s.srcApp, "Contents", "Resources")
			if shared.FileExists(srcRes) {
				icns := ""
				for _, n := range []string{"icon.icns", "electron.icns"} {
					if shared.FileExists(filepath.Join(srcRes, n)) {
						icns = n
						break
					}
				}
				if icns == "" {
					if entries, err := os.ReadDir(srcRes); err == nil {
						for _, e := range entries {
							if strings.HasSuffix(e.Name(), ".icns") {
								icns = e.Name()
								break
							}
						}
					}
				}
				if icns != "" && copyFile(filepath.Join(srcRes, icns), filepath.Join(resDir, "icon.icns"), 0o644) == nil {
					iconBase = "icon"
				}
			}
			plist := []string{
				`<?xml version="1.0" encoding="UTF-8"?>`,
				`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">`,
				`<plist version="1.0">`, `<dict>`,
				`  <key>CFBundleExecutable</key>`, `  <string>launcher</string>`,
				`  <key>CFBundleIdentifier</key>`, `  <string>` + s.bundleID + `</string>`,
				`  <key>CFBundleName</key>`, `  <string>` + s.displayName + `</string>`,
				`  <key>CFBundleDisplayName</key>`, `  <string>` + s.displayName + `</string>`,
				`  <key>CFBundlePackageType</key>`, `  <string>APPL</string>`,
				`  <key>CFBundleShortVersionString</key>`, `  <string>` + agytheme.Version + `</string>`,
				`  <key>CFBundleVersion</key>`, `  <string>110</string>`,
			}
			if iconBase != "" {
				plist = append(plist, `  <key>CFBundleIconFile</key>`, `  <string>`+iconBase+`</string>`)
			}
			plist = append(plist,
				`  <key>LSMinimumSystemVersion</key>`, `  <string>12.0</string>`,
				`  <key>LSUIElement</key>`, `  <true/>`,
				`  <key>NSHighResolutionCapable</key>`, `  <true/>`,
				`</dict>`, `</plist>`, ``)
			if os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(strings.Join(plist, "\n")), 0o644) != nil {
				continue
			}
			esc := regexp.MustCompile("([\"\\\\$`])").ReplaceAllString(s.srcApp, `\$1`)
			script := "#!/bin/sh\n# Managed by AgyTheme (Optional M1 CDP Opt-in Launcher)\nexec /usr/bin/open -a \"" + esc + "\" --args --remote-debugging-port=0 \"$@\"\n"
			bin := filepath.Join(macosDir, "launcher")
			if os.WriteFile(bin, []byte(script), 0o755) != nil {
				continue
			}
			_ = os.Chmod(bin, 0o755)
			if shared.FileExists(lsregister) {
				shared.RunCmd(4*time.Second, lsregister, "-f", dest)
			}
			created = append(created, dest)
		}
		return map[string]any{"platform": "darwin", "mode": "optional-m1-launcher", "launchers": created}
	case "windows":
		script := strings.Join([]string{
			`[Console]::OutputEncoding = [System.Text.Encoding]::UTF8`,
			`$ErrorActionPreference = 'SilentlyContinue'`,
			`$allowStable = ` + psBool(allowStable),
			`$allowInsiders = ` + psBool(allowInsiders),
			`$wsh = New-Object -ComObject WScript.Shell`,
			`$rawDirs = ` + winLnkDirsExpr,
			`$dirs = @($rawDirs | Where-Object { $_ -and (Test-Path -LiteralPath $_) })`,
			`$patched = @(); $already = @(); $skipped = @()`,
			`foreach ($d in $dirs) {`,
			`  Get-ChildItem -LiteralPath $d -Filter '*.lnk' -Recurse -ErrorAction SilentlyContinue | ForEach-Object {`,
			`    $lnkPath = $_.FullName`,
			`    try {`,
			`      $sc = $wsh.CreateShortcut($lnkPath)`,
			`      $leaf = [IO.Path]::GetFileName([string]$sc.TargetPath)`,
			`      $isStable = ($leaf -ieq 'Antigravity.exe')`,
			`      $isIns = ($leaf -ieq 'Antigravity - Insiders.exe')`,
			`      if ((-not $isStable -or -not $allowStable) -and (-not $isIns -or -not $allowInsiders)) { return }`,
			`      $argsStr = [string]$sc.Arguments`,
			`      if ($argsStr -match '--remote-debugging-port') { $already += $lnkPath; return }`,
			`      $sc.Arguments = if ($argsStr.Trim()) { "$($argsStr.Trim()) --remote-debugging-port=0" } else { '--remote-debugging-port=0' }`,
			`      $sc.Save()`,
			`      $patched += $lnkPath`,
			`    } catch { $skipped += $lnkPath }`,
			`  }`,
			`}`,
			`@{ patched = $patched; already = $already; skipped = $skipped } | ConvertTo-Json -Compress -Depth 3`,
		}, "\n")
		r := cdp.RunPowerShell(8*time.Second, script)
		var parsed struct {
			Patched, Already, Skipped json.RawMessage
		}
		_ = json.Unmarshal([]byte(shared.StripUtf8Bom(strings.TrimSpace(r.Stdout))), &parsed)
		return map[string]any{"platform": "win32", "mode": "in-place-lnk",
			"patched": strList(parsed.Patched), "already": strList(parsed.Already), "skipped": strList(parsed.Skipped)}
	case "linux":
		updated := []string{}
		sysApps := "/usr/share/applications"
		userApps := filepath.Join(home, ".local", "share", "applications")
		var names []string
		if allowStable {
			names = append(names, "antigravity.desktop")
		}
		if allowInsiders {
			names = append(names, "antigravity-insiders.desktop")
		}
		execRe := regexp.MustCompile(`(?m)^(Exec=.*)$`)
		for _, name := range names {
			src := filepath.Join(userApps, name)
			if !shared.FileExists(src) {
				src = filepath.Join(sysApps, name)
			}
			raw, err := os.ReadFile(src)
			if err != nil {
				continue
			}
			next := execRe.ReplaceAllStringFunc(string(raw), func(line string) string {
				if strings.Contains(line, "--remote-debugging-port") {
					return line
				}
				return line + " --remote-debugging-port=0"
			})
			if !strings.Contains(next, linuxDesktopMarker) {
				next = linuxDesktopMarker + "\n" + next
			}
			_ = os.MkdirAll(userApps, 0o755)
			dest := filepath.Join(userApps, name)
			if os.WriteFile(dest, []byte(next), 0o644) == nil {
				updated = append(updated, dest)
			}
		}
		return map[string]any{"platform": "linux", "mode": "desktop-entry", "desktopFiles": updated}
	}
	return map[string]any{"platform": shared.Platform(), "mode": "unknown"}
}

func strList(raw json.RawMessage) []string {
	out := []string{}
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return out
	}
	if s[0] == '[' {
		_ = json.Unmarshal(raw, &out)
		return out
	}
	var one string
	if json.Unmarshal(raw, &one) == nil && one != "" {
		out = append(out, one)
	}
	return out
}

func RemoveCdpLaunchers() []string {
	home, _ := os.UserHomeDir()
	removed := []string{}
	switch runtime.GOOS {
	case "darwin":
		appsDir := filepath.Join(home, "Applications")
		for _, name := range []string{
			"Antigravity (AgyTheme).app", "Antigravity - Insiders (AgyTheme).app",
		} {
			app := filepath.Join(appsDir, name)
			raw, err := os.ReadFile(filepath.Join(app, "Contents", "Info.plist"))
			if err != nil || !strings.Contains(string(raw), macLauncherBundlePrefix) {
				continue
			}
			if shared.FileExists(lsregister) {
				shared.RunCmd(4*time.Second, lsregister, "-u", app)
			}
			if shared.FileExists("/usr/bin/trash") {
				shared.RunCmd(4*time.Second, "/usr/bin/trash", app)
			} else {
				_ = os.RemoveAll(app)
			}
			removed = append(removed, app)
		}
	case "windows":
		script := strings.Join([]string{
			`[Console]::OutputEncoding = [System.Text.Encoding]::UTF8`,
			`$ErrorActionPreference = 'SilentlyContinue'`,
			`$wsh = New-Object -ComObject WScript.Shell`,
			`$rawDirs = ` + winLnkDirsExpr,
			`$dirs = @($rawDirs | Where-Object { $_ -and (Test-Path -LiteralPath $_) })`,
			`foreach ($d in $dirs) {`,
			`  Get-ChildItem -LiteralPath $d -Filter '*.lnk' -Recurse -ErrorAction SilentlyContinue | ForEach-Object {`,
			`    try {`,
			`      $sc = $wsh.CreateShortcut($_.FullName)`,
			`      $leaf = [IO.Path]::GetFileName([string]$sc.TargetPath)`,
			`      if (($leaf -ieq 'Antigravity.exe') -or ($leaf -ieq 'Antigravity - Insiders.exe')) {`,
			`        $orig = [string]$sc.Arguments`,
			`        $clean = ($orig -replace '(^|\s)--remote-debugging-port(=\d+)?(\s|$)', ' ').Trim()`,
			`        if ($clean -ne $orig.Trim()) { $sc.Arguments = $clean; $sc.Save() }`,
			`      }`,
			`    } catch {}`,
			`  }`,
			`}`,
		}, "\n")
		cdp.RunPowerShell(8*time.Second, script)
	case "linux":
		userApps := filepath.Join(home, ".local", "share", "applications")
		for _, name := range []string{"antigravity.desktop", "antigravity-insiders.desktop"} {
			p := filepath.Join(userApps, name)
			raw, err := os.ReadFile(p)
			if err == nil && strings.Contains(string(raw), linuxDesktopMarker) && os.Remove(p) == nil {
				removed = append(removed, p)
			}
		}
	}
	return removed
}

type ActiveWal struct {
	File  string  `json:"file"`
	AgeMs float64 `json:"ageMs"`
	Bytes int64   `json:"bytes"`
}

func HasRecentActiveConversationWal(channel string, window time.Duration) []ActiveWal {
	home, _ := os.UserHomeDir()
	stable := filepath.Join(home, ".gemini", "antigravity", "conversations")
	ins := filepath.Join(home, ".gemini", "antigravity-insiders", "conversations")
	dirs := []string{stable, ins}
	if channel == "insiders" {
		dirs = []string{ins}
	} else if channel == "stable" {
		dirs = []string{stable}
	}
	now := time.Now()
	out := []ActiveWal{}
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".db-wal") {
				continue
			}
			fi, err := os.Stat(filepath.Join(d, e.Name()))
			if err == nil && fi.Size() > 4096 && now.Sub(fi.ModTime()) < window {
				out = append(out, ActiveWal{File: e.Name(), AgeMs: math.Round(float64(now.Sub(fi.ModTime()).Milliseconds())), Bytes: fi.Size()})
			}
		}
	}
	return out
}

type RelaunchResult struct {
	ExitCode int
	Payload  any
}

func RelaunchAntigravityWithCdp(channel string, force bool, runDaemonOnce func()) RelaunchResult {
	if channel == "" {
		channel = "auto"
	}
	if !force {
		if wals := HasRecentActiveConversationWal(channel, 15*time.Second); len(wals) > 0 {
			return RelaunchResult{ExitCode: 2, Payload: map[string]any{
				"ok":         false,
				"reason":     "active_conversation_detected",
				"activeWals": wals,
				"hint":       "An Agent conversation updated its WAL within the last 15s. Wait until the turn finishes, or pass --force to relaunch anyway.",
			}}
		}
	}
	before := ReadHostVersionAndProcesses(channel)
	procs := before.RunningProcesses
	home, _ := os.UserHomeDir()

	prev := cdp.ReadRelaunchGuard()
	handled := prev.HandledPids
	if len(handled) > 15 {
		handled = handled[len(handled)-15:]
	}
	seen := map[int]bool{}
	var merged []int
	for _, p := range handled {
		if !seen[p] {
			seen[p] = true
			merged = append(merged, p)
		}
	}
	for _, p := range procs {
		if !seen[p.Pid] {
			seen[p.Pid] = true
			merged = append(merged, p.Pid)
		}
	}
	if merged == nil {
		merged = []int{}
	}
	guard := map[string]any{
		"lastRelaunchAtMs": time.Now().UnixMilli(),
		"lastChannel":      channel,
		"reason":           "manual-cli-relaunch",
		"handledPids":      merged,
	}
	_ = os.MkdirAll(filepath.Dir(cdp.RelaunchGuardFile), 0o700)
	_ = os.WriteFile(cdp.RelaunchGuardFile, shared.MarshalPretty(guard, "  "), 0o600)

	var launch cdp.HostProc
	if len(procs) > 0 {
		launch = procs[0]
	} else {
		ch := "stable"
		if channel == "insiders" {
			ch = "insiders"
		}
		launch.Channel = ch
		switch runtime.GOOS {
		case "darwin":
			if channel == "insiders" {
				launch.AppPath = "/Applications/Antigravity - Insiders.app"
			} else {
				launch.AppPath = firstExisting([]string{"/Applications/Antigravity.app", filepath.Join(home, "Applications/Antigravity.app")})
				if launch.AppPath == "" {
					launch.AppPath = "/Applications/Antigravity.app"
				}
			}
		case "windows":
			launch.ExePath = firstExisting(winExeCandidates(channel, false))
		default:
			launch.Channel = "stable"
			launch.ExePath = firstExisting([]string{"/usr/bin/antigravity", "/usr/local/bin/antigravity", "/opt/Antigravity/antigravity"})
			if launch.ExePath == "" {
				launch.ExePath = "antigravity"
			}
		}
	}

	for _, p := range procs {
		switch {
		case runtime.GOOS == "darwin" && p.AppPath != "":
			shared.RunCmd(3*time.Second, "osascript",
				"-e", "on run argv",
				"-e", "tell application (item 1 of argv) to quit",
				"-e", "end run",
				"--", p.AppPath)
			for i := 0; i < 25 && shared.IsPidAlive(p.Pid); i++ {
				shared.SleepMs(100)
			}
		case runtime.GOOS == "windows":
			cdp.RunPowerShell(5*time.Second, fmt.Sprintf("$gp = Get-Process -Id %d -ErrorAction SilentlyContinue; if ($gp) { [void]$gp.CloseMainWindow(); if (-not $gp.WaitForExit(1800)) { Stop-Process -Id %d -Force -ErrorAction SilentlyContinue } }", p.Pid, p.Pid))
		}
		if shared.IsPidAlive(p.Pid) {
			_ = shared.TerminatePid(p.Pid)
			for i := 0; i < 35 && shared.IsPidAlive(p.Pid); i++ {
				shared.SleepMs(100)
			}
		}
		if shared.IsPidAlive(p.Pid) && runtime.GOOS != "windows" {
			_ = shared.KillPid(p.Pid)
			for i := 0; i < 15 && shared.IsPidAlive(p.Pid); i++ {
				shared.SleepMs(80)
			}
		}
	}
	shared.SleepMs(250)

	switch {
	case runtime.GOOS == "darwin" && launch.AppPath != "":
		shared.RunCmd(5*time.Second, "/usr/bin/open", "-a", launch.AppPath, "--args", "--remote-debugging-port=0")
	case launch.ExePath != "":
		if _, err := shared.SpawnDetached(launch.ExePath, "--remote-debugging-port=0"); err != nil {
			return RelaunchResult{ExitCode: 1, Payload: map[string]any{"ok": false, "reason": "executable_not_found", "error": err.Error()}}
		}
	default:
		return RelaunchResult{ExitCode: 1, Payload: map[string]any{"ok": false, "reason": "executable_not_found"}}
	}

	live := Live{}
	targetCh := launch.Channel
	if targetCh == "" {
		targetCh = channel
	}
	for i := 0; i < 30; i++ {
		shared.SleepMs(500)
		env := DetectEnvironment(targetCh)
		key := launch.Channel
		if key == "" {
			key = "stable"
		}
		if ci := env.Channels[key]; ci != nil && !ci.PortFileExists {
			continue
		}
		if runDaemonOnce != nil {
			runDaemonOnce()
		}
		live = InspectLiveWindow(&env, "", 0)
		if live.Reachable && live.StateBool("hasStyle") {
			break
		}
	}
	ok := live.Reachable && live.StateBool("hasStyle")
	stopped := []int{}
	for _, p := range procs {
		stopped = append(stopped, p.Pid)
	}
	dict := live.StateValue("dictCount")
	if dict == nil {
		dict = 0
	}
	code := 1
	if ok {
		code = 0
	}
	return RelaunchResult{ExitCode: code, Payload: orderedPayload(
		"ok", ok,
		"stoppedPids", stopped,
		"version", live.StateValue("version"),
		"themeStyle", live.StateBool("hasStyle"),
		"dictCount", dict,
		"pillMounted", live.StateBool("pillMounted"),
		"live", live,
	)}
}

func orderedPayload(kv ...any) *shared.OrderedMap {
	om := shared.NewOrderedMap()
	for i := 0; i+1 < len(kv); i += 2 {
		om.Set(kv[i].(string), kv[i+1])
	}
	return om
}

func copyFile(src, dst string, perm os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, perm)
}
