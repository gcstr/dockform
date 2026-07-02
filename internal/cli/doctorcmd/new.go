package doctorcmd

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/gcstr/dockform/internal/cli/buildinfo"
	"github.com/gcstr/dockform/internal/cli/common"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/spf13/cobra"
)

type CheckStatus int

const (
	StatusPass CheckStatus = iota
	StatusWarn
	StatusFail
)

type checkResult struct {
	id      string
	title   string
	status  CheckStatus
	summary string
	note    string   // Remedy/Tip/Note line (single line)
	errMsg  string   // Optional error line for FAILs
	sub     []string // Additional informational lines to render under the item
}

// New creates the `doctor` command.
func New() *cobra.Command {
	var contextName string

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run a quick health scan and report system readiness for Dockform",
		RunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()

			// Resolve context override
			ctxOverride := strings.TrimSpace(contextName)
			ctxName := ctxOverride
			if ctxName == "" {
				ctxName = "default"
			}
			docker := dockercli.New(ctxName)

			// Header lines
			ctx := cmd.Context()
			host := ""
			if h, err := docker.ContextHost(ctx); err == nil {
				host = h
			}

			// Run checks in sequence; keep simple and deterministic output ordering
			var results []checkResult

			// [engine] — bounded so an unreachable daemon (e.g. a dead SSH context)
			// cannot hang the command.
			engRes := checkEngine(ctx, docker)
			results = append(results, engRes)

			// [context] — probe every context configured in the manifest (or just
			// the --context override, if given), each bounded by
			// common.ReachabilityProbeTimeout so a down host reports as
			// unreachable instead of hanging.
			results = append(results, checkContextsReachable(ctx, cmd, ctxOverride, ctxName, docker)...)

			// [compose]
			results = append(results, checkCompose(ctx, docker))

			// [sops]
			results = append(results, checkSops())
			// [gpg]
			results = append(results, checkGpg())

			// [helper]
			results = append(results, checkHelperImage(ctx, docker))

			// [net-perms]
			results = append(results, checkNetworkPerms(ctx, docker))

			// [vol-perms]
			results = append(results, checkVolumePerms(ctx, docker))

			// Render
			// Top header
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Dockform (v%s) Doctor — health scan\n", buildinfo.Version())
			if host != "" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Context: %s  •  Host: %s\n\n", ctxName, host)
			} else {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Context: %s\n\n", ctxName)
			}

			var pass, warn, fail int
			for _, r := range results {
				// Color/icon per status
				var icon, bracketedID, line string
				idStyled := ui.BlueText("[" + r.id + "]")
				bracketedID = idStyled
				switch r.status {
				case StatusPass:
					icon = ui.GreenText("✓")
				case StatusWarn:
					icon = ui.YellowText("!")
				case StatusFail:
					icon = ui.RedText("×")
				}
				line = fmt.Sprintf("│ %s %s %s — %s", icon, bracketedID, r.title, r.summary)
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), line)
				if r.status == StatusWarn && r.note != "" {
					PrintIndentedLines(cmd.OutOrStdout(), r.note)
				}
				if r.status == StatusFail {
					if r.errMsg != "" {
						PrintIndentedLines(cmd.OutOrStdout(), "Error: "+r.errMsg)
					}
					if r.note != "" {
						PrintIndentedLines(cmd.OutOrStdout(), r.note)
					}
				}
				// Render any additional sub-lines regardless of status
				for _, s := range r.sub {
					if strings.TrimSpace(s) == "" {
						continue
					}
					PrintIndentedLines(cmd.OutOrStdout(), s)
				}
				switch r.status {
				case StatusPass:
					pass++
				case StatusWarn:
					warn++
				case StatusFail:
					fail++
				}
			}

			// Footer
			total := len(results)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nSummary: %d checks • %d PASS, %d WARN, %d FAIL\n", total, pass, warn, fail)
			exitCode := 0
			if fail > 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Action needed: fix the FAIL items above, then re-run: dockform doctor")
				exitCode = 1
			} else if warn > 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Completed with warnings. Some features may be degraded.")
				exitCode = 2
			} else {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "All good!")
				exitCode = 0
			}
			elapsed := time.Since(start).Seconds()
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Completed in %.1fs • exit code %d\n", elapsed, exitCode)

			if exitCode != 0 {
				// Use cobra error path to set process exit status via Execute
				return fmt.Errorf("doctor checks completed with status %d", exitCode)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&contextName, "context", "", "Docker context to use (overrides active context)")
	return cmd
}

