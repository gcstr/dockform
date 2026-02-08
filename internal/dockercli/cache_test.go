package dockercli

import "testing"

func TestComposeCache_LoadReturnsMissWhenNil(t *testing.T) {
	c := &Client{}
	if _, ok := c.loadComposeCache("missing"); ok {
		t.Fatalf("expected cache miss when compose cache is nil")
	}
}

func TestComposeCache_StoreInitializesCacheAndLoadsValue(t *testing.T) {
	c := &Client{}
	doc := ComposeConfigDoc{
		Services: map[string]ComposeService{
			"web": {Image: "nginx:latest"},
		},
	}
	c.storeComposeCache("k", doc)
	if c.composeCache == nil {
		t.Fatalf("expected compose cache initialization")
	}
	got, ok := c.loadComposeCache("k")
	if !ok {
		t.Fatalf("expected cache hit after storing value")
	}
	if _, exists := got.Services["web"]; !exists {
		t.Fatalf("unexpected cached doc: %#v", got)
	}
}
