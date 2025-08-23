package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlan_PrintsRemovalGuidance_WhenRemovalsPresent_AndNoPrune_Solo(t *testing.T) {
	defer withStubDocker(t)()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"plan", "-c", filepath.Join("..", "..", "example", "dockform.yml")})
	if err := root.Execute(); err != nil {
		t.Fatalf("plan execute: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "[remove]") {
		t.Fatalf("expected remove lines in plan; got: %s", got)
	}
	if !strings.Contains(got, "No resources will be removed. Include --prune to delete them") {
		t.Fatalf("expected prune guidance; got: %s", got)
	}
}
