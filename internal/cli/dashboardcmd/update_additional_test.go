package dashboardcmd

import (
	"context"
	"io"
	"reflect"
	"testing"
	"unsafe"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/components"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/data"
	"github.com/gcstr/dockform/internal/dockercli"
)

func newDashboardModel() model {
	m := newModel(context.Background(), nil, testStackSummaries(), "1.0", "id", "manifest.yml", "ctx", "", "")
	m.logsPager.SetSize(40, 10)
	m.width = 120
	m.height = 30
	return m
}

func testStackSummaries() []data.StackSummary {
	return []data.StackSummary{
		{Name: "stack", Services: []data.ServiceSummary{{Service: "svc", ContainerName: "container", Image: "img"}}},
	}
}

type stubExec struct{}

func (stubExec) Run(ctx context.Context, args ...string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	switch args[0] {
	case "version":
		return "25.0.0", nil
	case "context":
		return "\"unix://\"", nil
	case "volume":
		if len(args) > 1 && args[1] == "ls" {
			return "vol\tlocal\n", nil
		}
		if len(args) > 1 && args[1] == "inspect" {
			return `[{"Name":"vol","Driver":"local","Mountpoint":"/data"}]`, nil
		}
	case "network":
		return "net\tbridge\n", nil
	case "ps":
		return `{"ID":"1","Names":"container","Image":"img","Status":"Up","State":"running","Labels":"com.docker.compose.project=stack,com.docker.compose.service=svc"}` + "\n", nil
	}
	return "", nil
}

func (stubExec) RunInDir(ctx context.Context, dir string, args ...string) (string, error) {
	return stubExec{}.Run(ctx, args...)
}

func (stubExec) RunInDirWithEnv(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	return stubExec{}.Run(ctx, args...)
}

func (stubExec) RunWithStdin(ctx context.Context, stdin io.Reader, args ...string) (string, error) {
	return stubExec{}.Run(ctx, args...)
}

func (stubExec) RunWithStdout(ctx context.Context, stdout io.Writer, args ...string) error {
	out, err := stubExec{}.Run(ctx, args...)
	if err != nil {
		return err
	}
	_, _ = stdout.Write([]byte(out))
	return nil
}

func (stubExec) RunDetailed(ctx context.Context, opts dockercli.Options, args ...string) (dockercli.Result, error) {
	out, err := stubExec{}.Run(ctx, args...)
	return dockercli.Result{Stdout: out}, err
}

func setDockerExec(c *dockercli.Client, exec dockercli.Exec) {
	val := reflect.ValueOf(c).Elem().FieldByName("exec")
	reflect.NewAt(val.Type(), unsafe.Pointer(val.UnsafeAddr())).Elem().Set(reflect.ValueOf(exec))
}

func newStubDockerClient() *dockercli.Client {
	client := dockercli.New("default")
	setDockerExec(client, stubExec{})
	return client
}

func TestModelHandlesStatusesAndHelpToggle(t *testing.T) {
	m := newDashboardModel()
	statuses := map[data.Key]data.Status{
		{Stack: "stack", Service: "svc"}: {ContainerName: "container", State: "running", StatusText: "Up"},
	}
	updated, _ := m.Update(statusesMsg{statuses: statuses})
	m = updated.(model)
	it, ok := m.list.SelectedItem().(components.StackItem)
	if !ok || it.StatusKind != "success" {
		t.Fatalf("expected status to update, got %+v", it)
	}
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: '?', Text: "?"}))
	m = updated.(model)
	if !m.help.ShowAll {
		t.Fatalf("expected help to toggle")
	}
}

func TestModelHandlesWindowSizeAndLogs(t *testing.T) {
	m := newDashboardModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(model)
	if m.width != 100 || m.height != 40 {
		t.Fatalf("expected window size to update")
	}
	m.logsPager = components.NewLogsPager()
	m.logsPager.SetSize(20, 5)
	m.logLines = make(chan string, 1)
	m.logLines <- "log-entry"
	updated, _ = m.Update(logsTickMsg{})
	m = updated.(model)
	if len(m.logsBuf) == 0 {
		t.Fatalf("expected logs buffer to flush")
	}
}

func TestModelCommandPaletteAndQuit(t *testing.T) {
	m := newDashboardModel()
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: 'p', Mod: tea.ModCtrl}))
	m = updated.(model)
	if !m.commandPaletteOpen {
		t.Fatalf("expected command palette to open")
	}
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	m = updated.(model)
	if m.commandPaletteOpen {
		t.Fatalf("expected command palette to close")
	}
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: 'q', Text: "q"}))
	m = updated.(model)
	if !m.quitting {
		t.Fatalf("expected quit flag to be set")
	}
}

func TestModelStartLogsAndDockerInfoMsgs(t *testing.T) {
	m := newDashboardModel()
	m.ctx = context.Background()
	m.statusProvider = nil
	updated, cmd := m.Update(startLogsFor{name: "container"})
	m = updated.(model)
	if cmd != nil {
		t.Fatalf("expected nil command when status provider missing")
	}
	updated, _ = m.Update(dockerInfoMsg{host: "unix://", version: "25"})
	m = updated.(model)
	if m.dockerHost != "unix://" || m.engineVersion != "25" {
		t.Fatalf("expected docker info to update")
	}
	updated, _ = m.Update(commandActionResultMsg{action: actionPause, container: "c"})
	m = updated.(model) // ensure branch coverage

	updated, _ = m.Update(statusTickMsg{})
	m = updated.(model)
	updated, _ = m.Update(volumesMsg{volumes: []dockercli.VolumeSummary{{Name: "vol"}}})
	m = updated.(model)
	if len(m.volumes) != 1 {
		t.Fatalf("expected volume summary to be stored")
	}
	updated, _ = m.Update(networksMsg{networks: []dockercli.NetworkSummary{{Name: "net"}}})
	m = updated.(model)
	if len(m.networks) != 1 {
		t.Fatalf("expected network summary to be stored")
	}
	updated, _ = m.Update(logStreamStartedMsg{cancel: func() {}})
	m = updated.(model)
	if m.logCancel == nil {
		t.Fatalf("expected log cancel to be set")
	}
	m.commandPaletteOpen = true
	m.commandList = newCommandPalette()
	updated, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(model)
	if cmd == nil {
		t.Fatalf("expected command when selecting palette entry")
	}
}

func TestModelFetchCommandsWithStubDocker(t *testing.T) {
	m := newDashboardModel()
	m.ctx = context.Background()
	client := newStubDockerClient()
	m.dockerClient = client
	m.statusProvider = data.NewStatusProvider(client, "")
	if cmd := m.fetchDockerInfoCmd(); cmd == nil {
		t.Fatalf("expected docker info command")
	} else if msg := cmd(); msg == nil {
		t.Fatalf("expected info message")
	}
	if cmd := m.fetchVolumesCmd(); cmd == nil {
		t.Fatalf("expected volumes command")
	} else if msg := cmd().(volumesMsg); len(msg.volumes) == 0 {
		t.Fatalf("expected volume data")
	}
	if cmd := m.fetchNetworksCmd(); cmd == nil {
		t.Fatalf("expected networks command")
	} else if msg := cmd().(networksMsg); len(msg.networks) == 0 {
		t.Fatalf("expected network data")
	}
	if cmd := m.refreshStatusesCmd(); cmd == nil {
		t.Fatalf("expected refresh command")
	}
	if cmd := m.Init(); cmd == nil {
		t.Fatalf("expected init command batch")
	}
}
