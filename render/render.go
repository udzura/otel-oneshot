// Package render turns a layout.Layout into the final ASCII string written to
// stdout. It is the only package that knows about terminal width detection,
// ANSI colors and box-drawing glyphs; everything upstream deals in plain data.
package render

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/udzura/otel-oneshot/layout"
	"github.com/udzura/otel-oneshot/timefmt"
)

// Options carries the render-affecting configuration.
type Options struct {
	HighlightErrors bool
	ShowAttributes  []string
}

// Meta is the header information for the trace being rendered.
type Meta struct {
	TraceID  string
	RootName string // "" or "N roots" when there is not a single named root
}

// Render writes the full timeline to w.
func Render(w io.Writer, l layout.Layout, meta Meta, opt Options, st Styler) error {
	var b strings.Builder

	// Header.
	total := timefmt.Format(l.ViewEnd-l.ViewStart, l.Unit)
	if meta.RootName != "" {
		fmt.Fprintf(&b, "Trace: %s  (root: %s, duration: %s)\n\n", meta.TraceID, meta.RootName, total)
	} else {
		fmt.Fprintf(&b, "Trace: %s  (duration: %s)\n\n", meta.TraceID, total)
	}

	// Scale row.
	b.WriteString(scaleRow(l, st))
	b.WriteByte('\n')

	// Span rows.
	for _, r := range l.Rows {
		line := treeCell(r, l.TreeWidth, opt, st) +
			" " +
			barCell(r, l.BarWidth, opt, st) +
			" " +
			padLeft(r.DurationLabel, l.DurWidth)
		b.WriteString(strings.TrimRight(line, " "))
		b.WriteByte('\n')

		if r.HiddenChildren > 0 {
			b.WriteString(strings.TrimRight(hiddenChildrenLine(r, l.TreeWidth, st), " "))
			b.WriteByte('\n')
		}
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// scaleRow builds the dim time-axis row aligned with the bar pane.
func scaleRow(l layout.Layout, st Styler) string {
	head := padRight("span", l.TreeWidth)

	viewDur := l.ViewEnd - l.ViewStart
	buf := make([]rune, l.BarWidth)
	for i := range buf {
		buf[i] = ' '
	}

	n := tickCount(l.BarWidth)
	for i := 0; i < n; i++ {
		var off uint64
		var pos int
		if n > 1 {
			off = viewDur * uint64(i) / uint64(n-1)
			pos = l.BarWidth * i / (n - 1)
		}
		label := timefmt.Format(off, l.Unit)
		align := alignCenter
		if i == 0 {
			align = alignLeft
		} else if i == n-1 {
			align = alignRight
		}
		placeLabel(buf, pos, label, align)
	}

	return st.Dim(head + " " + string(buf))
}

func tickCount(barWidth int) int {
	switch {
	case barWidth >= 72:
		return 5
	case barWidth >= 24:
		return 3
	default:
		return 2
	}
}

type align int

const (
	alignLeft align = iota
	alignCenter
	alignRight
)

// placeLabel writes text into buf near pos, honoring the alignment and never
// overwriting non-space runes (so labels don't clobber each other) or running
// out of bounds.
func placeLabel(buf []rune, pos int, text string, a align) {
	rs := []rune(text)
	n := len(rs)
	if n == 0 || len(buf) == 0 {
		return
	}
	var start int
	switch a {
	case alignLeft:
		start = pos
	case alignRight:
		start = pos - n
	default:
		start = pos - n/2
	}
	if start < 0 {
		start = 0
	}
	if start+n > len(buf) {
		start = len(buf) - n
	}
	if start < 0 {
		return
	}
	for i := 0; i < n; i++ {
		if buf[start+i] != ' ' {
			return // would collide with an already-placed label; skip
		}
	}
	for i := 0; i < n; i++ {
		buf[start+i] = rs[i]
	}
}

// --- small display-width string helpers (rune-count based) ---

func runeWidth(s string) int { return utf8.RuneCountInString(s) }

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= w {
		return s
	}
	rs := []rune(s)
	if w <= 3 {
		return string(rs[:w])
	}
	return string(rs[:w-3]) + "..."
}

func padRight(s string, w int) string {
	n := utf8.RuneCountInString(s)
	if n >= w {
		return s
	}
	return s + strings.Repeat(" ", w-n)
}

func padLeft(s string, w int) string {
	n := utf8.RuneCountInString(s)
	if n >= w {
		return s
	}
	return strings.Repeat(" ", w-n) + s
}

func itoa(n int) string { return strconv.Itoa(n) }
