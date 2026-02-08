package apperr

import (
	"errors"
	"fmt"
	"strings"
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

// MultiError groups multiple errors and supports errors.Is/errors.As traversal.
type MultiError struct {
	Errors []error
}

func (m *MultiError) Error() string {
	if m == nil || len(m.Errors) == 0 {
		return ""
	}
	if len(m.Errors) == 1 {
		return m.Errors[0].Error()
	}
	lines := make([]string, 0, len(m.Errors))
	for _, err := range m.Errors {
		if err == nil {
			continue
		}
		lines = append(lines, err.Error())
	}
	return strings.Join(lines, "; ")
}

// Unwrap exposes all inner errors for errors.Is/errors.As in Go 1.20+.
func (m *MultiError) Unwrap() []error {
	if m == nil {
		return nil
	}
	return m.Errors
}

// Aggregate wraps one or more errors with a stable kind and operation.
// nil errors are ignored. Returns nil when errs has no non-nil entries.
func Aggregate(op string, kind Kind, msg string, errs ...error) error {
	nonNil := make([]error, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			nonNil = append(nonNil, err)
		}
	}
	switch len(nonNil) {
	case 0:
		return nil
	case 1:
		return &E{Op: op, Kind: kind, Err: nonNil[0], Msg: msg}
	default:
		return &E{
			Op:   op,
			Kind: kind,
			Err:  &MultiError{Errors: nonNil},
			Msg:  msg,
		}
	}
}
