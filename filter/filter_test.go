package filter

import (
	"testing"

	"github.com/udzura/otel-oneshot/domain"
)

// buildTrace makes a small trace:
//
//	root(0-100)
//	├─ a(10-90)  [dur 80]
//	│  └─ a1(20-30) [dur 10]
//	└─ b(40-50)  [dur 10]
func buildTrace() *domain.Trace {
	spans := []*domain.Span{
		{SpanID: "root", ParentSpanID: "", TraceID: "t", Name: "root", StartNanos: 0, EndNanos: 100},
		{SpanID: "a", ParentSpanID: "root", TraceID: "t", Name: "a", StartNanos: 10, EndNanos: 90},
		{SpanID: "a1", ParentSpanID: "a", TraceID: "t", Name: "a1", StartNanos: 20, EndNanos: 30},
		{SpanID: "b", ParentSpanID: "root", TraceID: "t", Name: "b", StartNanos: 40, EndNanos: 50},
	}
	return domain.BuildTree("t", spans)
}

func countSpans(roots []*domain.Span) int {
	n := 0
	var walk func(*domain.Span)
	walk = func(s *domain.Span) {
		n++
		for _, c := range s.Children {
			walk(c)
		}
	}
	for _, r := range roots {
		walk(r)
	}
	return n
}

func find(roots []*domain.Span, id string) *domain.Span {
	for _, r := range roots {
		if r.SpanID == id {
			return r
		}
		if got := find(r.Children, id); got != nil {
			return got
		}
	}
	return nil
}

