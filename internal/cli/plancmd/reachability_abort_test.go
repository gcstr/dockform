package plancmd_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/cli"
	"github.com/gcstr/dockform/internal/cli/clitest"
)

// TestPlan_AbortsWhenContextUnreachable verifies that the plan command aborts
// with an Unavailable error (exit 69) when the Docker daemon cannot be reached.
// The stub makes "docker version" fail for every context, simulating a down daemon.
func TestPlan_AbortsWhenContextUnreachable(t *testing.T) {
	t.Helper()
	undo := clitest.WithCustomDockerStub(t, `#!/bin/sh
case "$1" in
  version) echo 'Cannot connect to the Docker daemon' >&2; exit 1 ;;
esac
exit 0
`)
	defer undo()

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"plan", "--manifest", clitest.BasicConfigPath(t)})

	err := root.Execute()

	// The command must abort — plan must not proceed when a daemon is unreachable.
	if err == nil {
		t.Fatalf("expected plan to abort when context is unreachable, got nil error (output: %s)", out.String())
	}

	// The error must be classified as Unavailable (exit 69) — that's the exit-code contract.
	if !apperr.IsKind(err, apperr.Unavailable) {
		t.Fatalf("expected Unavailable error kind (exit 69), got: %v", err)
	}

	// The error message must reference the failing context, confirming the gate fired.
	// The validator probes the daemon during config validation and returns an Unavailable
	// error with the context name when the daemon cannot be reached.
	msg := err.Error()
	if !strings.Contains(msg, "context") {
		t.Fatalf("expected error message to reference the unreachable context, got: %v", err)
	}
}
