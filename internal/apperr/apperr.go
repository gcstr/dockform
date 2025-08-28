package apperr

import (
	"errors"
	"fmt"
)

// Kind is a stable category for application errors.
type Kind string

const (
	// Stable kinds you can switch/branch on across packages.
	InvalidInput Kind = "invalid_input"
	NotFound     Kind = "not_found"
	Conflict     Kind = "conflict"
	Unauthorized Kind = "unauthorized"
	Forbidden    Kind = "forbidden"
	Precondition Kind = "precondition_failed"
	Timeout      Kind = "timeout"
	Unavailable  Kind = "unavailable" // network/docker daemon down
	External     Kind = "external"    // external tool failed (docker/sops)
	Internal     Kind = "internal"    // programmer bug, invariant broken
)

// E is a rich, chainable error.
type E struct {
	Op   string // where it happened, e.g. "dockercli.SyncVolume"
	Kind Kind   // category
	Err  error  // wrapped cause
	Msg  string // optional, short context message
}

func (e *E) Error() string {
	base := e.Msg
	if base == "" && e.Err != nil {
		base = e.Err.Error()
	}
	if e.Op != "" && base != "" {
		return fmt.Sprintf("%s: %s", e.Op, base)
	}
	if e.Op != "" {
		return e.Op
	}
	return base
}

func (e *E) Unwrap() error { return e.Err }

// Wrap creates an E that wraps the provided error with operation, kind, and message.
func Wrap(op string, kind Kind, err error, msg string, args ...any) error {
	if err == nil {
		return nil
	}
	return &E{Op: op, Kind: kind, Err: err, Msg: fmt.Sprintf(msg, args...)}
}

// New creates a new E with no wrapped cause.
func New(op string, kind Kind, msg string, args ...any) error {
	return &E{Op: op, Kind: kind, Msg: fmt.Sprintf(msg, args...)}
}

// IsKind reports whether any error in the chain is an *E of the provided Kind.
func IsKind(err error, k Kind) bool {
	var e *E
	if errors.As(err, &e) {
		return e.Kind == k
	}
	return false
}
