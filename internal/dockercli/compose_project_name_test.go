package dockercli

import (
	"context"
	"testing"
)

func TestComposeConfigFull_ParsesProjectName(t *testing.T) {
	cases := map[string]*fakeExec{
		"json": {outConfigJSON: `{"name":"custom-project","services":{"web":{"image":"nginx"}}}`},
		"yaml": {outConfigJSON: "notjson", outConfigYAML: "name: custom-project\nservices:\n  web:\n    image: nginx\n"},
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			c := &Client{exec: f}
			doc, err := c.ComposeConfigFull(context.Background(), ".", nil, nil, nil, nil)
			if err != nil {
				t.Fatalf("config full: %v", err)
			}
			if doc.Name != "custom-project" {
				t.Fatalf("Name = %q, want custom-project", doc.Name)
			}
		})
	}
}
