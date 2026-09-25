package imagescmd_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/cli"
	"github.com/gcstr/dockform/internal/cli/common"
)

// TestImagesCommands_UseSSHTunnel verifies images check, pull and upgrade set
// up the SSH transport like plan/apply: with the default tunnel transport, a
// context whose tunnel cannot open stops the command with the tunnel error
// instead of falling back to docker's own ssh helper.
func TestImagesCommands_UseSSHTunnel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses Unix shell script; skipping on Windows")
	}
	binDir := t.TempDir()
	sshScript := "#!/bin/sh\necho 'nobody@remote.invalid: Permission denied (publickey).' >&2\nexit 255\n"
	if err := os.WriteFile(filepath.Join(binDir, "ssh"), []byte(sshScript), 0o755); err != nil {
		t.Fatalf("write ssh stub: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKFORM_SSH_TRANSPORT", "")

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "dockform.yml")
	manifestYAML := "identifier: demo\ncontexts:\n  remote:\n    host: ssh://nobody@remote.invalid\n"
	if err := os.WriteFile(manifestPath, []byte(manifestYAML), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	for _, sub := range []string{"check", "pull", "upgrade"} {
		t.Run(sub, func(t *testing.T) {
			root := cli.TestNewRootCmd()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs([]string{"images", sub, "--manifest", manifestPath})

			err := root.Execute()
			if err == nil {
				t.Fatalf("expected images %s to fail when the SSH tunnel cannot open, output: %q", sub, out.String())
			}
			if !errors.Is(err, common.ErrSSHTunnel) {
				t.Errorf("expected an SSH tunnel error, got: %v", err)
			}
			if !strings.Contains(err.Error(), "remote") {
				t.Errorf("error should name the context, got: %v", err)
			}
		})
	}
}
