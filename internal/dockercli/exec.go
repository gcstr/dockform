package dockercli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/logger"
)

// Exec abstracts docker command execution for ease of testing.
type Exec interface {
	Run(ctx context.Context, args ...string) (string, error)
	RunInDir(ctx context.Context, dir string, args ...string) (string, error)
	RunInDirWithEnv(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error)
	RunWithStdin(ctx context.Context, stdin io.Reader, args ...string) (string, error)
	// RunWithStdout streams the stdout of the docker command to the provided writer without buffering it in memory.
	// Stderr is still captured and included in any returned error for debuggability.
	RunWithStdout(ctx context.Context, stdout io.Writer, args ...string) error
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
	l := logger.FromContext(ctx).With("component", "dockercli")
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
	st := logger.StartStep(l, "docker_exec", strings.Join(args, " "), "resource_kind", "process")

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
	// Allow caller-provided Stdout when opts.Stdout is set via RunWithStdout
	if sw, ok := ctx.Value(stdOutWriterKey{}).(io.Writer); ok && sw != nil {
		cmd.Stdout = sw
	} else {
		cmd.Stdout = &stdout
	}
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	dur := time.Since(start)

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	// If stdout was streamed to a writer, avoid materializing it in Result
	outStr := ""
	if _, ok := ctx.Value(stdOutWriterKey{}).(io.Writer); !ok {
		outStr = stdout.String()
	}
	res := Result{Stdout: outStr, Stderr: stderr.String(), ExitCode: exitCode, Duration: dur}

	if s.Logger != nil {
		s.Logger(ExecEvent{Phase: "finish", Args: args, Dir: opts.Dir, Duration: dur, ExitCode: exitCode, Err: runErr})
	}

	if runErr != nil {
		_ = st.Fail(runErr, "exit_code", exitCode)
		return res, apperr.Wrap("dockercli.Exec", apperr.External, runErr, "%s", res.Stderr)
	}
	st.OK(exitCode == 0, "exit_code", exitCode)
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

// stdOutWriterKey is a context key type used to pass a stdout writer to RunDetailed
type stdOutWriterKey struct{}

// RunWithStdout executes the docker command and streams stdout to the provided writer.
// It does not buffer stdout in memory.
func (s SystemExec) RunWithStdout(ctx context.Context, stdout io.Writer, args ...string) error {
	if stdout == nil {
		return apperr.New("dockercli.Exec", apperr.InvalidInput, "stdout writer required")
	}
	// Pass writer via context to avoid changing Options signature
	ctxWith := context.WithValue(ctx, stdOutWriterKey{}, stdout)
	_, err := s.RunDetailed(ctxWith, Options{}, args...)
	return err
}
