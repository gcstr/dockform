package dashboardcmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/gcstr/dockform/internal/apperr"
)

type commandAction string

const (
	actionPause   commandAction = "pause"
	actionRestart commandAction = "restart"
	actionStop    commandAction = "stop"
	actionDelete  commandAction = "delete"
)

type commandActionResultMsg struct {
	action    commandAction
	container string
	err       error
}

func (a commandAction) label() string {
	switch a {
	case actionPause:
		return "pause"
	case actionRestart:
		return "restart"
	case actionStop:
		return "stop"
	case actionDelete:
		return "delete"
	default:
		return string(a)
	}
}

func (a commandAction) pastTense() string {
	switch a {
	case actionPause:
		return "paused"
	case actionRestart:
		return "restarted"
	case actionStop:
		return "stopped"
	case actionDelete:
		return "deleted"
	default:
		return a.label()
	}
}

func (m model) executeCommand(action commandAction, container string) tea.Cmd {
	container = strings.TrimSpace(container)
	if container == "" {
		return func() tea.Msg {
			return commandActionResultMsg{
				action:    action,
				container: container,
				err:       apperr.New("dashboard.command", apperr.InvalidInput, "no container selected"),
			}
		}
	}
	docker := m.dockerClient
	if docker == nil {
		return func() tea.Msg {
			return commandActionResultMsg{
				action:    action,
				container: container,
				err:       errors.New("docker client not available"),
			}
		}
	}
	baseCtx := m.ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(baseCtx, 15*time.Second)
		defer cancel()

		var err error
		switch action {
		case actionPause:
			err = docker.PauseContainer(ctx, container)
		case actionRestart:
			err = docker.RestartContainer(ctx, container)
		case actionStop:
			err = docker.StopContainers(ctx, []string{container})
		case actionDelete:
			err = docker.RemoveContainer(ctx, container, true)
		default:
			err = fmt.Errorf("unknown command action %q", action)
		}

		return commandActionResultMsg{
			action:    action,
			container: container,
			err:       err,
		}
	}
}

func formatCommandSuccess(action commandAction, container string) string {
	past := action.pastTense()
	if past == "" {
		past = action.label()
	}
	if past != "" {
		past = strings.ToUpper(past[:1]) + past[1:]
	}
	if container == "" {
		return past
	}
	return fmt.Sprintf("%s %s", past, container)
}

func formatCommandFailure(action commandAction, container string, err error) string {
	if err == nil {
		return fmt.Sprintf("Failed to %s %s", action.label(), container)
	}
	return fmt.Sprintf("Failed to %s %s: %v", action.label(), container, err)
}
