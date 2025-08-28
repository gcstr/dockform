package apperr_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/config"
)

func TestWrapPreservesSentinel(t *testing.T) {
	base := config.ErrMissingRequired
	err := apperr.Wrap("config.Load", apperr.InvalidInput, base, "field %q is required", "compose.project")
	if !errors.Is(err, config.ErrMissingRequired) {
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
