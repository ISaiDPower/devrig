package service

import (
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// State is where a service process is in its life.
type State string

const (
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateStopped  State = "stopped"
	StateFailed   State = "failed"
)

// Status describes a service process at one moment.
type Status struct {
	Name     string         `json:"name"`
	Version  string         `json:"version"`
	State    State          `json:"state"`
	PID      int            `json:"pid,omitempty"`
	Ports    map[string]int `json:"ports,omitempty"`
	Since    time.Time      `json:"since"`
	Restarts int            `json:"restarts"`
	Error    string         `json:"error,omitempty"`
	Log      string         `json:"log"`
}

// Active reports whether the service is running or on its way to or from
// running, as opposed to stopped for good.
func (s Status) Active() bool {
	return s.State != StateStopped && s.State != StateFailed
}

// Timings. Variables so tests can shorten them.
var (
	readyTimeout = 15 * time.Second // how long a start may take
	stopTimeout  = 10 * time.Second // how long a clean stop may take
	stableAfter  = 30 * time.Second // a run this long resets the failure count
	firstBackoff = time.Second      // delay before the first restart, doubling after
	maxBackoff   = 30 * time.Second
)

// maxFailures is how many quick crashes in a row make a service give up.
const maxFailures = 5

// Process runs an Instance and keeps it running until Stop is called.
type Process struct {
	inst *Instance
	log  *os.File

	stop     chan struct{} // closed by Stop
	stopOnce sync.Once
	done     chan struct{} // closed when supervision has ended

	mu       sync.Mutex
	state    State
	pid      int
	since    time.Time
	restarts int
	lastErr  string
}

// processTree controls a started process together with every process it
// starts in turn.
type processTree interface {
	// terminate asks the tree to exit. On Windows, where console programs
	// without a console cannot be asked politely, it kills.
	terminate()
	// kill ends the tree at once.
	kill()
	// close releases the tree once the main process has exited, ending any
	// processes it left behind. terminate and kill do nothing afterwards.
	close()
}

// run is one launch of the service's executable.
type run struct {
	cmd     *exec.Cmd
	tree    processTree
	started time.Time
	exited  chan struct{} // closed once cmd has been waited for
	err     error         // what Wait returned, set before exited closes
}

// Start prepares inst, launches it and waits until it accepts connections on
// its ready port. If that does not happen, Start stops it again and returns
// an error that ends with the last lines of its log.
func Start(inst *Instance) (*Process, error) {
	if err := inst.Prepare(); err != nil {
		return nil, err
	}
	if err := checkPorts(inst.Ports); err != nil {
		return nil, fmt.Errorf("start %s: %w", inst.Name, err)
	}

	log, err := os.OpenFile(inst.LogFile(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}

	p := &Process{
		inst:  inst,
		log:   log,
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
		state: StateStarting,
		since: time.Now(),
	}

	r, err := p.launch()
	if err == nil {
		if err = p.waitReady(r); err != nil {
			r.tree.kill()
			<-r.exited
		}
	}
	if err != nil {
		p.logf("start failed: %v", err)
		log.Close()
		return nil, fmt.Errorf("start %s %s: %w%s", inst.Name, inst.Version, err, logExcerpt(inst.LogFile()))
	}

	p.running(r, false)
	go p.supervise(r)
	return p, nil
}

// Stop stops the process and waits until it has exited.
func (p *Process) Stop() {
	p.stopOnce.Do(func() { close(p.stop) })
	<-p.done
}

// Done is closed once the process is stopped for good, either by Stop or
// because it kept crashing.
func (p *Process) Done() <-chan struct{} {
	return p.done
}

// Status returns the current status.
func (p *Process) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Status{
		Name:     p.inst.Name,
		Version:  p.inst.Version,
		State:    p.state,
		PID:      p.pid,
		Ports:    maps.Clone(p.inst.Ports),
		Since:    p.since,
		Restarts: p.restarts,
		Error:    p.lastErr,
		Log:      p.inst.LogFile(),
	}
}

func (p *Process) setState(state State, errText string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state, p.lastErr, p.since = state, errText, time.Now()
	if state != StateRunning && state != StateStopping {
		p.pid = 0
	}
}

func (p *Process) running(r *run, restarted bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state, p.pid, p.since = StateRunning, r.cmd.Process.Pid, time.Now()
	if restarted {
		p.restarts++
	}
}

// logf adds a line from DevRig itself to the service log.
func (p *Process) logf(format string, args ...any) {
	fmt.Fprintf(p.log, "%s devrig: %s\n", time.Now().Format(time.DateTime), fmt.Sprintf(format, args...))
}

func (p *Process) launch() (*run, error) {
	args, err := p.inst.args(p.inst.Spec.Args)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(p.inst.Exe, args...)
	cmd.Dir = p.inst.State
	cmd.Stdout, cmd.Stderr = p.log, p.log

	tree, err := startTree(cmd)
	if err != nil {
		return nil, err
	}
	p.logf("started %s %s (pid %d)", p.inst.Name, p.inst.Version, cmd.Process.Pid)

	r := &run{cmd: cmd, tree: tree, started: time.Now(), exited: make(chan struct{})}
	go func() {
		r.err = cmd.Wait()
		// Whatever the main process left behind goes with it.
		tree.close()
		close(r.exited)
	}()
	return r, nil
}

var errStopRequested = errors.New("stop requested")

// waitReady waits until the ready port accepts connections.
func (p *Process) waitReady(r *run) error {
	name := p.inst.Spec.Ready
	if name == "" {
		return nil
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(p.inst.Ports[name]))
	deadline := time.Now().Add(readyTimeout)

	for {
		if conn, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("nothing accepted connections on %s within %s", addr, readyTimeout)
		}
		select {
		case <-r.exited:
			return fmt.Errorf("it exited with %s", exitText(r.err))
		case <-p.stop:
			return errStopRequested
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// supervise watches a running process and restarts it when it exits
// without being asked to.
func (p *Process) supervise(r *run) {
	defer close(p.done)
	defer p.log.Close()

	failures := 0
	for {
		select {
		case <-p.stop:
			p.shutdown(r)
			p.setState(StateStopped, "")
			return
		case <-r.exited:
		}

		reason := "exited with " + exitText(r.err)
		p.logf("%s", reason)
		if time.Since(r.started) >= stableAfter {
			failures = 0
		}
		failures++
		if failures >= maxFailures {
			p.logf("giving up after %d failed starts", failures)
			p.setState(StateFailed, reason)
			return
		}

		delay := min(firstBackoff<<(failures-1), maxBackoff)
		p.logf("restarting in %s", delay)
		p.setState(StateStarting, reason)
		select {
		case <-p.stop:
			p.setState(StateStopped, "")
			return
		case <-time.After(delay):
		}

		next, err := p.launch()
		if err != nil {
			p.logf("restart failed: %v", err)
			p.setState(StateFailed, err.Error())
			return
		}
		r = next
		if err := p.waitReady(r); err != nil {
			// Kill it and let the top of the loop count the failure, or
			// handle the stop request.
			if err != errStopRequested {
				p.logf("restart failed: %v", err)
			}
			r.tree.kill()
			continue
		}
		p.running(r, true)
	}
}

// shutdown stops the live process: through the recipe's stop command if it
// has one, otherwise by asking the process tree to terminate. Whatever is
// still running after stopTimeout is killed.
func (p *Process) shutdown(r *run) {
	select {
	case <-r.exited:
		return
	default:
	}
	p.setState(StateStopping, "")
	p.logf("stopping")

	if len(p.inst.Spec.Stop) > 0 {
		if err := p.runStopCommand(); err != nil {
			p.logf("stop command failed: %v", err)
			r.tree.terminate()
		}
	} else {
		r.tree.terminate()
	}

	select {
	case <-r.exited:
	case <-time.After(stopTimeout):
		p.logf("still running after %s, killing it", stopTimeout)
		r.tree.kill()
		<-r.exited
	}
	p.logf("stopped")
}

func (p *Process) runStopCommand() error {
	args, err := p.inst.args(p.inst.Spec.Stop)
	if err != nil {
		return err
	}
	cmd := exec.Command(p.inst.Exe, args...)
	cmd.Dir = p.inst.State
	cmd.Stdout, cmd.Stderr = p.log, p.log
	hideWindow(cmd)

	if err := cmd.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		return err
	case <-time.After(stopTimeout):
		cmd.Process.Kill()
		return fmt.Errorf("no answer within %s", stopTimeout)
	}
}

// checkPorts fails if one of ports is already taken on the loopback
// interface.
func checkPorts(ports map[string]int) error {
	for _, name := range slices.Sorted(maps.Keys(ports)) {
		l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(ports[name])))
		if err != nil {
			return fmt.Errorf("port %d (%s) is already in use", ports[name], name)
		}
		l.Close()
	}
	return nil
}

func exitText(err error) string {
	var exit *exec.ExitError
	switch {
	case err == nil:
		return "exit code 0"
	case errors.As(err, &exit):
		return "exit code " + strconv.Itoa(exit.ExitCode())
	default:
		return err.Error()
	}
}

// logExcerpt returns the last lines of a log, formatted to follow an error.
func logExcerpt(file string) string {
	lines, err := Tail(file, 10)
	if err != nil || len(lines) == 0 {
		return ""
	}
	return "\n\nLast lines of " + file + ":\n  " + strings.Join(lines, "\n  ")
}
