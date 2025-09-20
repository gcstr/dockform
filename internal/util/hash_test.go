package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSha256Hex_And_String(t *testing.T) {
	// Known SHA-256 of "abc"
	const input = "abc"
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := Sha256Hex([]byte(input)); got != want {
		t.Fatalf("Sha256Hex mismatch: got %s want %s", got, want)
	}
	if got := Sha256StringHex(input); got != want {
		t.Fatalf("Sha256StringHex mismatch: got %s want %s", got, want)
	}
}

func TestSha256FileHex_SuccessAndError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Compute expected from string helper
	want := Sha256StringHex("hello")
	got, err := Sha256FileHex(p)
	if err != nil {
		t.Fatalf("Sha256FileHex error: %v", err)
	}
	if got != want {
		t.Fatalf("Sha256FileHex mismatch: got %s want %s", got, want)
	}

	// Non-existent file should error
	if _, err := Sha256FileHex(filepath.Join(dir, "nope")); err == nil {
		t.Fatalf("expected error for missing file")
	}
}
