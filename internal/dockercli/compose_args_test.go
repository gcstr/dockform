package dockercli

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestComposeBaseArgs_OrderAndCleaning(t *testing.T) {
	c := &Client{}
	files := []string{"./a.yml", "../b.yml"}
	profiles := []string{"dev", "test"}
	envs := []string{"./.env", "../app.env"}
	got := c.composeBaseArgs(files, profiles, envs, "proj")
	// Expect: compose -f <clean(a)> -f <clean(b)> -p proj --env-file <clean(.env)> --env-file <clean(app.env)> --profile dev --profile test
	want := []string{"compose"}
	for _, f := range files {
		want = append(want, "-f", filepath.Clean(f))
	}
	want = append(want, "-p", "proj")
	for _, e := range envs {
		want = append(want, "--env-file", filepath.Clean(e))
	}
	for _, p := range profiles {
		want = append(want, "--profile", p)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("composeBaseArgs mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestComposeBaseArgs_NoProject_OmitsFlag(t *testing.T) {
	c := &Client{}
	got := c.composeBaseArgs(nil, nil, nil, "")
	want := []string{"compose"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected only base 'compose' when no args; got %#v", got)
	}
}
