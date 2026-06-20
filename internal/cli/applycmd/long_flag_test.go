package applycmd_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/cli"
	"github.com/gcstr/dockform/internal/cli/clitest"
)

// applyUpToDateDockerStub mirrors the plancmd stub: nginx is running and
// up-to-date (hash matches) but orphan-vol needs deletion.
// Provides an apply-phase response for volume rm as well.
const applyUpToDateDockerStub = `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    exit 0 ;;
  volume)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "orphan-vol"; exit 0; fi
    if [ "$sub" = "rm" ]; then exit 0; fi ;;
  network)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then exit 0; fi ;;
  compose)
    for a in "$@"; do [ "$a" = "--services" ] && { echo "nginx"; exit 0; }; done
    prev=""
    for a in "$@"; do
      if [ "$prev" = "--hash" ]; then echo "nginx deadbeef"; exit 0; fi
      prev="$a"
    done
    saw_ps=0; saw_format=0; saw_json=0
    for a in "$@"; do
      [ "$a" = "ps" ] && saw_ps=1
      [ "$a" = "--format" ] && saw_format=1
      [ "$a" = "json" ] && saw_json=1
    done
    if [ "$saw_ps" = "1" ] && [ "$saw_format" = "1" ] && [ "$saw_json" = "1" ]; then
      printf '[{"Name":"website_nginx_1","Service":"nginx","State":"running","Image":"nginx","Project":"website","Publishers":[]}]\n'
      exit 0
    fi
    exit 0 ;;
  inspect)
    fmt_arg=""
    first_container=""
    skip_next=0
    for a in "$@"; do
      if [ "$skip_next" = "1" ]; then
        skip_next=0
        fmt_arg="$a"
        continue
      fi
      if [ "$a" = "-f" ]; then
        skip_next=1
        continue
      fi
      if [ -z "$first_container" ] && [ "${a#-}" = "$a" ]; then
        first_container="$a"
      fi
    done
    labels='{"com.docker.compose.config-hash":"deadbeef","io.dockform.identifier":"demo"}'
    case "$fmt_arg" in
      *Name*tab*)
        echo "${first_container}	${labels}" ;;
      *)
        echo "$labels" ;;
    esac
    exit 0 ;;
  ps)
    exit 0 ;;
esac
exit 0
`

// TestApply_DefaultChangesOnly verifies that the apply plan review output omits
// no-op lines in the default (changes-only) mode.
func TestApply_DefaultChangesOnly(t *testing.T) {
	undo := clitest.WithCustomDockerStub(t, applyUpToDateDockerStub)
	defer undo()

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("no\n")) // decline — only test the plan review
	root.SetArgs([]string{"apply", "--manifest", clitest.BasicConfigPath(t)})

	if err := root.Execute(); err != nil {
		t.Fatalf("apply execute: %v", err)
	}
	got := out.String()

	// Default (changes-only) must NOT show per-service no-op line.
	if strings.Contains(got, "up-to-date") {
		t.Fatalf("default apply output should omit 'up-to-date' lines; got: %s", got)
	}
	// It should still show a changed resource (the orphan-vol deletion).
	if !strings.Contains(got, "unchanged") && !strings.Contains(got, "No changes") {
		t.Fatalf("expected 'unchanged' or 'No changes' in default apply output; got: %s", got)
	}
}

// TestApply_LongShowsNoOpLines verifies that --long produces "up-to-date" lines
// in the plan review that are absent from the default changes-only output.
func TestApply_LongShowsNoOpLines(t *testing.T) {
	undo := clitest.WithCustomDockerStub(t, applyUpToDateDockerStub)
	defer undo()

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("no\n")) // decline — only test the plan review
	root.SetArgs([]string{"apply", "--long", "--manifest", clitest.BasicConfigPath(t)})

	if err := root.Execute(); err != nil {
		t.Fatalf("apply --long execute: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, "up-to-date") {
		t.Fatalf("expected 'up-to-date' lines in --long apply output; got: %s", got)
	}
}
