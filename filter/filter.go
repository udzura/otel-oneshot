// Package filter selects and reshapes the spans to display.
//
// The pipeline order is fixed and significant (see DESIGN.md §5):
//
//  1. root resolution   — pick the subtree(s) to display
//  2. hide               — drop spans matching a pattern, reparenting children
//  3. max-depth          — prune descendants beyond a depth limit
//  4. fold               — collapse the subtree of spans matching a pattern
//  5. top-n              — keep only the N longest spans plus their ancestors
//  6. sort               — order siblings by start time or duration
//
// hide, max-depth and fold are applied together in a single reshaping clone so
// that depth is counted against the already-elided tree.
//
// filter never mutates the domain tree it is given: it returns a freshly cloned
// tree so that domain.Trace (and its AllSpans index) stay consistent and tests
// remain hermetic.
package filter

import (
	"fmt"
	"sort"

	"github.com/udzura/otel-oneshot/domain"
)

// Options controls filtering. Zero values mean "no limit" / defaults.
type Options struct {
	RootSpanID   string
	RootSpanName string
	MaxDepth     int    // 0 = unlimited
	TopN         int    // 0 = all
	Sort         string // "start_time" | "duration"

	// FoldPatterns collapse the subtree of any matching span (the span itself
	// stays, its descendants are hidden behind a marker). HidePatterns drop
	// matching spans entirely, reparenting their children onto the nearest
	// surviving ancestor. MatchMode selects how patterns are interpreted:
	// "regex" (default) or "exact". Empty pattern lists are no-ops.
	FoldPatterns []string
	HidePatterns []string
	MatchMode    string
}

// Apply runs the fixed filter pipeline and returns the resulting root spans
// (cloned; safe to mutate). The returned slice may be empty.
func Apply(tr *domain.Trace, opt Options) ([]*domain.Span, error) {
	roots, err := resolveRoots(tr, opt)
	if err != nil {
		return nil, err
	}

	hide, err := compileMatcher(opt.HidePatterns, opt.MatchMode)
	if err != nil {
		return nil, fmt.Errorf("--hide: %w", err)
	}
	fold, err := compileMatcher(opt.FoldPatterns, opt.MatchMode)
	if err != nil {
		return nil, fmt.Errorf("--fold: %w", err)
	}

	// hide + max-depth + fold are applied together during the clone.
	sh := shaper{hide: hide, fold: fold, maxDepth: opt.MaxDepth}
	cloned := sh.forest(roots, 0)

	if opt.TopN > 0 {
		cloned = applyTopN(cloned, opt.TopN)
	}

	sortTree(cloned, opt.Sort)
	return cloned, nil
}

// resolveRoots picks the starting root spans before cloning.
func resolveRoots(tr *domain.Trace, opt Options) ([]*domain.Span, error) {
	switch {
	case opt.RootSpanID != "":
		s, ok := tr.AllSpans[opt.RootSpanID]
		if !ok {
			return nil, fmt.Errorf("root span id %q not found in trace %s", opt.RootSpanID, tr.TraceID)
		}
		return []*domain.Span{s}, nil

	case opt.RootSpanName != "":
		s := findFirstByName(tr.Roots, opt.RootSpanName)
		if s == nil {
			return nil, fmt.Errorf("no span named %q found in trace %s", opt.RootSpanName, tr.TraceID)
		}
		return []*domain.Span{s}, nil

	default:
		return tr.Roots, nil
	}
}

// findFirstByName returns the first span (pre-order, start-time ordered) whose
// name matches.
func findFirstByName(roots []*domain.Span, name string) *domain.Span {
	for _, r := range roots {
		if r.Name == name {
			return r
		}
		if got := findFirstByName(r.Children, name); got != nil {
			return got
		}
	}
	return nil
}

// shaper reshapes the tree during a single cloning pass, applying (in this
// order per node) hide, then max-depth, then fold. It never mutates the input.
type shaper struct {
	hide     *matcher // nil = no hiding
	fold     *matcher // nil = no folding
	maxDepth int      // 0 = unlimited
}

// forest reshapes a sibling list at the given depth. hide can expand a single
// input node into zero or more output nodes (its lifted children), so this
// returns a flat slice.
func (sh shaper) forest(spans []*domain.Span, relDepth int) []*domain.Span {
	var out []*domain.Span
	for _, s := range spans {
		out = append(out, sh.node(s, relDepth)...)
	}
	return out
}

