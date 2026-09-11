package dockercli

import (
	"context"
	"testing"
)

// `docker compose ps --format json` prints nothing for a project with no
// containers. That is an empty stack, not malformed output.
func TestComposePs_EmptyOutputMeansNoContainers(t *testing.T) {
	for _, out := range []string{"", "\n", "  \n"} {
		c := &Client{exec: &fakeExec{outPs: out}}
		items, err := c.ComposePs(context.Background(), ".", nil, nil, nil, "proj", nil)
		if err != nil {
			t.Fatalf("output %q: unexpected error: %v", out, err)
		}
		if len(items) != 0 {
			t.Fatalf("output %q: items = %v, want none", out, items)
		}
	}
}
