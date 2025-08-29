package secrets

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	age "filippo.io/age"
	"github.com/gcstr/dockform/internal/apperr"
)

// AgeRecipientsFromKeyFile reads an age identity file and returns the corresponding recipient(s).
// It supports resolving ~/ in the path similarly to DecryptAndParse.
func AgeRecipientsFromKeyFile(ageKeyFile string) ([]string, error) {
	if ageKeyFile == "" {
		return nil, apperr.New("secrets.AgeRecipientsFromKeyFile", apperr.InvalidInput, "age key file path is empty")
	}
	key := ageKeyFile
	if strings.HasPrefix(key, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			key = filepath.Join(home, key[2:])
		}
	}
	f, err := os.Open(key)
	if err != nil {
		return nil, apperr.Wrap("secrets.AgeRecipientsFromKeyFile", apperr.NotFound, err, "open age key file")
	}
	defer func() { _ = f.Close() }()
	identities, err := age.ParseIdentities(f)
	if err != nil {
		return nil, apperr.Wrap("secrets.AgeRecipientsFromKeyFile", apperr.InvalidInput, err, "parse age identities")
	}
	recips := make([]string, 0, len(identities))
	for _, id := range identities {
		if r, ok := id.(interface{ Recipient() (age.Recipient, error) }); ok {
			rr, err := r.Recipient()
			if err != nil {
				return nil, apperr.Wrap("secrets.AgeRecipientsFromKeyFile", apperr.InvalidInput, err, "derive recipient")
			}
			recips = append(recips, fmt.Sprint(rr))
		}
	}
	if len(recips) == 0 {
		if _, err := f.Seek(0, io.SeekStart); err == nil {
			b, _ := io.ReadAll(f)
			for _, ln := range strings.Split(string(b), "\n") {
				ln = strings.TrimSpace(ln)
				if strings.HasPrefix(ln, "# public key:") {
					pk := strings.TrimSpace(strings.TrimPrefix(ln, "# public key:"))
					if pk != "" {
						recips = append(recips, pk)
					}
				}
			}
		}
	}
	return recips, nil
}

// EncryptDotenvFileWithSops encrypts a plaintext dotenv file in-place using the system SOPS binary with provided age recipients.
func EncryptDotenvFileWithSops(ctx context.Context, path string, recipients []string, ageKeyFile string) error {
	if len(recipients) == 0 {
		return apperr.New("secrets.EncryptDotenvFileWithSops", apperr.InvalidInput, "no recipients provided")
	}
	// Ensure file exists
	if _, err := os.Stat(path); err != nil {
		return apperr.Wrap("secrets.EncryptDotenvFileWithSops", apperr.NotFound, err, "read plaintext")
	}
	// Ensure sops binary exists
	if _, err := exec.LookPath("sops"); err != nil {
		return apperr.New("secrets.EncryptDotenvFileWithSops", apperr.NotFound, "sops binary not found on PATH; please install sops")
	}
	// Ensure SOPS_AGE_KEY_FILE set for decrypt compatibility and environments that need it
	if ageKeyFile != "" {
		key := ageKeyFile
		if strings.HasPrefix(key, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				key = filepath.Join(home, key[2:])
			}
		}
		_ = os.Setenv("SOPS_AGE_KEY_FILE", key)
	}

	args := []string{"--encrypt", "--input-type", "dotenv", "--output-type", "dotenv", "--in-place"}
	for _, r := range recipients {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		// Basic recipient format validation to fail fast with a clear message for tests
		if !strings.HasPrefix(r, "age1") {
			return apperr.New("secrets.EncryptDotenvFileWithSops", apperr.InvalidInput, "age recipient: invalid format")
		}
		args = append(args, "--age", r)
	}
	args = append(args, path)
	cmd := exec.CommandContext(ctx, "sops", args...)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		return apperr.Wrap("secrets.EncryptDotenvFileWithSops", apperr.External, fmt.Errorf("%s", string(out)), "sops encrypt %s", path)
	}
	return nil
}
