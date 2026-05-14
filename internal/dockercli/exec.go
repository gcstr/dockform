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
	// RunDetailed runs a command and returns structured Result with stdout, stderr, and exit code.
	RunDetailed(ctx context.Context, opts Options, args ...string) (Result, error)
}

// MaxConcurrentSSH is the maximum number of concurrent docker CLI invocations
// allowed against a single remote (SSH-based) Docker context. SSH daemons have
// a MaxStartups threshold (default 10:30:100) that randomly drops connections
// when too many arrive at once. Keeping this well below 10 avoids transient
// "Connection reset by peer" failures during parallel plan building.
const MaxConcurrentSSH = 4

const (
	sshMaxRetries     = 3
	sshRetryBaseDelay = 500 * time.Millisecond
)

// SystemExec is a real implementation that shells out to the docker CLI.
type SystemExec struct {
	ContextName    string
	HostOverride   string // When set, uses DOCKER_HOST instead of DOCKER_CONTEXT
	DefaultTimeout time.Duration
	Logger         LoggerHook
	sem            chan struct{} // limits concurrent commands; nil means unlimited
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

func isSSHConnectionError(stderr string) bool {
	return strings.Contains(stderr, "kex_exchange_identification") ||
		strings.Contains(stderr, "Connection reset by peer") ||
		strings.Contains(stderr, "Connection closed by") ||
		strings.Contains(stderr, "ssh_exchange_identification") ||
		strings.Contains(stderr, "banner exchange")
}

func (s SystemExec) RunDetailed(ctx context.Context, opts Options, args ...string) (Result, error) {
	l := logger.FromContext(ctx).With("component", "dockercli")
	if opts.Timeout <= 0 && s.DefaultTimeout > 0 {
		opts.Timeout = s.DefaultTimeout
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	if s.sem != nil {
		select {
		case s.sem <- struct{}{}:
			defer func() { <-s.sem }()
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}

	if s.Logger != nil {
		s.Logger(ExecEvent{Phase: "start", Args: args, Dir: opts.Dir})
	}
	st := logger.StartStep(l, "docker_exec", strings.Join(args, " "), "resource_kind", "process")

	baseEnv := os.Environ()
	if s.HostOverride != "" {
		baseEnv = append(baseEnv, fmt.Sprintf("DOCKER_HOST=%s", s.HostOverride))
	} else if s.ContextName != "" {
		baseEnv = append(baseEnv, fmt.Sprintf("DOCKER_CONTEXT=%s", s.ContextName))
	}
	if len(opts.Env) > 0 {
		baseEnv = append(baseEnv, opts.Env...)
	}

	_, streamingStdout := ctx.Value(stdOutWriterKey{}).(io.Writer)
	canRetry := s.sem != nil && opts.Stdin == nil && !streamingStdout
	maxAttempts := 1
	if canRetry {
		maxAttempts = sshMaxRetries + 1
	}

	start := time.Now()
	var res Result
	var runErr error

	for attempt := range maxAttempts {
		if attempt > 0 {
			delay := sshRetryBaseDelay * time.Duration(1<<uint(attempt-1))
			l.Debug("ssh_retry", "attempt", attempt+1, "delay", delay.String(), "args", strings.Join(args, " "))
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return res, ctx.Err()
			}
		}

		cmd := exec.CommandContext(ctx, "docker", args...)
		cmd.Env = baseEnv
		if opts.Dir != "" {
			cmd.Dir = opts.Dir
		}
		if opts.Stdin != nil {
			cmd.Stdin = opts.Stdin
		}

		var stdout, stderr bytes.Buffer
		if sw, ok := ctx.Value(stdOutWriterKey{}).(io.Writer); ok && sw != nil {
			cmd.Stdout = sw
		} else {
			cmd.Stdout = &stdout
		}
		cmd.Stderr = &stderr

		runErr = cmd.Run()

		exitCode := 0
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		outStr := ""
		if !streamingStdout {
			outStr = stdout.String()
		}
		res = Result{Stdout: outStr, Stderr: stderr.String(), ExitCode: exitCode, Duration: time.Since(start)}

		if runErr == nil || !isSSHConnectionError(res.Stderr) {
			break
		}
	}

	if s.Logger != nil {
		s.Logger(ExecEvent{Phase: "finish", Args: args, Dir: opts.Dir, Duration: res.Duration, ExitCode: res.ExitCode, Err: runErr})
	}

	if runErr != nil {
		_ = st.Fail(runErr, "exit_code", res.ExitCode)
		return res, apperr.Wrap("dockercli.Exec", apperr.External, runErr, "%s", res.Stderr)
	}
	st.OK(res.ExitCode == 0, "exit_code", res.ExitCode)
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
	// For long-running streams, avoid buffering stderr growth; ignore stdout buffering entirely
	_, err := s.RunDetailed(ctxWith, Options{Timeout: 0}, args...)
	return err
}