func checkEngine(ctx context.Context, docker *dockercli.Client) checkResult {
	// Bounded: exec.CommandContext only kills the docker CLI once the deadline
	// fires, and the plain command context has none. Without this timeout, a
	// docker-over-SSH call to a dead host hangs the doctor command forever.
	probeCtx, cancel := context.WithTimeout(ctx, common.ReachabilityProbeTimeout)
	defer cancel()
	if err := docker.CheckDaemon(probeCtx); err != nil {
		summary := "daemon not reachable"
		if probeCtx.Err() != nil && ctx.Err() == nil {
			summary = fmt.Sprintf("timed out after %s", common.ReachabilityProbeTimeout)
		}
		return checkResult{
			id:      "engine",
			title:   "Docker Engine reachable",
			status:  StatusFail,
			summary: summary,
			errMsg:  err.Error(),
			note:    "Remedy: Ensure Docker is running and your user can access it.",
		}
	}
	ver, err := docker.ServerVersion(probeCtx)
	if err != nil || strings.TrimSpace(ver) == "" {
		return checkResult{id: "engine", title: "Docker Engine reachable", status: StatusPass, summary: "version unknown"}
	}
	return checkResult{id: "engine", title: "Docker Engine reachable", status: StatusPass, summary: "v" + ver}
}

// checkContextsReachable determines which context(s) doctor should probe for
// daemon reachability and returns one checkResult per context.
//
//   - When --context is given, only that context is probed (bounded), matching
//     the pre-existing single-context "Active context reachable" behavior.
//   - Otherwise, doctor best-effort loads the manifest and probes every
//     configured context in parallel via common.ProbeContextsReachability, the
//     same bounded-parallel mechanism the plan/apply reachability gate uses.
//   - If no manifest can be loaded (or it has no contexts), doctor degrades
//     gracefully to the single active/default context, noting that manifest
//     contexts were not checked.
func checkContextsReachable(ctx context.Context, cmd *cobra.Command, ctxOverride, ctxName string, docker *dockercli.Client) []checkResult {
	if ctxOverride != "" {
		return []checkResult{checkSingleContextReachable(ctx, docker, ctxName, "")}
	}

	cfg, err := loadManifestQuietly(cmd)
	if err != nil || cfg == nil || len(cfg.Contexts) == 0 {
		return []checkResult{checkSingleContextReachable(ctx, docker, ctxName,
			"Note: manifest contexts were not checked (no manifest loaded); only the active context was probed.")}
	}

	factory := common.CreateClientFactory()
	probeResults := common.ProbeContextsReachability(ctx, cfg, factory)
	results := make([]checkResult, 0, len(probeResults))
	for _, r := range probeResults {
		id := fmt.Sprintf("context:%s", r.Name)
		if r.Reachable() {
			results = append(results, checkResult{
				id:      id,
				title:   fmt.Sprintf("Context %q reachable", r.Name),
				status:  StatusPass,
				summary: "ok",
			})
			continue
		}
		results = append(results, checkResult{
			id:      id,
			title:   fmt.Sprintf("Context %q reachable", r.Name),
			status:  StatusFail,
			summary: "unreachable",
			errMsg:  r.Cause,
			note:    "Remedy: Verify the host is up and the Docker context is correct (docker context ls).",
		})
	}
	return results
}

// checkSingleContextReachable performs the bounded, single-context reachability
// check used when no manifest is available or when --context scopes doctor to
// one context explicitly. Daemon liveness itself is already covered by
// checkEngine (also bounded); this check resolves the named context to a host,
// which is local metadata lookup (docker context inspect) rather than a call to
// the remote daemon, but is still bounded defensively.
func checkSingleContextReachable(ctx context.Context, docker *dockercli.Client, ctxName, degradedNote string) checkResult {
	probeCtx, cancel := context.WithTimeout(ctx, common.ReachabilityProbeTimeout)
	defer cancel()

	var sub []string
	if degradedNote != "" {
		sub = append(sub, degradedNote)
	}

	// If engine is reachable, context is implicitly resolvable; still try to display
	if _, err := docker.ContextHost(probeCtx); err != nil {
		return checkResult{id: "context", title: "Active context reachable", status: StatusFail, summary: fmt.Sprintf("%q unreachable", ctxName), errMsg: err.Error(), note: "Remedy: Verify context exists: docker context ls", sub: sub}
	}
	return checkResult{id: "context", title: "Active context reachable", status: StatusPass, summary: fmt.Sprintf("%q", ctxName), sub: sub}
}

