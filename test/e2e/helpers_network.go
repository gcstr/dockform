package e2e

import (
	"os/exec"
	"strings"
	"testing"
)

// ensureNetworkCreatableOrSkip attempts to create a temporary network to verify
// the daemon can allocate new networks. If it cannot (e.g., managed remote
// daemons with exhausted address pools), the test is skipped to avoid false
// negatives in environments where network creation is restricted.
func ensureNetworkCreatableOrSkip(t *testing.T, identifier string) {
	t.Helper()
	name := "df_e2e_" + identifier + "_probe_net"
	cmd := exec.Command("docker", "network", "create", "--label", "io.dockform.identifier="+identifier, name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.ToLower(string(out))
		if strings.Contains(msg, "predefined address pools") || strings.Contains(msg, "address pool") || strings.Contains(msg, "subnetted") {
			t.Skip("skipping: docker daemon cannot allocate new networks (address pools exhausted)")
		}
		// Unknown failure; be conservative and skip rather than failing e2e on host-specific issues
		t.Skipf("skipping: unable to create test network: %v\n%s", err, string(out))
		return
	}
	// Best-effort cleanup
	_ = exec.Command("docker", "network", "rm", name).Run()
}
