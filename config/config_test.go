package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg, err := Load([]string{"in.json"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InputPath != "in.json" {
		t.Errorf("InputPath = %q", cfg.InputPath)
	}
	if cfg.Color != "auto" || cfg.Sort != "start_time" || cfg.TimeUnit != "auto" {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	if !cfg.HighlightErrors {
		t.Error("HighlightErrors should default true")
	}
}

func TestFlagOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "config.yaml")
	yaml := `
trace_id: "from-file"
max_depth: 5
show_attributes: ["a", "b"]
highlight_errors: false
color: never
`
	if err := os.WriteFile(yamlPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	// CLI overrides max_depth and color; yaml supplies the rest.
	cfg, err := Load([]string{"--config", yamlPath, "--max-depth", "2", "--color", "always"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TraceID != "from-file" {
		t.Errorf("TraceID = %q, want from-file (from yaml)", cfg.TraceID)
	}
	if cfg.MaxDepth != 2 {
		t.Errorf("MaxDepth = %d, want 2 (cli override)", cfg.MaxDepth)
	}
	if cfg.Color != "always" {
		t.Errorf("Color = %q, want always (cli override)", cfg.Color)
	}
	if cfg.HighlightErrors {
		t.Error("HighlightErrors should be false (from yaml)")
	}
	if len(cfg.ShowAttributes) != 2 || cfg.ShowAttributes[0] != "a" {
		t.Errorf("ShowAttributes = %v, want [a b]", cfg.ShowAttributes)
	}
}

func TestArgOrderIndependence(t *testing.T) {
	// The input path and flags must be accepted in any order.
	cases := [][]string{
		{"in.json", "--width", "90", "--color", "never"},
		{"--width", "90", "--color", "never", "in.json"},
		{"--width", "90", "in.json", "--color", "never"},
	}
	for _, args := range cases {
		cfg, err := Load(args)
		if err != nil {
			t.Fatalf("Load(%v): %v", args, err)
		}
		if cfg.InputPath != "in.json" {
			t.Errorf("Load(%v): InputPath = %q, want in.json", args, cfg.InputPath)
		}
		if cfg.Width != 90 || cfg.Color != "never" {
			t.Errorf("Load(%v): width=%d color=%q", args, cfg.Width, cfg.Color)
		}
	}
}

func TestTooManyInputs(t *testing.T) {
	if _, err := Load([]string{"a.json", "b.json"}); err == nil {
		t.Fatal("expected error for two input files")
	}
}

func TestInvalidEnum(t *testing.T) {
	if _, err := Load([]string{"--color", "rainbow"}); err == nil {
		t.Fatal("expected error for invalid --color")
	}
	if _, err := Load([]string{"--sort", "banana"}); err == nil {
		t.Fatal("expected error for invalid --sort")
	}
}

func TestFoldHideRepeatableFlags(t *testing.T) {
	cfg, err := Load([]string{
		"--fold", "^Sinatra", "--fold", "Rack::",
		"--hide", "a{2,3}", // regex with a comma: must not be CSV-split
		"in.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.FoldPatterns) != 2 || cfg.FoldPatterns[0] != "^Sinatra" || cfg.FoldPatterns[1] != "Rack::" {
		t.Errorf("FoldPatterns = %v, want two entries", cfg.FoldPatterns)
	}
	if len(cfg.HidePatterns) != 1 || cfg.HidePatterns[0] != "a{2,3}" {
		t.Errorf("HidePatterns = %v, want [a{2,3}] (no comma split)", cfg.HidePatterns)
	}
	if cfg.MatchMode != "regex" {
		t.Errorf("MatchMode = %q, want regex (default)", cfg.MatchMode)
	}
}

func TestMatchModeValidationAndOverride(t *testing.T) {
	if _, err := Load([]string{"--match-mode", "fuzzy"}); err == nil {
		t.Fatal("expected error for invalid --match-mode")
	}
	cfg, err := Load([]string{"--match-mode", "exact", "in.json"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MatchMode != "exact" {
		t.Errorf("MatchMode = %q, want exact", cfg.MatchMode)
	}
}

func TestFoldHideFromYAML(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "config.yaml")
	yaml := `
fold_patterns: ["^Sinatra", "^Rack"]
hide_patterns: ["Kernel#"]
match_mode: exact
`
	if err := os.WriteFile(yamlPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load([]string{"--config", yamlPath, "in.json"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.FoldPatterns) != 2 || len(cfg.HidePatterns) != 1 || cfg.MatchMode != "exact" {
		t.Errorf("YAML fold/hide/match_mode not applied: %+v", cfg)
	}
}

func TestShowAttributesCSV(t *testing.T) {
	cfg, err := Load([]string{"--show-attributes", "http.status_code, db.statement ,"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ShowAttributes) != 2 {
		t.Fatalf("ShowAttributes = %v, want 2 entries", cfg.ShowAttributes)
	}
	if cfg.ShowAttributes[0] != "http.status_code" || cfg.ShowAttributes[1] != "db.statement" {
		t.Errorf("ShowAttributes not trimmed: %v", cfg.ShowAttributes)
	}
}