// node reshapes a single span. A hidden span contributes its lifted children in
// place of itself; every other span yields exactly one cloned node.
func (sh shaper) node(s *domain.Span, relDepth int) []*domain.Span {
	if sh.hide.match(s.Name) {
		// Drop this span; its children move up to take its place.
		return sh.forest(s.Children, relDepth)
	}

	c := *s // shallow copy of scalar/map/slice fields
	c.Depth = relDepth
	c.Children = nil
	c.HiddenChildren = 0
	c.HiddenByFold = false

	// max-depth wins over fold: once at the limit, nothing deeper is shown.
	if sh.maxDepth != 0 && relDepth >= sh.maxDepth {
		c.HiddenChildren = sh.visibleChildCount(s.Children)
		return []*domain.Span{&c}
	}

	if sh.fold.match(s.Name) {
		c.HiddenChildren = sh.visibleChildCount(s.Children)
		c.HiddenByFold = true
		return []*domain.Span{&c}
	}

	c.Children = sh.forest(s.Children, relDepth+1)
	return []*domain.Span{&c}
}

// visibleChildCount counts how many direct children would remain after hiding,
// so a fold/max-depth marker reports the count the viewer would actually see.
// It only recurses through hidden nodes (to reach the children they lift up),
// not through the whole subtree.
func (sh shaper) visibleChildCount(spans []*domain.Span) int {
	n := 0
	for _, s := range spans {
		if sh.hide.match(s.Name) {
			n += sh.visibleChildCount(s.Children)
		} else {
			n++
		}
	}
	return n
}

// applyTopN keeps only the TopN longest spans plus every ancestor needed to
// keep them connected to a root. Ancestors are retained even if they are not
// themselves in the top N (otherwise the tree would fall apart).
func applyTopN(roots []*domain.Span, n int) []*domain.Span {
	// Flatten.
	var all []*domain.Span
	var walk func(s *domain.Span)
	walk = func(s *domain.Span) {
		all = append(all, s)
		for _, c := range s.Children {
			walk(c)
		}
	}
	for _, r := range roots {
		walk(r)
	}

	if len(all) <= n {
		return roots
	}

	// Rank by duration desc; stable tie-break by start then span id.
	ranked := make([]*domain.Span, len(all))
	copy(ranked, all)
	sort.SliceStable(ranked, func(i, j int) bool {
		di, dj := ranked[i].DurationNanos(), ranked[j].DurationNanos()
		if di != dj {
			return di > dj
		}
		if ranked[i].StartNanos != ranked[j].StartNanos {
			return ranked[i].StartNanos < ranked[j].StartNanos
		}
		return ranked[i].SpanID < ranked[j].SpanID
	})

	keep := make(map[string]bool, n)
	for _, s := range ranked[:n] {
		keep[s.SpanID] = true
	}

	// Force-keep ancestors of kept spans.
	parentOf := make(map[string]*domain.Span, len(all))
	for _, s := range all {
		for _, c := range s.Children {
			parentOf[c.SpanID] = s
		}
	}
	for _, s := range all {
		if !keep[s.SpanID] {
			continue
		}
		for p := parentOf[s.SpanID]; p != nil && !keep[p.SpanID]; p = parentOf[p.SpanID] {
			keep[p.SpanID] = true
		}
	}

	// Rebuild child lists, dropping unkept branches.
	var prune func(s *domain.Span) *domain.Span
	prune = func(s *domain.Span) *domain.Span {
		if !keep[s.SpanID] {
			return nil
		}
		kept := s.Children[:0:0]
		for _, c := range s.Children {
			if pc := prune(c); pc != nil {
				kept = append(kept, pc)
			}
		}
		s.Children = kept
		return s
	}
	var out []*domain.Span
	for _, r := range roots {
		if pr := prune(r); pr != nil {
			out = append(out, pr)
		}
	}
	return out
}

// sortTree orders siblings (and roots) by the chosen key, recursively.
func sortTree(roots []*domain.Span, key string) {
	less := lessByStart
	if key == "duration" {
		less = lessByDuration
	}
	var rec func(ss []*domain.Span)
	rec = func(ss []*domain.Span) {
		sort.SliceStable(ss, less(ss))
		for _, s := range ss {
			rec(s.Children)
		}
	}
	rec(roots)
}

func lessByStart(ss []*domain.Span) func(i, j int) bool {
	return func(i, j int) bool {
		if ss[i].StartNanos != ss[j].StartNanos {
			return ss[i].StartNanos < ss[j].StartNanos
		}
		return ss[i].SpanID < ss[j].SpanID
	}
}

func lessByDuration(ss []*domain.Span) func(i, j int) bool {
	return func(i, j int) bool {
		di, dj := ss[i].DurationNanos(), ss[j].DurationNanos()
		if di != dj {
			return di > dj // longest first
		}
		if ss[i].StartNanos != ss[j].StartNanos {
			return ss[i].StartNanos < ss[j].StartNanos
		}
		return ss[i].SpanID < ss[j].SpanID
	}
}
