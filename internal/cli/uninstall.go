package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ISaiDPower/devrig/internal/version"
)

func newUninstallCmd(load loader) *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <package>@<version>...",
		Short: "Remove installed releases",
		Long: "Remove installed releases. The version may be a prefix as long as it\n" +
			"matches exactly one installed release.",
		Example: "  rig uninstall php@8.3.35\n  rig uninstall php@8.3",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := load()
			if err != nil {
				return err
			}
			for _, spec := range args {
				if err := a.uninstall(cmd, spec); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func (a *app) uninstall(cmd *cobra.Command, spec string) error {
	name, constraint := parseSpec(spec)
	if constraint == "" {
		return fmt.Errorf("say which release of %s to remove, for example %s@8.3", name, name)
	}
	prefix, err := version.Parse(constraint)
	if err != nil {
		return err
	}

	installed, err := a.installer.Installed(name)
	if err != nil {
		return err
	}
	var matches []version.Version
	for _, v := range installed {
		if v.HasPrefix(prefix) {
			matches = append(matches, v)
		}
	}

	switch len(matches) {
	case 0:
		return fmt.Errorf("%s@%s is not installed", name, constraint)
	case 1:
	default:
		list := make([]string, len(matches))
		for i, v := range matches {
			list[i] = v.String()
		}
		return fmt.Errorf("%s@%s matches several installed releases (%s), pick one", name, constraint, strings.Join(list, ", "))
	}

	if err := a.installer.Uninstall(name, matches[0]); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed %s %s\n", name, matches[0])
	return nil
}
