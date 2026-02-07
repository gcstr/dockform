package composecmd_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/cli"
	"github.com/gcstr/dockform/internal/cli/clitest"
	"github.com/gcstr/dockform/internal/cli/composecmd"
	"github.com/spf13/cobra"
)

func TestNewRegistersRenderSubcommand(t *testing.T) {
	cmd := composecmd.New()
	if cmd.Use != "compose" {
		t.Fatalf("expected command use 'compose', got %q", cmd.Use)
	}
	var render *cobra.Command
	for _, c := range cmd.Commands() {
		if c.Name() == "render" {
			render = c
			break
		}
	}
	if render == nil {
		t.Fatalf("expected render subcommand to be registered on compose")
	}
	if render.Flags().Lookup("show-secrets") == nil {
		t.Fatalf("render command missing --show-secrets flag")
	}
	if render.Flags().Lookup("mask") == nil {
		t.Fatalf("render command missing --mask flag")
	}
}

func TestComposeRenderMasksSecretsByDefault(t *testing.T) {
	const secret = "topsecretvalue"
	cfgPath := writeComposeManifest(t, secret, "", nil)
	undo := clitest.WithCustomDockerStub(t, composeConfigStub())
	defer undo()

	stdout, stderr, err := runComposeRender(t, "default/web", cfgPath)
	if err != nil {
		t.Fatalf("compose render: %v", err)
	}
	if stdout == "" {
		t.Fatalf("expected compose render to produce output")
	}
	if !strings.Contains(stdout, "secret: ********") {
		t.Fatalf("expected secret to be masked, got output: %s", stdout)
	}
	if strings.Contains(stdout, secret) {
		t.Fatalf("masked output should not include original secret; output: %s", stdout)
	}
	if stderr != "" {
		t.Fatalf("expected no stderr output, got: %s", stderr)
	}
}

func TestComposeRenderHonorsMaskStrategy(t *testing.T) {
	const secret = "supersecret"
	cfgPath := writeComposeManifest(t, secret, "", nil)
	undo := clitest.WithCustomDockerStub(t, composeConfigStub())
	defer undo()

	stdout, _, err := runComposeRender(t, "default/web", cfgPath, "--mask", "partial")
	if err != nil {
		t.Fatalf("compose render (mask partial): %v", err)
	}
	want := secret[:2] + strings.Repeat("*", len(secret)-4) + secret[len(secret)-2:]
	if !strings.Contains(stdout, "secret: "+want) {
		t.Fatalf("expected partial mask %q, got output: %s", want, stdout)
	}
}

func TestComposeRenderShowsSecretsWhenRequested(t *testing.T) {
	const secret = "visible-secret"
	cfgPath := writeComposeManifest(t, secret, "", nil)
	undo := clitest.WithCustomDockerStub(t, composeConfigStub())
	defer undo()

	stdout, _, err := runComposeRender(t, "default/web", cfgPath, "--show-secrets")
	if err != nil {
		t.Fatalf("compose render --show-secrets: %v", err)
	}
	if !strings.Contains(stdout, "secret: "+secret) {
		t.Fatalf("expected raw secret in output, got: %s", stdout)
	}
}

func TestComposeRenderWarnsOnMissingEnv(t *testing.T) {
	const secret = "warn-secret"
	cfgPath := writeComposeManifest(t, secret, "", []string{"OPTIONAL=${MISSING_IDENTIFIER}"})
	undo := clitest.WithCustomDockerStub(t, composeConfigStub())
	defer undo()

	stdout, stderr, err := runComposeRender(t, "default/web", cfgPath)
	if err != nil {
		t.Fatalf("compose render with warning: %v", err)
	}
	if stdout == "" {
		t.Fatalf("expected compose render output, got empty stdout")
	}
	if !strings.Contains(stderr, "environment variable MISSING_IDENTIFIER is not set") {
		t.Fatalf("expected warning about missing identifier env, stderr: %s", stderr)
	}
}

func TestComposeRenderUnknownStackReturnsError(t *testing.T) {
	cfg := clitest.BasicConfigPath(t)
	root := cli.TestNewRootCmd()
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetIn(bytes.NewBuffer(nil))
	root.SetArgs([]string{"compose", "render", "does-not-exist", "--manifest", cfg})

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error for unknown stack")
	}
	if !apperr.IsKind(err, apperr.InvalidInput) {
		t.Fatalf("expected invalid input error, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "unknown stack") {
		t.Fatalf("error should mention unknown stack, got: %v", err)
	}
}

// writeComposeManifest creates a manifest and compose file for tests.
func writeComposeManifest(t *testing.T, secret string, dockerExtras string, extraInline []string) string {
	t.Helper()
	baseDir := t.TempDir()
	stackDir := filepath.Join(baseDir, "app")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatalf("mkdir stack dir: %v", err)
	}
	composePath := filepath.Join(stackDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte("services:\n  web:\n    image: nginx:alpine\n"), 0o644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	identifierBlock := "identifier: demo\n"
	if strings.TrimSpace(dockerExtras) != "" {
		// dockerExtras might be "identifier: xyz" - extract it
		if strings.HasPrefix(strings.TrimSpace(dockerExtras), "identifier:") {
			identifierBlock = strings.TrimSpace(dockerExtras) + "\n"
		}
	}
	daemonBlock := identifierBlock + "contexts:\n  default: {}\n"

	inlineVals := append([]string{"API_SECRET=" + secret}, extraInline...)
	var inlineBuilder strings.Builder
	for _, entry := range inlineVals {
		inlineBuilder.WriteString("        - ")
		inlineBuilder.WriteString(entry)
		inlineBuilder.WriteString("\n")
	}

	manifest := fmt.Sprintf(`%sstacks:
  default/web:
    root: app
    files:
      - docker-compose.yml
    environment:
      inline:
%s`, daemonBlock, inlineBuilder.String())
	manifestPath := filepath.Join(baseDir, "dockform.yml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return manifestPath
}

func composeConfigStub() string {
	if runtime.GOOS == "windows" {
		return `@echo off
if "%1"=="compose" (
    for %%a in (%*) do if "%%a"=="config" (
        if "%API_SECRET%"=="" (
            echo API_SECRET missing 1>&2
            exit /b 3
        )
        echo services:
        echo   web:
        echo     environment:
        echo       secret: %API_SECRET%
        exit /b 0
    )
)
if "%1"=="version" exit /b 0
exit /b 0
`
	}
	return `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  compose)
    for arg in "$@"; do
      if [ "$arg" = "config" ]; then
        if [ -z "${API_SECRET}" ]; then
          echo "API_SECRET missing" >&2
          exit 3
        fi
        cat <<EOF
services:
  web:
    environment:
      secret: ${API_SECRET}
EOF
        exit 0
      fi
    done
    ;;
  version)
    exit 0
    ;;
esac
exit 0
`
}

func runComposeRender(t *testing.T, stack, cfgPath string, extraArgs ...string) (string, string, error) {
	t.Helper()
	root := cli.TestNewRootCmd()
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetIn(bytes.NewBuffer(nil))
	args := []string{"compose", "render", stack, "--manifest", cfgPath}
	args = append(args, extraArgs...)
	root.SetArgs(args)
	err := root.Execute()
	return outBuf.String(), errBuf.String(), err
}
