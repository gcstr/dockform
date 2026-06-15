package imagescmd

import (
	"testing"

	"github.com/gcstr/dockform/internal/images"
)

func TestUnanchoredTagPatterns(t *testing.T) {
	inputs := []images.CheckInput{
		{
			StackKey: "ctx/backup",
			Services: map[string]images.ServiceSpec{
				// Unanchored: lone '$' was eaten by Compose -> flagged.
				"db": {Image: "postgres:16", TagPattern: `^v\d+\.\d+\.\d+`},
				// Properly anchored ($$ -> $) -> not flagged.
				"app": {Image: "app:1", TagPattern: `^v\d+\.\d+\.\d+$`},
			},
		},
		{
			StackKey: "ctx/web",
			Services: map[string]images.ServiceSpec{
				// Digest-only (no pattern) -> not flagged.
				"nginx": {Image: "nginx:1"},
				// Unanchored -> flagged.
				"api": {Image: "api:2", TagPattern: `^\d+`},
			},
		},
	}

	got := unanchoredTagPatterns(inputs)

	want := []tagPatternIssue{
		{stack: "ctx/backup", service: "db", pattern: `^v\d+\.\d+\.\d+`},
		{stack: "ctx/web", service: "api", pattern: `^\d+`},
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d issues, got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("issue %d = %+v, want %+v (full: %+v)", i, got[i], want[i], got)
		}
	}
}

func TestUnanchoredTagPatterns_NoneWhenAllAnchoredOrEmpty(t *testing.T) {
	inputs := []images.CheckInput{
		{
			StackKey: "ctx/a",
			Services: map[string]images.ServiceSpec{
				"anchored": {Image: "x:1", TagPattern: `^v\d+$`},
				"none":     {Image: "y:1", TagPattern: ""},
			},
		},
	}
	if got := unanchoredTagPatterns(inputs); len(got) != 0 {
		t.Fatalf("expected no issues, got %+v", got)
	}
}
