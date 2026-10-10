// Package launcher owns the local server used by the Windows desktop launcher.
package launcher

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Controller is called from the UI thread; it does not launch a second process.
type Controller struct {
	handler http.Handler
	open    func(string) error
	server  *http.Server
	done    chan error
	url     string
}

func New(handler http.Handler, openBrowser func(string) error) *Controller {
	return &Controller{handler: handler, open: openBrowser}
}

func (c *Controller) URL() string { return c.url }

// Start binds before opening the browser. A browser error leaves the server
// running so its URL can still be opened manually or retried from the launcher.
func (c *Controller) Start(port string) error {
	if c.server != nil {
		return fmt.Errorf("the server is already running")
	}
	n, err := strconv.Atoi(strings.TrimSpace(port))
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("enter a port between 1 and 65535")
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(n))
	listener, err := net.Listen("tcp4", addr)
	if err != nil {
		return fmt.Errorf("cannot start on port %d; choose another port: %w", n, err)
	}
	server := &http.Server{Handler: c.handler, ReadHeaderTimeout: 10 * time.Second}
	done := make(chan error, 1)
	c.server, c.done, c.url = server, done, "http://"+addr
	go func() { done <- server.Serve(listener) }()
	return c.OpenBrowser()
}

func (c *Controller) OpenBrowser() error {
	if c.server == nil {
		return fmt.Errorf("start the server first")
	}
	if err := c.open(c.url); err != nil {
		return fmt.Errorf("server is running, but the browser could not open; open %s manually: %w", c.url, err)
	}
	return nil
}

func (c *Controller) Stop() error {
	if c.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := c.server.Shutdown(ctx)
	if err != nil {
		c.server.Close()
	}
	<-c.done
	c.server, c.done, c.url = nil, nil, ""
	return err
}

// PollError lets the UI report an unexpected server exit without blocking its
// message loop. The serving goroutine never updates UI or controller state.
func (c *Controller) PollError() error {
	select {
	case err := <-c.done:
		c.server.Close()
		c.server, c.done, c.url = nil, nil, ""
		return fmt.Errorf("server stopped unexpectedly: %v", err)
	default:
		return nil
	}
}
