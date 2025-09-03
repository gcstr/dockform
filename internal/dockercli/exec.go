package dockercli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/gcstr/dockform/internal/apperr"
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
	ContextName    string
	DefaultTimeout time.Duration
	Logger         LoggerHook
}

// Options controls execution behavior per call.
type Options struct {
	Dir     string
	Env     []string
	Stdin   io.Reader
	Timeout time.Duration
}

// Result contains structured outcome of a command.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// LoggerHook receives start and finish events for command execution.
type LoggerHook func(event ExecEvent)

// ExecEvent describes a loggable moment in command execution.
type ExecEvent struct {
	Phase    string // "start" or "finish"
	Args     []string
	Dir      string
	Duration time.Duration
	ExitCode int
	Err      error
}

// WithDefaultTimeout sets a default timeout for all runs when Options.Timeout is not provided.
func (s *SystemExec) WithDefaultTimeout(d time.Duration) *SystemExec { s.DefaultTimeout = d; return s }

// WithLogger sets a logger hook to observe command execution.
func (s *SystemExec) WithLogger(h LoggerHook) *SystemExec { s.Logger = h; return s }

func (s SystemExec) RunDetailed(ctx context.Context, opts Options, args ...string) (Result, error) {
	// Honor per-call timeout, falling back to default if set
	if opts.Timeout <= 0 && s.DefaultTimeout > 0 {
		opts.Timeout = s.DefaultTimeout
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	if s.Logger != nil {
		s.Logger(ExecEvent{Phase: "start", Args: args, Dir: opts.Dir})
	}

	start := time.Now()
	cmd := exec.CommandContext(ctx, "docker", args...)
	baseEnv := os.Environ()
	if s.ContextName != "" {
		baseEnv = append(baseEnv, fmt.Sprintf("DOCKER_CONTEXT=%s", s.ContextName))
	}
	if len(opts.Env) > 0 {
		baseEnv = append(baseEnv, opts.Env...)
	}
	cmd.Env = baseEnv
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}
	if opts.Stdin != nil {
		cmd.Stdin = opts.Stdin
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	dur := time.Since(start)

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	res := Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode, Duration: dur}

	if s.Logger != nil {
		s.Logger(ExecEvent{Phase: "finish", Args: args, Dir: opts.Dir, Duration: dur, ExitCode: exitCode, Err: runErr})
	}

	if runErr != nil {
		return res, apperr.Wrap("dockercli.Exec", apperr.External, runErr, "%s", res.Stderr)
	}
	return res, nil
}

func (s SystemExec) Run(ctx context.Context, args ...string) (string, error) {
	res, err := s.RunDetailed(ctx, Options{}, args...)
	return res.Stdout, err
}

func (s SystemExec) RunInDir(ctx context.Context, dir string, args ...string) (string, error) {
	res, err := s.RunDetailed(ctx, Options{Dir: dir}, args...)
	return res.Stdout, err
}

func (s SystemExec) RunInDirWithEnv(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error) {
	res, err := s.RunDetailed(ctx, Options{Dir: dir, Env: extraEnv}, args...)
	return res.Stdout, err
}

func (s SystemExec) RunWithStdin(ctx context.Context, stdin io.Reader, args ...string) (string, error) {
	res, err := s.RunDetailed(ctx, Options{Stdin: stdin}, args...)
	return res.Stdout, err
}
