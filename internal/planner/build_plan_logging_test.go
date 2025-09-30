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
	docker.networks = []string{"existing-net1"}

	// Create test configuration
	cfg := manifest.Config{
		Docker: manifest.DockerConfig{
			Context:    "test",
			Identifier: "test-app",
		},
		Volumes: map[string]manifest.TopLevelResourceSpec{
			"new-vol":       {},
			"existing-vol1": {}, // This one exists
		},
		Networks: map[string]manifest.NetworkSpec{
			"new-net":       {},
			"existing-net1": {}, // This one exists
		},
		Applications: map[string]manifest.Application{
			"app1": {
				Root:  "/tmp/app1",
				Files: []string{"docker-compose.yml"},
			},
			"app2": {
				Root:  "/tmp/app2",
				Files: []string{"docker-compose.yml"},
			},
		},
		Filesets: map[string]manifest.FilesetSpec{
			"config": {
				Source:       "./config",
				SourceAbs:    "/tmp/config",
				TargetVolume: "config-vol",
				TargetPath:   "/etc/config",
			},
		},
	}

	// Create planner and build plan
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
	if len(logLines) < 2 {
		t.Fatalf("Expected at least 2 log lines (start + debug + end), got %d: %s", len(logLines), logBuf.String())
	}

	// Parse first log line (start)
	var startLog map[string]interface{}
	if err := json.Unmarshal([]byte(logLines[0]), &startLog); err != nil {
		t.Fatalf("Failed to parse start log: %v", err)
	}

	// Verify start log fields
	expectedStartFields := map[string]interface{}{
		"level":                "info",
		"msg":                  "plan_build",
		"action":               "plan_build",
		"component":            "planner",
		"resource":             "test-app",
		"resource_kind":        "plan",
		"volumes_desired":      float64(2),
		"networks_desired":     float64(2),
		"filesets_desired":     float64(1),
		"applications_desired": float64(2),
		"status":               "started",
		"run_id":               "test123",
		"command":              "dockform plan",
	}

	for key, expected := range expectedStartFields {
		if actual, ok := startLog[key]; !ok {
			t.Errorf("Missing field %s in start log", key)
		} else if actual != expected {
			t.Errorf("Field %s: expected %v, got %v", key, expected, actual)
		}
	}

	// Find the debug log line (resource discovery)
	var debugLog map[string]interface{}
	debugFound := false
	for _, line := range logLines {
		var logEntry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &logEntry); err != nil {
			continue
		}
		if logEntry["level"] == "debug" && logEntry["msg"] == "resource_discovery" {
			debugLog = logEntry
			debugFound = true
			break
		}
	}

	if !debugFound {
		t.Fatal("Expected debug log for resource discovery not found")
	}

	// Verify debug log fields
	expectedDebugFields := map[string]interface{}{
		"level":          "debug",
		"msg":            "resource_discovery",
		"component":      "planner",
		"volumes_found":  float64(2),
		"networks_found": float64(1),
		"run_id":         "test123",
		"command":        "dockform plan",
	}

	for key, expected := range expectedDebugFields {
		if actual, ok := debugLog[key]; !ok {
			t.Errorf("Missing field %s in debug log", key)
		} else if actual != expected {
			t.Errorf("Debug field %s: expected %v, got %v", key, expected, actual)
		}
	}

	// Parse last log line (completion)
	var endLog map[string]interface{}
	lastLine := logLines[len(logLines)-1]
	if err := json.Unmarshal([]byte(lastLine), &endLog); err != nil {
		t.Fatalf("Failed to parse end log: %v", err)
	}

	// Verify end log fields
	expectedEndFields := map[string]interface{}{
		"level":             "info",
		"msg":               "plan_build",
		"action":            "plan_build",
		"component":         "planner",
		"resource":          "test-app",
		"status":            "ok",
		"volumes_existing":  float64(2),
		"networks_existing": float64(1),
		"run_id":            "test123",
		"command":           "dockform plan",
	}

	for key, expected := range expectedEndFields {
		if actual, ok := endLog[key]; !ok {
			t.Errorf("Missing field %s in end log", key)
		} else if actual != expected {
			t.Errorf("End field %s: expected %v, got %v", key, expected, actual)
		}
	}

	// Verify change counts are present and reasonable
	if _, ok := endLog["changes_create"]; !ok {
		t.Error("Missing changes_create in end log")
	}
	if _, ok := endLog["changes_update"]; !ok {
		t.Error("Missing changes_update in end log")
	}
	if _, ok := endLog["changes_delete"]; !ok {
		t.Error("Missing changes_delete in end log")
	}
	if _, ok := endLog["changes_total"]; !ok {
		t.Error("Missing changes_total in end log")
	}
	if _, ok := endLog["changed"]; !ok {
		t.Error("Missing changed in end log")
	}
	if _, ok := endLog["duration_ms"]; !ok {
		t.Error("Missing duration_ms in end log")
	}
}
