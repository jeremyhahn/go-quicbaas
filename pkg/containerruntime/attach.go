// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-trusted-platform.
//
// go-trusted-platform is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package containerruntime

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"syscall"
)

// AttachChunk is one framed slice of container console output produced by an
// attach session.
type AttachChunk struct {
	// Stream identifies the origin of the data: "console" for a TTY session,
	// or "stdout"/"stderr" for a demultiplexed session.
	Stream string

	// Data is the raw output bytes.
	Data []byte
}

// AttachSession is an interactive attach broker over a pseudo-terminal. Output
// delivers console chunks until the session ends and the channel is closed;
// Wait blocks for the process exit code; Close tears the session down and
// reaps the process. It is safe to call Wait and Close from different
// goroutines.
type AttachSession interface {
	// Output returns the channel of console output chunks. The channel is
	// closed when the underlying process exits or the session is closed.
	Output() <-chan AttachChunk

	// Wait blocks until the attached process exits and returns its exit code.
	Wait() (int, error)

	// Close terminates the session, releases the pseudo-terminal, and reaps
	// the process. It is idempotent.
	Close() error
}

// Attacher is the capability of attaching an interactive session to a running
// container. The embedded podmanManager satisfies it.
type Attacher interface {
	AttachContainer(ctx context.Context, id string, opts ExecOptions) (AttachSession, error)
}

// Attach starts an interactive attach session against a running container by
// running a process on a freshly allocated pseudo-terminal inside it. When no
// command is supplied it defaults to a shell. The session's console output is
// delivered on the returned channel and fanned out by the transport layer
// (e.g. the gRPC AttachContainer server stream).
func (r *Runtime) Attach(ctx context.Context, id string, opts ExecOptions) (AttachSession, error) {
	state, err := r.lookupContainer(id)
	if err != nil {
		return nil, err
	}

	if len(opts.Command) == 0 {
		opts.Command = []string{"/bin/sh"}
	}

	r.logger.Debug("attaching to container",
		"id", id,
		"name", state.Name,
		"command", opts.Command,
	)

	session, err := r.startConsoleSession(state, opts)
	if err != nil {
		return nil, NewContainerError(id, "attach", err)
	}
	return session, nil
}

// startConsoleSession runs opts as a TTY process in the container and starts
// fanning its console output onto a session channel.
func (r *Runtime) startConsoleSession(state *containerState, opts ExecOptions) (*consoleSession, error) {
	opts.TTY = true
	process, err := r.backends.engine.Exec(state.ID, execProcessSpec(state, opts))
	if err != nil {
		return nil, err
	}

	s := &consoleSession{
		process: process,
		console: process.Console(),
		output:  make(chan AttachChunk, 16),
		done:    make(chan struct{}),
	}
	go s.readLoop()
	return s, nil
}

// consoleSession is the AttachSession backed by a container process's
// pseudo-terminal master.
type consoleSession struct {
	process containerProcess
	console io.ReadWriteCloser
	output  chan AttachChunk
	done    chan struct{}

	waitOnce  sync.Once
	exitCode  int
	waitErr   error
	closeOnce sync.Once
	closeErr  error
}

// readLoop copies console output onto the session channel until the console
// returns an error (EOF or EIO once the process exits) or the session is
// closed, then closes the channel.
func (s *consoleSession) readLoop() {
	defer close(s.output)
	buf := make([]byte, 4096)
	for {
		n, err := s.console.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			select {
			case s.output <- AttachChunk{Stream: "console", Data: data}:
			case <-s.done:
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// Output returns the session's console output channel.
func (s *consoleSession) Output() <-chan AttachChunk {
	return s.output
}

// Wait blocks until the attached process exits and returns its exit code.
func (s *consoleSession) Wait() (int, error) {
	s.waitOnce.Do(func() {
		s.exitCode, s.waitErr = s.process.Wait()
	})
	return s.exitCode, s.waitErr
}

// Close terminates the session: it closes the console, kills the process if
// it is still running, and reaps it. It is idempotent.
func (s *consoleSession) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		closeErr := s.console.Close()
		killErr := s.process.Signal(syscall.SIGKILL)
		if errors.Is(killErr, os.ErrProcessDone) {
			killErr = nil
		}
		_, waitErr := s.Wait()
		s.closeErr = errors.Join(closeErr, killErr, waitErr)
	})
	return s.closeErr
}
