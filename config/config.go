// Package config parses CLI flags and an optional YAML file and merges them
// into a single validated Config. Precedence is: CLI flag > YAML > default.
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the fully resolved runtime configuration.
type Config struct {
	InputPath string // positional arg; "" means stdin

	TraceID         string
	RootSpanID      string
	RootSpanName    string
	MaxDepth        int
	TopN            int
	Width           int
	ShowAttributes  []string
	HighlightErrors bool
	Color           string // auto | always | never
	Sort            string // start_time | duration
	TimeUnit        string // auto | ms | us | ns
}

// Defaults returns a Config populated with the documented default values.
func Defaults() Config {
	return Config{
		MaxDepth:        0,
		TopN:            0,
		Width:           0,
		HighlightErrors: true,
		Color:           "auto",
		Sort:            "start_time",
		TimeUnit:        "auto",
	}
}

// fileConfig mirrors the YAML config file. Pointers let us distinguish
// "absent" from "explicitly set to the zero value".
type fileConfig struct {
	TraceID         *string  `yaml:"trace_id"`
	RootSpanID      *string  `yaml:"root_span_id"`
	RootSpanName    *string  `yaml:"root_span_name"`
	MaxDepth        *int     `yaml:"max_depth"`
	TopN            *int     `yaml:"top_n"`
	Width           *int     `yaml:"width"`
	ShowAttributes  []string `yaml:"show_attributes"`
	HighlightErrors *bool    `yaml:"highlight_errors"`
	Color           *string  `yaml:"color"`
	Sort            *string  `yaml:"sort"`
	TimeUnit        *string  `yaml:"time_unit"`
}

// rawFlags holds parsed flag values plus which flags were explicitly set on the
// command line. Only explicitly-set flags win over the YAML file.
type rawFlags struct {
	set map[string]bool

	config          string
	traceID         string
	rootSpanID      string
	rootSpanName    string
	maxDepth        int
	topN            int
	width           int
	showAttributes  string
	highlightErrors bool
	color           string
	sort            string
	timeUnit        string
}

// Load resolves the effective configuration from CLI args (excluding argv[0]).
// It returns the config, or an error, or a request to print usage (usageErr).
func Load(args []string) (*Config, error) {
	rf, positional, err := parseFlags(args)
	if err != nil {
		return nil, err
	}

	cfg := Defaults()

	// Layer 1: YAML file (if any).
	configPath := rf.config
	if configPath != "" {
		fc, err := loadYAML(configPath)
		if err != nil {
			return nil, err
		}
		applyYAML(&cfg, fc)
	}

	// Layer 2: explicitly-set CLI flags override the file.
	applyFlags(&cfg, rf)

	// Positional input path.
	switch len(positional) {
	case 0:
		cfg.InputPath = "" // stdin
	case 1:
		cfg.InputPath = positional[0]
	default:
		return nil, fmt.Errorf("expected at most one input file, got %d", len(positional))
	}

	if err := validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func loadYAML(path string) (*fileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %q: %w", path, err)
	}
	var fc fileConfig
	if err := yaml.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("parse config file %q: %w", path, err)
	}
	return &fc, nil
}

func applyYAML(cfg *Config, fc *fileConfig) {
	if fc.TraceID != nil {
		cfg.TraceID = *fc.TraceID
	}
	if fc.RootSpanID != nil {
		cfg.RootSpanID = *fc.RootSpanID
	}
	if fc.RootSpanName != nil {
		cfg.RootSpanName = *fc.RootSpanName
	}
	if fc.MaxDepth != nil {
		cfg.MaxDepth = *fc.MaxDepth
	}
	if fc.TopN != nil {
		cfg.TopN = *fc.TopN
	}
	if fc.Width != nil {
		cfg.Width = *fc.Width
	}
	if fc.ShowAttributes != nil {
		cfg.ShowAttributes = fc.ShowAttributes
	}
	if fc.HighlightErrors != nil {
		cfg.HighlightErrors = *fc.HighlightErrors
	}
	if fc.Color != nil {
		cfg.Color = *fc.Color
	}
	if fc.Sort != nil {
		cfg.Sort = *fc.Sort
	}
	if fc.TimeUnit != nil {
		cfg.TimeUnit = *fc.TimeUnit
	}
}

func applyFlags(cfg *Config, rf *rawFlags) {
	if rf.set["trace-id"] {
		cfg.TraceID = rf.traceID
	}
	if rf.set["root-span-id"] {
		cfg.RootSpanID = rf.rootSpanID
	}
	if rf.set["root-span-name"] {
		cfg.RootSpanName = rf.rootSpanName
	}
	if rf.set["max-depth"] {
		cfg.MaxDepth = rf.maxDepth
	}
	if rf.set["top-n"] {
		cfg.TopN = rf.topN
	}
	if rf.set["width"] {
		cfg.Width = rf.width
	}
	if rf.set["show-attributes"] {
		cfg.ShowAttributes = splitCSV(rf.showAttributes)
	}
	if rf.set["highlight-errors"] {
		cfg.HighlightErrors = rf.highlightErrors
	}
	if rf.set["color"] {
		cfg.Color = rf.color
	}
	if rf.set["sort"] {
		cfg.Sort = rf.sort
	}
	if rf.set["time-unit"] {
		cfg.TimeUnit = rf.timeUnit
	}
}

func validate(cfg *Config) error {
	switch cfg.Color {
	case "auto", "always", "never":
	default:
		return fmt.Errorf("invalid --color %q (want auto|always|never)", cfg.Color)
	}
	switch cfg.Sort {
	case "start_time", "duration":
	default:
		return fmt.Errorf("invalid --sort %q (want start_time|duration)", cfg.Sort)
	}
	switch cfg.TimeUnit {
	case "auto", "ms", "us", "ns", "s":
	default:
		return fmt.Errorf("invalid --time-unit %q (want auto|s|ms|us|ns)", cfg.TimeUnit)
	}
	if cfg.MaxDepth < 0 {
		return fmt.Errorf("--max-depth must be >= 0")
	}
	if cfg.TopN < 0 {
		return fmt.Errorf("--top-n must be >= 0")
	}
	if cfg.Width < 0 {
		return fmt.Errorf("--width must be >= 0")
	}
	if cfg.RootSpanID != "" && cfg.RootSpanName != "" {
		// Not an error: id wins per spec. Nothing to validate here.
		_ = cfg
	}
	return nil
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
