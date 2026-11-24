// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package ssh

import (
	"context"
	stderrors "errors"
	"net"
	"time"

	gossh "golang.org/x/crypto/ssh"

	xkmssession "github.com/jeremyhahn/go-xkms/pkg/ssh/session"
)

// dialSSHClient dials addr over TCP and runs the SSH client handshake.
// Unlike a socket deadline alone, ctx ending at any point before the
// handshake completes closes the connection, so a peer that accepts TCP
// and withholds its banner cannot hold the caller past cancellation.
//
// Errors are go-xkms's exported session error types: a *TimeoutError when
// ctx ended, a *DialError for a TCP failure and a *HandshakeError for an
// SSH failure, each of which unwraps to its cause.
func dialSSHClient(ctx context.Context, host, addr string, cfg *gossh.ClientConfig, timeout time.Duration) (*gossh.Client, error) {
	timedOut := func() error {
		return &xkmssession.TimeoutError{Host: host, Duration: timeout, Err: ctx.Err()}
	}
	d := net.Dialer{Timeout: timeout}
	netConn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		if ctx.Err() != nil {
			return nil, timedOut()
		}
		return nil, &xkmssession.DialError{Host: host, Err: err}
	}
	deadline := time.Now().Add(timeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	if err := netConn.SetDeadline(deadline); err != nil {
		_ = netConn.Close()
		return nil, &xkmssession.DialError{Host: host, Err: err}
	}
	stop := context.AfterFunc(ctx, func() { _ = netConn.Close() })
	sshConn, chans, reqs, hsErr := gossh.NewClientConn(netConn, addr, cfg)
	if hsErr != nil {
		closeErr := netConn.Close()
		if ctx.Err() != nil {
			if stderrors.Is(closeErr, net.ErrClosed) {
				closeErr = nil
			}
			return nil, stderrors.Join(timedOut(), closeErr)
		}
		return nil, &xkmssession.HandshakeError{Host: host, User: cfg.User, Err: hsErr}
	}
	if !stop() {
		// The watcher already fired: ctx ended as the handshake finished.
		_ = sshConn.Close()
		return nil, timedOut()
	}
	if err := netConn.SetDeadline(time.Time{}); err != nil {
		_ = sshConn.Close()
		return nil, &xkmssession.DialError{Host: host, Err: err}
	}
	return gossh.NewClient(sshConn, chans, reqs), nil
}
