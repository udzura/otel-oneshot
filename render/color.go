package render

import (
	"os"

	"golang.org/x/term"
)

// ANSI escape sequences we use. Kept minimal and applied by Styler only.
const (
	ansiReset = "\x1b[0m"
	ansiRed   = "\x1b[31m"
	ansiDim   = "\x1b[2m"
)

// Styler applies ANSI styling, or passes text through untouched when disabled.
type Styler struct {
	enabled bool
}

// NewStyler decides whether color is on, following --color and, for "auto",
// the TTY state of out plus the NO_COLOR convention.
func NewStyler(mode string, out *os.File) Styler {
	switch mode {
	case "always":
		return Styler{enabled: true}
	case "never":
		return Styler{enabled: false}
	default: // auto
		if os.Getenv("NO_COLOR") != "" {
			return Styler{enabled: false}
		}
		return Styler{enabled: isTerminal(out)}
	}
}

// Enabled reports whether styling is active.
func (s Styler) Enabled() bool { return s.enabled }

// Red wraps text in red (used for ERROR spans).
func (s Styler) Red(text string) string { return s.wrap(ansiRed, text) }

// Dim wraps text in dim (used for UNSET spans and the scale row).
func (s Styler) Dim(text string) string { return s.wrap(ansiDim, text) }

func (s Styler) wrap(code, text string) string {
	if !s.enabled || text == "" {
		return text
	}
	return code + text + ansiReset
}

// ResolveWidth returns the output width: the explicit config value if > 0,
// otherwise the detected terminal width, otherwise 120.
func ResolveWidth(configWidth int, out *os.File) int {
	if configWidth > 0 {
		return configWidth
	}
	if w := terminalWidth(out); w > 0 {
		return w
	}
	return 120
}

func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func terminalWidth(f *os.File) int {
	if f == nil {
		return 0
	}
	w, _, err := term.GetSize(int(f.Fd()))
	if err != nil || w <= 0 {
		return 0
	}
	return w
}
