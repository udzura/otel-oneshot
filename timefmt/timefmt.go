// Package timefmt formats nanosecond durations into human-readable labels.
//
// It is a leaf package shared by layout (which needs the rendered width to size
// the duration column) and render (which prints the labels), so the unit-
// selection logic lives in exactly one place.
package timefmt

import "strconv"

// Unit is a duration display unit.
type Unit string

const (
	Nanos  Unit = "ns"
	Micros Unit = "us"
	Millis Unit = "ms"
	Secs   Unit = "s"
)

// ResolveUnit maps the config --time-unit value to a concrete Unit. For "auto"
// (or anything unrecognized) it chooses based on the overall duration.
func ResolveUnit(configUnit string, totalNanos uint64) Unit {
	switch configUnit {
	case "ns":
		return Nanos
	case "us":
		return Micros
	case "ms":
		return Millis
	case "s":
		return Secs
	default:
		return autoUnit(totalNanos)
	}
}

func autoUnit(n uint64) Unit {
	switch {
	case n >= 1_000_000_000: // >= 1s
		return Secs
	case n >= 1_000_000: // >= 1ms
		return Millis
	case n >= 1_000: // >= 1us
		return Micros
	default:
		return Nanos
	}
}

// Format renders a nanosecond duration in the given unit, e.g. "842ms".
// Sub-unit precision is shown with up to two decimals for s/ms/us.
func Format(nanos uint64, u Unit) string {
	switch u {
	case Secs:
		return trimDecimal(float64(nanos)/1e9) + "s"
	case Millis:
		return trimDecimal(float64(nanos)/1e6) + "ms"
	case Micros:
		return trimDecimal(float64(nanos)/1e3) + "us"
	default:
		return strconv.FormatUint(nanos, 10) + "ns"
	}
}

// trimDecimal formats a float with up to two decimals, dropping trailing zeros
// and a trailing dot.
func trimDecimal(f float64) string {
	s := strconv.FormatFloat(f, 'f', 2, 64)
	// strip trailing zeros
	for len(s) > 0 && s[len(s)-1] == '0' {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}
	return s
}
