package agytheme

import "embed"

// Release builds override Version via -ldflags "-X github.com/Spanless/AgyTheme.Version=x.y.z".
var Version = "0.1.0"

//go:embed core/runtime/*.js core/styles/*.css core/i18n.json core/pane/*.js core/pane/*.css
var CoreFS embed.FS

//go:embed user/config.json user/custom.css user/custom.js user/i18n.override.json
var UserFS embed.FS

//go:embed SKILL.md
var PluginSkill []byte

//go:embed assets/plugin/logo.svg
var PluginLogo []byte
