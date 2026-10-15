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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestContainer registers a container in the runtime state and returns
// its ID.
func createTestContainer(t *testing.T, rt *Runtime, name string) string {
	t.Helper()

	state, err := rt.CreateContainer(context.Background(), &ContainerConfig{
		Name:  name,
		Image: "alpine:latest",
	})
	require.NoError(t, err)
	require.NotNil(t, state)
	return state.ID
}

func TestRuntimeExec_Success(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	id := createTestContainer(t, rt, "exec-ok")
	doubles.engine.execResult = fakeResult{stdout: "hello\n"}

	result, err := rt.Exec(context.Background(), id, ExecOptions{
		Command:    []string{"echo", "hello"},
		Env:        []string{"FOO=bar"},
		WorkingDir: "/work",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "hello\n", string(result.Stdout))

	// The exec process runs the command in the container's environment,
	// extended by the caller's, in the requested directory.
	spec := doubles.engine.lastExec()
	assert.False(t, spec.TTY)
	assert.Equal(t, []string{"echo", "hello"}, spec.Args)
	assert.Contains(t, spec.Env, "FOO=bar")
	assert.Contains(t, spec.Env, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	assert.Equal(t, "/work", spec.Cwd)
}

func TestRuntimeExec_DefaultsToContainerWorkingDirAndUser(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	state, err := rt.CreateContainer(context.Background(), &ContainerConfig{
		Name:    "exec-defaults",
		Image:   "alpine:latest",
		WorkDir: "/srv",
		User:    "1000:1000",
	})
	require.NoError(t, err)

	_, err = rt.Exec(context.Background(), state.ID, ExecOptions{Command: []string{"id"}})
	require.NoError(t, err)

	spec := doubles.engine.lastExec()
	assert.Equal(t, "/srv", spec.Cwd)
	assert.Equal(t, uint32(1000), spec.User.UID)
	assert.Equal(t, uint32(1000), spec.User.GID)
}

func TestRuntimeExec_NonZeroExit(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	id := createTestContainer(t, rt, "exec-nonzero")
	doubles.engine.execResult = fakeResult{stderr: "boom", exitCode: 7}

	result, err := rt.Exec(context.Background(), id, ExecOptions{Command: []string{"false"}})
	require.NoError(t, err)
	assert.Equal(t, 7, result.ExitCode)
	assert.Equal(t, "boom", string(result.Stderr))
}

func TestRuntimeExec_TTY(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	id := createTestContainer(t, rt, "exec-tty")
	doubles.engine.execResult = fakeResult{stdout: "tty-out", exitCode: 3}

	result, err := rt.Exec(context.Background(), id, ExecOptions{Command: []string{"/bin/sh"}, TTY: true})
	require.NoError(t, err)
	assert.Equal(t, "tty-out", string(result.Stdout))
	assert.Equal(t, 3, result.ExitCode)
	assert.True(t, doubles.engine.lastExec().TTY)
}

func TestRuntimeExec_TTYContextCancelled(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	id := createTestContainer(t, rt, "exec-tty-cancel")
	doubles.engine.execBlocks = true

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := rt.Exec(ctx, id, ExecOptions{Command: []string{"/bin/sh"}, TTY: true})
	require.ErrorIs(t, err, context.Canceled)
}

func TestRuntimeExec_MissingContainer(t *testing.T) {
	rt := newTestRuntime(t)

	_, err := rt.Exec(context.Background(), "does-not-exist", ExecOptions{Command: []string{"ls"}})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrContainerNotFound)
}

func TestRuntimeExec_EmptyCommand(t *testing.T) {
	rt := newTestRuntime(t)
	id := createTestContainer(t, rt, "exec-empty")

	_, err := rt.Exec(context.Background(), id, ExecOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrExecCommandRequired)
}

func TestRuntimeExec_RuntimeFailure(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	id := createTestContainer(t, rt, "exec-fail")
	doubles.engine.execErr = errTestBackend

	_, err := rt.Exec(context.Background(), id, ExecOptions{Command: []string{"ls"}})
	require.Error(t, err)
	assert.ErrorIs(t, err, errTestBackend)
}

func TestManagerExec_Delegates(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	id := createTestContainer(t, rt, "mgr-exec")
	doubles.engine.execResult = fakeResult{stdout: "ok"}

	m := &podmanManager{config: DefaultManagerConfig(), logger: rt.logger, runtime: rt}
	m.initialized.Store(true)

	result, err := m.ExecContainer(context.Background(), id, ExecOptions{Command: []string{"echo", "ok"}})
	require.NoError(t, err)
	assert.Equal(t, "ok", string(result.Stdout))
}

func TestManagerExec_NotInitialized(t *testing.T) {
	m := &podmanManager{config: DefaultManagerConfig(), logger: newTestRuntime(t).logger}
	_, err := m.ExecContainer(context.Background(), "cid", ExecOptions{Command: []string{"ls"}})
	require.ErrorIs(t, err, ErrClientNotConnected)
}
