package planner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/config"
	"github.com/gcstr/dockform/internal/ui"
)

func TestPlan_MinimalExample(t *testing.T) {
	cfgPath := filepath.Join("..", "..", "example", "dockform.yml")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("example config missing: %v", err)
	}

	// Parse using config.Load to ensure defaults
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	pln, err := New().BuildPlan(context.Background(), cfg)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}

	got := ui.StripANSI(pln.String()) + "\n"
	goldenPath := filepath.Join("..", "..", "test", "golden", "planner_minimal.txt")

	if os.Getenv("TEST_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	canonicalize := func(s string) string {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.TrimRight(s, "\n")
		return s + "\n"
	}

	if canonicalize(string(want)) != canonicalize(got) {
		t.Fatalf("planner output mismatch\n--- want\n%s\n--- got\n%s", string(want), got)
	}
}
