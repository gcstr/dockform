package secrets

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	age "filippo.io/age"
	"github.com/gcstr/dockform/internal/apperr"
	sopsv3 "github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	sopsv3age "github.com/getsops/sops/v3/age"
	sopsconfig "github.com/getsops/sops/v3/config"
	"github.com/getsops/sops/v3/keys"
	"github.com/getsops/sops/v3/stores/dotenv"
	"github.com/getsops/sops/v3/version"
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
	defer f.Close()
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

// EncryptDotenvFileWithSops encrypts a plaintext dotenv file in-place using SOPS with provided age recipients.
func EncryptDotenvFileWithSops(ctx context.Context, path string, recipients []string, ageKeyFile string) error {
	if len(recipients) == 0 {
		return apperr.New("secrets.EncryptDotenvFileWithSops", apperr.InvalidInput, "no recipients provided")
	}
	// Ensure SOPS_AGE_KEY_FILE set for decrypt compatibility
	if ageKeyFile != "" {
		key := ageKeyFile
		if strings.HasPrefix(key, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				key = filepath.Join(home, key[2:])
			}
		}
		_ = os.Setenv("SOPS_AGE_KEY_FILE", key)
	}

	// Load plaintext dotenv tree
	store := dotenv.NewStore(&sopsconfig.DotenvStoreConfig{})
	b, rerr := os.ReadFile(path)
	if rerr != nil {
		return apperr.Wrap("secrets.EncryptDotenvFileWithSops", apperr.NotFound, rerr, "read plaintext")
	}
	branches, err := store.LoadPlainFile(b)
	if err != nil {
		return apperr.Wrap("secrets.EncryptDotenvFileWithSops", apperr.InvalidInput, err, "load dotenv")
	}
	inputTree := sopsv3.Tree{Branches: branches}

	// Build metadata with age recipients as a single keygroup
	var ageKeys []keys.MasterKey
	for _, r := range recipients {
		k, err := sopsv3age.MasterKeyFromRecipient(r)
		if err != nil {
			return apperr.Wrap("secrets.EncryptDotenvFileWithSops", apperr.InvalidInput, err, "age recipient")
		}
		ageKeys = append(ageKeys, k)
	}
	metadata := sopsv3.Metadata{KeyGroups: []sopsv3.KeyGroup{ageKeys}}
	metadata.Version = version.Version
	metadata.LastModified = time.Now()

	inputTree.Metadata = metadata
	// Generate data key and encrypt with master keys per sops common flow
	dataKey := make([]byte, 32)
	if _, err := rand.Read(dataKey); err != nil {
		return apperr.Wrap("secrets.EncryptDotenvFileWithSops", apperr.Internal, err, "generate data key")
	}
	if errs := inputTree.Metadata.UpdateMasterKeys(dataKey); len(errs) > 0 {
		return apperr.New("secrets.EncryptDotenvFileWithSops", apperr.External, "update master keys: %v", errs)
	}
	mac, err := inputTree.Encrypt(dataKey, aes.NewCipher())
	if err != nil {
		return apperr.Wrap("secrets.EncryptDotenvFileWithSops", apperr.External, err, "encrypt tree")
	}
	// Populate metadata for sops CLI compatibility
	inputTree.Metadata.LastModified = time.Now().UTC()
	inputTree.Metadata.Version = version.Version
	encMac, err := aes.NewCipher().Encrypt(mac, dataKey, inputTree.Metadata.LastModified.Format(time.RFC3339))
	if err != nil {
		return apperr.Wrap("secrets.EncryptDotenvFileWithSops", apperr.External, err, "encrypt mac")
	}
	inputTree.Metadata.MessageAuthenticationCode = encMac
	out, err := store.EmitEncryptedFile(inputTree)
	if err != nil {
		return apperr.Wrap("secrets.EncryptDotenvFileWithSops", apperr.External, err, "emit encrypted dotenv")
	}
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		return apperr.Wrap("secrets.EncryptDotenvFileWithSops", apperr.Internal, err, "write encrypted")
	}
	return nil
}
