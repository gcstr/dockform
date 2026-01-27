package volumecmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/cli/buildinfo"
	"github.com/gcstr/dockform/internal/cli/common"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/gcstr/dockform/internal/util"
	"github.com/spf13/cobra"
)

// New creates the `volume` command.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "volume",
		Short: "Manage Docker volumes (snapshots, restore)",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newSnapshotCmd())
	cmd.AddCommand(newRestoreCmd())
	return cmd
}

type snapshotMeta struct {
	DockformVersion   string            `json:"dockform_version"`
	CreatedAt         string            `json:"created_at"`
	VolumeName        string            `json:"volume_name"`
	SpecHash          string            `json:"spec_hash"`
	Driver            string            `json:"driver"`
	DriverOpts        map[string]string `json:"driver_opts"`
	Labels            map[string]string `json:"labels"`
	UncompressedBytes int64             `json:"uncompressed_bytes"`
	FileCount         int64             `json:"file_count"`
	Checksum          struct {
		Algo   string `json:"algo"`
		TarZst string `json:"tar_zst"`
	} `json:"checksum"`
	Notes string `json:"notes,omitempty"`
}

func computeSpecHash(d dockercli.VolumeDetails) string {
	// Build deterministic string: driver|opts(k=v;..)|labels(k=v;..)
	optsKeys := make([]string, 0, len(d.Options))
	for k := range d.Options {
		optsKeys = append(optsKeys, k)
	}
	sort.Strings(optsKeys)
	labelsKeys := make([]string, 0, len(d.Labels))
	for k := range d.Labels {
		labelsKeys = append(labelsKeys, k)
	}
	sort.Strings(labelsKeys)
	var b strings.Builder
	b.WriteString("driver=")
	b.WriteString(d.Driver)
	b.WriteString("|opts=")
	first := true
	for _, k := range optsKeys {
		if !first {
			b.WriteByte(';')
		} else {
			first = false
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(d.Options[k])
	}
	b.WriteString("|labels=")
	first = true
	for _, k := range labelsKeys {
		if !first {
			b.WriteByte(';')
		} else {
			first = false
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(d.Labels[k])
	}
	full := util.Sha256StringHex(b.String())
	if len(full) >= 8 {
		return full[:8]
	}
	return full
}

func manifestHasVolume(cfg *manifest.Config, name string) bool {
	if cfg == nil {
		return false
	}
	// In the new multi-daemon schema, volumes come from filesets only
	for _, fs := range cfg.GetAllFilesets() {
		if fs.TargetVolume == name {
			return true
		}
	}
	return false
}

func newSnapshotCmd() *cobra.Command {
	var outDirFlag string
	var note string
	cmd := &cobra.Command{
		Use:   "snapshot <volume>",
		Short: "Create a snapshot of a Docker volume to local storage",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			clictx, err := common.SetupCLIContext(cmd)
			if err != nil {
				return err
			}
			pr := clictx.Printer
			docker := clictx.GetDefaultClient()
			volName := args[0]
			// Default output next to manifest
			outDir := outDirFlag
			if strings.TrimSpace(outDir) == "" {
				outDir = filepath.Join(clictx.Config.BaseDir, ".dockform", "snapshots")
			}
			// Inspect volume to get spec
			details, err := docker.InspectVolume(ctx, volName)
			if err != nil {
				return err
			}
			short := computeSpecHash(details)
			ts := time.Now().UTC().Format("2006-01-02T15-04-05Z")
			volDir := filepath.Join(outDir, volName)
			if err := os.MkdirAll(volDir, 0o755); err != nil {
				return apperr.Wrap("cli.volume.snapshot", apperr.Internal, err, "mkdir %s", volDir)
			}
			base := fmt.Sprintf("%s__spec-%s", ts, short)
			tarPath := filepath.Join(volDir, base+".tar.zst")
			jsonPath := filepath.Join(volDir, base+".json")

			// Stream tar.zst to file
			f, err := os.Create(tarPath)
			if err != nil {
				return apperr.Wrap("cli.volume.snapshot", apperr.Internal, err, "create tar.zst")
			}
			defer func() { _ = f.Close() }()

			stdPr := pr.(ui.StdPrinter)
			if err := common.SpinnerOperation(stdPr, "Creating snapshot...", func() error {
				return docker.StreamTarZstdFromVolume(ctx, volName, f)
			}); err != nil {
				return err
			}

			// Compute stats and checksum
			uncompressed, fileCount, err := docker.TarStatsFromVolume(ctx, volName)
			if err != nil {
				// Non-fatal, but helpful; continue without stats
				uncompressed, fileCount = 0, 0
			}
			sum, err := util.Sha256FileHex(tarPath)
			if err != nil {
				return apperr.Wrap("cli.volume.snapshot", apperr.Internal, err, "checksum tar.zst")
			}

			meta := snapshotMeta{
				DockformVersion:   buildinfo.Version(),
				CreatedAt:         time.Now().UTC().Format(time.RFC3339),
				VolumeName:        volName,
				SpecHash:          short,
				Driver:            details.Driver,
				DriverOpts:        details.Options,
				Labels:            details.Labels,
				UncompressedBytes: uncompressed,
				FileCount:         fileCount,
				Notes:             note,
			}
			meta.Checksum.Algo = "sha256"
			meta.Checksum.TarZst = sum

			// Write JSON sidecar
			jb, err := json.MarshalIndent(meta, "", "  ")
			if err != nil {
				return apperr.Wrap("cli.volume.snapshot", apperr.Internal, err, "encode json")
			}
			if err := os.WriteFile(jsonPath, jb, 0o644); err != nil {
				return apperr.Wrap("cli.volume.snapshot", apperr.Internal, err, "write json")
			}
			pr.Info("Snapshot written: %s", tarPath)
			pr.Plain("Metadata: %s", jsonPath)
			return nil
		},
	}
	cmd.Flags().StringVarP(&outDirFlag, "output", "o", "", "Output directory for snapshots (defaults to ./.dockform/snapshots next to manifest)")
	cmd.Flags().StringVar(&note, "note", "", "Optional note to include in metadata")
	return cmd
}

func newRestoreCmd() *cobra.Command {
	var force bool
	var stopContainers bool
	cmd := &cobra.Command{
		Use:   "restore <volume> <snapshot-path>",
		Short: "Restore a snapshot into a Docker volume",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			clictx, err := common.SetupCLIContext(cmd)
			if err != nil {
				return err
			}
			pr := clictx.Printer
			docker := clictx.GetDefaultClient()
			volName := args[0]
			snapPath := args[1]

			// Ensure volume is in manifest
			if !manifestHasVolume(clictx.Config, volName) {
				return apperr.New("cli.volume.restore", apperr.InvalidInput, "volume %q is not defined in manifest", volName)
			}
			// Ensure volume exists in context
			exists, err := docker.VolumeExists(ctx, volName)
			if err != nil {
				return err
			}
			if !exists {
				return apperr.New("cli.volume.restore", apperr.NotFound, "volume %q not found in Docker context", volName)
			}

			// Validate snapshot file extension early
			if !strings.HasSuffix(snapPath, ".tar.zst") && !strings.HasSuffix(snapPath, ".tar") {
				return apperr.New("cli.volume.restore", apperr.InvalidInput, "unsupported snapshot extension (expected .tar.zst or .tar)")
			}

			// Validate snapshot file exists and is readable
			if _, err := os.Stat(snapPath); err != nil {
				if os.IsNotExist(err) {
					return apperr.New("cli.volume.restore", apperr.NotFound, "snapshot file not found: %s", snapPath)
				}
				return apperr.Wrap("cli.volume.restore", apperr.Internal, err, "stat snapshot file")
			}

			// Validate checksum and spec hash if sidecar exists (before stopping containers)
			sidecar := strings.TrimSuffix(snapPath, filepath.Ext(snapPath)) + ".json"
			if b, err := os.ReadFile(sidecar); err == nil {
				var meta snapshotMeta
				if jerr := json.Unmarshal(b, &meta); jerr == nil {
					// Verify checksum
					if strings.HasSuffix(snapPath, ".tar.zst") && meta.Checksum.TarZst != "" {
						if sum, _ := util.Sha256FileHex(snapPath); sum != meta.Checksum.TarZst {
							return apperr.New("cli.volume.restore", apperr.InvalidInput, "checksum mismatch for %s", snapPath)
						}
					}
					// Verify spec hash (warn if mismatch)
					if details, derr := docker.InspectVolume(ctx, volName); derr == nil {
						if h := computeSpecHash(details); h != meta.SpecHash {
							pr.Warn("snapshot spec hash (%s) differs from current volume (%s)", meta.SpecHash, h)
						}
					}
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return apperr.Wrap("cli.volume.restore", apperr.Internal, err, "read sidecar")
			}

			// Check if volume is empty (before stopping containers, unless --force)
			empty, err := docker.IsVolumeEmpty(ctx, volName)
			if err != nil {
				return err
			}
			if !empty && !force {
				return apperr.New("cli.volume.restore", apperr.Conflict, "destination volume is not empty; use --force to overwrite")
			}

			// Check containers using volume and track which were running
			allUsers, err := docker.ListContainersUsingVolume(ctx, volName)
			if err != nil {
				return err
			}
			runningUsers, err := docker.ListRunningContainersUsingVolume(ctx, volName)
			if err != nil {
				return err
			}
			runningSet := map[string]struct{}{}
			for _, n := range runningUsers {
				runningSet[n] = struct{}{}
			}
			if len(allUsers) > 0 && !stopContainers {
				return apperr.New("cli.volume.restore", apperr.Conflict, "containers are using volume %q: %s (use --stop-containers)", volName, strings.Join(allUsers, ", "))
			}

			// Stop containers now that all validations passed
			if len(allUsers) > 0 {
				if err := docker.StopContainers(ctx, allUsers); err != nil {
					return err
				}
				// Set up deferred restart of running containers to handle restore failures
				defer func() {
					if len(runningSet) > 0 {
						var toStart []string
						for name := range runningSet {
							toStart = append(toStart, name)
						}
						sort.Strings(toStart)
						_ = docker.StartContainers(context.Background(), toStart)
					}
				}()
			}

			// Clear volume if needed (requires stopped containers)
			if !empty && force {
				if err := docker.ClearVolume(ctx, volName); err != nil {
					return err
				}
			}

			// Perform the actual restore operation
			stdPr := pr.(ui.StdPrinter)
			if strings.HasSuffix(snapPath, ".tar.zst") {
				in, err := os.Open(snapPath)
				if err != nil {
					return apperr.Wrap("cli.volume.restore", apperr.Internal, err, "open snapshot")
				}
				defer func() { _ = in.Close() }()
				if err := common.SpinnerOperation(stdPr, "Restoring snapshot...", func() error {
					return docker.ExtractZstdTarToVolume(ctx, volName, in)
				}); err != nil {
					return err
				}
			} else if strings.HasSuffix(snapPath, ".tar") {
				in, err := os.Open(snapPath)
				if err != nil {
					return apperr.Wrap("cli.volume.restore", apperr.Internal, err, "open snapshot")
				}
				defer func() { _ = in.Close() }()
				if err := common.SpinnerOperation(stdPr, "Restoring snapshot...", func() error {
					return docker.ExtractTarToVolume(ctx, volName, "/dst", io.Reader(in))
				}); err != nil {
					return err
				}
			}

			pr.Info("Restored snapshot into volume %s", volName)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite non-empty destination volume")
	cmd.Flags().BoolVar(&stopContainers, "stop-containers", false, "Stop containers using the target volume before restore")
	return cmd
}
