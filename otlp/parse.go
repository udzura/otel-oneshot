// Package otlp parses OTLP/JSON into the internal domain model.
//
// It intentionally tolerates two real-world quirks of OTLP/JSON:
//   - Unix-nano timestamps arrive either as JSON strings ("169...") or as JSON
//     numbers. They are uint64 and must never be routed through float64, which
//     would silently lose precision. NanoTime handles both without float.
//   - A single file may batch multiple traces together.
package otlp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// NanoTime is a uint64 nanosecond timestamp that unmarshals from either a JSON
// string or a JSON number, without ever going through float64.
type NanoTime uint64

func (n *NanoTime) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*n = 0
		return nil
	}
	// Strip surrounding quotes if present (string-encoded uint64).
	if b[0] == '"' {
		if len(b) < 2 || b[len(b)-1] != '"' {
			return fmt.Errorf("otlp: malformed quoted timestamp %q", string(b))
		}
		b = b[1 : len(b)-1]
		if len(b) == 0 {
			*n = 0
			return nil
		}
	}
	v, err := strconv.ParseUint(string(b), 10, 64)
	if err != nil {
		return fmt.Errorf("otlp: invalid uint64 timestamp %q: %w", string(b), err)
	}
	*n = NanoTime(v)
	return nil
}

// rawFile is the top-level OTLP/JSON structure.
type rawFile struct {
	ResourceSpans []rawResourceSpans `json:"resourceSpans"`
}

type rawResourceSpans struct {
	Resource   rawResource     `json:"resource"`
	ScopeSpans []rawScopeSpans `json:"scopeSpans"`
}

type rawResource struct {
	Attributes []rawAttribute `json:"attributes"`
}

type rawScopeSpans struct {
	Spans []rawSpan `json:"spans"`
}

type rawSpan struct {
	TraceID           string         `json:"traceId"`
	SpanID            string         `json:"spanId"`
	ParentSpanID      string         `json:"parentSpanId"`
	Name              string         `json:"name"`
	StartTimeUnixNano NanoTime       `json:"startTimeUnixNano"`
	EndTimeUnixNano   NanoTime       `json:"endTimeUnixNano"`
	Attributes        []rawAttribute `json:"attributes"`
	Events            []rawEvent     `json:"events"`
	Status            rawStatus      `json:"status"`
}

type rawEvent struct {
	TimeUnixNano NanoTime       `json:"timeUnixNano"`
	Name         string         `json:"name"`
	Attributes   []rawAttribute `json:"attributes"`
}

type rawStatus struct {
	Code string `json:"code"`
}

// rawAttribute is a single OTLP key/value attribute. The value is any of the
// AnyValue variants; we render it to a flat string.
type rawAttribute struct {
	Key   string      `json:"key"`
	Value rawAnyValue `json:"value"`
}

type rawAnyValue struct {
	StringValue *string          `json:"stringValue"`
	BoolValue   *bool            `json:"boolValue"`
	IntValue    *json.RawMessage `json:"intValue"` // string or number
	DoubleValue *float64         `json:"doubleValue"`
	ArrayValue  *rawArrayValue   `json:"arrayValue"`
	KvlistValue *rawKvlistValue  `json:"kvlistValue"`
	BytesValue  *string          `json:"bytesValue"`
}

type rawArrayValue struct {
	Values []rawAnyValue `json:"values"`
}

type rawKvlistValue struct {
	Values []rawAttribute `json:"values"`
}

// parseFile unmarshals the OTLP/JSON bytes. A hard JSON error here is fatal to
// the whole run; per-span problems are handled later in convert.go.
func parseFile(data []byte) (*rawFile, error) {
	var f rawFile
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("otlp: decode JSON: %w", err)
	}
	return &f, nil
}
