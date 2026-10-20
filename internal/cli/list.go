package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ISaiDPower/devrig/internal/registry"
)

func newListCmd(load loader) *cobra.Command {
	var available bool

	cmd := &cobra.Command{
		Use:   "list [package]",
		Short: "List installed releases",
		Long: "List installed releases. With --available, list the releases that can\n" +
			"be installed on this machine instead.",
		Example: "  rig list\n  rig list --available php",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := load()
			if err != nil {
				return err
			}
			if available {
				return a.listAvailable(cmd.OutOrStdout(), args)
			}
			return a.listInstalled(cmd.OutOrStdout(), args)
		},
	}
	cmd.Flags().BoolVarP(&available, "available", "a", false, "list the releases that can be installed")

	return cmd
}

func (a *app) listInstalled(out io.Writer, names []string) error {
	if len(names) == 0 {
		var err error
		if names, err = a.installer.Packages(); err != nil {
			return err
		}
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PACKAGE\tVERSION")
	rows := 0
	for _, name := range names {
		versions, err := a.installer.Installed(name)
		if err != nil {
			return err
		}
		for _, v := range versions {
			fmt.Fprintf(tw, "%s\t%s\n", name, v)
			rows++
		}
	}

	if rows == 0 {
		fmt.Fprintln(out, "Nothing installed yet. See what you can install with: rig list --available")
		return nil
	}
	return tw.Flush()
}

func (a *app) listAvailable(out io.Writer, names []string) error {
	pkgs := a.registry.Packages()
	if len(names) > 0 {
		p, err := a.registry.Package(names[0])
		if err != nil {
			return err
		}
		pkgs = []*registry.Package{p}
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PACKAGE\tVERSION\tSTATUS")
	rows := 0
	for _, p := range pkgs {
		for _, rel := range p.Releases(a.platform) {
			fmt.Fprintf(tw, "%s\t%s", p.Name, rel.Version)
			if a.installer.IsInstalled(p.Name, rel.Version) {
				fmt.Fprint(tw, "\tinstalled")
			}
			fmt.Fprintln(tw)
			rows++
		}
	}

	if rows == 0 {
		fmt.Fprintf(out, "No releases available for %s yet.\n", a.platform)
		return nil
	}
	return tw.Flush()
}
