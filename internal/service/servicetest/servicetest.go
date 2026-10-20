// Package servicetest turns a test binary into a fake service, so tests can
// run real processes without depending on real services.
//
// Call Main first thing in TestMain. When the binary is started with the
// environment variable Env set, it then acts as the service selected by its
// first argument instead of running tests:
//
//	listen <port> <dir>   accept connections until <dir>/stop exists
//	crash                 print a line and exit with code 3
//	sleep                 run for a minute without listening
//	stop <dir>            create <dir>/stop, asking "listen" to exit
package servicetest

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Env makes a test binary act as a fake service.
const Env = "DEVRIG_TEST_SERVICE"

// Main runs the fake service and exits if Env is set, and returns otherwise.
func Main() {
	if os.Getenv(Env) != "" {
		os.Exit(run(os.Args[1:]))
	}
}

// Enable makes processes started by the test act as fake services.
func Enable(t *testing.T) {
	t.Setenv(Env, "1")
}

// Install copies the test binary to dir as the executable name (with .exe on
// Windows), enables fake services and returns the copy's path.
func Install(t *testing.T, dir, name string) string {
	t.Helper()
	Enable(t)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dst := filepath.Join(dir, name)

	src, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, src); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return dst
}

// FreePort returns a loopback TCP port nothing listens on right now.
func FreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func run(args []string) int {
	if len(args) == 0 {
		return 2
	}
	switch args[0] {
	case "listen":
		return listen(args[1], args[2])
	case "crash":
		fmt.Println("fake service crashing")
		return 3
	case "sleep":
		time.Sleep(time.Minute)
		return 0
	case "stop":
		if err := os.WriteFile(filepath.Join(args[1], "stop"), nil, 0o644); err != nil {
			return 1
		}
		return 0
	}
	return 2
}

func listen(port, dir string) int {
	l, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		fmt.Println(err)
		return 1
	}
	fmt.Println("fake service listening")

	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	for {
		if _, err := os.Stat(filepath.Join(dir, "stop")); err == nil {
			os.Remove(filepath.Join(dir, "stop"))
			fmt.Println("fake service stopping cleanly")
			return 0
		}
		time.Sleep(20 * time.Millisecond)
	}
}
