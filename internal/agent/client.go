package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/ISaiDPower/devrig/internal/home"
	"github.com/ISaiDPower/devrig/internal/service"
)

// ErrNotRunning is returned when no agent answers.
var ErrNotRunning = errors.New("the DevRig agent is not running")

// Client talks to the agent of one home directory.
type Client struct {
	http *http.Client
}

// NewClient returns a client for the agent serving h. It does not start an
// agent. See Connect for that.
func NewClient(h home.Dir) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			conn, err := dial(ctx, h)
			if err != nil {
				return nil, fmt.Errorf("%w (%v)", ErrNotRunning, err)
			}
			return conn, nil
		},
	}
	// Starting a service may wait for it to become ready, and restarting it
	// for it to stop first.
	return &Client{http: &http.Client{Transport: transport, Timeout: time.Minute}}
}

// Status returns the agent's status and every service it knows about.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var st Status
	err := c.do(ctx, http.MethodGet, "/v1/status", nil, &st)
	return st, err
}

// Start starts an installed release of a service. Starting the release that
// already runs does nothing.
func (c *Client) Start(ctx context.Context, name, version string) (service.Status, error) {
	var st service.Status
	err := c.do(ctx, http.MethodPost, "/v1/services/"+url.PathEscape(name)+"/start", startRequest{Version: version}, &st)
	return st, err
}

// Stop stops a running service.
func (c *Client) Stop(ctx context.Context, name string) (service.Status, error) {
	var st service.Status
	err := c.do(ctx, http.MethodPost, "/v1/services/"+url.PathEscape(name)+"/stop", nil, &st)
	return st, err
}

// Restart stops a service and starts the same release again.
func (c *Client) Restart(ctx context.Context, name string) (service.Status, error) {
	var st service.Status
	err := c.do(ctx, http.MethodPost, "/v1/services/"+url.PathEscape(name)+"/restart", nil, &st)
	return st, err
}

// Shutdown stops every service and then the agent itself.
func (c *Client) Shutdown(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/shutdown", nil, nil)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(data)
	}

	// The host is ignored: every connection goes to the agent's pipe or
	// socket.
	req, err := http.NewRequestWithContext(ctx, method, "http://devrig-agent"+path, payload)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, ErrNotRunning) {
			return ErrNotRunning
		}
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(resp.Body).Decode(&e) != nil || e.Error == "" {
			e.Error = resp.Status
		}
		return errors.New(e.Error)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Connect returns a client for the agent serving h, starting the agent in
// the background first if it is not running. exe is the rig executable,
// which runs the agent with "rig agent run".
func Connect(ctx context.Context, h home.Dir, exe string) (*Client, error) {
	c := NewClient(h)
	_, err := c.Status(ctx)
	if err == nil {
		return c, nil
	}
	if !errors.Is(err, ErrNotRunning) {
		return nil, err
	}

	if err := os.MkdirAll(h.Root, 0o755); err != nil {
		return nil, err
	}
	if err := spawn(h, exe); err != nil {
		return nil, fmt.Errorf("start the DevRig agent: %w", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err = c.Status(ctx); err == nil {
			return c, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("the DevRig agent did not come up, see %s: %w", h.AgentLog(), err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
