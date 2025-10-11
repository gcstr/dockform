package secretcmd

import (
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/manifest"
)

func TestResolveRecipientsAndKeyRequiresConfig(t *testing.T) {
	if _, err := resolveRecipientsAndKey(manifest.Config{}); err == nil || !apperr.IsKind(err, apperr.InvalidInput) {
		t.Fatalf("expected invalid input error for missing sops config, got: %v", err)
	}
}

func TestResolveRecipientsAndKeyWithAgeRecipients(t *testing.T) {
	cfg := manifest.Config{
		Sops: &manifest.SopsConfig{
			Age: &manifest.SopsAgeConfig{
				KeyFile:    "/etc/age/key.txt",
				Recipients: []string{"age1example"},
			},
		},
	}
	res, err := resolveRecipientsAndKey(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := res.opts.AgeRecipients; len(got) != 1 || got[0] != "age1example" {
		t.Fatalf("expected age recipient propagated, got: %v", got)
	}
}

func TestResolveRecipientsAndKeyWithPgpRecipients(t *testing.T) {
	cfg := manifest.Config{
		Sops: &manifest.SopsConfig{
			Pgp: &manifest.SopsPgpConfig{Recipients: []string{"ABC123"}},
		},
	}
	res, err := resolveRecipientsAndKey(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.opts.PgpRecipients) != 1 || res.opts.PgpRecipients[0] != "ABC123" {
		t.Fatalf("expected pgp recipients propagated, got: %v", res.opts.PgpRecipients)
	}
}

func TestResolveRecipientsAndKeyRequiresRecipients(t *testing.T) {
	cfg := manifest.Config{Sops: &manifest.SopsConfig{Age: &manifest.SopsAgeConfig{}}}
	if _, err := resolveRecipientsAndKey(cfg); err == nil || !apperr.IsKind(err, apperr.InvalidInput) {
		t.Fatalf("expected invalid input error for missing recipients, got: %v", err)
	}
}
