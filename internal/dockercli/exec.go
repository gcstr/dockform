package dockercli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/util"
)

// Exec abstracts docker command execution for ease of testing.
type Exec interface {
	Run(ctx context.Context, args ...string) (string, error)
	RunInDir(ctx context.Context, dir string, args ...string) (string, error)
	RunInDirWithEnv(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error)
	RunWithStdin(ctx context.Context, stdin io.Reader, args ...string) (string, error)
}

// SystemExec is a real implementation that shells out to the docker CLI.
type SystemExec struct {
	ContextName string
}

func (s SystemExec) Run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	if s.ContextName != "" {
		cmd.Env = append(os.Environ(), fmt.Sprintf("DOCKER_CONTEXT=%s", s.ContextName))
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), apperr.Wrap("dockercli.Exec.Run", apperr.External, err, "%s", util.Truncate(stderr.String(), 512))
	}
	return stdout.String(), nil
}

func (s SystemExec) RunInDir(ctx context.Context, dir string, args ...string) (string, error) {
	return s.RunInDirWithEnv(ctx, dir, nil, args...)
}

func (s SystemExec) RunInDirWithEnv(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	baseEnv := os.Environ()
	if s.ContextName != "" {
		baseEnv = append(baseEnv, fmt.Sprintf("DOCKER_CONTEXT=%s", s.ContextName))
	}
	if len(extraEnv) > 0 {
		baseEnv = append(baseEnv, extraEnv...)
	}
	cmd.Env = baseEnv
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), apperr.Wrap("dockercli.Exec.RunInDirWithEnv", apperr.External, err, "%s", util.Truncate(stderr.String(), 512))
	}
	return stdout.String(), nil
}

func (s SystemExec) RunWithStdin(ctx context.Context, stdin io.Reader, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	baseEnv := os.Environ()
	if s.ContextName != "" {
		baseEnv = append(baseEnv, fmt.Sprintf("DOCKER_CONTEXT=%s", s.ContextName))
	}
	cmd.Env = baseEnv
	var stdout, stderr bytes.Buffer
	cmd.Stdin = stdin
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), apperr.Wrap("dockercli.Exec.RunWithStdin", apperr.External, err, "%s", util.Truncate(stderr.String(), 512))
	}
	return stdout.String(), nil
}