// loadManifestQuietly best-effort loads the manifest without emitting warnings
// or prompting interactively, so `dockform doctor` keeps working from any
// directory (matching its pre-existing no-manifest-required behavior). Any
// error (missing manifest, invalid YAML, ambiguous selection, etc.) is treated
// as "no manifest available" and the caller falls back to single-context mode.
func loadManifestQuietly(cmd *cobra.Command) (*manifest.Config, error) {
	return common.LoadConfigWithWarnings(cmd, ui.NoopPrinter{})
}

func checkCompose(ctx context.Context, docker *dockercli.Client) checkResult {
	ver, err := docker.ComposeVersion(ctx)
	if err != nil {
		return checkResult{id: "compose", title: "Docker Compose (v2+)", status: StatusFail, summary: "not found", note: "Remedy: Install docker compose plugin (v2+).", errMsg: err.Error()}
	}
	if !isComposeV2OrLater(ver) {
		summary := strings.TrimSpace(ver)
		if summary == "" {
			summary = "not found"
		}
		return checkResult{id: "compose", title: "Docker Compose (v2+)", status: StatusFail, summary: summary, note: "Remedy: Install docker compose plugin (v2+)."}
	}
	short := strings.TrimSpace(ver)
	return checkResult{id: "compose", title: "Docker Compose plugin", status: StatusPass, summary: short}
}

// isComposeV2OrLater parses a version string (e.g. "2.29.0", "v5.0.2") and returns true if major >= 2.
func isComposeV2OrLater(s string) bool {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if s == "" {
		return false
	}
	major, _, _ := strings.Cut(s, ".")
	n, err := strconv.Atoi(major)
	return err == nil && n >= 2
}

func checkSops() checkResult {
	if _, err := exec.LookPath("sops"); err != nil {
		// Warn only
		return checkResult{id: "sops", title: "SOPS", status: StatusWarn, summary: "not found", note: "Tip: Install SOPS to decrypt secrets: https://github.com/getsops/sops"}
	}
	// Try version
	out, err := exec.Command("sops", "--version").CombinedOutput()
	combined := string(out)
	lines := strings.Split(strings.ReplaceAll(combined, "\r\n", "\n"), "\n")
	ver := strings.TrimSpace(lines[0])
	var sub []string
	if len(lines) > 1 {
		for _, ln := range lines[1:] {
			ln = strings.TrimSpace(ln)
			if ln == "" {
				continue
			}
			// Normalize bracket prefix into plain text; we'll render with pipe
			if strings.HasPrefix(ln, "[info]") {
				ln = strings.TrimSpace(strings.TrimPrefix(ln, "[info]"))
			}
			if strings.HasPrefix(ln, "[warning]") {
				ln = strings.TrimSpace(strings.TrimPrefix(ln, "[warning]"))
			}
			sub = append(sub, ln)
		}
	}
	if err != nil || ver == "" {
		return checkResult{id: "sops", title: "SOPS present", status: StatusPass, summary: "installed", sub: sub}
	}
	return checkResult{id: "sops", title: "SOPS present", status: StatusPass, summary: ver, sub: sub}
}

func checkGpg() checkResult {
	if _, err := exec.LookPath("gpg"); err != nil {
		// Not fatal; only warn if gpg not present
		return checkResult{id: "gpg", title: "GnuPG", status: StatusWarn, summary: "gpg not found", note: "Tip: Install GnuPG if using PGP with SOPS."}
	}
	// Version and loopback support
	out, _ := exec.Command("gpg", "--version").CombinedOutput()
	lines := strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
	var ver string
	if len(lines) > 0 {
		ver = strings.TrimSpace(lines[0])
	}
	sub := []string{}
	// Agent socket dir (best effort)
	if _, err := exec.LookPath("gpgconf"); err == nil {
		if b, err := exec.Command("gpgconf", "--list-dirs", "agent-socket").CombinedOutput(); err == nil {
			socket := strings.TrimSpace(string(b))
			if socket != "" {
				sub = append(sub, "agent socket: "+socket)
			}
		}
	}
	// Check loopback support by looking for pinentry-mode mention in help
	helpOut, _ := exec.Command("gpg", "--help").CombinedOutput()
	loopbackSupported := strings.Contains(string(helpOut), "pinentry-mode")
	if loopbackSupported {
		sub = append(sub, "loopback: supported")
	} else {
		sub = append(sub, "loopback: unknown (try gpg>=2.1)")
	}
	if ver == "" {
		ver = "installed"
	}
	return checkResult{id: "gpg", title: "GnuPG present", status: StatusPass, summary: ver, sub: sub}
}

