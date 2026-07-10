package otlp

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/udzura/otel-oneshot/domain"
)

// Result holds every trace found in an OTLP/JSON file, keyed by trace ID, in
// first-seen order.
type Result struct {
	// Order preserves the order in which trace IDs first appeared, so
	// "the first trace" is well defined.
	Order  []string
	Traces map[string]*domain.Trace
}

// Load reads OTLP/JSON from data, converts it to the domain model and groups it
// by trace. Individual malformed spans are skipped with a warning written to
// warnOut; only a top-level JSON error is returned as an error.
func Load(data []byte, warnOut io.Writer) (*Result, error) {
	raw, err := parseFile(data)
	if err != nil {
		return nil, err
	}

	// Group spans by trace ID, keeping first-seen ordering.
	order := make([]string, 0)
	byTrace := make(map[string][]*domain.Span)
	skipped := 0

	for _, rs := range raw.ResourceSpans {
		serviceName := serviceNameOf(rs.Resource)
		for _, ss := range rs.ScopeSpans {
			for i := range ss.Spans {
				rsp := &ss.Spans[i]
				sp, err := convertSpan(rsp, serviceName)
				if err != nil {
					skipped++
					fmt.Fprintf(warnOut, "warning: skipping span (name=%q, spanId=%q): %v\n",
						rsp.Name, rsp.SpanID, err)
					continue
				}
				if _, seen := byTrace[sp.TraceID]; !seen {
					order = append(order, sp.TraceID)
				}
				byTrace[sp.TraceID] = append(byTrace[sp.TraceID], sp)
			}
		}
	}

	res := &Result{
		Order:  order,
		Traces: make(map[string]*domain.Trace, len(order)),
	}
	for _, tid := range order {
		res.Traces[tid] = domain.BuildTree(tid, byTrace[tid])
	}

	if skipped > 0 {
		fmt.Fprintf(warnOut, "warning: %d span(s) skipped due to missing/invalid fields\n", skipped)
	}
	return res, nil
}

// convertSpan validates and converts a single raw span. Missing required
// fields (spanId, traceId, start/end time) cause it to be skipped.
func convertSpan(r *rawSpan, serviceName string) (*domain.Span, error) {
	if r.SpanID == "" {
		return nil, fmt.Errorf("missing spanId")
	}
	if r.TraceID == "" {
		return nil, fmt.Errorf("missing traceId")
	}
	if r.StartTimeUnixNano == 0 {
		return nil, fmt.Errorf("missing startTimeUnixNano")
	}
	if r.EndTimeUnixNano == 0 {
		return nil, fmt.Errorf("missing endTimeUnixNano")
	}

	sp := &domain.Span{
		SpanID:       r.SpanID,
		ParentSpanID: r.ParentSpanID,
		TraceID:      r.TraceID,
		Name:         r.Name,
		ServiceName:  serviceName,
		StartNanos:   uint64(r.StartTimeUnixNano),
		EndNanos:     uint64(r.EndTimeUnixNano),
		Attributes:   attrsToMap(r.Attributes),
		Status:       statusCodeOf(r.Status.Code),
	}
	for _, e := range r.Events {
		sp.Events = append(sp.Events, domain.Event{
			TimeNanos:  uint64(e.TimeUnixNano),
			Name:       e.Name,
			Attributes: attrsToMap(e.Attributes),
		})
	}
	return sp, nil
}

func serviceNameOf(r rawResource) string {
	for _, a := range r.Attributes {
		if a.Key == "service.name" {
			return anyValueString(a.Value)
		}
	}
	return ""
}

func statusCodeOf(code string) domain.StatusCode {
	switch code {
	case "STATUS_CODE_OK", "OK", "1":
		return domain.StatusOK
	case "STATUS_CODE_ERROR", "ERROR", "2":
		return domain.StatusError
	default:
		return domain.StatusUnset
	}
}

func attrsToMap(attrs []rawAttribute) map[string]string {
	if len(attrs) == 0 {
		return nil
	}
	m := make(map[string]string, len(attrs))
	for _, a := range attrs {
		m[a.Key] = anyValueString(a.Value)
	}
	return m
}

// anyValueString flattens an OTLP AnyValue into a display string.
func anyValueString(v rawAnyValue) string {
	switch {
	case v.StringValue != nil:
		return *v.StringValue
	case v.BoolValue != nil:
		return strconv.FormatBool(*v.BoolValue)
	case v.IntValue != nil:
		// intValue may be a quoted string or a number; unquote if needed.
		s := string(*v.IntValue)
		if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
			s = s[1 : len(s)-1]
		}
		return s
	case v.DoubleValue != nil:
		return strconv.FormatFloat(*v.DoubleValue, 'g', -1, 64)
	case v.BytesValue != nil:
		return *v.BytesValue
	case v.ArrayValue != nil:
		parts := make([]string, 0, len(v.ArrayValue.Values))
		for _, e := range v.ArrayValue.Values {
			parts = append(parts, anyValueString(e))
		}
		return "[" + joinComma(parts) + "]"
	case v.KvlistValue != nil:
		m := make(map[string]string, len(v.KvlistValue.Values))
		for _, kv := range v.KvlistValue.Values {
			m[kv.Key] = anyValueString(kv.Value)
		}
		b, _ := json.Marshal(m)
		return string(b)
	default:
		return ""
	}
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}
