package planner

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/logger"
	"github.com/gcstr/dockform/internal/manifest"
)

func TestBuildPlan_Logging(t *testing.T) {
	// Create a buffer to capture logs
	var logBuf bytes.Buffer

	// Create logger with JSON format to buffer
	l, closer, err := logger.New(logger.Options{
		Out:             &logBuf,
		Format:          "json",
		Level:           "debug",
		ReportTimestamp: func() *bool { b := false; return &b }(), // Disable timestamps for consistent testing
	})
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	if closer != nil {
		defer func() { _ = closer.Close() }()
	}

	// Add run context
	l = l.With("run_id", "test123", "command", "dockform plan")
	ctx := logger.WithContext(context.Background(), l)

	// Create mock Docker client with some existing resources
	docker := newMockDocker()
	docker.volumes = []string{"existing-vol1", "existing-vol2"}

	// Create test configuration using multi-context schema
	sourceDir := t.TempDir()
	cfg := manifest.Config{
		Identifier: "test-app",
		Contexts: map[string]manifest.ContextConfig{
			"default": {},
		},
		Stacks: map[string]manifest.Stack{
			"default/app1": {
				Root:  "/tmp/app1",
				Files: []string{"docker-compose.yml"},
			},
			"default/app2": {
				Root:  "/tmp/app2",
				Files: []string{"docker-compose.yml"},
			},
		},
		DiscoveredFilesets: map[string]manifest.FilesetSpec{
			"default/app1/config": {
				Source:       "./config",
				SourceAbs:    sourceDir,
				TargetVolume: "config-vol",
				TargetPath:   "/etc/config",
				Context:      "default",
			},
		},
	}

	// Create planner with mock docker and build plan
	planner := NewWithDocker(docker)
	plan, err := planner.BuildPlan(ctx, cfg)
	if err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}

	// Verify plan was created
	if plan == nil {
		t.Fatal("Expected plan to be created")
	}

	// Parse log lines
	logLines := strings.Split(strings.TrimSpace(logBuf.String()), "\n")
	if len(logLines) < 1 {
		t.Fatalf("Expected at least 1 log line, got %d: %s", len(logLines), logBuf.String())
	}

	// Parse first log line (start)
	var startLog map[string]interface{}
	if err := json.Unmarshal([]byte(logLines[0]), &startLog); err != nil {
		t.Fatalf("Failed to parse start log: %v", err)
	}

	// Verify basic start log fields
	expectedStartFields := map[string]interface{}{
		"level":     "info",
		"msg":       "plan_build",
		"action":    "plan_build",
		"component": "planner",
		"status":    "started",
		"run_id":    "test123",
		"command":   "dockform plan",
	}

	for key, expected := range expectedStartFields {
		if actual, ok := startLog[key]; !ok {
			t.Errorf("Missing field %s in start log", key)
		} else if actual != expected {
			t.Errorf("Field %s: expected %v, got %v", key, expected, actual)
		}
	}

	// Parse last log line (completion)
	var endLog map[string]interface{}
	lastLine := logLines[len(logLines)-1]
	if err := json.Unmarshal([]byte(lastLine), &endLog); err != nil {
		t.Fatalf("Failed to parse end log: %v", err)
	}

	// Verify end log fields - check key fields that should be present
	endBasicFields := []string{"level", "msg", "action", "component", "status", "run_id", "command"}
	for _, key := range endBasicFields {
		if _, ok := endLog[key]; !ok {
			t.Errorf("Missing field %s in end log", key)
		}
	}

	// Verify change counts are present
	changeFields := []string{"changes_create", "changes_update", "changes_delete", "changes_total", "changed", "duration_ms"}
	for _, key := range changeFields {
		if _, ok := endLog[key]; !ok {
			t.Errorf("Missing %s in end log", key)
		}
	}
}
