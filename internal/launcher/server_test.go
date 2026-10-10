package launcher

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dhwanikher/balloon/internal/api"
	"github.com/dhwanikher/balloon/web"
)

func availablePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	l.Close()
	return port
}

func fetchEditor(url string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 || !strings.Contains(strings.ToLower(string(body)), "balloon") {
		return fmt.Errorf("editor unavailable: HTTP %d", resp.StatusCode)
	}
	return nil
}

func TestStartOpensWorkingEditorAndStopReleasesPort(t *testing.T) {
	var opened []string
	s := New(api.New(web.FS), func(url string) error {
		opened = append(opened, url)
		return fetchEditor(url)
	})
	t.Cleanup(func() { s.Stop() })
	port := availablePort(t)
	if err := s.Start(port); err != nil {
		t.Fatal(err)
	}
	if len(opened) != 1 || s.URL() != "http://127.0.0.1:"+port {
		t.Fatalf("browser opened %v; server URL %q", opened, s.URL())
	}
	if err := s.Start(port); err == nil {
		t.Fatal("double start succeeded")
	}
	if len(opened) != 1 {
		t.Fatal("double start opened browser")
	}
	if err := s.OpenBrowser(); err != nil {
		t.Fatal(err)
	}
	if len(opened) != 2 {
		t.Fatal("browser did not reopen")
	}
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	if s.URL() != "" {
		t.Fatal("URL remains after stop")
	}
	l, err := net.Listen("tcp4", "127.0.0.1:"+port)
	if err != nil {
		t.Fatalf("port not released: %v", err)
	}
	l.Close()
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(port); err != nil {
		t.Fatalf("restart failed: %v", err)
	}
}

func TestStartRejectsInvalidAndOccupiedPortsWithoutOpeningBrowser(t *testing.T) {
	opened := false
	s := New(api.New(web.FS), func(string) error { opened = true; return nil })
	t.Cleanup(func() { s.Stop() })
	for _, port := range []string{"", "abc", "0", "-1", "65536", "8080x", "127.0.0.1:8080"} {
		if err := s.Start(port); err == nil {
			t.Fatalf("accepted invalid port %q", port)
		}
	}
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := s.Start(strconv.Itoa(l.Addr().(*net.TCPAddr).Port)); err == nil {
		t.Fatal("accepted occupied port")
	}
	if opened || s.URL() != "" {
		t.Fatal("failed start opened a browser or kept a URL")
	}
}

func TestBrowserFailureKeepsServerAvailable(t *testing.T) {
	s := New(api.New(web.FS), func(string) error { return fmt.Errorf("no default browser") })
	t.Cleanup(func() { s.Stop() })
	if err := s.Start(availablePort(t)); err == nil || !strings.Contains(err.Error(), "browser") {
		t.Fatalf("missing browser error: %v", err)
	}
	if err := fetchEditor(s.URL()); err != nil {
		t.Fatalf("server stopped on browser failure: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := s.OpenBrowser(); err == nil {
		t.Fatal("opened browser without a server")
	}
}

func TestUnexpectedExitAllowsRestart(t *testing.T) {
	s := New(api.New(web.FS), fetchEditor)
	t.Cleanup(func() { s.Stop() })
	port := availablePort(t)
	if err := s.Start(port); err != nil {
		t.Fatal(err)
	}
	if err := s.PollError(); err != nil {
		t.Fatalf("running server reported an error: %v", err)
	}
	// Simulate a server failure independent of the launcher's Stop action.
	if err := s.server.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for s.PollError() == nil {
		if time.Now().After(deadline) {
			t.Fatal("unexpected exit was not reported")
		}
		time.Sleep(time.Millisecond)
	}
	if s.URL() != "" {
		t.Fatal("failed server kept its URL")
	}
	if err := s.Start(port); err != nil {
		t.Fatalf("restart after unexpected exit failed: %v", err)
	}
}
