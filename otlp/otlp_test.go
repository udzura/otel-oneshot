package otlp

import (
	"bytes"
	"testing"

	"github.com/udzura/otel-oneshot/domain"
)

func TestNanoTimeUnmarshal(t *testing.T) {
	cases := []struct {
		in   string
		want uint64
	}{
		{`"1700000000000000000"`, 1700000000000000000},
		{`1700000000000000000`, 1700000000000000000},
		{`"18446744073709551615"`, 18446744073709551615}, // max uint64, would lose precision via float
		{`18446744073709551615`, 18446744073709551615},
		{`""`, 0},
		{`null`, 0},
	}
	for _, c := range cases {
		var n NanoTime
		if err := n.UnmarshalJSON([]byte(c.in)); err != nil {
			t.Fatalf("UnmarshalJSON(%s): %v", c.in, err)
		}
		if uint64(n) != c.want {
			t.Errorf("UnmarshalJSON(%s) = %d, want %d", c.in, uint64(n), c.want)
		}
	}
}

const twoTraces = `{"resourceSpans":[{"resource":{"attributes":[
  {"key":"service.name","value":{"stringValue":"svc"}}]},"scopeSpans":[{"spans":[
  {"traceId":"aaaa","spanId":"01","parentSpanId":"","name":"a-root","startTimeUnixNano":"100","endTimeUnixNano":"200","status":{"code":"STATUS_CODE_OK"}},
  {"traceId":"bbbb","spanId":"02","parentSpanId":"","name":"b-root","startTimeUnixNano":100,"endTimeUnixNano":300,"status":{"code":"STATUS_CODE_ERROR"}},
  {"traceId":"aaaa","spanId":"","name":"broken","startTimeUnixNano":"1","endTimeUnixNano":"2"}
]}]}]}`

func TestLoadMultiTraceAndSkip(t *testing.T) {
	var warn bytes.Buffer
	res, err := Load([]byte(twoTraces), &warn)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(res.Order) != 2 || res.Order[0] != "aaaa" || res.Order[1] != "bbbb" {
		t.Fatalf("Order = %v, want [aaaa bbbb]", res.Order)
	}
	// Broken span (empty spanId) must be skipped with a warning.
	if got := len(res.Traces["aaaa"].AllSpans); got != 1 {
		t.Errorf("trace aaaa span count = %d, want 1 (broken span skipped)", got)
	}
	if warn.Len() == 0 {
		t.Error("expected a warning for skipped span")
	}
	// service.name propagated.
	if got := res.Traces["aaaa"].Roots[0].ServiceName; got != "svc" {
		t.Errorf("ServiceName = %q, want svc", got)
	}
	// status mapping.
	if res.Traces["bbbb"].Roots[0].Status != domain.StatusError {
		t.Error("expected trace bbbb root to be StatusError")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	if _, err := Load([]byte("{not json"), &bytes.Buffer{}); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestBuildTreeOrphanIsRoot(t *testing.T) {
	// parentSpanId points to a span not present -> treated as root.
	in := `{"resourceSpans":[{"scopeSpans":[{"spans":[
	  {"traceId":"t","spanId":"child","parentSpanId":"missing","name":"c","startTimeUnixNano":"10","endTimeUnixNano":"20"}
	]}]}]}`
	res, err := Load([]byte(in), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	tr := res.Traces["t"]
	if len(tr.Roots) != 1 || tr.Roots[0].SpanID != "child" {
		t.Fatalf("orphan span not treated as root: %+v", tr.Roots)
	}
}
