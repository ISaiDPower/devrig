package cli

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ISaiDPower/devrig/internal/agent"
	"github.com/ISaiDPower/devrig/internal/registry"
	"github.com/ISaiDPower/devrig/internal/service"
	"github.com/ISaiDPower/devrig/internal/version"
)

func newStartCmd(load loader) *cobra.Command {
	return &cobra.Command{
		Use:   "start <service>[@<version>]...",
		Short: "Start services in the background",
		Long: "Start services in the background. A service that is not installed yet is\n" +
			"installed first. Without a version, the newest installed release runs.",
		Example: "  rig start mailpit\n  rig start nginx@1.30 mailpit",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := load()
			if err != nil {
				return err
			}
			for _, spec := range args {
				if err := a.start(cmd, spec); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func (a *app) start(cmd *cobra.Command, spec string) error {
	name, constraint := parseSpec(spec)
	pkg, err := a.registry.Package(name)
	if err != nil {
		return err
	}
	if pkg.Service == nil {
		return fmt.Errorf("%s is not a service", name)
	}

	v, err := a.pickRelease(cmd, pkg, constraint)
	if err != nil {
		return err
	}
	client, err := a.connect(cmd.Context())
	if err != nil {
		return err
	}
	st, err := client.Start(cmd.Context(), name, v.String())
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s %s is running: %s\n", st.Name, st.Version, formatPorts(st.Ports))
	return nil
}

// pickRelease returns the release of pkg to use for constraint: the newest
// installed one that matches, or else the newest available one, which it
// installs first.
func (a *app) pickRelease(cmd *cobra.Command, pkg *registry.Package, constraint string) (version.Version, error) {
	var prefix version.Version
	if constraint != "" && constraint != "latest" {
		p, err := version.Parse(constraint)
		if err != nil {
			return version.Version{}, err
		}
		prefix = p
	}

	installed, err := a.installer.Installed(pkg.Name)
	if err != nil {
		return version.Version{}, err
	}
	for _, v := range installed {
		if v.HasPrefix(prefix) {
			return v, nil
		}
	}

	rel, _, err := pkg.Resolve(constraint, a.platform)
	if err != nil {
		return version.Version{}, err
	}
	if err := a.install(cmd, pkg.Name+"@"+rel.Version.String()); err != nil {
		return version.Version{}, err
	}
	return rel.Version, nil
}

func newStopCmd(load loader) *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:     "stop [<service>...]",
		Short:   "Stop running services",
		Example: "  rig stop mailpit\n  rig stop --all",
		RunE: func(cmd *cobra.Command, args []string) error {
			if all == (len(args) > 0) {
				return errors.New("name the services to stop, or use --all")
			}
			a, err := load()
			if err != nil {
				return err
			}

			client := a.agent()
			if all {
				st, err := client.Status(cmd.Context())
				if errors.Is(err, agent.ErrNotRunning) {
					fmt.Fprintln(cmd.OutOrStdout(), "Nothing is running.")
					return nil
				}
				if err != nil {
					return err
				}
				for _, s := range st.Services {
					if s.Active() {
						args = append(args, s.Name)
					}
				}
				if len(args) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "Nothing is running.")
					return nil
				}
			}

			for _, name := range args {
				st, err := client.Stop(cmd.Context(), name)
				if errors.Is(err, agent.ErrNotRunning) {
					return fmt.Errorf("%s is not running", name)
				}
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Stopped %s %s\n", st.Name, st.Version)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "stop every running service")

	return cmd
}

func newRestartCmd(load loader) *cobra.Command {
	return &cobra.Command{
		Use:   "restart <service>...",
		Short: "Restart running services",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := load()
			if err != nil {
				return err
			}
			client := a.agent()
			for _, name := range args {
				st, err := client.Restart(cmd.Context(), name)
				if errors.Is(err, agent.ErrNotRunning) {
					return fmt.Errorf("%s is not running", name)
				}
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s is running: %s\n", st.Name, st.Version, formatPorts(st.Ports))
			}
			return nil
		},
	}
}

func newStatusCmd(load loader) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the services the agent runs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := load()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()

			st, err := a.agent().Status(cmd.Context())
			if errors.Is(err, agent.ErrNotRunning) || (err == nil && len(st.Services) == 0) {
				fmt.Fprintln(out, "No services are running. Start one with: rig start mailpit")
				return nil
			}
			if err != nil {
				return err
			}

			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "SERVICE\tVERSION\tSTATE\tPID\tPORTS")
			for _, s := range st.Services {
				pid, ports := "", ""
				if s.Active() {
					pid, ports = strconv.Itoa(s.PID), formatPorts(s.Ports)
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", s.Name, s.Version, describeState(s), pid, ports)
			}
			if err := tw.Flush(); err != nil {
				return err
			}

			for _, s := range st.Services {
				if s.State == service.StateFailed {
					fmt.Fprintf(out, "\n%s failed: %s\nSee what happened with: rig logs %s\n", s.Name, s.Error, s.Name)
				}
			}
			return nil
		},
	}
}

// describeState says how a service is doing, such as "running for 5m".
func describeState(s service.Status) string {
	text := string(s.State)
	if s.State == service.StateRunning {
		text += " for " + formatUptime(time.Since(s.Since))
	}
	if s.Restarts == 1 {
		text += ", restarted once"
	} else if s.Restarts > 1 {
		text += fmt.Sprintf(", restarted %d times", s.Restarts)
	}
	return text
}

func formatUptime(d time.Duration) string {
	switch {
	case d < time.Minute:
		return d.Round(time.Second).String()
	case d < time.Hour:
		return d.Round(time.Minute).String()
	default:
		return d.Round(time.Hour).String()
	}
}

// formatPorts lists ports by name, with web ports as URLs.
func formatPorts(ports map[string]int) string {
	names := slices.Sorted(maps.Keys(ports))
	parts := make([]string, len(names))
	for i, name := range names {
		addr := "127.0.0.1:" + strconv.Itoa(ports[name])
		if name == "http" {
			parts[i] = "http://" + addr
		} else {
			parts[i] = name + " " + addr
		}
	}
	return strings.Join(parts, ", ")
}
