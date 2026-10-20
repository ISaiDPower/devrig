// Package cli implements the rig command line interface.
package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/ISaiDPower/devrig/internal/buildinfo"
)

// Execute runs rig with the process arguments.
func Execute(ctx context.Context) error {
	return newRootCmd().ExecuteContext(ctx)
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "rig",
		Short: "Native development environments, no containers",
		Long: "rig installs and runs the runtimes and services your projects need\n" +
			"(PHP, Nginx, Mailpit, Node.js and more) natively, without containers.",
		Version:      buildinfo.Read().Version,
		SilenceUsage: true,
	}

	root.AddCommand(newVersionCmd())

	return root
}
