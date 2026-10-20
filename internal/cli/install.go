package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newInstallCmd(load loader) *cobra.Command {
	return &cobra.Command{
		Use:   "install <package>[@<version>]...",
		Short: "Install package releases",
		Long: "Install package releases. A version is a prefix: php@8.3 installs the\n" +
			"newest 8.3.x release, php installs the newest release of all.",
		Example: "  rig install php@8.3\n  rig install php@8.4 php@8.5",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := load()
			if err != nil {
				return err
			}
			for _, spec := range args {
				if err := a.install(cmd, spec); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func (a *app) install(cmd *cobra.Command, spec string) error {
	name, constraint := parseSpec(spec)
	pkg, err := a.registry.Package(name)
	if err != nil {
		return err
	}
	rel, asset, err := pkg.Resolve(constraint, a.platform)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if a.installer.IsInstalled(pkg.Name, rel.Version) {
		fmt.Fprintf(out, "%s %s is already installed\n", pkg.Name, rel.Version)
		return nil
	}

	fmt.Fprintf(out, "Installing %s %s (%s)\n", pkg.Name, rel.Version, formatSize(asset.Size))

	bar := newProgressBar(cmd.ErrOrStderr())
	in := *a.installer
	in.Progress = bar.update
	dir, err := in.Install(cmd.Context(), pkg, rel, asset)
	bar.done()
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Installed %s %s in %s\n", pkg.Name, rel.Version, dir)
	return nil
}
