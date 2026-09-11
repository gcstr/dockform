package common

import (
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/spf13/cobra"
)

// newParallelCmd registers the flags the way plan and apply do.
func newParallelCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().Int("parallel", DefaultParallel, "")
	cmd.Flags().Bool("sequential", false, "")
	return cmd
}

func TestResolveParallel(t *testing.T) {
	cases := []struct {
		name    string
		flags   map[string]string
		want    int
		wantErr bool
	}{
		{name: "default keeps today's per-host cap", want: DefaultParallel},
		{name: "explicit value", flags: map[string]string{"parallel": "4"}, want: 4},
		{name: "one means fully sequential", flags: map[string]string{"parallel": "1"}, want: 1},
		{name: "deprecated --sequential means 1", flags: map[string]string{"sequential": "true"}, want: 1},
		{name: "--sequential=false changes nothing", flags: map[string]string{"sequential": "false"}, want: DefaultParallel},
		{name: "zero is rejected", flags: map[string]string{"parallel": "0"}, wantErr: true},
		{name: "negative is rejected", flags: map[string]string{"parallel": "-3"}, wantErr: true},
		{name: "--sequential with --parallel 3 conflicts", flags: map[string]string{"sequential": "true", "parallel": "3"}, wantErr: true},
		{name: "--sequential with --parallel 1 agrees", flags: map[string]string{"sequential": "true", "parallel": "1"}, want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newParallelCmd()
			for k, v := range tc.flags {
				if err := cmd.Flags().Set(k, v); err != nil {
					t.Fatalf("set --%s=%s: %v", k, v, err)
				}
			}
			got, err := ResolveParallel(cmd)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %d", got)
				}
				if !apperr.IsKind(err, apperr.InvalidInput) {
					t.Errorf("error should be InvalidInput (exit code 2), got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("parallel = %d, want %d", got, tc.want)
			}
		})
	}
}

// Commands that do not register the flags (destroy, volume, validate, dashboard)
// keep today's behaviour.
func TestResolveParallel_FlagsNotRegistered(t *testing.T) {
	got, err := ResolveParallel(&cobra.Command{})
	if err != nil || got != DefaultParallel {
		t.Fatalf("got %d, %v; want %d, nil", got, err, DefaultParallel)
	}
}
