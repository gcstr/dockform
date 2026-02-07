package planner

// CleanupOptions controls behavior for cleanup operations (prune/destroy).
type CleanupOptions struct {
	// Strict makes cleanup errors fail the command.
	Strict bool
	// VerboseErrors logs full aggregated cleanup errors when Strict is false.
	VerboseErrors bool
}
