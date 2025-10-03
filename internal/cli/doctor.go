package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/spf13/cobra"
)

type checkStatus int

const (
	statusPass checkStatus = iota
	statusWarn
	statusFail
)

type checkResult struct {
	id      string
	title   string
	status  checkStatus
	summary string
	note    string   // Remedy/Tip/Note line (single line)
	errMsg  string   // Optional error line for FAILs
	sub     []string // Additional informational lines to render under the item
}

func newDoctorCmd() *cobra.Command {
	var contextName string

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run a quick health scan and report system readiness for Dockform",
		RunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()

			// Resolve context override
			ctxName := strings.TrimSpace(contextName)
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

			// [engine]
			engRes := checkEngine(ctx, docker)
			results = append(results, engRes)

			// [context]
			results = append(results, checkContextReachable(ctx, docker, ctxName))

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
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Dockform Doctor — health scan")
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
				case statusPass:
					icon = ui.GreenText("✓")
				case statusWarn:
					icon = ui.YellowText("!")
				case statusFail:
					icon = ui.RedText("×")
				}
				line = fmt.Sprintf("│ %s %s %s — %s", icon, bracketedID, r.title, r.summary)
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), line)
				if r.status == statusWarn && r.note != "" {
					printIndentedLines(cmd.OutOrStdout(), r.note)
				}
				if r.status == statusFail {
					if r.errMsg != "" {
						printIndentedLines(cmd.OutOrStdout(), "Error: "+r.errMsg)
					}
					if r.note != "" {
						printIndentedLines(cmd.OutOrStdout(), r.note)
					}
				}
				// Render any additional sub-lines regardless of status
				for _, s := range r.sub {
					if strings.TrimSpace(s) == "" {
						continue
					}
					printIndentedLines(cmd.OutOrStdout(), s)
				}
				switch r.status {
				case statusPass:
					pass++
				case statusWarn:
					warn++
				case statusFail:
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
	if err := docker.CheckDaemon(ctx); err != nil {
		return checkResult{
			id:      "engine",
			title:   "Docker Engine reachable",
			status:  statusFail,
			summary: "daemon not reachable",
			errMsg:  err.Error(),
			note:    "Remedy: Ensure Docker is running and your user can access it.",
		}
	}
	ver, err := docker.ServerVersion(ctx)
	if err != nil || strings.TrimSpace(ver) == "" {
		return checkResult{id: "engine", title: "Docker Engine reachable", status: statusPass, summary: "version unknown"}
	}
	return checkResult{id: "engine", title: "Docker Engine reachable", status: statusPass, summary: "v" + ver}
}

func checkContextReachable(ctx context.Context, docker *dockercli.Client, ctxName string) checkResult {
	// If engine is reachable, context is implicitly resolvable; still try to display
	if _, err := docker.ContextHost(ctx); err != nil {
		return checkResult{id: "context", title: "Active context reachable", status: statusFail, summary: fmt.Sprintf("%q unreachable", ctxName), errMsg: err.Error(), note: "Remedy: Verify context exists: docker context ls"}
	}
	return checkResult{id: "context", title: "Active context reachable", status: statusPass, summary: fmt.Sprintf("%q", ctxName)}
}

func checkCompose(ctx context.Context, docker *dockercli.Client) checkResult {
	ver, err := docker.ComposeVersion(ctx)
	if err != nil {
		return checkResult{id: "compose", title: "Docker Compose (v2)", status: statusFail, summary: "not found", note: "Remedy: Install docker compose plugin (v2+).", errMsg: err.Error()}
	}
	// Heuristic: ensure it mentions v2
	if !strings.Contains(ver, "v2") && !isSemver2(ver) {
		return checkResult{id: "compose", title: "Docker Compose (v2)", status: statusFail, summary: "not found", note: "Remedy: Install docker compose plugin (v2+)."}
	}
	// Extract short if full text
	short := strings.TrimSpace(ver)
	return checkResult{id: "compose", title: "Docker Compose plugin", status: statusPass, summary: short}
}

