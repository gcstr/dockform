package dashboardcmd

import (
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/gcstr/dockform/internal/cli/common"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/data"
	"github.com/spf13/cobra"
)

// New creates the `dockform dashboard` command.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Launch the Dockform dashboard (fullscreen TUI)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cliCtx, err := common.SetupCLIContext(cmd)
			if err != nil {
				return err
			}

			loader, err := data.NewLoader(cliCtx.Config, cliCtx.Docker)
			if err != nil {
				return err
			}
			stacks, err := loader.StackSummaries(cliCtx.Ctx)
			if err != nil {
				return err
			}

			m := newModel(stacks)
			m.statusProvider = data.NewStatusProvider(cliCtx.Docker, cliCtx.Config.Docker.Identifier)

			p := tea.NewProgram(m, tea.WithAltScreen())
			_, err = p.Run()
			return err
		},
	}
	return cmd
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
