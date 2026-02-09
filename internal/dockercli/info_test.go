package dockercli

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type infoExecStub struct {
	calls [][]string

	serverOut string
	serverErr error

	contextOut string
	contextErr error

	composeShortOut string
	composeShortErr error
	composeLongOut  string
	composeLongErr  error

	imageInspectErr error
}

func (s *infoExecStub) Run(ctx context.Context, args ...string) (string, error) {
	s.calls = append(s.calls, args)
	switch {
	case len(args) == 3 && args[0] == "version" && args[1] == "--format":
		return s.serverOut, s.serverErr
	case len(args) == 5 && args[0] == "context" && args[1] == "inspect":
		return s.contextOut, s.contextErr
	case len(args) == 3 && args[0] == "compose" && args[1] == "version" && args[2] == "--short":
		return s.composeShortOut, s.composeShortErr
	case len(args) == 2 && args[0] == "compose" && args[1] == "version":
		return s.composeLongOut, s.composeLongErr
	case len(args) >= 3 && args[0] == "image" && args[1] == "inspect":
		if s.imageInspectErr != nil {
			return "", s.imageInspectErr
		}
		return "[]", nil
	default:
		return "", nil
	}
}

func (s *infoExecStub) RunInDir(ctx context.Context, dir string, args ...string) (string, error) {
	return s.Run(ctx, args...)
}

func (s *infoExecStub) RunInDirWithEnv(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error) {
	return s.Run(ctx, args...)
}

func (s *infoExecStub) RunWithStdin(ctx context.Context, stdin io.Reader, args ...string) (string, error) {
	return s.Run(ctx, args...)
}

func (s *infoExecStub) RunWithStdout(ctx context.Context, stdout io.Writer, args ...string) error {
	_, err := s.Run(ctx, args...)
	return err
}

func (s *infoExecStub) RunDetailed(ctx context.Context, opts Options, args ...string) (Result, error) {
	out, err := s.Run(ctx, args...)
	return Result{Stdout: out, Stderr: "", ExitCode: 0}, err
}

func TestServerVersion_TrimsOutputAndReturnsErrors(t *testing.T) {
	stub := &infoExecStub{serverOut: " 24.0.7 \n"}
	c := &Client{exec: stub}

	got, err := c.ServerVersion(context.Background())
	if err != nil {
		t.Fatalf("server version: %v", err)
	}
	if got != "24.0.7" {
		t.Fatalf("unexpected server version: %q", got)
	}

	stub.serverErr = errors.New("docker unavailable")
	if _, err := c.ServerVersion(context.Background()); err == nil {
		t.Fatalf("expected server version error")
	}
}

func TestContextHost_DefaultContextAndQuoteTrimming(t *testing.T) {
	stub := &infoExecStub{contextOut: "\"unix:///var/run/docker.sock\"\n"}
	c := &Client{exec: stub}

	got, err := c.ContextHost(context.Background())
	if err != nil {
		t.Fatalf("context host: %v", err)
	}
	if got != "unix:///var/run/docker.sock" {
		t.Fatalf("unexpected host: %q", got)
	}
	if len(stub.calls) == 0 || !containsArgSeq(stub.calls[0], []string{"context", "inspect", "default"}) {
		t.Fatalf("expected inspect default context call, got: %#v", stub.calls)
	}
}

func TestContextHost_ReturnsOverrideDirectly(t *testing.T) {
	stub := &infoExecStub{}
	c := &Client{exec: stub, hostOverride: "ssh://user@server"}

	got, err := c.ContextHost(context.Background())
	if err != nil {
		t.Fatalf("context host: %v", err)
	}
	if got != "ssh://user@server" {
		t.Fatalf("expected host override, got %q", got)
	}
	if len(stub.calls) != 0 {
		t.Fatalf("expected no exec calls when host override is set, got %d", len(stub.calls))
	}
}

func TestComposeVersion_PrefersShortAndFallsBack(t *testing.T) {
	stub := &infoExecStub{composeShortOut: "v2.31.0\n"}
	c := &Client{exec: stub}

	got, err := c.ComposeVersion(context.Background())
	if err != nil {
		t.Fatalf("compose short version: %v", err)
	}
	if got != "v2.31.0" {
		t.Fatalf("unexpected short version: %q", got)
	}

	stub.composeShortErr = errors.New("short not supported")
	stub.composeLongOut = "Docker Compose version v2.30.1\n"
	got, err = c.ComposeVersion(context.Background())
	if err != nil {
		t.Fatalf("compose fallback version: %v", err)
	}
	if got != "Docker Compose version v2.30.1" {
		t.Fatalf("unexpected fallback version: %q", got)
	}

	stub.composeLongErr = errors.New("compose failed")
	if _, err := c.ComposeVersion(context.Background()); err == nil {
		t.Fatalf("expected compose fallback error")
	}
}

func TestImageExists_HandlesBlankAndInspectErrors(t *testing.T) {
	stub := &infoExecStub{}
	c := &Client{exec: stub}

	exists, err := c.ImageExists(context.Background(), "   ")
	if err != nil || exists {
		t.Fatalf("blank image should return false,nil; got exists=%v err=%v", exists, err)
	}
	if len(stub.calls) != 0 {
		t.Fatalf("blank image should not call docker inspect, got calls: %#v", stub.calls)
	}

	stub.imageInspectErr = errors.New("not found")
	exists, err = c.ImageExists(context.Background(), "nginx:latest")
	if err != nil || exists {
		t.Fatalf("missing image should return false,nil; got exists=%v err=%v", exists, err)
	}
	if len(stub.calls) == 0 || !strings.Contains(strings.Join(stub.calls[len(stub.calls)-1], " "), "image inspect nginx:latest") {
		t.Fatalf("expected image inspect call, got: %#v", stub.calls)
	}

	stub.imageInspectErr = nil
	exists, err = c.ImageExists(context.Background(), "nginx:latest")
	if err != nil || !exists {
		t.Fatalf("expected existing image true,nil; got exists=%v err=%v", exists, err)
	}
}
