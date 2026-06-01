package cmd

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattsu2020/kubectl-hpa-status/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newTUICommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Interactive TUI dashboard for HPA status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// TUI requires a terminal.
			out := cmd.OutOrStdout()
			file, ok := out.(*os.File)
			if !ok || !term.IsTerminal(int(file.Fd())) {
				return fmt.Errorf("tui requires an interactive terminal; use 'watch' for non-interactive streaming")
			}

			client, err := opts.newClient()
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %w", err)
			}

			namespace := client.Namespace
			if opts.allNamespaces {
				namespace = ""
			}

			model := tui.NewModel(client.Interface, namespace, tui.Options{
				AllNamespaces: opts.allNamespaces,
				Debug:         opts.debug,
			})

			program, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
			_ = program
			return err
		},
	}
	return cmd
}
