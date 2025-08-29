package util

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// Sha256Hex returns the lowercase hexadecimal SHA-256 of the provided bytes.
func Sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Sha256StringHex returns the lowercase hexadecimal SHA-256 of the provided string.
func Sha256StringHex(s string) string { return Sha256Hex([]byte(s)) }

// Sha256FileHex streams the file at path and returns the lowercase hexadecimal SHA-256.
func Sha256FileHex(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
