// Package agent is the per user background process that runs services, and
// the client that the CLI and the desktop app use to talk to it.
//
// The agent serves a small JSON API over a named pipe on Windows and a Unix
// socket elsewhere, so only the user who started it can reach it.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ISaiDPower/devrig/internal/buildinfo"
	"github.com/ISaiDPower/devrig/internal/config"
	"github.com/ISaiDPower/devrig/internal/home"
	"github.com/ISaiDPower/devrig/internal/registry"
	"github.com/ISaiDPower/devrig/internal/service"
	"github.com/ISaiDPower/devrig/internal/version"
)

// ErrAlreadyRunning is returned by Serve when another agent already serves
// the same home directory.
var ErrAlreadyRunning = errors.New("the DevRig agent is already running")

// Status is the agent's answer to a status request.
type Status struct {
	Version  string           `json:"version"`
	PID      int              `json:"pid"`
	Home     string           `json:"home"`
	Services []service.Status `json:"services"`
}

// Server runs services on behalf of its clients.
type Server struct {
	home     home.Dir
	registry *registry.Registry

	mu    sync.Mutex
	procs map[string]*service.Process // by service name, including stopped ones
	ops   map[string]*sync.Mutex      // serialises start and stop per service

	quit     chan struct{}
	quitOnce sync.Once
}

// NewServer returns an agent for the home directory h.
func NewServer(h home.Dir, reg *registry.Registry) *Server {
	return &Server{
		home:     h,
		registry: reg,
		procs:    make(map[string]*service.Process),
		ops:      make(map[string]*sync.Mutex),
		quit:     make(chan struct{}),
	}
}

// Serve answers clients until ctx is canceled or a client asks the agent to
// shut down. Every service is stopped before it returns.
func (s *Server) Serve(ctx context.Context) error {
	if err := os.MkdirAll(s.home.Root, 0o755); err != nil {
		return err
	}
	if _, err := NewClient(s.home).Status(ctx); err == nil {
		return ErrAlreadyRunning
	}
	l, err := listen(s.home)
	if err != nil {
		return err
	}

	srv := &http.Server{Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(l) }()

	select {
	case <-ctx.Done():
	case <-s.quit:
	case err := <-served:
		s.stopAll()
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
	s.stopAll()
	return nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", s.handleStatus)
	mux.HandleFunc("POST /v1/services/{name}/start", s.handleStart)
	mux.HandleFunc("POST /v1/services/{name}/stop", s.handleStop)
	mux.HandleFunc("POST /v1/services/{name}/restart", s.handleRestart)
	mux.HandleFunc("POST /v1/shutdown", s.handleShutdown)
	return mux
}

// apiError is an error with the HTTP status to answer it with.
type apiError struct {
	status int
	msg    string
}

func (e *apiError) Error() string { return e.msg }

func errorf(status int, format string, args ...any) error {
	return &apiError{status: status, msg: fmt.Sprintf(format, args...)}
}

func reply(w http.ResponseWriter, v any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		status := http.StatusInternalServerError
		var apiErr *apiError
		if errors.As(err, &apiErr) {
			status = apiErr.status
		}
		w.WriteHeader(status)
		v = map[string]string{"error": err.Error()}
	}
	json.NewEncoder(w).Encode(v)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	reply(w, s.status(), nil)
}

type startRequest struct {
	Version string `json:"version"`
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	var req startRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		reply(w, nil, errorf(http.StatusBadRequest, "invalid request: %v", err))
		return
	}
	st, err := s.start(r.PathValue("name"), req.Version)
	reply(w, st, err)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	st, err := s.stop(r.PathValue("name"))
	reply(w, st, err)
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	st, err := s.restart(r.PathValue("name"))
	reply(w, st, err)
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	s.stopAll()
	reply(w, s.status(), nil)
	s.quitOnce.Do(func() { close(s.quit) })
}

func (s *Server) status() Status {
	s.mu.Lock()
	procs := make([]*service.Process, 0, len(s.procs))
	for _, p := range s.procs {
		procs = append(procs, p)
	}
	s.mu.Unlock()

	services := make([]service.Status, len(procs))
	for i, p := range procs {
		services[i] = p.Status()
	}
	slices.SortFunc(services, func(a, b service.Status) int { return strings.Compare(a.Name, b.Name) })

	return Status{
		Version:  buildinfo.Read().Version,
		PID:      os.Getpid(),
		Home:     s.home.Root,
		Services: services,
	}
}

// lock serialises operations on one service and returns the unlock function.
func (s *Server) lock(name string) func() {
	s.mu.Lock()
	m, ok := s.ops[name]
	if !ok {
		m = new(sync.Mutex)
		s.ops[name] = m
	}
	s.mu.Unlock()

	m.Lock()
	return m.Unlock
}

func (s *Server) proc(name string) *service.Process {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.procs[name]
}

func (s *Server) start(name, ver string) (service.Status, error) {
	defer s.lock(name)()
	return s.startLocked(name, ver)
}

func (s *Server) startLocked(name, ver string) (service.Status, error) {
	pkg, err := s.registry.Package(name)
	if err != nil {
		return service.Status{}, errorf(http.StatusNotFound, "%v", err)
	}
	if pkg.Service == nil {
		return service.Status{}, errorf(http.StatusBadRequest, "%s is not a service", name)
	}
	v, err := version.Parse(ver)
	if err != nil {
		return service.Status{}, errorf(http.StatusBadRequest, "%v", err)
	}
	if _, err := os.Stat(s.home.Release(name, v.String())); errors.Is(err, fs.ErrNotExist) {
		return service.Status{}, errorf(http.StatusNotFound, "%s %s is not installed", name, v)
	}

	if p := s.proc(name); p != nil {
		if st := p.Status(); st.Active() {
			if st.Version == v.String() {
				return st, nil
			}
			return st, errorf(http.StatusConflict, "%s %s is already running, stop it first", name, st.Version)
		}
	}

	global, err := config.LoadGlobal(s.home.Config())
	if err != nil {
		return service.Status{}, err
	}
	inst, err := service.NewInstance(s.home, pkg, v, global.Services[name].Ports)
	if err != nil {
		return service.Status{}, err
	}
	p, err := service.Start(inst)
	if err != nil {
		return service.Status{}, err
	}

	s.mu.Lock()
	s.procs[name] = p
	s.mu.Unlock()
	return p.Status(), nil
}

func (s *Server) stop(name string) (service.Status, error) {
	defer s.lock(name)()

	p := s.proc(name)
	if p == nil || !p.Status().Active() {
		return service.Status{}, errorf(http.StatusNotFound, "%s is not running", name)
	}
	p.Stop()
	return p.Status(), nil
}

func (s *Server) restart(name string) (service.Status, error) {
	defer s.lock(name)()

	p := s.proc(name)
	if p == nil {
		return service.Status{}, errorf(http.StatusNotFound, "%s is not running", name)
	}
	p.Stop()
	return s.startLocked(name, p.Status().Version)
}

// stopAll stops every service at once and waits for all of them.
func (s *Server) stopAll() {
	s.mu.Lock()
	procs := make([]*service.Process, 0, len(s.procs))
	for _, p := range s.procs {
		procs = append(procs, p)
	}
	s.mu.Unlock()

	var wg sync.WaitGroup
	for _, p := range procs {
		wg.Go(p.Stop)
	}
	wg.Wait()
}
