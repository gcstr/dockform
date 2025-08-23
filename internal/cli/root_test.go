package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRoot_HasSubcommandsAndConfigFlag(t *testing.T) {
	cmd := newRootCmd()
	if cmd.PersistentFlags().Lookup("config") == nil {
		t.Fatalf("expected persistent --config flag on root command")
	}
	foundPlan := false
	foundApply := false
	for _, c := range cmd.Commands() {
		if c.Name() == "plan" {
			foundPlan = true
		}
		if c.Name() == "apply" {
			foundApply = true
		}
	}
	if !foundPlan || !foundApply {
		t.Fatalf("expected plan and apply subcommands; got plan=%v apply=%v", foundPlan, foundApply)
	}
}

func TestRoot_VersionFlagPrints(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute --version: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, Version()+"\n") {
		t.Fatalf("version output mismatch; got: %q", got)
	}
}

func TestRoot_HelpShowsProjectHome(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute --help: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Project home: https://github.com/gcstr/dockform") {
		t.Fatalf("help output missing project home; got: %q", got)
	}
}

func TestPlan_HasPruneFlag(t *testing.T) {
	cmd := newPlanCmd()
	if cmd.Flags().Lookup("prune") == nil {
		t.Fatalf("expected --prune flag on plan command")
	}
}

func TestApply_HasPruneFlag(t *testing.T) {
	cmd := newApplyCmd()
	if cmd.Flags().Lookup("prune") == nil {
		t.Fatalf("expected --prune flag on apply command")
	}
}
