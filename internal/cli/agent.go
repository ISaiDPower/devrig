package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/ISaiDPower/devrig/internal/agent"
	"github.com/ISaiDPower/devrig/internal/buildinfo"
)

func newAgentCmd(load loader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage the background agent that runs services",
		Long: "The agent is a background process that runs services and keeps them\n" +
			"running. rig starts it by itself when a service starts.",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "run",
		Short: "Run the agent in the foreground",
		Long: "Run the agent in the foreground until Ctrl+C. rig normally starts it in\n" +
			"the background by itself, so this is mostly useful for debugging.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := load()
			if err != nil {
				return err
			}

			logf := func(format string, args ...any) {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s %s\n", time.Now().Format(time.DateTime), fmt.Sprintf(format, args...))
			}
			logf("agent %s starting (pid %d, home %s)", buildinfo.Read().Version, os.Getpid(), a.home.Root)
			err = agent.NewServer(a.home, a.registry).Serve(cmd.Context())
			if err != nil {
				logf("agent failed: %v", err)
				return err
			}
			logf("agent stopped")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "stop",
		Short: "Stop every service and the agent",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := load()
			if err != nil {
				return err
			}
			err = a.agent().Shutdown(cmd.Context())
			if errors.Is(err, agent.ErrNotRunning) {
				fmt.Fprintln(cmd.OutOrStdout(), "The agent is not running.")
				return nil
			}
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Stopped every service and the agent.")
			return nil
		},
	})

	return cmd
}
