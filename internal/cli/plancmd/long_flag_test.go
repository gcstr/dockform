package plancmd_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/cli"
	"github.com/gcstr/dockform/internal/cli/clitest"
)

// upToDateDockerStub is a docker stub where the nginx service is running and
// up-to-date (hash matches), but there is an orphan volume that will be deleted.
// This produces a plan with at least one change (delete orphan-vol) and at
// least one no-op (nginx up-to-date).
const upToDateDockerStub = `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    exit 0 ;;
  volume)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "orphan-vol"; exit 0; fi ;;
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
    # Handle both single-label ({{json .Config.Labels}}) and
    # multi-label ({{.Name}}\t{{json .Config.Labels}}) format strings.
    for a in "$@"; do
      case "$a" in
        *Name*tab*) ;;  # ignored sentinel
      esac
    done
    # The -f flag is $1 (already shifted off cmd). Remaining args include
    # the format string then container names. We respond with the appropriate
    # label JSON regardless of format variant — the caller parses what it needs.
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
        # Multi-container format: "{{.Name}}\t{{json .Config.Labels}}"
        echo "${first_container}	${labels}" ;;
      *)
        # Single-container format: "{{json .Config.Labels}}"
        echo "$labels" ;;
    esac
    exit 0 ;;
  ps)
    exit 0 ;;
esac
exit 0
`

// TestPlan_DefaultChangesOnly verifies that the default plan output omits
// no-op lines (like "up-to-date") and instead shows an "unchanged" summary.
func TestPlan_DefaultChangesOnly(t *testing.T) {
	t.Helper()
	undo := clitest.WithCustomDockerStub(t, upToDateDockerStub)
	defer undo()

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"plan", "--manifest", clitest.BasicConfigPath(t)})

	if err := root.Execute(); err != nil {
		t.Fatalf("plan execute: %v", err)
	}
	got := out.String()

	// Default (changes-only) must NOT show the per-service no-op line.
	if strings.Contains(got, "up-to-date") {
		t.Fatalf("default output should omit 'up-to-date' lines; got: %s", got)
	}
	// It should mention how many resources are unchanged.
	if !strings.Contains(got, "unchanged") && !strings.Contains(got, "No changes") {
		t.Fatalf("expected 'unchanged' or 'No changes' in default output; got: %s", got)
	}
}

// TestPlan_LongShowsNoOpLines verifies that --long produces "up-to-date" lines
// that are absent from the default changes-only output.
func TestPlan_LongShowsNoOpLines(t *testing.T) {
	t.Helper()
	undo := clitest.WithCustomDockerStub(t, upToDateDockerStub)
	defer undo()

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"plan", "--long", "--manifest", clitest.BasicConfigPath(t)})

	if err := root.Execute(); err != nil {
		t.Fatalf("plan --long execute: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, "up-to-date") {
		t.Fatalf("expected 'up-to-date' lines in --long output; got: %s", got)
	}
}
