# Security Policy

## Supported Versions

Only the latest published release receives security fixes.

## Reporting a Vulnerability

Please do not open a public GitHub issue for security vulnerabilities.

Report privately via [GitHub Security Advisories](https://github.com/Spanless/AgyTheme/security/advisories/new) with reproduction steps and affected versions.

## Security Model & Known Boundaries

- **Local CDP debugging port**: AgyTheme injects UI patches into Google Antigravity through Electron's `--remote-debugging-port` interface. Enabling this port allows any process running under the same OS user account to attach to Antigravity windows via CDP.
- **Loopback-only services**: The sidecar HTTP panel binds exclusively to `127.0.0.1`, requires the host-issued `ANTIGRAVITY_SIDECAR_UI_TOKEN`, and restricts mutating requests with `HttpOnly; SameSite=Strict` cookies plus a custom request header. CDP WebSocket and Language Server RPC clients only connect to loopback hosts.
- **Updates**: `agytheme update` downloads releases only over HTTPS from official GitHub release hosts and verifies the archive's SHA-256 against `SHA256SUMS` before replacing the binary.
- **User extensions**: `user/custom.js` runs inside the Antigravity renderer by design. Treat `user/` files as trusted code; use `agytheme verify --safe-mode` to bypass them.
