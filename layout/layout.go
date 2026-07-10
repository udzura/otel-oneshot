// Package layout flattens the filtered span tree into an ordered list of Rows
// and computes every column position. The Row list is the single source of
// truth shared by the tree pane and the bar pane, so they can never drift out
// of alignment.
//
// layout deals only in integer column geometry. It does not detect the terminal
// or emit any ANSI/box-drawing bytes — those are render's job. The total width
// is passed in as a resolved integer.
package layout

import (
	"unicode/utf8"

	"github.com/udzura/otel-oneshot/domain"
	"github.com/udzura/otel-oneshot/timefmt"
)

const (
	minBarWidth  = 10
	minTreeWidth = 8
	sepWidth     = 1 // space between tree pane and bar pane
	gapWidth     = 1 // space between bar pane and duration label
)

// Row is one displayed span. Tree rendering and bar rendering both index into
// the same Row slice.
type Row struct {
	Span          *domain.Span
	Depth         int
	IsLast        bool   // last among its siblings (for └─ vs ├─)
	AncestorsLast []bool // for each ancestor (root-first): was it the last child?

	StartCol int // bar start column within the bar pane [0, BarWidth)
	EndCol   int // bar end column within the bar pane (StartCol, BarWidth]

	HiddenChildren int    // > 0 => renderer shows "... (N children hidden)"
	DurationLabel  string // pre-formatted, e.g. "842ms"
}

// Layout is the fully computed drawing plan.
type Layout struct {
	Rows       []Row
	TotalWidth int
	TreeWidth  int
	BarWidth   int
	DurWidth   int // width reserved for the duration label column
	ViewStart  uint64
	ViewEnd    uint64
	Unit       timefmt.Unit
}

// Options configures layout.
type Options struct {
	TotalWidth      int      // resolved output width in columns
	TimeUnit        string   // "auto" | "s" | "ms" | "us" | "ns"
	ShowAttributes  []string // attribute keys appended to the label
	HighlightErrors bool     // whether "[ERROR]" contributes to label width
}

// Build produces the Layout for the given filtered roots.
func Build(roots []*domain.Span, opt Options) Layout {
	l := Layout{TotalWidth: opt.TotalWidth}

	// 1. Flatten depth-first, preserving tree order.
	flatten(roots, nil, &l.Rows)
	if len(l.Rows) == 0 {
		return l
	}

	// 2. View time bounds over the flattened spans.
	l.ViewStart, l.ViewEnd = timeBounds(l.Rows)
	viewDur := l.ViewEnd - l.ViewStart
	if viewDur == 0 {
		viewDur = 1 // avoid divide-by-zero; every bar collapses to 1 char
	}

	// 3. Duration labels + unit (needs the total view duration for auto).
	l.Unit = timefmt.ResolveUnit(opt.TimeUnit, l.ViewEnd-l.ViewStart)
	for i := range l.Rows {
		l.Rows[i].DurationLabel = timefmt.Format(l.Rows[i].Span.DurationNanos(), l.Unit)
	}
	l.DurWidth = maxDurWidth(l.Rows)

	// 4. Pane widths (duration column is explicitly reserved so lines never
	//    exceed TotalWidth — this fixes the gap in DESIGN.md §6.3).
	l.TreeWidth, l.BarWidth = paneWidths(l.Rows, opt, l.DurWidth)

	// 5. Bar columns.
	for i := range l.Rows {
		s := l.Rows[i].Span
		start := colOf(s.StartNanos, l.ViewStart, viewDur, l.BarWidth)
		end := colOf(s.EndNanos, l.ViewStart, viewDur, l.BarWidth)
		if start > l.BarWidth-1 {
			start = l.BarWidth - 1
		}
		if start < 0 {
			start = 0
		}
		if end < start+1 {
			end = start + 1 // guarantee a visible 1-char bar
		}
		if end > l.BarWidth {
			end = l.BarWidth
		}
		l.Rows[i].StartCol = start
		l.Rows[i].EndCol = end
	}

	return l
}

