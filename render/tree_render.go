package render

import (
	"strings"

	"github.com/udzura/otel-oneshot/domain"
	"github.com/udzura/otel-oneshot/layout"
)

// treeCell builds the left pane for one row: the box-drawing branch prefix plus
// the (possibly truncated, possibly colored) span label, padded to treeWidth
// display columns.
func treeCell(r layout.Row, treeWidth int, opt Options, st Styler) string {
	prefix := branchPrefix(r)
	nameCell := treeWidth - runeWidth(prefix)
	if nameCell < 1 {
		nameCell = 1
	}

	label := spanLabel(r.Span, opt)
	label = truncate(label, nameCell)
	label = padRight(label, nameCell)

	return prefix + colorizeByStatus(label, r.Span.Status, opt, st)
}

// branchPrefix assembles "│  "/"   " for each ancestor followed by the node's
// own "├─ " or "└─ " connector.
func branchPrefix(r layout.Row) string {
	var b strings.Builder
	for _, ancestorLast := range r.AncestorsLast {
		if ancestorLast {
			b.WriteString("   ")
		} else {
			b.WriteString("│  ")
		}
	}
	if r.IsLast {
		b.WriteString("└─ ")
	} else {
		b.WriteString("├─ ")
	}
	return b.String()
}

// spanLabel is the plain-text label: name + optional [ERROR] + optional attrs.
func spanLabel(s *domain.Span, opt Options) string {
	label := s.Name
	if opt.HighlightErrors && s.Status == domain.StatusError {
		label += " [ERROR]"
	}
	if ann := layout.AttributeAnnotation(s, opt.ShowAttributes); ann != "" {
		label += " " + ann
	}
	return label
}

// hiddenChildrenLine renders the pseudo-row shown under a node whose children
// were pruned by --max-depth.
func hiddenChildrenLine(r layout.Row, treeWidth int, st Styler) string {
	// The hidden marker sits one level deeper than the node, as its last child.
	var b strings.Builder
	for _, ancestorLast := range r.AncestorsLast {
		if ancestorLast {
			b.WriteString("   ")
		} else {
			b.WriteString("│  ")
		}
	}
	// The node itself becomes an ancestor line for the marker.
	if r.IsLast {
		b.WriteString("   ")
	} else {
		b.WriteString("│  ")
	}
	b.WriteString("└─ ")

	text := pluralHidden(r.HiddenChildren)
	cell := padRight(truncate(b.String()+text, treeWidth), treeWidth)
	return st.Dim(cell)
}

func pluralHidden(n int) string {
	if n == 1 {
		return "... (1 child hidden by --max-depth)"
	}
	return "... (" + itoa(n) + " children hidden by --max-depth)"
}
