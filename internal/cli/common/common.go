// Package common provides shared utilities for CLI commands.
package common

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/planner"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/gcstr/dockform/internal/validator"
	"github.com/spf13/cobra"
)

// AddTargetFlags adds deployment targeting flags to a command.
func AddTargetFlags(cmd *cobra.Command) {
	cmd.Flags().StringSlice("context", nil, "Target specific context(s)")
	cmd.Flags().StringSlice("stack", nil, "Target specific stack(s) in context/stack format")
	cmd.Flags().String("deployment", "", "Target a named deployment group")
}

// ReadTargetOptions reads targeting flags from a command.
func ReadTargetOptions(cmd *cobra.Command) TargetOptions {
	contexts, _ := cmd.Flags().GetStringSlice("context")
	stacks, _ := cmd.Flags().GetStringSlice("stack")
	deployment, _ := cmd.Flags().GetString("deployment")
	return TargetOptions{
		Contexts:   contexts,
		Stacks:     stacks,
		Deployment: deployment,
	}
}

// CreateClientFactory creates a Docker client factory for multi-context support.
func CreateClientFactory() *dockercli.DefaultClientFactory {
	return dockercli.NewClientFactory()
}

// ValidateWithFactory runs validation against the configuration using a client factory.
func ValidateWithFactory(ctx context.Context, cfg *manifest.Config, factory *dockercli.DefaultClientFactory) error {
	return validator.Validate(ctx, *cfg, factory)
}

// CreatePlannerWithFactory creates a planner with client factory and printer configured.
func CreatePlannerWithFactory(factory *dockercli.DefaultClientFactory, pr ui.Printer) *planner.Planner {
	return planner.NewWithFactory(factory).WithPrinter(pr)
}

// DisplayDaemonInfo shows the context configuration information.
func DisplayDaemonInfo(pr ui.Printer, cfg *manifest.Config) {
	if len(cfg.Contexts) == 0 {
		pr.Plain("\n│ No contexts configured")
		return
	}

	var lines []string
	lines = append(lines, "")
	for name := range cfg.Contexts {
		lines = append(lines, fmt.Sprintf("│ Context: %s", ui.Italic(name)))
	}
	if cfg.Identifier != "" {
		lines = append(lines, fmt.Sprintf("│ Identifier: %s", ui.Italic(cfg.Identifier)))
	}
	pr.Plain("%s", strings.Join(lines, "\n"))
}

// GetFirstIdentifier returns the project identifier (for destroy confirmation).
func GetFirstIdentifier(cfg *manifest.Config) string {
	return cfg.Identifier
}

// GetFirstDaemon returns the name and config of the first context.
func GetFirstDaemon(cfg *manifest.Config) (string, manifest.ContextConfig) {
	for name, context := range cfg.Contexts {
		return name, context
	}
	return "", manifest.ContextConfig{}
}

// MaskSecretsSimple redacts secret-like values from a YAML string based on stack config.
// This is a pragmatic heuristic: it masks occurrences of values provided via stack/environment
// inline env and sops secrets (after decryption via BuildInlineEnv), as well as common sensitive keys.
func MaskSecretsSimple(yamlStr string, stack manifest.Stack, strategy string) string {
	// Determine mask replacement based on strategy
	mask := func(s string) string {
		switch strategy {
		case "partial":
			if len(s) <= 4 {
				return "****"
			}
			return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
		case "preserve-length":
			if l := len(s); l > 0 {
				return strings.Repeat("*", l)
			}
			return ""
		case "full":
			fallthrough
		default:
			return "********"
		}
	}

	// Mask by common sensitive keys patterns: password, secret, token, key
	// YAML format allows: key: value or key: "value"
	// We keep it simple and mask the value part.
	keyPatterns := []string{"password", "secret", "token", "key", "apikey", "api_key", "access_key", "private_key"}
	for _, kp := range keyPatterns {
		// (?i) case-insensitive; match lines like "kp: something"
		re := regexp.MustCompile(`(?i)(` + kp + `\s*:\s*)([^\n#]+)`) // stop at newline or comment
		yamlStr = re.ReplaceAllStringFunc(yamlStr, func(m string) string {
			parts := re.FindStringSubmatch(m)
			if len(parts) != 3 {
				return m
			}
			prefix := parts[1]
			val := strings.TrimSpace(parts[2])
			// Keep quotes if present
			if strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"") && len(val) >= 2 {
				inner := val[1 : len(val)-1]
				return prefix + "\"" + mask(inner) + "\""
			}
			return prefix + mask(val)
		})
	}

	return yamlStr
}
