package common

import (
	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/spf13/cobra"
)

// DefaultParallel is how many docker operations dockform runs at once against
// each remote host when --parallel is not given. It is the long-standing
// per-host cap, so the default behaviour does not change.
const DefaultParallel = dockercli.MaxConcurrentSSH

// ResolveParallel reads --parallel and the deprecated --sequential (which means
// --parallel 1). Setting both to different values is an error. Commands that
// register neither get DefaultParallel.
func ResolveParallel(cmd *cobra.Command) (int, error) {
	n := DefaultParallel
	parallelSet := false
	if f := cmd.Flags().Lookup("parallel"); f != nil && f.Changed {
		v, err := cmd.Flags().GetInt("parallel")
		if err != nil {
			return 0, apperr.Wrap("common.ResolveParallel", apperr.InvalidInput, err, "invalid --parallel")
		}
		if v < 1 {
			return 0, apperr.New("common.ResolveParallel", apperr.InvalidInput, "invalid --parallel %d: must be 1 or more", v)
		}
		n, parallelSet = v, true
	}
	if f := cmd.Flags().Lookup("sequential"); f != nil && f.Changed {
		if seq, _ := cmd.Flags().GetBool("sequential"); seq {
			if parallelSet && n != 1 {
				return 0, apperr.New("common.ResolveParallel", apperr.InvalidInput,
					"--sequential conflicts with --parallel %d; --sequential means --parallel 1, so set only --parallel", n)
			}
			n = 1
		}
	}
	return n, nil
}
