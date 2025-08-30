package manifest

import "errors"

var (
	// ErrMissingRequired indicates a required field or value is missing.
	ErrMissingRequired = errors.New("missing required field")
	// ErrInvalidValue indicates a field has an invalid value.
	ErrInvalidValue = errors.New("invalid value")
)
