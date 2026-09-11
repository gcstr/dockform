package dockercli

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// netLsExec is fakeExec with a canned `docker network ls` answer.
type netLsExec struct {
	fakeExec
	out string
}

func (n *netLsExec) Run(ctx context.Context, args ...string) (string, error) {
	n.lastArgs = args
	return n.out, nil
}

// Pruning a removed stack's network needs to know which compose project owns it,
// so ListComposeNetworks must return the project label, not just the name.
func TestListComposeNetworks_ReturnsOwningProject(t *testing.T) {
	e := &netLsExec{out: "app_default|app\nzz-probe-a_default|zz-probe-a\nodd|\n"}
	c := &Client{exec: e, identifier: "homeserver"}

	got, err := c.ListComposeNetworks(context.Background())
	if err != nil {
		t.Fatalf("ListComposeNetworks: %v", err)
	}
	want := map[string]string{"app_default": "app", "zz-probe-a_default": "zz-probe-a", "odd": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListComposeNetworks = %v, want %v", got, want)
	}
	joined := strings.Join(e.lastArgs, " ")
	for _, w := range []string{
		"label=io.dockform.identifier=homeserver",
		"label=com.docker.compose.project",
		`{{.Label "com.docker.compose.project"}}`,
	} {
		if !strings.Contains(joined, w) {
			t.Errorf("docker args should contain %q, got: %v", w, e.lastArgs)
		}
	}
}
