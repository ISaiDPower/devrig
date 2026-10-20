package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ISaiDPower/devrig/internal/buildinfo"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := buildinfo.Read()
			out := cmd.OutOrStdout()

			fmt.Fprintf(out, "rig %s\n", info.Version)
			if info.Commit != "" {
				commit := info.Commit
				if len(commit) > 12 {
					commit = commit[:12]
				}
				if info.Modified {
					commit += " (modified)"
				}
				fmt.Fprintf(out, "commit: %s\n", commit)
			}
			fmt.Fprintf(out, "go: %s %s\n", info.GoVersion, info.Platform)

			return nil
		},
	}
}
