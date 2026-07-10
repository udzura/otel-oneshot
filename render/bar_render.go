package render

import (
	"strings"

	"github.com/udzura/otel-oneshot/domain"
	"github.com/udzura/otel-oneshot/layout"
)

// barGlyph is the timeline bar fill character.
const barGlyph = '▓'

// barCell renders the timeline bar for one row within barWidth columns.
func barCell(r layout.Row, barWidth int, opt Options, st Styler) string {
	if barWidth <= 0 {
		return ""
	}
	start, end := r.StartCol, r.EndCol
	if start < 0 {
		start = 0
	}
	if end > barWidth {
		end = barWidth
	}
	if end <= start {
		end = start + 1
	}

	var b strings.Builder
	b.WriteString(strings.Repeat(" ", start))
	bar := strings.Repeat(string(barGlyph), end-start)
	b.WriteString(colorizeByStatus(bar, r.Span.Status, opt, st))
	if trailing := barWidth - end; trailing > 0 {
		b.WriteString(strings.Repeat(" ", trailing))
	}
	return b.String()
}

// colorizeByStatus applies the status color to text (no-op when styling off):
// ERROR -> red, UNSET -> dim, OK -> unchanged.
func colorizeByStatus(text string, status domain.StatusCode, opt Options, st Styler) string {
	switch status {
	case domain.StatusError:
		if opt.HighlightErrors {
			return st.Red(text)
		}
		return text
	case domain.StatusUnset:
		return st.Dim(text)
	default:
		return text
	}
}
