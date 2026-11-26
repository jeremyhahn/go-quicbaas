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
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntimeAttach_Success(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	id := createTestContainer(t, rt, "attach-ok")
	doubles.engine.execResult = fakeResult{stdout: "line1\nline2\n"}

	session, err := rt.Attach(context.Background(), id, ExecOptions{})
	require.NoError(t, err)
	require.NotNil(t, session)

	var output []byte
	for chunk := range session.Output() {
		assert.Equal(t, "console", chunk.Stream)
		output = append(output, chunk.Data...)
	}
	assert.Equal(t, "line1\nline2\n", string(output))

	code, err := session.Wait()
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	require.NoError(t, session.Close())

	// The default command is a shell, on a terminal.
	spec := doubles.engine.lastExec()
	assert.Equal(t, []string{"/bin/sh"}, spec.Args)
	assert.True(t, spec.TTY)
}

func TestRuntimeAttach_CustomCommand(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	id := createTestContainer(t, rt, "attach-cmd")

	session, err := rt.Attach(context.Background(), id, ExecOptions{Command: []string{"/bin/bash", "-l"}})
	require.NoError(t, err)
	require.NoError(t, session.Close())
	assert.Equal(t, []string{"/bin/bash", "-l"}, doubles.engine.lastExec().Args)
}

func TestRuntimeAttach_CloseKillsRunningSession(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	id := createTestContainer(t, rt, "attach-close")
	doubles.engine.execBlocks = true

	session, err := rt.Attach(context.Background(), id, ExecOptions{})
	require.NoError(t, err)
	require.NoError(t, session.Close())

	// Closing ends the output stream and reaps the killed process.
	for range session.Output() {
		t.Fatal("a silent session produced output")
	}
	code, err := session.Wait()
	require.NoError(t, err)
	assert.Equal(t, 128+int(syscall.SIGKILL), code)
}

func TestRuntimeAttach_MissingContainer(t *testing.T) {
	rt := newTestRuntime(t)
	_, err := rt.Attach(context.Background(), "ghost", ExecOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrContainerNotFound)
}

func TestRuntimeAttach_StartFailure(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	id := createTestContainer(t, rt, "attach-fail")
	doubles.engine.execErr = errTestBackend

	_, err := rt.Attach(context.Background(), id, ExecOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, errTestBackend)
}

func TestManagerAttach_Delegates(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	id := createTestContainer(t, rt, "mgr-attach")
	doubles.engine.execResult = fakeResult{stdout: "hi"}

	m := &podmanManager{config: DefaultManagerConfig(), logger: rt.logger, runtime: rt}
	m.initialized.Store(true)

	session, err := m.AttachContainer(context.Background(), id, ExecOptions{})
	require.NoError(t, err)
	require.NotNil(t, session)
	chunk := <-session.Output()
	assert.Equal(t, "hi", string(chunk.Data))
	require.NoError(t, session.Close())
}

func TestManagerAttach_NotInitialized(t *testing.T) {
	m := &podmanManager{config: DefaultManagerConfig(), logger: newTestRuntime(t).logger}
	_, err := m.AttachContainer(context.Background(), "cid", ExecOptions{})
	require.ErrorIs(t, err, ErrClientNotConnected)
}
