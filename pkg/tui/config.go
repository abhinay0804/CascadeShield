package tui

import (
	"time"
)

// Config holds all tunables for the Terminal UI (TUI) engine.
//
// ZERO HARDCODING POLICY: Every refresh rate, dimension, and formatting
// option is configurable here with sensible defaults.
type Config struct {
	// RefreshInterval is how often the TUI updates its view from background components.
	// Default: 500ms
	RefreshInterval time.Duration

	// MaxTopCascadePaths is the maximum number of predicted failure paths to display.
	// Default: 3
	MaxTopCascadePaths int

	// MinWidth is the minimum terminal width in characters required for full view.
	// Default: 80
	MinWidth int

	// MinHeight is the minimum terminal height in rows required for full view.
	// Default: 24
	MinHeight int

	// EnableColor toggles ANSI color highlighting on or off.
	// Default: true
	EnableColor bool
}

// DefaultConfig returns production-ready TUI default settings.
func DefaultConfig() Config {
	return Config{
		RefreshInterval:    500 * time.Millisecond,
		MaxTopCascadePaths: 3,
		MinWidth:           80,
		MinHeight:          24,
		EnableColor:        true,
	}
}
