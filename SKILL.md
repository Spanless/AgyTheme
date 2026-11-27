---
name: agytheme
description: Install, verify, configure, roll back, or uninstall AgyTheme (light/dark themes, Chinese UI, and live usage telemetry for Google Antigravity) on macOS, Windows, and Linux.
---

# AgyTheme

Single Go binary running as a Google Antigravity plugin sidecar. Antigravity starts and stops it with the app; no extra runtime or system autostart entry is required.

## Rules

- Do not install Go, Node.js, Bun, or packages; do not clone or build from source.
- Do not copy binaries manually, modify `PATH`, or create aliases, shortcuts, login items, scheduled tasks, or system services.
- Do not edit Antigravity app files, `argv.json`, or settings manually.
- Pass `--with-launcher` only if requested (macOS Dock launcher), `--sync-user` only to reset user settings, and `--only` / `--without` for partial features.
- Extract release zips into a temporary directory and delete it after installation.

## 1. Install or upgrade

| System | Binary |
| :--- | :--- |
| macOS arm64 | `bin/agytheme-darwin-arm64` |
| macOS x86_64 | `bin/agytheme-darwin-amd64` |
| Windows x64 / arm64 | `bin\agytheme-windows-amd64.exe` / `bin\agytheme-windows-arm64.exe` |
| Linux x86_64 / aarch64 | `bin/agytheme-linux-amd64` / `bin/agytheme-linux-arm64` |

1. Extract the zip via CLI:
   - macOS / Linux: `unzip -q <zip> -d "$TMPDIR/agytheme-install"`
   - Windows: `Expand-Archive <zip> "$env:TEMP\agytheme-install" -Force`
2. Run install:
   - macOS / Linux: `chmod +x <dir>/agytheme/bin/<binary> && <dir>/agytheme/bin/<binary> install`
   - Windows: `& "<dir>\agytheme\bin\<binary>" install`
3. Relay the final stderr summary line to the user.
4. Remove the temporary extraction directory.

Re-running `install` with a newer zip upgrades in place and preserves `user/`.

**Activation (`activation` field):**

- `not_needed` / `relaunched`: active immediately.
- `scheduled`: restarts automatically after ~30 seconds of idle time when triggered inside Antigravity (`installer_inside_antigravity`) or during an active conversation (`active_conversation`). Never run `relaunch --force` or kill Antigravity from inside Antigravity.
- `disabled` / `relaunch_failed`: ask the user to restart Antigravity manually.

**Flags:**

- `--only theme,i18n,metrics,fixes` / `--without <list>`
- `--preset light|dark`
- `--sync-user`
- `--channel auto|stable|insiders`
- `--no-relaunch`
- `--with-launcher` (macOS)
- `--target-dir <dir>` / `--plugins-dir <dir>`

## 2. Paths

| Item | Path |
| :--- | :--- |
| Plugin dir | `~/.gemini/config/plugins/agytheme` |
| Binary | `<plugin>/bin/agytheme` (`.exe`); rollback copy `agytheme.prev` |
| Sidecar manifest | `<plugin>/sidecars/agytheme/sidecar.json` |
| Data dir | `~/.gemini/antigravity/agytheme` |
| User config | `<data>/user/config.json`, `custom.css`, `custom.js`, `i18n.override.json` |
| Installed features | `<data>/state.json` |
| Log | `<data>/logs/sidecar.log` |
| Pending restart | `<data>/pending-activation.json` |

Plugin CLI: `agy plugin list`, `agy plugin disable agytheme`, `agy plugin enable agytheme`.

## 3. Verify

Let `AGY` be `~/.gemini/config/plugins/agytheme/bin/agytheme` (Windows: `%USERPROFILE%\.gemini\config\plugins\agytheme\bin\agytheme.exe`).

Run `AGY doctor`:

- `ok === true` (`antigravity.cdpStatus === "missing_opt_in_flag"` with `install.pendingActivation === true` means restart is pending).
- `install.binaryInstalled`, `install.sidecarConfigured`, `install.plugin.enabled` are `true`, and `install.sidecarPid` is present while Antigravity is running.
- `liveWindow.reachable` and `liveWindow.state.hasStyle` are `true`; with `i18n`, `dictCount >= 1800`; with `metrics` in an active chat, `pillMounted === true`.

`AGY verify` runs a single injection check. `AGY verify --safe-mode` ignores `user/`.

## 4. Features and config

| Feature | Config keys | Scope |
| :--- | :--- | :--- |
| `theme` | `modules.theme` | Light and dark themes following Antigravity appearance |
| `i18n` | `modules.i18n` | Chinese UI strings (model and effort names stay English) |
| `metrics` | `modules.metrics` | Input status bar, native side pane (`用量统计`, `Alt+Shift+M`), per-turn stats |
| `fixes` | `modules.deadlockHealer`, `modules.zoomShortcuts` | Settings modal fix and `Cmd/Ctrl + -/=/0` zoom |

`<data>/user/config.json` hot-reloads in ~2.5 s:

- `enabled`: `true` | `false`
- `preset`: `"light"` | `"dark"`
- `telemetryLayout`: `"compact"` | `"detailed"`
- `telemetryCacheMode`: `"session"` | `"step"`
- `contextLimit`: `"auto"` | number
- `modules.cdpAutoRelaunch`: `true` | `false`
- `themeVars`: `{ "<css-var>": "<value>" }`

## 5. Roll back and uninstall

```bash
AGY rollback
AGY uninstall [--purge]
```

`rollback` swaps in `agytheme.prev` (run again to revert). `uninstall` stops the sidecar, restores the original UI, and removes the plugin; `--purge` also deletes `<data>`.
