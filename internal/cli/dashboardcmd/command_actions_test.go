package dashboardcmd

import (
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/dockercli"
)

func TestExecuteCommand_NoContainerSelected(t *testing.T) {
	m := model{}
	cmd := m.executeCommand(actionPause, "   ")
	msg := cmd().(commandActionResultMsg)
	if msg.container != "" {
		t.Fatalf("expected empty container in result, got %q", msg.container)
	}
	if !apperr.IsKind(msg.err, apperr.InvalidInput) {
		t.Fatalf("expected invalid input error, got: %v", msg.err)
	}
}

func TestExecuteCommand_NoDockerClient(t *testing.T) {
	m := model{}
	cmd := m.executeCommand(actionRestart, "web")
	msg := cmd().(commandActionResultMsg)
	if msg.err == nil || msg.err.Error() != "docker client not available" {
		t.Fatalf("expected docker client missing error, got: %v", msg.err)
	}
}

func TestExecuteCommand_UnknownAction(t *testing.T) {
	m := model{dockerClient: &dockercli.Client{}}
	cmd := m.executeCommand(commandAction("bogus"), "web")
	msg := cmd().(commandActionResultMsg)
	if msg.err == nil || msg.err.Error() == "" {
		t.Fatalf("expected error for unknown action, got: %v", msg.err)
	}
	if msg.err.Error() != `unknown command action "bogus"` {
		t.Fatalf("unexpected error message: %v", msg.err)
	}
}