func flatten(spans []*domain.Span, ancestorsLast []bool, out *[]Row) {
	for i, s := range spans {
		isLast := i == len(spans)-1
		row := Row{
			Span:           s,
			Depth:          len(ancestorsLast),
			IsLast:         isLast,
			AncestorsLast:  append([]bool(nil), ancestorsLast...),
			HiddenChildren: s.HiddenChildren,
		}
		*out = append(*out, row)
		flatten(s.Children, append(ancestorsLast, isLast), out)
	}
}

func timeBounds(rows []Row) (min, max uint64) {
	min, max = rows[0].Span.StartNanos, rows[0].Span.EndNanos
	for _, r := range rows {
		if r.Span.StartNanos < min {
			min = r.Span.StartNanos
		}
		if r.Span.EndNanos > max {
			max = r.Span.EndNanos
		}
	}
	return min, max
}

// colOf maps a timestamp to a bar column using integer math (rounded), never
// routing the uint64 nanos through float64.
func colOf(t, viewStart, viewDur uint64, barWidth int) int {
	if barWidth <= 0 {
		return 0
	}
	if t <= viewStart {
		return 0
	}
	off := t - viewStart
	// round( off * barWidth / viewDur )
	c := (off*uint64(barWidth) + viewDur/2) / viewDur
	if c > uint64(barWidth) {
		c = uint64(barWidth)
	}
	return int(c)
}

// paneWidths decides the tree and bar column counts.
func paneWidths(rows []Row, opt Options, durWidth int) (treeW, barW int) {
	maxLabel := 0
	for _, r := range rows {
		w := prefixWidth(r.Depth) + labelWidth(r, opt)
		if w > maxLabel {
			maxLabel = w
		}
	}

	total := opt.TotalWidth
	reserve := sepWidth + gapWidth + durWidth
	avail := total - reserve
	if avail < minTreeWidth+1 {
		// Extremely narrow output: degrade gracefully.
		avail = minTreeWidth + 1
	}

	// Cap the tree pane at 40% of total, and at the longest label (+1 pad).
	cap40 := total * 40 / 100
	treeW = maxLabel + 1
	if treeW > cap40 {
		treeW = cap40
	}
	// Ensure the bar pane keeps at least minBarWidth columns.
	if treeW > avail-minBarWidth {
		treeW = avail - minBarWidth
	}
	if treeW < minTreeWidth {
		treeW = minTreeWidth
	}
	if treeW > avail-1 {
		treeW = avail - 1
	}
	if treeW < 1 {
		treeW = 1
	}

	barW = avail - treeW
	if barW < 1 {
		barW = 1
	}
	return treeW, barW
}

// prefixWidth is the column count of the tree branch prefix for a node at the
// given depth: three columns per ancestor plus the node's own connector.
func prefixWidth(depth int) int { return (depth + 1) * 3 }

// labelWidth is the display width of a row's text label (name + optional
// [ERROR] tag + optional attribute annotations), measured in runes.
func labelWidth(r Row, opt Options) int {
	w := utf8.RuneCountInString(r.Span.Name)
	if opt.HighlightErrors && r.Span.Status == domain.StatusError {
		w += len(" [ERROR]")
	}
	if ann := AttributeAnnotation(r.Span, opt.ShowAttributes); ann != "" {
		w += 1 + utf8.RuneCountInString(ann)
	}
	return w
}

func maxDurWidth(rows []Row) int {
	w := len("dur") // header minimum
	for _, r := range rows {
		if n := utf8.RuneCountInString(r.DurationLabel); n > w {
			w = n
		}
	}
	return w
}

// AttributeAnnotation builds the "(k=v, k2=v2)" suffix for the requested
// attribute keys that are present on the span. Returns "" if none apply.
func AttributeAnnotation(s *domain.Span, keys []string) string {
	if len(keys) == 0 || len(s.Attributes) == 0 {
		return ""
	}
	out := ""
	for _, k := range keys {
		v, ok := s.Attributes[k]
		if !ok {
			continue
		}
		if out != "" {
			out += ", "
		}
		out += k + "=" + v
	}
	if out == "" {
		return ""
	}
	return "(" + out + ")"
}
