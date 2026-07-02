package apperr_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/manifest"
)

func TestWrapPreservesSentinel(t *testing.T) {
	base := manifest.ErrMissingRequired
	err := apperr.Wrap("manifest.Load", apperr.InvalidInput, base, "field %q is required", "compose.project")
	if !errors.Is(err, manifest.ErrMissingRequired) {
		t.Fatalf("want Is(..., ErrMissingRequired)=true")
	}
	if !apperr.IsKind(err, apperr.InvalidInput) {
		t.Fatalf("want kind=InvalidInput")
	}
}

func TestErrorStringIncludesOpAndMsg(t *testing.T) {
	err := apperr.New("dockercli.Sync", apperr.External, "docker run failed")
	got := err.Error()
	if !strings.Contains(got, "dockercli.Sync: docker run failed") {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestDeepestMessage_WalksNestedChain(t *testing.T) {
	// Simulates: dockercli.Exec captures stderr as Msg, planner.Apply wraps it
	// with a short "compose up ctx/stack" message. (*E).Error() on the outer
	// error collapses to the outer Msg only, but DeepestMessage should surface
	// the innermost stderr-bearing message.
	leaf := &apperr.E{
		Op:   "dockercli.Exec",
		Kind: apperr.External,
		Err:  errors.New("exit status 1"),
		Msg:  "manifest for henrygd/beszel-agent:0.18.8 not found: manifest unknown",
	}
	mid := apperr.Wrap("planner.Apply", apperr.External, leaf, "compose up hetzner-one/beszel")

	if got := apperr.DeepestMessage(mid); got != "manifest for henrygd/beszel-agent:0.18.8 not found: manifest unknown" {
		t.Fatalf("expected deepest stderr message, got: %q", got)
	}

	// Sanity: confirm that the outer error's own Error() collapses to only
	// Op+Msg, i.e. it does NOT include the stderr on its own. This is the
	// root-cause behavior DeepestMessage exists to work around.
	if strings.Contains(mid.Error(), "manifest unknown") {
		t.Fatalf("expected (*E).Error() to NOT include nested stderr, got: %q", mid.Error())
	}
}

func TestDeepestMessage_ThroughContextError(t *testing.T) {
	leaf := &apperr.E{Op: "dockercli.Exec", Kind: apperr.External, Err: errors.New("exit status 1"), Msg: "pull access denied: denied"}
	mid := apperr.Wrap("planner.Apply", apperr.External, leaf, "compose up ctx/stack")
	ctxErr := &apperr.ContextError{ContextName: "hetzner-three", Err: mid}

	if got := apperr.DeepestMessage(ctxErr); got != "pull access denied: denied" {
		t.Fatalf("expected deepest message through ContextError, got: %q", got)
	}
}

func TestDeepestMessage_PlainError(t *testing.T) {
	plain := errors.New("boom")
	if got := apperr.DeepestMessage(plain); got != "boom" {
		t.Fatalf("expected plain error message, got: %q", got)
	}
	if got := apperr.DeepestMessage(nil); got != "" {
		t.Fatalf("expected empty string for nil, got: %q", got)
	}
}

func TestContextError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("compose up failed")
	ce := &apperr.ContextError{ContextName: "hetzner-one", Err: cause}

	if got := ce.Error(); got != "context hetzner-one: compose up failed" {
		t.Fatalf("unexpected Error(): %q", got)
	}
	if !errors.Is(ce, cause) {
		t.Fatalf("expected errors.Is to find wrapped cause through Unwrap")
	}
}

func TestMultiError_PreservesChildCauses(t *testing.T) {
	// This exercises the aggregation contract relied on by
	// planner.executeParallel: wrapping child errors in ContextError and
	// collecting them in a MultiError (rather than pre-stringifying with %v)
	// must keep each child's underlying cause reachable via errors.As/Unwrap.
	leaf1 := &apperr.E{Op: "dockercli.Exec", Kind: apperr.External, Err: errors.New("exit status 1"), Msg: "manifest unknown"}
	leaf2 := &apperr.E{Op: "dockercli.Exec", Kind: apperr.External, Err: errors.New("exit status 1"), Msg: "no space left on device"}

	multi := &apperr.MultiError{Errors: []error{
		&apperr.ContextError{ContextName: "alpha", Err: apperr.Wrap("planner.Apply", apperr.External, leaf1, "compose up alpha/svc")},
		&apperr.ContextError{ContextName: "beta", Err: apperr.Wrap("planner.Apply", apperr.External, leaf2, "compose up beta/svc")},
	}}

	agg := &apperr.E{Op: "planner.ExecuteAcrossContexts", Kind: apperr.External, Err: multi, Msg: "multiple context errors"}

	var gotMulti *apperr.MultiError
	if !errors.As(agg.Err, &gotMulti) {
		t.Fatalf("expected to recover *MultiError from aggregate's Err")
	}
	if len(gotMulti.Errors) != 2 {
		t.Fatalf("expected 2 child errors, got %d", len(gotMulti.Errors))
	}
	if got := apperr.DeepestMessage(gotMulti.Errors[0]); got != "manifest unknown" {
		t.Fatalf("expected first child's deepest message preserved, got: %q", got)
	}
	if got := apperr.DeepestMessage(gotMulti.Errors[1]); got != "no space left on device" {
		t.Fatalf("expected second child's deepest message preserved, got: %q", got)
	}
}
