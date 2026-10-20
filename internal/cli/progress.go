package cli

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

// progressBar draws download progress on a single terminal line. On anything
// that is not a terminal it stays silent, so logs and pipes get clean output.
type progressBar struct {
	w       io.Writer
	enabled bool
	drawn   bool
	percent int
}

func newProgressBar(w io.Writer) *progressBar {
	f, ok := w.(*os.File)
	return &progressBar{w: w, enabled: ok && term.IsTerminal(int(f.Fd())), percent: -1}
}

func (p *progressBar) update(done, total int64) {
	if !p.enabled || total <= 0 {
		return
	}
	percent := int(done * 100 / total)
	if percent == p.percent {
		return
	}
	p.percent = percent
	p.drawn = true
	fmt.Fprintf(p.w, "\r  %3d%%  %s / %s ", percent, formatSize(done), formatSize(total))
}

func (p *progressBar) done() {
	if p.drawn {
		fmt.Fprintln(p.w)
	}
}
