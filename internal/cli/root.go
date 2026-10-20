// Package cli implements the rig command line interface.
package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/ISaiDPower/devrig/internal/buildinfo"
)

// Execute runs rig with the process arguments.
func Execute(ctx context.Context) error {
	return newRootCmd(loadApp).ExecuteContext(ctx)
}

func newRootCmd(load loader) *cobra.Command {
	// List commands in the order they are added, which follows how they are
	// used, rather than alphabetically.
	cobra.EnableCommandSorting = false

	root := &cobra.Command{
		Use:   "rig",
		Short: "Native development environments, no containers",
		Long: "rig installs and runs the runtimes and services your projects need\n" +
			"(PHP, Nginx, Mailpit, Node.js and more) natively, without containers.",
		Version:      buildinfo.Read().Version,
		SilenceUsage: true,
	}

	root.AddGroup(
		&cobra.Group{ID: "packages", Title: "Packages:"},
		&cobra.Group{ID: "services", Title: "Services:"},
	)
	for _, c := range []*cobra.Command{newInstallCmd(load), newListCmd(load), newUninstallCmd(load)} {
		c.GroupID = "packages"
		root.AddCommand(c)
	}
	for _, c := range []*cobra.Command{
		newStartCmd(load), newStopCmd(load), newRestartCmd(load),
		newStatusCmd(load), newLogsCmd(load), newAgentCmd(load),
	} {
		c.GroupID = "services"
		root.AddCommand(c)
	}
	root.AddCommand(newVersionCmd())

	return root
}
