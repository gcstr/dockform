package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNetworkDrift_Recreate ensures that when a manifest specifies a different driver/options
// than an existing network, Dockform recreates the network safely and services start.
func TestNetworkDrift_Recreate(t *testing.T) {
	ctx := context.Background()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}
	_ = logDockerContext(t)
	runID := uniqueID()
	identifier := runID

	ensureNetworkCreatableOrSkip(t, identifier)

	// Pre-create external volume referenced by compose
	_ = exec.Command("docker", "volume", "create", "--label", "io.dockform.identifier="+identifier, "df_e2e_"+runID+"_vol").Run()

	// Pre-create the network with the expected name but without desired options to induce drift
	netName := "df_e2e_" + runID + "_net"
	_ = exec.Command("docker", "network", "create", "--label", "io.dockform.identifier="+identifier, netName).Run()

	// Prepare simple scenario that references the external network name
	// Prepare temp workdir by copying scenario
	tempDir := t.TempDir()
	src := filepath.Join("testdata", "scenarios", "simple")
	if err := copyTree(src, tempDir); err != nil {
		t.Fatalf("copy scenario: %v", err)
	}
	bin := buildDockform(t)
	env := append([]string{}, os.Environ()...)
	env = append(env, "DOCKFORM_RUN_ID="+runID)

	// Modify manifest to specify driver/options so drift is detectable
	manifestPath := filepath.Join(tempDir, "dockform.yml")
	b, err := os.ReadFile(manifestPath)
	if err == nil {
		s := string(b)
		// The network entry is indented 6 spaces, so driver/options need 8 spaces
		s = strings.Replace(s,
			"df_e2e_${DOCKFORM_RUN_ID}_net: {}",
			"df_e2e_${DOCKFORM_RUN_ID}_net:\n        driver: bridge\n        options:\n          com.docker.network.bridge.enable_icc: \"false\"",
			1,
		)
		_ = os.WriteFile(manifestPath, []byte(s), 0644)
	}

	// First plan/apply should detect drift and recreate network, then start service
	_ = runCmd(t, tempDir, env, bin, "plan", "-c", tempDir)
	_ = runCmd(t, tempDir, env, bin, "apply", "--skip-confirmation", "-c", tempDir)

	// Verify container is running with our label and network exists
	names := dockerLines(t, ctx, "ps", "--format", "{{.Names}}", "--filter", "label=io.dockform.identifier="+identifier)
	if len(names) == 0 {
		t.Fatalf("expected running container with label io.dockform.identifier=%s", identifier)
	}
	nets := dockerLines(t, ctx, "network", "ls", "--format", "{{.Name}}", "--filter", "label=io.dockform.identifier="+identifier)
	if !contains(nets, netName) {
		t.Fatalf("expected network labeled io.dockform.identifier=%s", identifier)
	}

	// Clean up via prune to leave environment tidy
	_ = runCmd(t, tempDir, env, bin, "destroy", "--skip-confirmation", "-c", tempDir)
}

func contains(ss []string, s string) bool {
	for _, it := range ss {
		if strings.TrimSpace(it) == s {
			return true
		}
	}
	return false
}
