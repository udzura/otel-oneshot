// Package domain defines the internal data model for otel-oneshot.
//
// This package has no dependencies on other packages in this module and knows
// nothing about UI concerns such as terminal width or ANSI colors. It is the
// stable core that the rest of the one-directional pipeline
// (otlp -> domain -> filter -> layout -> render) builds on.
package domain

import "sort"

// StatusCode mirrors the OTLP span status code, reduced to the three cases we
// care about for rendering.
type StatusCode int

const (
	StatusUnset StatusCode = iota
	StatusOK
	StatusError
)

// Event is a single span event (a point-in-time annotation on a span).
type Event struct {
	TimeNanos  uint64
	Name       string
	Attributes map[string]string
}

// Span is the internal representation of a single OTLP span.
//
// Children and Depth are populated by BuildTree and are meaningless before it
// runs.
type Span struct {
	SpanID       string
	ParentSpanID string
	TraceID      string
	Name         string
	ServiceName  string
	StartNanos   uint64
	EndNanos     uint64
	Attributes   map[string]string
	Events       []Event
	Status       StatusCode

	Children []*Span // set by BuildTree
	Depth    int     // set by BuildTree (root == 0)

	// HiddenChildren is the number of direct children that were pruned by a
	// depth limit or a fold pattern. It is set by the filter layer (0
	// otherwise) and lets the renderer show a "... (N children hidden)" marker.
	HiddenChildren int

	// HiddenByFold distinguishes why HiddenChildren were pruned: true when the
	// span matched a --fold pattern (subtree collapsed), false when the prune
	// came from --max-depth. Only meaningful when HiddenChildren > 0.
	HiddenByFold bool
}

// DurationNanos returns the span duration, clamped at 0 for spans whose end is
// not strictly after their start (clock skew, missing end time, etc.).
func (s *Span) DurationNanos() uint64 {
	if s.EndNanos <= s.StartNanos {
		return 0
	}
	return s.EndNanos - s.StartNanos
}

// Trace bundles the spans of a single trace after parsing.
type Trace struct {
	TraceID  string
	Roots    []*Span          // spans whose parent is empty or not present
	AllSpans map[string]*Span // spanID -> Span
	MinStart uint64
	MaxEnd   uint64
}

// BuildTree wires up parent/child relationships, assigns depth, sorts children
// by start time, and computes the trace time bounds.
//
// A span is treated as a root when its ParentSpanID is empty or refers to a
// span that is not present in the input (a common situation when only a subtree
// of a larger trace was exported).
func BuildTree(traceID string, spans []*Span) *Trace {
	t := &Trace{
		TraceID:  traceID,
		AllSpans: make(map[string]*Span, len(spans)),
	}
	for _, s := range spans {
		t.AllSpans[s.SpanID] = s
	}

	for _, s := range spans {
		parent, ok := t.AllSpans[s.ParentSpanID]
		if s.ParentSpanID == "" || !ok {
			t.Roots = append(t.Roots, s)
			continue
		}
		parent.Children = append(parent.Children, s)
	}

	// Stable display order: roots and every child list sorted by start time.
	sortSpansByStart(t.Roots)
	for _, s := range t.AllSpans {
		sortSpansByStart(s.Children)
	}

	// Assign depth via BFS from the roots. Guard against cyclic parent links
	// (malformed input) by only visiting each span once.
	visited := make(map[string]bool, len(spans))
	queue := make([]*Span, 0, len(t.Roots))
	for _, r := range t.Roots {
		r.Depth = 0
		queue = append(queue, r)
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if visited[cur.SpanID] {
			continue
		}
		visited[cur.SpanID] = true
		for _, c := range cur.Children {
			if visited[c.SpanID] {
				continue
			}
			c.Depth = cur.Depth + 1
			queue = append(queue, c)
		}
	}

	// Time bounds over every span (not just reachable ones).
	first := true
	for _, s := range spans {
		if first {
			t.MinStart, t.MaxEnd = s.StartNanos, s.EndNanos
			first = false
			continue
		}
		if s.StartNanos < t.MinStart {
			t.MinStart = s.StartNanos
		}
		if s.EndNanos > t.MaxEnd {
			t.MaxEnd = s.EndNanos
		}
	}

	return t
}

func sortSpansByStart(spans []*Span) {
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].StartNanos != spans[j].StartNanos {
			return spans[i].StartNanos < spans[j].StartNanos
		}
		// Tie-break on span ID for deterministic output.
		return spans[i].SpanID < spans[j].SpanID
	})
}
