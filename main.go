// Command otel-oneshot renders a single OTLP/JSON trace as an ASCII timeline.
//
// It is a strictly one-shot, non-interactive pipeline:
//
//	read → parse → filter → layout → render → stdout → exit
//
// There is no input loop, no screen refresh, no key handling. All display
// choices are fixed at startup from CLI flags and/or a YAML config file.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/udzura/otel-oneshot/config"
	"github.com/udzura/otel-oneshot/domain"
	"github.com/udzura/otel-oneshot/filter"
	"github.com/udzura/otel-oneshot/layout"
	"github.com/udzura/otel-oneshot/otlp"
	"github.com/udzura/otel-oneshot/render"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run is the testable entry point. It returns the process exit code.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cfg, err := config.Load(args)
	if err != nil {
		var ue *config.UsageError
		if errors.As(err, &ue) {
			fmt.Fprint(stdout, config.Usage())
			return 0
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	data, err := readInput(cfg.InputPath, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	result, err := otlp.Load(data, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	tr, err := selectTrace(result, cfg.TraceID, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	roots, err := filter.Apply(tr, filter.Options{
		RootSpanID:   cfg.RootSpanID,
		RootSpanName: cfg.RootSpanName,
		MaxDepth:     cfg.MaxDepth,
		TopN:         cfg.TopN,
		Sort:         cfg.Sort,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	if countSpans(roots) == 0 {
		fmt.Fprintln(stdout, "no spans to display")
		return 0
	}

	// Width + color must be resolved against the *real* stdout file so TTY
	// detection works; fall back cleanly when stdout is not an *os.File.
	stdoutFile, _ := stdout.(*os.File)
	width := render.ResolveWidth(cfg.Width, stdoutFile)
	styler := render.NewStyler(cfg.Color, stdoutFile)

	lay := layout.Build(roots, layout.Options{
		TotalWidth:      width,
		TimeUnit:        cfg.TimeUnit,
		ShowAttributes:  cfg.ShowAttributes,
		HighlightErrors: cfg.HighlightErrors,
	})

	meta := render.Meta{
		TraceID:  tr.TraceID,
		RootName: rootName(roots),
	}
	if err := render.Render(stdout, lay, meta, render.Options{
		HighlightErrors: cfg.HighlightErrors,
		ShowAttributes:  cfg.ShowAttributes,
	}, styler); err != nil {
		fmt.Fprintf(stderr, "error: writing output: %v\n", err)
		return 1
	}
	return 0
}

func readInput(path string, stdin io.Reader) ([]byte, error) {
	if path == "" || path == "-" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading input file: %w", err)
	}
	return data, nil
}

// selectTrace picks the trace to render, honoring --trace-id and warning when a
// batch of multiple traces is auto-resolved to the first one.
func selectTrace(res *otlp.Result, traceID string, warnOut io.Writer) (*domain.Trace, error) {
	if len(res.Order) == 0 {
		return nil, fmt.Errorf("no valid spans found in input")
	}

	if traceID != "" {
		tr, ok := res.Traces[traceID]
		if !ok {
			return nil, fmt.Errorf("trace id %q not found in input", traceID)
		}
		return tr, nil
	}

	chosen := res.Order[0]
	if len(res.Order) > 1 {
		fmt.Fprintf(warnOut,
			"warning: input contains %d traces; showing the first (%s). Use --trace-id to choose another.\n",
			len(res.Order), chosen)
	}
	return res.Traces[chosen], nil
}

func countSpans(roots []*domain.Span) int {
	n := 0
	var walk func(s *domain.Span)
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

func rootName(roots []*domain.Span) string {
	switch len(roots) {
	case 0:
		return ""
	case 1:
		return roots[0].Name
	default:
		return fmt.Sprintf("%d roots", len(roots))
	}
}
