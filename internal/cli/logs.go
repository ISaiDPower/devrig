package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/ISaiDPower/devrig/internal/service"
)

func newLogsCmd(load loader) *cobra.Command {
	var (
		lines  int
		follow bool
	)

	cmd := &cobra.Command{
		Use:     "logs <service>",
		Short:   "Show a service's log",
		Example: "  rig logs nginx\n  rig logs -f mailpit",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := load()
			if err != nil {
				return err
			}
			file, err := a.logFile(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			tail, err := service.Tail(file, lines)
			if err != nil {
				return err
			}
			for _, line := range tail {
				fmt.Fprintln(out, line)
			}
			if follow {
				return followFile(cmd.Context(), out, file)
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&lines, "lines", "n", 50, "how many lines to show")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep printing new lines until Ctrl+C")

	return cmd
}

// logFile finds the log of a service: the one of the release the agent runs,
// or else the most recently written one.
func (a *app) logFile(ctx context.Context, name string) (string, error) {
	if st, err := a.agent().Status(ctx); err == nil {
		for _, s := range st.Services {
			if s.Name == name {
				return s.Log, nil
			}
		}
	}

	files, _ := filepath.Glob(filepath.Join(a.home.Service(name, "*"), name+".log"))
	var newest string
	var newestTime time.Time
	for _, f := range files {
		if info, err := os.Stat(f); err == nil && info.ModTime().After(newestTime) {
			newest, newestTime = f, info.ModTime()
		}
	}
	if newest == "" {
		return "", fmt.Errorf("%s has no log yet", name)
	}
	return newest, nil
}

// followFile prints what is appended to file until ctx is canceled.
func followFile(ctx context.Context, out io.Writer, file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	offset, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}

	buf := make([]byte, 32<<10)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(250 * time.Millisecond):
		}

		info, err := f.Stat()
		if err != nil {
			return err
		}
		if info.Size() < offset {
			offset = 0 // the log was truncated
		}
		for {
			n, err := f.ReadAt(buf, offset)
			out.Write(buf[:n])
			offset += int64(n)
			if errors.Is(err, io.EOF) || n == 0 {
				break
			}
			if err != nil {
				return err
			}
		}
	}
}
