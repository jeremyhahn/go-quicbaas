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
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"syscall"
	"time"
)

// ErrExecCommandRequired is returned when an exec request carries no command.
var ErrExecCommandRequired = errors.New("exec command is required")

// ErrExecNotSupported is returned when the backend cannot execute commands
// inside containers.
var ErrExecNotSupported = errors.New("container exec not supported by backend")

// ExecOptions configures a command executed inside a running container. It
// complements the pre-existing ExecConfig by carrying the OCI-oriented
// environment list and TTY flag the runtime needs.
type ExecOptions struct {
	// Command is the argv executed inside the container (argv[0] is the binary).
	Command []string

	// Env is a list of KEY=VALUE strings added to the exec environment.
	Env []string

	// WorkingDir is the working directory for the executed process.
	WorkingDir string

	// TTY allocates a pseudo-terminal for the process. When set on a buffered
	// Exec the combined output is returned on the result's Stdout.
	TTY bool

	// Stdin is fed to a buffered exec's standard input. Nil leaves it
	// closed, which is what a non-interactive command should see. It is
	// ignored when TTY is set.
	Stdin io.Reader
}

// Executor is the capability of running a command inside a running container.
// The embedded podmanManager satisfies it; callers type-assert against it so
// the base PodmanManager interface stays minimal. The buffered result reuses
// the package's ExecResult type.
type Executor interface {
	ExecContainer(ctx context.Context, id string, opts ExecOptions) (*ExecResult, error)
}

// Exec runs a command inside a running container and returns its buffered
// output and exit code. The process runs as the container's user, with the
// container's environment plus opts.Env, in opts.WorkingDir or the
// container's working directory.
func (r *Runtime) Exec(ctx context.Context, id string, opts ExecOptions) (*ExecResult, error) {
	state, err := r.lookupContainer(id)
	if err != nil {
		return nil, err
	}

	if len(opts.Command) == 0 {
		return nil, NewContainerError(id, "exec", ErrExecCommandRequired)
	}

	r.logger.Debug("executing command in container",
		"id", id,
		"name", state.Name,
		"command", opts.Command,
		"tty", opts.TTY,
	)

	var result *ExecResult
	if opts.TTY {
		result, err = r.execTTY(ctx, state, opts)
	} else {
		result, err = r.execBuffered(ctx, state, opts)
	}
	if err != nil {
		return nil, NewContainerError(id, "exec", err)
	}
	return result, nil
}

// execBuffered runs an exec process with its stdout and stderr captured
// separately.
// execKillGrace bounds the wait for a cancelled exec's exit: a child the
// exec left behind can hold its output open past SIGKILL. The result's
// buffers are never read on that path, so the wait may outlive the call.
var execKillGrace = 5 * time.Second

func (r *Runtime) execBuffered(ctx context.Context, state *containerState, opts ExecOptions) (*ExecResult, error) {
	var stdout, stderr bytes.Buffer
	spec := execProcessSpec(state, opts)
	spec.Stdout = &stdout
	spec.Stderr = &stderr

	process, err := r.backends.engine.Exec(state.ID, spec)
	if err != nil {
		return nil, err
	}

	type exit struct {
		code int
		err  error
	}
	done := make(chan exit, 1)
	go func() {
		code, err := process.Wait()
		done <- exit{code: code, err: err}
	}()

	var exited exit
	select {
	case exited = <-done:
	case <-ctx.Done():
		// A cancelled exec is killed and reaped; its exit status is not the
		// command's answer, so the cancellation is what is reported.
		killErr := process.Signal(syscall.SIGKILL)
		select {
		case <-done:
		case <-time.After(execKillGrace):
		}
		return nil, errors.Join(ctx.Err(), killErr)
	}
	exitCode, err := exited.code, exited.err
	if err != nil {
		return nil, err
	}

	return &ExecResult{
		ExitCode: exitCode,
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
	}, nil
}

// execTTY runs an exec process on a pseudo-terminal and returns the combined
// terminal output on Stdout. It is the buffered counterpart of Attach.
func (r *Runtime) execTTY(ctx context.Context, state *containerState, opts ExecOptions) (*ExecResult, error) {
	session, err := r.startConsoleSession(state, opts)
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	for collecting := true; collecting; {
		select {
		case chunk, ok := <-session.Output():
			collecting = ok
			out.Write(chunk.Data)
		case <-ctx.Done():
			return nil, errors.Join(ctx.Err(), session.Close())
		}
	}

	exitCode, err := session.Wait()
	if err != nil {
		return nil, errors.Join(err, session.Close())
	}
	if err := session.Close(); err != nil {
		return nil, err
	}

	return &ExecResult{
		ExitCode: exitCode,
		Stdout:   out.Bytes(),
	}, nil
}

// execProcessSpec builds the process an exec runs: opts layered over the
// container's init process.
func execProcessSpec(state *containerState, opts ExecOptions) processSpec {
	cwd := state.init.Cwd
	if opts.WorkingDir != "" {
		cwd = opts.WorkingDir
	}
	return processSpec{
		Args: opts.Command,
		Env:  slices.Concat(state.init.Env, opts.Env),
		Cwd:  cwd,
		User: state.init.User,
		TTY:  opts.TTY,

		Stdin: opts.Stdin,
	}
}
