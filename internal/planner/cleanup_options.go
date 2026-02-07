package planner

import (
	"context"

	"github.com/gcstr/dockform/internal/logger"
)

// CleanupOptions controls behavior for cleanup operations (prune/destroy).
type CleanupOptions struct {
	// Strict makes cleanup errors fail the command.
	Strict bool
	// VerboseErrors logs full aggregated cleanup errors when Strict is false.
	VerboseErrors bool
}

// handleCleanupError returns err when opts.Strict is true, otherwise logs and
// swallows the error so the command exits 0.
func handleCleanupError(ctx context.Context, err error, opts CleanupOptions, action string) error {
	if err == nil {
		return nil
	}
	if opts.Strict {
		return err
	}
	log := logger.FromContext(ctx).With("component", "planner", "action", action)
	if opts.VerboseErrors {
		log.Warn(action+"_non_strict_errors", "error", err.Error())
	} else {
		log.Warn(action + "_non_strict_errors")
	}
	return nil
}
