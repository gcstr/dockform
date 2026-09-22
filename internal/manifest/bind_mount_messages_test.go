package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
)

// The load-time message must not change when its migration steps are shared.
const loadTimeGolden = `stack default/web contains bind mounts with relative paths which will not work correctly with remote Docker contexts.

Bind mounts found:
  - ./config

Bind mounts reference paths on the Docker daemon's filesystem, not your local machine.
When using remote Docker contexts, these paths would be resolved on the remote server.

Solution: Use Dockform filesets for syncing local files to remote volumes.

Migration steps:
  1. Create a 'volumes/' directory in your stack: default/web/volumes/
  2. Move your bind mount directories into volumes/
     Example: ./config → default/web/volumes/config/
  3. Change compose volumes to use named volumes:
     - Old: ./config:/app/config
     - New: web_config:/app/config
  4. Declare the volume in dockform.yaml:
     contexts:
       default:
         volumes:
           web_config: {}

Dockform will auto-discover the fileset and sync files correctly to the remote server.
See: https://github.com/gcstr/dockform#filesets for more information.`

func TestLoadTimeBindMountMessage_Unchanged(t *testing.T) {
	dir := t.TempDir()
	compose := "services:\n  web:\n    image: nginx\n    volumes:\n      - ./config:/app/config\n"
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	err := validateBindMountsInComposeFile("default/web", Stack{Root: dir, Files: []string{"compose.yaml"}})
	if err == nil {
		t.Fatal("expected the relative bind mount to be rejected")
	}
	if got := apperr.DeepestMessage(err); got != loadTimeGolden {
		t.Errorf("load-time message changed.\n--- got ---\n%s\n--- want ---\n%s", got, loadTimeGolden)
	}
}

func TestLocalBindMountsMessage(t *testing.T) {
	msg := LocalBindMountsMessage("prod/web", []string{"/home/me/proj/web/cfg", "/home/me/proj/shared"})
	for _, want := range []string{
		"stack prod/web",
		"context prod is remote",
		"  - /home/me/proj/web/cfg\n",
		"  - /home/me/proj/shared\n",
		"empty directory",
		"Solution: Use Dockform filesets",
		"See: https://github.com/gcstr/dockform#filesets",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q:\n%s", want, msg)
		}
	}
}
