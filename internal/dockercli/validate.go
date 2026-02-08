package dockercli

import (
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
)

// requireNonEmpty returns an InvalidInput error when s is blank (empty or whitespace-only).
func requireNonEmpty(s, op, msg string) error {
	if strings.TrimSpace(s) == "" {
		return apperr.New(op, apperr.InvalidInput, "%s", msg)
	}
	return nil
}
