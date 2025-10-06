package versioncmd_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/cli"
	"github.com/gcstr/dockform/internal/cli/versioncmd"
)

func TestVersionCmd_OutputsDetailedInfo(t *testing.T) {
	cmd := versioncmd.New()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute version command: %v", err)
	}

	got := out.String()

	// Check that it contains the expected sections
	expectedLines := []string{
		"Dockform",
		"Version:",
		"Go version:",
		"Git commit:",
		"Built:",
		"OS/Arch:",
	}

	for _, line := range expectedLines {
		if !strings.Contains(got, line) {
			t.Errorf("version output should contain %q; got: %q", line, got)
		}
	}

	// Should contain the version number
	if !strings.Contains(got, cli.Version()) {
		t.Errorf("version output should contain version %q; got: %q", cli.Version(), got)
	}
}

func TestVersionCmd_NoArgs(t *testing.T) {
	cmd := versioncmd.New()
	cmd.SetArgs([]string{"extra"})

	if err := cmd.Execute(); err == nil {
		t.Error("version command should reject extra arguments")
	}
}
