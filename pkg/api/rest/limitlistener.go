//go:build rest

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package rest

import (
	"net"
	"sync"
)

// limitListener wraps a net.Listener to enforce a maximum number of
// concurrent connections using a channel-based semaphore. When the
// limit is reached, Accept blocks until an existing connection closes.
type limitListener struct {
	net.Listener
	sem chan struct{}
}

// newLimitListener returns a listener that accepts at most max concurrent
// connections. max must be greater than zero.
func newLimitListener(inner net.Listener, max int) *limitListener {
	return &limitListener{
		Listener: inner,
		sem:      make(chan struct{}, max),
	}
}

// Accept waits for and returns the next connection, blocking if the
// maximum concurrent connection limit has been reached.
func (l *limitListener) Accept() (net.Conn, error) {
	l.sem <- struct{}{}
	conn, err := l.Listener.Accept()
	if err != nil {
		<-l.sem
		return nil, err
	}
	return &limitConn{Conn: conn, sem: l.sem}, nil
}

// limitConn wraps a net.Conn to release the semaphore slot on close.
type limitConn struct {
	net.Conn
	sem chan struct{}

	// releaseOnce makes the semaphore release idempotent. net/http closes
	// a connection from more than one path (the connection's own serve
	// loop AND Server.Shutdown's closeIdleConns), and net.Conn.Close is
	// documented as safe to call multiple times — but a second receive on
	// an already-released semaphore slot blocks forever, wedging
	// http.Server.Shutdown while it holds the REST server's state under
	// closeIdleConns. One release per connection, exactly once.
	releaseOnce sync.Once
}

// Close closes the connection and releases the semaphore slot (once).
func (c *limitConn) Close() error {
	err := c.Conn.Close()
	c.releaseOnce.Do(func() { <-c.sem })
	return err
}