func checkHelperImage(ctx context.Context, docker *dockercli.Client) checkResult {
	// We use dockercli.HelperImage
	const img = dockercli.HelperImage
	exists, err := docker.ImageExists(ctx, img)
	if err != nil {
		// Non-fatal; treat as warn because registry may be offline
		return checkResult{id: "helper", title: "Helper image", status: StatusWarn, summary: fmt.Sprintf("check failed — %s", strings.TrimSpace(err.Error())), note: "Note: Could not verify helper image presence."}
	}
	if !exists {
		return checkResult{id: "helper", title: "Helper image missing", status: StatusWarn, summary: img, note: "Note: Skipped pulling (no registry access). Run again when online."}
	}

	// Image exists - alpine:3.22 includes all required binaries by default
	// (sh, find, xargs, getent, chown, chmod, cut)
	var sub []string
	sub = append(sub, "provides: sh, find, xargs, getent, chown, chmod, cut")
	return checkResult{id: "helper", title: "Helper image ready", status: StatusPass, summary: img, sub: sub}
}

func checkNetworkPerms(ctx context.Context, docker *dockercli.Client) checkResult {
	name := fmt.Sprintf("df-doctor-net-%d", time.Now().UnixNano())
	labels := map[string]string{"io.dockform.doctor": "1"}
	if err := docker.CreateNetwork(ctx, name, labels); err != nil {
		return checkResult{id: "net-perms", title: "Network create/remove", status: StatusFail, summary: "Cannot create network", errMsg: err.Error(), note: "Remedy: Ensure your user can access the Docker daemon (docker group)."}
	}
	// Best-effort cleanup
	if err := docker.RemoveNetwork(ctx, name); err != nil {
		// still pass but mention remove failure
		return checkResult{id: "net-perms", title: "Network create/remove", status: StatusWarn, summary: "Created but failed to remove", note: "Tip: Manually remove network: docker network rm " + name}
	}
	return checkResult{id: "net-perms", title: "Network create/remove", status: StatusPass, summary: "ok"}
}

func checkVolumePerms(ctx context.Context, docker *dockercli.Client) checkResult {
	name := fmt.Sprintf("df-doctor-vol-%d", time.Now().UnixNano())
	labels := map[string]string{"io.dockform.doctor": "1"}
	if err := docker.CreateVolume(ctx, name, labels); err != nil {
		return checkResult{id: "vol-perms", title: "Volume create/remove", status: StatusFail, summary: "Cannot create volume", errMsg: err.Error(), note: "Remedy: Ensure daemon is running and you have access to volumes."}
	}
	if err := docker.RemoveVolume(ctx, name); err != nil {
		return checkResult{id: "vol-perms", title: "Volume create/remove", status: StatusWarn, summary: "Created but failed to remove", note: "Tip: Manually remove volume: docker volume rm " + name}
	}
	return checkResult{id: "vol-perms", title: "Volume create/remove", status: StatusPass, summary: "ok"}
}

// printIndentedLines prints multi-line text with proper indentation and pipe continuation.
// Each line is prefixed with "│     " to maintain visual alignment under the check item.
func PrintIndentedLines(w io.Writer, text string) {
	// Wrap text at reasonable width (e.g., 80 chars minus the prefix "│     ")
	const maxWidth = 80
	const prefix = "│     "
	availWidth := maxWidth - len(prefix)

	words := strings.Fields(text)
	if len(words) == 0 {
		return
	}

	var line strings.Builder
	for i, word := range words {
		// Start new line or add word
		if line.Len() == 0 {
			line.WriteString(word)
		} else if line.Len()+1+len(word) <= availWidth {
			line.WriteString(" ")
			line.WriteString(word)
		} else {
			// Flush current line and start new one
			_, _ = fmt.Fprintf(w, "%s%s\n", prefix, line.String())
			line.Reset()
			line.WriteString(word)
		}
		// Flush last line
		if i == len(words)-1 {
			_, _ = fmt.Fprintf(w, "%s%s\n", prefix, line.String())
		}
	}
}
