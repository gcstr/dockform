package imagescmd

import (
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/images"
)

func sampleInputs() []images.CheckInput {
	return []images.CheckInput{
		{
			StackKey: "hetzner-one/linkwarden",
			Services: map[string]images.ServiceSpec{
				"app":      {Image: "linkwarden/linkwarden:v2.10"},
				"postgres": {Image: "postgres:17"},
				"backup":   {Image: "offen/docker-volume-backup:v2"},
			},
		},
		{
			StackKey: "hetzner-two/bitwarden",
			Services: map[string]images.ServiceSpec{
				"app": {Image: "bitwarden/lite:2026.4.0"},
			},
		},
	}
}

func TestFilterInputsByServices_NoNamesReturnsInputsUnchanged(t *testing.T) {
	in := sampleInputs()
	out, err := filterInputsByServices(in, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("expected %d inputs, got %d", len(in), len(out))
	}
}

func TestFilterInputsByServices_SingleServiceOneStack(t *testing.T) {
	out, err := filterInputsByServices(sampleInputs(), []string{"backup"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 stack, got %d", len(out))
	}
	if out[0].StackKey != "hetzner-one/linkwarden" {
		t.Errorf("unexpected stack: %s", out[0].StackKey)
	}
	if _, ok := out[0].Services["backup"]; !ok {
		t.Errorf("expected 'backup' in services, got %v", out[0].Services)
	}
	if len(out[0].Services) != 1 {
		t.Errorf("expected exactly one service, got %d", len(out[0].Services))
	}
}

func TestFilterInputsByServices_ServiceAcrossStacks(t *testing.T) {
	out, err := filterInputsByServices(sampleInputs(), []string{"app"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 stacks, got %d", len(out))
	}
	for _, in := range out {
		if len(in.Services) != 1 {
			t.Errorf("stack %s: expected 1 service, got %d", in.StackKey, len(in.Services))
		}
		if _, ok := in.Services["app"]; !ok {
			t.Errorf("stack %s: expected 'app', got %v", in.StackKey, in.Services)
		}
	}
}

func TestFilterInputsByServices_MultipleNamesOR(t *testing.T) {
	out, err := filterInputsByServices(sampleInputs(), []string{"backup", "postgres"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 stack (linkwarden), got %d", len(out))
	}
	if len(out[0].Services) != 2 {
		t.Errorf("expected 2 services, got %v", out[0].Services)
	}
}

func TestFilterInputsByServices_ZeroMatchErrorsWithAvailableList(t *testing.T) {
	_, err := filterInputsByServices(sampleInputs(), []string{"backp"})
	if err == nil {
		t.Fatal("expected error for zero matches, got nil")
	}
	if !apperr.IsKind(err, apperr.InvalidInput) {
		t.Errorf("expected InvalidInput kind, got: %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, `"backp"`) {
		t.Errorf("expected requested name in error, got: %s", msg)
	}
	if !strings.Contains(msg, "app") || !strings.Contains(msg, "backup") {
		t.Errorf("expected available services listed, got: %s", msg)
	}
	if !strings.Contains(msg, "hetzner-one/linkwarden") {
		t.Errorf("expected stack key listed, got: %s", msg)
	}
}

func TestFilterInputsByServices_ScopedStackEmpty(t *testing.T) {
	// When --stack already narrowed inputs to bitwarden, asking for "backup"
	// should fail even though backup exists in another stack (out of scope).
	scoped := []images.CheckInput{
		{
			StackKey: "hetzner-two/bitwarden",
			Services: map[string]images.ServiceSpec{
				"app": {Image: "bitwarden/lite:2026.4.0"},
			},
		},
	}
	_, err := filterInputsByServices(scoped, []string{"backup"})
	if err == nil {
		t.Fatal("expected zero-match error, got nil")
	}
	if !strings.Contains(err.Error(), "hetzner-two/bitwarden") {
		t.Errorf("expected only in-scope stack listed, got: %s", err.Error())
	}
	if strings.Contains(err.Error(), "linkwarden") {
		t.Errorf("out-of-scope stack should not appear, got: %s", err.Error())
	}
}

func TestFilterInputsByServices_PartialMatchKeepsMatched(t *testing.T) {
	// "app" matches both stacks; "unknownsvc" matches neither. Should succeed
	// (not zero-match) and return only the matched services.
	out, err := filterInputsByServices(sampleInputs(), []string{"app", "unknownsvc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 stacks, got %d", len(out))
	}
}