func isSemver2(s string) bool {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if s == "" {
		return false
	}
	// crude check: starts with 2.
	return strings.HasPrefix(s, "2.")
}

func checkSops() checkResult {
	if _, err := exec.LookPath("sops"); err != nil {
		// Warn only
		return checkResult{id: "sops", title: "SOPS", status: statusWarn, summary: "not found", note: "Tip: Install SOPS to decrypt secrets: https://github.com/getsops/sops"}
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
		return checkResult{id: "sops", title: "SOPS present", status: statusPass, summary: "installed", sub: sub}
	}
	return checkResult{id: "sops", title: "SOPS present", status: statusPass, summary: ver, sub: sub}
}

func checkGpg() checkResult {
    if _, err := exec.LookPath("gpg"); err != nil {
        // Not fatal; only warn if gpg not present
        return checkResult{id: "gpg", title: "GnuPG", status: statusWarn, summary: "gpg not found", note: "Tip: Install GnuPG if using PGP with SOPS."}
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
    return checkResult{id: "gpg", title: "GnuPG present", status: statusPass, summary: ver, sub: sub}
}

func checkHelperImage(ctx context.Context, docker *dockercli.Client) checkResult {
	// We use dockercli.HelperImage
	const img = dockercli.HelperImage
	exists, err := docker.ImageExists(ctx, img)
	if err != nil {
		// Non-fatal; treat as warn because registry may be offline
		return checkResult{id: "helper", title: "Helper image", status: statusWarn, summary: fmt.Sprintf("check failed — %s", strings.TrimSpace(err.Error())), note: "Note: Could not verify helper image presence."}
	}
	if exists {
		return checkResult{id: "helper", title: "Helper image present", status: statusPass, summary: img}
	}
	return checkResult{id: "helper", title: "Helper image missing", status: statusWarn, summary: img, note: "Note: Skipped pulling (no registry access). Run again when online."}
}

func checkNetworkPerms(ctx context.Context, docker *dockercli.Client) checkResult {
	name := fmt.Sprintf("df-doctor-net-%d", time.Now().UnixNano())
	labels := map[string]string{"io.dockform.doctor": "1"}
	if err := docker.CreateNetwork(ctx, name, labels); err != nil {
		return checkResult{id: "net-perms", title: "Network create/remove", status: statusFail, summary: "Cannot create network", errMsg: err.Error(), note: "Remedy: Ensure your user can access the Docker daemon (docker group)."}
	}
	// Best-effort cleanup
	if err := docker.RemoveNetwork(ctx, name); err != nil {
		// still pass but mention remove failure
		return checkResult{id: "net-perms", title: "Network create/remove", status: statusWarn, summary: "Created but failed to remove", note: "Tip: Manually remove network: docker network rm " + name}
	}
	return checkResult{id: "net-perms", title: "Network create/remove", status: statusPass, summary: "ok"}
}

func checkVolumePerms(ctx context.Context, docker *dockercli.Client) checkResult {
	name := fmt.Sprintf("df-doctor-vol-%d", time.Now().UnixNano())
	labels := map[string]string{"io.dockform.doctor": "1"}
	if err := docker.CreateVolume(ctx, name, labels); err != nil {
		return checkResult{id: "vol-perms", title: "Volume create/remove", status: statusFail, summary: "Cannot create volume", errMsg: err.Error(), note: "Remedy: Ensure daemon is running and you have access to volumes."}
	}
	if err := docker.RemoveVolume(ctx, name); err != nil {
		return checkResult{id: "vol-perms", title: "Volume create/remove", status: statusWarn, summary: "Created but failed to remove", note: "Tip: Manually remove volume: docker volume rm " + name}
	}
	return checkResult{id: "vol-perms", title: "Volume create/remove", status: statusPass, summary: "ok"}
}

// printIndentedLines prints multi-line text with proper indentation and pipe continuation.
// Each line is prefixed with "│     " to maintain visual alignment under the check item.
func printIndentedLines(w io.Writer, text string) {
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
