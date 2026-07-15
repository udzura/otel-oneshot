package config

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// usageText is printed for -h/--help and on flag errors.
const usageText = `otel-oneshot — render an OTLP/JSON trace as an ASCII timeline.

Usage:
  otel-oneshot [input.json] [flags]

If input.json is omitted, OTLP/JSON is read from stdin.

Flags:
  --config string           Path to a YAML config file
  --trace-id string         Target trace ID (default: first trace in file)
  --root-span-id string     Render only the subtree rooted at this span ID
  --root-span-name string   Render only the subtree of the first span with this name
  --max-depth int           Max tree depth to display (0 = unlimited)
  --top-n int               Keep only the N longest spans, plus their ancestors (0 = all)
  --width int               Output width in columns (0 = detect terminal, fallback 120)
  --show-attributes string  Comma-separated attribute keys to annotate on each row
  --highlight-errors        Highlight ERROR spans (default true)
  --color string            auto | always | never (default auto)
  --sort string             start_time | duration (default start_time)
  --time-unit string        auto | s | ms | us | ns (default auto)
  --fold string             Collapse the subtree of spans matching this pattern (repeatable)
  --hide string             Drop spans matching this pattern, reparenting children (repeatable)
  --match-mode string       How --fold/--hide patterns match: regex | exact (default regex)
  --version                 Print the version and exit
`

// parseFlags parses argv into rawFlags, tracking which flags were explicitly
// set. Precedence handling relies on that set.
func parseFlags(args []string) (*rawFlags, []string, error) {
	rf := &rawFlags{set: map[string]bool{}}

	fs := flag.NewFlagSet("otel-oneshot", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // we render our own usage/errors
	fs.Usage = func() {}

	fs.StringVar(&rf.config, "config", "", "")
	fs.StringVar(&rf.traceID, "trace-id", "", "")
	fs.StringVar(&rf.rootSpanID, "root-span-id", "", "")
	fs.StringVar(&rf.rootSpanName, "root-span-name", "", "")
	fs.IntVar(&rf.maxDepth, "max-depth", 0, "")
	fs.IntVar(&rf.topN, "top-n", 0, "")
	fs.IntVar(&rf.width, "width", 0, "")
	fs.StringVar(&rf.showAttributes, "show-attributes", "", "")
	fs.BoolVar(&rf.highlightErrors, "highlight-errors", true, "")
	fs.StringVar(&rf.color, "color", "auto", "")
	fs.StringVar(&rf.sort, "sort", "start_time", "")
	fs.StringVar(&rf.timeUnit, "time-unit", "auto", "")
	fs.Var((*stringSlice)(&rf.foldPatterns), "fold", "")
	fs.Var((*stringSlice)(&rf.hidePatterns), "hide", "")
	fs.StringVar(&rf.matchMode, "match-mode", "regex", "")
	fs.BoolVar(&rf.version, "version", false, "")

	// The standard flag package stops at the first non-flag argument, so
	// "input.json --width 100" would treat the flags as positionals. Parse in
	// a loop, peeling off one positional at a time, to accept flags and the
	// input path in any order.
	var positional []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			if err == flag.ErrHelp {
				return nil, nil, &UsageError{}
			}
			return nil, nil, fmt.Errorf("%w\n\n%s", err, usageText)
		}
		rest = fs.Args()
		if len(rest) == 0 {
			break
		}
		positional = append(positional, rest[0])
		rest = rest[1:]
	}

	fs.Visit(func(f *flag.Flag) { rf.set[f.Name] = true })

	if rf.version {
		return nil, nil, &VersionError{}
	}

	return rf, positional, nil
}

// stringSlice is a flag.Value that accumulates a value each time its flag is
// given, so --fold/--hide can be repeated. This avoids CSV splitting, which
// would break regex patterns containing commas (e.g. "a{2,3}").
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }

func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// UsageError signals that usage should be printed and the process should exit 0.
type UsageError struct{}

func (*UsageError) Error() string { return "usage requested" }

// VersionError signals that the version should be printed and the process
// should exit 0.
type VersionError struct{}

func (*VersionError) Error() string { return "version requested" }

// Usage returns the help text.
func Usage() string { return usageText }

// VersionString returns the "otel-oneshot vX.Y.Z" line printed for --version.
func VersionString() string { return "otel-oneshot v" + Version }