func TestRootResolveByID(t *testing.T) {
	tr := buildTrace()
	roots, err := Apply(tr, Options{RootSpanID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0].SpanID != "a" {
		t.Fatalf("want single root a, got %d roots", len(roots))
	}
	if roots[0].Depth != 0 {
		t.Errorf("subtree root depth = %d, want 0", roots[0].Depth)
	}
	if countSpans(roots) != 2 { // a + a1
		t.Errorf("subtree span count = %d, want 2", countSpans(roots))
	}
}

func TestRootResolveByNameNotFound(t *testing.T) {
	if _, err := Apply(buildTrace(), Options{RootSpanName: "ghost"}); err == nil {
		t.Fatal("expected error for missing root-span-name")
	}
}

func TestMaxDepth(t *testing.T) {
	tr := buildTrace()
	roots, err := Apply(tr, Options{MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	// depth 0 (root) and depth 1 (a,b) shown; a1 (depth 2) hidden.
	if find(roots, "a1") != nil {
		t.Error("a1 should be pruned by max-depth=1")
	}
	a := find(roots, "a")
	if a == nil || a.HiddenChildren != 1 {
		t.Errorf("node a HiddenChildren = %v, want 1", a)
	}
	// Non-mutation: original tree still has a1.
	if _, ok := tr.AllSpans["a1"]; !ok {
		t.Error("original domain tree was mutated")
	}
	if len(tr.AllSpans["a"].Children) != 1 {
		t.Error("original node a children were mutated")
	}
}

func TestTopNKeepsAncestors(t *testing.T) {
	tr := buildTrace()
	// Durations: root=100, a=80, a1=10, b=10. Top-1 by duration = root.
	// But keep a1 test: choose top-2 => root(100), a(80). Ancestors of a = root.
	roots, err := Apply(tr, Options{TopN: 2})
	if err != nil {
		t.Fatal(err)
	}
	if find(roots, "root") == nil || find(roots, "a") == nil {
		t.Error("top-2 should keep root and a")
	}
	if find(roots, "a1") != nil || find(roots, "b") != nil {
		t.Error("a1 and b should be dropped by top-2")
	}
}

func TestTopNForcesAncestorOfDeepSpan(t *testing.T) {
	// Make a1 the longest so its ancestor 'a' must be force-kept even though
	// 'a' itself may not be in the top set.
	spans := []*domain.Span{
		{SpanID: "root", TraceID: "t", Name: "root", StartNanos: 0, EndNanos: 5},                   // dur 5
		{SpanID: "a", ParentSpanID: "root", TraceID: "t", Name: "a", StartNanos: 10, EndNanos: 15}, // dur 5
		{SpanID: "a1", ParentSpanID: "a", TraceID: "t", Name: "a1", StartNanos: 11, EndNanos: 99},  // dur 88
	}
	tr := domain.BuildTree("t", spans)
	roots, err := Apply(tr, Options{TopN: 1}) // top-1 by dur = a1 (88)
	if err != nil {
		t.Fatal(err)
	}
	// a1 kept, plus forced ancestors a and root.
	if find(roots, "a1") == nil {
		t.Fatal("a1 (longest) must be kept")
	}
	if find(roots, "a") == nil || find(roots, "root") == nil {
		t.Error("ancestors of a1 must be force-kept for a connected tree")
	}
}

func TestHideReparentsChildren(t *testing.T) {
	tr := buildTrace()
	// Hide "a" (exact regex): a is dropped, its child a1 is lifted to become a
	// direct child of root at depth 1.
	roots, err := Apply(tr, Options{HidePatterns: []string{"^a$"}, MatchMode: "regex"})
	if err != nil {
		t.Fatal(err)
	}
	if find(roots, "a") != nil {
		t.Error("a should be hidden")
	}
	a1 := find(roots, "a1")
	if a1 == nil {
		t.Fatal("a1 should survive (reparented)")
	}
	if a1.Depth != 1 {
		t.Errorf("a1 depth = %d, want 1 (reparented under root)", a1.Depth)
	}
	root := find(roots, "root")
	if root == nil || len(root.Children) != 2 {
		t.Fatalf("root should have 2 children (a1, b), got %v", root)
	}
	// Non-mutation of the source tree.
	if len(tr.AllSpans["root"].Children) != 2 || find(tr.Roots, "a") == nil {
		t.Error("original tree was mutated by hide")
	}
}

func TestFoldCollapsesSubtree(t *testing.T) {
	tr := buildTrace()
	roots, err := Apply(tr, Options{FoldPatterns: []string{"^a$"}, MatchMode: "regex"})
	if err != nil {
		t.Fatal(err)
	}
	a := find(roots, "a")
	if a == nil {
		t.Fatal("a should remain when folded")
	}
	if find(roots, "a1") != nil {
		t.Error("a1 should be collapsed under folded a")
	}
	if a.HiddenChildren != 1 || !a.HiddenByFold {
		t.Errorf("a HiddenChildren=%d HiddenByFold=%v, want 1/true", a.HiddenChildren, a.HiddenByFold)
	}
}

func TestMatchModeExactVsRegex(t *testing.T) {
	// exact: only the span literally named "a" is hidden; a1 survives.
	roots, err := Apply(buildTrace(), Options{HidePatterns: []string{"a"}, MatchMode: "exact"})
	if err != nil {
		t.Fatal(err)
	}
	if find(roots, "a") != nil {
		t.Error("exact: a should be hidden")
	}
	if find(roots, "a1") == nil {
		t.Error("exact: a1 should NOT be hidden (name is 'a1', not 'a')")
	}

	// regex: pattern "a" matches any name containing 'a', so a and a1 both go.
	roots, err = Apply(buildTrace(), Options{HidePatterns: []string{"a"}, MatchMode: "regex"})
	if err != nil {
		t.Fatal(err)
	}
	if find(roots, "a") != nil || find(roots, "a1") != nil {
		t.Error("regex: both a and a1 should be hidden")
	}
}

func TestInvalidRegexReturnsError(t *testing.T) {
	if _, err := Apply(buildTrace(), Options{FoldPatterns: []string{"("}, MatchMode: "regex"}); err == nil {
		t.Fatal("expected error for invalid fold regex")
	}
}

func TestNoPatternsIsNoOp(t *testing.T) {
	// Empty pattern lists must leave the full tree intact regardless of mode.
	roots, err := Apply(buildTrace(), Options{MatchMode: "exact"})
	if err != nil {
		t.Fatal(err)
	}
	if countSpans(roots) != 4 {
		t.Errorf("span count = %d, want 4 (no filtering)", countSpans(roots))
	}
}

func TestSortDuration(t *testing.T) {
	tr := buildTrace()
	roots, err := Apply(tr, Options{Sort: "duration"})
	if err != nil {
		t.Fatal(err)
	}
	// root's children sorted by duration desc: a(80) before b(10).
	root := roots[0]
	if len(root.Children) != 2 || root.Children[0].SpanID != "a" || root.Children[1].SpanID != "b" {
		t.Errorf("children not sorted by duration desc: %v", root.Children)
	}
}
