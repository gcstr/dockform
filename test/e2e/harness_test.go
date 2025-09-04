package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestMain sets a local Docker context for e2e runs when possible.
// Priority:
// - E2E_DOCKER_CONTEXT if provided
// - "default" if available
// - otherwise, leave DOCKER_CONTEXT unchanged
func TestMain(m *testing.M) {
	// If user provided explicit context, honor it
	if want := strings.TrimSpace(os.Getenv("E2E_DOCKER_CONTEXT")); want != "" {
		if hasContext(want) {
			_ = os.Setenv("DOCKER_CONTEXT", want)
			fmt.Fprintf(os.Stderr, "[e2e] Using Docker context from E2E_DOCKER_CONTEXT: %s\n", want)
		} else {
			fmt.Fprintf(os.Stderr, "[e2e] Warning: E2E_DOCKER_CONTEXT=%s not found; leaving DOCKER_CONTEXT unchanged\n", want)
		}
	} else if hasContext("default") {
		// Prefer the local default context when available
		_ = os.Setenv("DOCKER_CONTEXT", "default")
		fmt.Fprintln(os.Stderr, "[e2e] Using Docker context: default")
	}

	os.Exit(m.Run())
}

func hasContext(name string) bool {
	out, err := exec.Command("docker", "context", "inspect", name).CombinedOutput()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}
