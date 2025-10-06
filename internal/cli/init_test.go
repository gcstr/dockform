package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCmd_CurrentDirectory(t *testing.T) {
	// Create a temporary directory and change to it
	tempDir := t.TempDir()
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get current dir: %v", err)
	}
	defer func() { _ = os.Chdir(oldCwd) }()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("change to temp dir: %v", err)
	}

	// Run init command
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"init"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init command failed: %v", err)
	}

	// Check output message
	output := out.String()
	if !strings.Contains(output, "Created dockform.yml:") {
		t.Fatalf("expected success message, got: %q", output)
	}

	// Check that file was created
	configPath := filepath.Join(tempDir, "dockform.yml")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("dockform.yml not created: %v", err)
	}

	// Check file content
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read created file: %v", err)
	}

	contentStr := string(content)
	expectedSections := []string{"docker:", "stacks:", "networks:", "filesets:"}
	for _, section := range expectedSections {
		if !strings.Contains(contentStr, section) {
			t.Fatalf("missing section %q in generated file", section)
		}
	}

	// Should contain comments
	if !strings.Contains(contentStr, "# Dockform Configuration Template") {
		t.Fatalf("missing template header comment")
	}
}

func TestInitCmd_WithDirectory(t *testing.T) {
	tempDir := t.TempDir()
	targetDir := filepath.Join(tempDir, "project")

	// Create target directory
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("create target dir: %v", err)
	}

	// Run init command with directory argument
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"init", targetDir})

	if err := root.Execute(); err != nil {
		t.Fatalf("init command failed: %v", err)
	}

	// Check that file was created in the right place
	configPath := filepath.Join(targetDir, "dockform.yml")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("dockform.yml not created in target dir: %v", err)
	}
}

func TestInitCmd_FileAlreadyExists(t *testing.T) {
	tempDir := t.TempDir()

	// Create an existing dockform.yml file
	configPath := filepath.Join(tempDir, "dockform.yml")
	if err := os.WriteFile(configPath, []byte("existing content"), 0644); err != nil {
		t.Fatalf("create existing file: %v", err)
	}

	// Run init command
	root := newRootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"init", tempDir})

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error when file already exists")
	}

	// Check error message (should be in error output or error string)
	errOutput := errOut.String()
	errStr := err.Error()
	if !strings.Contains(errOutput, "already exists") && !strings.Contains(errStr, "already exists") {
		t.Fatalf("expected 'already exists' error message, got err: %q, errOut: %q", errStr, errOutput)
	}
}

func TestInitCmd_NonExistentDirectory(t *testing.T) {
	tempDir := t.TempDir()
	nonExistentDir := filepath.Join(tempDir, "does-not-exist")

	// Run init command with non-existent directory
	root := newRootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"init", nonExistentDir})

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error for non-existent directory")
	}

	// Check error message (should be in error output or error string)
	errOutput := errOut.String()
	errStr := err.Error()
	if !strings.Contains(errOutput, "does not exist") && !strings.Contains(errStr, "does not exist") {
		t.Fatalf("expected 'does not exist' error message, got err: %q, errOut: %q", errStr, errOutput)
	}
}
