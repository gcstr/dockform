package dockercli

import (
	"context"
	"io"
	"strings"
	"testing"
)

type netExecStub struct{ lastArgs []string }

func (n *netExecStub) Run(ctx context.Context, args ...string) (string, error) {
	n.lastArgs = args
	if len(args) >= 2 && args[0] == "network" && args[1] == "ls" {
		return "net1\n\nnet2\n", nil
	}
	return "", nil
}
func (n *netExecStub) RunInDir(ctx context.Context, dir string, args ...string) (string, error) {
	return n.Run(ctx, args...)
}
func (n *netExecStub) RunInDirWithEnv(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error) {
	return n.Run(ctx, args...)
}
func (n *netExecStub) RunWithStdin(ctx context.Context, stdin io.Reader, args ...string) (string, error) {
	n.lastArgs = args
	return "", nil
}
func (n *netExecStub) RunWithStdout(ctx context.Context, stdout io.Writer, args ...string) error {
	n.lastArgs = args
	return nil
}
func (n *netExecStub) RunDetailed(ctx context.Context, opts Options, args ...string) (Result, error) {
	out, err := n.Run(ctx, args...)
	return Result{Stdout: out, Stderr: "", ExitCode: 0}, err
}

func TestListNetworks_ParsesAndFilters(t *testing.T) {
	stub := &netExecStub{}
	c := &Client{exec: stub}
	nets, err := c.ListNetworks(context.Background())
	if err != nil || len(nets) != 2 || nets[0] != "net1" || nets[1] != "net2" {
		t.Fatalf("list networks parse: %v %#v", err, nets)
	}

	// With identifier, ensure filter flag is present
	c = &Client{exec: stub, identifier: "demo"}
	_, _ = c.ListNetworks(context.Background())
	joined := strings.Join(stub.lastArgs, " ")
	if !strings.Contains(joined, "--filter label=io.dockform.identifier=demo") {
		t.Fatalf("expected identifier filter in args: %s", joined)
	}
}

func TestCreateNetwork_AddsLabels(t *testing.T) {
	stub := &netExecStub{}
	c := &Client{exec: stub}
	if err := c.CreateNetwork(context.Background(), "n1", map[string]string{"a": "1", "b": "2"}); err != nil {
		t.Fatalf("create network: %v", err)
	}
	if len(stub.lastArgs) == 0 || stub.lastArgs[0] != "network" || stub.lastArgs[1] != "create" {
		t.Fatalf("unexpected args: %#v", stub.lastArgs)
	}
	if !contains(stub.lastArgs, "--label") || !contains(stub.lastArgs, "a=1") || !contains(stub.lastArgs, "b=2") {
		t.Fatalf("missing label args: %#v", stub.lastArgs)
	}
	if stub.lastArgs[len(stub.lastArgs)-1] != "n1" {
		t.Fatalf("network name position mismatch: %#v", stub.lastArgs)
	}
}

func TestCreateNetwork_WithDriverAndOptions(t *testing.T) {
	stub := &netExecStub{}
	c := &Client{exec: stub}
	opts := NetworkCreateOpts{Driver: "bridge", Options: map[string]string{"com.docker.network.bridge.name": "df_mynet", "com.docker.network.bridge.enable_icc": "false"}, IPv6: true, Subnet: "172.18.0.0/16", Gateway: "172.18.0.1", IPRange: "172.18.0.0/24", AuxAddresses: map[string]string{"host1": "172.18.0.2"}}
	if err := c.CreateNetwork(context.Background(), "mynet", map[string]string{"io.dockform.identifier": "demo"}, opts); err != nil {
		t.Fatalf("create network with opts: %v", err)
	}
	joined := strings.Join(stub.lastArgs, " ")
	if !strings.Contains(joined, "--driver bridge") {
		t.Fatalf("expected driver flag, got: %s", joined)
	}
	if !strings.Contains(joined, "--opt com.docker.network.bridge.name=df_mynet") || !strings.Contains(joined, "--opt com.docker.network.bridge.enable_icc=false") {
		t.Fatalf("expected options flags, got: %s", joined)
	}
	if !strings.Contains(joined, "--ipv6") || !strings.Contains(joined, "--subnet 172.18.0.0/16") || !strings.Contains(joined, "--gateway 172.18.0.1") || !strings.Contains(joined, "--ip-range 172.18.0.0/24") || !strings.Contains(joined, "--aux-address host1=172.18.0.2") {
		t.Fatalf("expected ipam flags, got: %s", joined)
	}
	if stub.lastArgs[len(stub.lastArgs)-1] != "mynet" {
		t.Fatalf("network name position mismatch: %#v", stub.lastArgs)
	}
}

func TestRemoveNetwork_Args(t *testing.T) {
	stub := &netExecStub{}
	c := &Client{exec: stub}
	if err := c.RemoveNetwork(context.Background(), "n1"); err != nil {
		t.Fatalf("remove network: %v", err)
	}
	if !containsArgSeq(stub.lastArgs, []string{"network", "rm", "n1"}) {
		t.Fatalf("unexpected args: %#v", stub.lastArgs)
	}
}
