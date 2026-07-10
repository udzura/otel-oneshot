package layout

import (
	"testing"

	"github.com/udzura/otel-oneshot/domain"
)

func TestColOfBoundaries(t *testing.T) {
	const bw = 100
	cases := []struct {
		t, start, dur uint64
		want          int
	}{
		{0, 0, 100, 0},
		{100, 0, 100, 100},
		{50, 0, 100, 50},
		{25, 0, 100, 25},
		{5, 0, 1, bw},   // t beyond view end clamps to barWidth
		{0, 10, 100, 0}, // t before view start clamps to 0
	}
	for _, c := range cases {
		got := colOf(c.t, c.start, c.dur, bw)
		if got != c.want {
			t.Errorf("colOf(%d,%d,%d,%d)=%d want %d", c.t, c.start, c.dur, bw, got, c.want)
		}
	}
}

func TestBuildZeroDuration(t *testing.T) {
	// A single instantaneous span (start==end) must not divide by zero and must
	// still produce a 1-char bar.
	s := &domain.Span{SpanID: "x", Name: "x", StartNanos: 5, EndNanos: 5}
	l := Build([]*domain.Span{s}, Options{TotalWidth: 80, TimeUnit: "auto"})
	if len(l.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(l.Rows))
	}
	r := l.Rows[0]
	if r.EndCol <= r.StartCol {
		t.Errorf("bar collapsed: start=%d end=%d", r.StartCol, r.EndCol)
	}
}

func TestBuildEmpty(t *testing.T) {
	l := Build(nil, Options{TotalWidth: 80})
	if len(l.Rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(l.Rows))
	}
}

func TestPaneWidthsRespectTotal(t *testing.T) {
	spans := []*domain.Span{
		{SpanID: "root", Name: "root", StartNanos: 0, EndNanos: 100},
		{SpanID: "c", ParentSpanID: "root", Name: "child", StartNanos: 10, EndNanos: 90},
	}
	tr := domain.BuildTree("t", spans)
	l := Build(tr.Roots, Options{TotalWidth: 100, TimeUnit: "ms"})
	// tree + sep + bar + gap + dur must not exceed total.
	sum := l.TreeWidth + sepWidth + l.BarWidth + gapWidth + l.DurWidth
	if sum > l.TotalWidth {
		t.Errorf("panes overflow: tree=%d bar=%d dur=%d sum=%d total=%d",
			l.TreeWidth, l.BarWidth, l.DurWidth, sum, l.TotalWidth)
	}
	if l.BarWidth < 1 {
		t.Errorf("bar width must be >= 1, got %d", l.BarWidth)
	}
}

func TestAttributeAnnotation(t *testing.T) {
	s := &domain.Span{Attributes: map[string]string{"http.status_code": "500", "db.system": "pg"}}
	got := AttributeAnnotation(s, []string{"http.status_code", "missing", "db.system"})
	want := "(http.status_code=500, db.system=pg)"
	if got != want {
		t.Errorf("AttributeAnnotation = %q, want %q", got, want)
	}
	if AttributeAnnotation(s, nil) != "" {
		t.Error("no keys should yield empty annotation")
	}
}
