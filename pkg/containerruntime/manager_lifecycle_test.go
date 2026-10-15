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

// newTestManager returns an initialized manager over a runtime with doubles.
func newTestManager(t *testing.T) (*podmanManager, *testBackends, *testAttestor) {
	t.Helper()
	rt, doubles := newTestRuntimeWithBackends(t)
	attestor := &testAttestor{}
	m := &podmanManager{config: DefaultManagerConfig(), logger: rt.logger, runtime: rt, attestor: attestor}
	m.initialized.Store(true)
	return m, doubles, attestor
}

func TestManager_ContainerLifecycle(t *testing.T) {
	m, doubles, attestor := newTestManager(t)
	doubles.engine.initResult = &fakeResult{stdout: "done\n", exitCode: 5}
	ctx := context.Background()

	created, err := m.CreateContainer(ctx, &ContainerConfig{Name: "web", Image: "alpine:latest"})
	require.NoError(t, err)
	assert.Equal(t, ContainerStateCreated, created.State)

	require.NoError(t, m.StartContainer(ctx, "web"))
	assert.True(t, attestor.startCalled)

	code, err := m.WaitContainer(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, 5, code)

	inspected, err := m.InspectContainer(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, ContainerStateExited, inspected.State)
	assert.Equal(t, 5, inspected.ExitCode)

	logs, err := m.GetContainerLogs(ctx, created.ID, false, nil)
	require.NoError(t, err)
	assert.Equal(t, "done\n", string(logs.Stdout))

	listed, err := m.ListContainers(ctx, true)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, created.ID, listed[0].ID)

	require.NoError(t, m.RemoveContainer(ctx, "web", false))
	assert.Zero(t, m.containerCount.Load())
	_, ok := m.cachedContainer(created.ID)
	assert.False(t, ok)
}

func TestManager_StopRecordsTheStopAndAttests(t *testing.T) {
	m, _, attestor := newTestManager(t)
	ctx := context.Background()
	created, err := m.CreateContainer(ctx, &ContainerConfig{Name: "stop", Image: "alpine:latest"})
	require.NoError(t, err)
	require.NoError(t, m.StartContainer(ctx, created.ID))

	timeout := 5
	require.NoError(t, m.StopContainer(ctx, created.ID, &timeout))

	assert.True(t, attestor.stopCalled)
	cached, ok := m.cachedContainer(created.ID)
	require.True(t, ok)
	assert.Equal(t, ContainerStateStopped, cached.State)
	assert.NotNil(t, cached.FinishedAt)
}

func TestManager_CreateFailures(t *testing.T) {
	m, doubles, _ := newTestManager(t)
	ctx := context.Background()

	_, err := m.CreateContainer(ctx, nil)
	require.ErrorIs(t, err, ErrInvalidContainerConfig)

	_, err = m.CreateContainer(ctx, &ContainerConfig{Name: "no-image"})
	require.Error(t, err)

	doubles.engine.createErr = errTestBackend
	_, err = m.CreateContainer(ctx, &ContainerConfig{Name: "refused", Image: "alpine:latest"})
	require.ErrorIs(t, err, errTestBackend)
	assert.Zero(t, m.containerCount.Load())
}

func TestManager_CreateHonoursTheContainerLimit(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.config.MaxContainers = 1
	ctx := context.Background()

	_, err := m.CreateContainer(ctx, &ContainerConfig{Name: "first", Image: "alpine:latest"})
	require.NoError(t, err)
	_, err = m.CreateContainer(ctx, &ContainerConfig{Name: "second", Image: "alpine:latest"})
	require.ErrorIs(t, err, ErrContainerAlreadyExists)
}

func TestManager_OperationsOnAMissingContainer(t *testing.T) {
	m, _, attestor := newTestManager(t)
	ctx := context.Background()

	for _, id := range []string{"", "ghost"} {
		require.ErrorIs(t, m.StartContainer(ctx, id), ErrContainerNotFound, "start %q", id)
		require.ErrorIs(t, m.StopContainer(ctx, id, nil), ErrContainerNotFound, "stop %q", id)
		require.ErrorIs(t, m.RemoveContainer(ctx, id, true), ErrContainerNotFound, "remove %q", id)
		_, err := m.InspectContainer(ctx, id)
		require.ErrorIs(t, err, ErrContainerNotFound, "inspect %q", id)
		_, err = m.WaitContainer(ctx, id)
		require.ErrorIs(t, err, ErrContainerNotFound, "wait %q", id)
	}
	assert.False(t, attestor.startCalled)
	assert.False(t, attestor.stopCalled)
}

func TestManager_RemoveOfARunningContainer(t *testing.T) {
	m, _, _ := newTestManager(t)
	ctx := context.Background()
	created, err := m.CreateContainer(ctx, &ContainerConfig{Name: "busy", Image: "alpine:latest"})
	require.NoError(t, err)
	require.NoError(t, m.StartContainer(ctx, created.ID))

	require.ErrorIs(t, m.RemoveContainer(ctx, created.ID, false), ErrContainerRunning)
	assert.Equal(t, int64(1), m.containerCount.Load())
}

func TestManager_ResourceStats(t *testing.T) {
	m, _, _ := newTestManager(t)
	ctx := context.Background()
	created, err := m.CreateContainer(ctx, &ContainerConfig{Name: "stats", Image: "alpine:latest"})
	require.NoError(t, err)

	// The manager delegates to the runtime, which samples only running
	// containers.
	_, err = m.ResourceStats(ctx, created.ID)
	require.ErrorIs(t, err, ErrContainerNotRunning)

	_, err = m.ResourceStats(ctx, "")
	require.ErrorIs(t, err, ErrContainerNotFound)
	_, err = m.ResourceStats(ctx, "ghost")
	require.ErrorIs(t, err, ErrContainerNotFound)

	m.initialized.Store(false)
	_, err = m.ResourceStats(ctx, created.ID)
	require.ErrorIs(t, err, ErrClientNotConnected)
}

// failingAttestor is a ContainerAttestor whose start and stop hooks fail.
type failingAttestor struct {
	testAttestor
}

func (a *failingAttestor) OnContainerStart(ctx context.Context, container *Container) error {
	return errTestBackend
}

func (a *failingAttestor) OnContainerStop(ctx context.Context, container *Container) error {
	return errTestBackend
}

func TestManager_AttestationFailureDoesNotFailTheLifecycle(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.attestor = &failingAttestor{}
	ctx := context.Background()
	created, err := m.CreateContainer(ctx, &ContainerConfig{Name: "attest", Image: "alpine:latest"})
	require.NoError(t, err)

	require.NoError(t, m.StartContainer(ctx, created.ID))
	require.NoError(t, m.StopContainer(ctx, created.ID, nil))
}

func TestManager_PullImage(t *testing.T) {
	m, doubles, _ := newTestManager(t)
	ctx := context.Background()

	require.NoError(t, m.PullImage(ctx, "busybox:latest"))
	require.ErrorIs(t, m.PullImage(ctx, ""), ErrImageNotFound)

	doubles.images.pullErr = errTestBackend
	require.ErrorIs(t, m.PullImage(ctx, "busybox:latest"), errTestBackend)
}

func TestManager_ExecAndAttachNeedAContainerID(t *testing.T) {
	m, _, _ := newTestManager(t)

	_, err := m.ExecContainer(context.Background(), "", ExecOptions{Command: []string{"true"}})
	require.ErrorIs(t, err, ErrContainerNotFound)
	_, err = m.AttachContainer(context.Background(), "", ExecOptions{})
	require.ErrorIs(t, err, ErrContainerNotFound)
}

func TestManagerConfig_InvalidRuntime(t *testing.T) {
	config := DefaultManagerConfig()
	config.Runtime = &RuntimeConfig{}
	require.Error(t, config.Validate())
}

func TestManager_LifecycleRequiresInitialization(t *testing.T) {
	m := &podmanManager{config: DefaultManagerConfig(), logger: newTestRuntime(t).logger}
	ctx := context.Background()

	_, err := m.CreateContainer(ctx, &ContainerConfig{Name: "x", Image: "alpine:latest"})
	require.ErrorIs(t, err, ErrClientNotConnected)
	require.ErrorIs(t, m.StartContainer(ctx, "x"), ErrClientNotConnected)
	require.ErrorIs(t, m.StopContainer(ctx, "x", nil), ErrClientNotConnected)
	require.ErrorIs(t, m.RemoveContainer(ctx, "x", false), ErrClientNotConnected)
	_, err = m.InspectContainer(ctx, "x")
	require.ErrorIs(t, err, ErrClientNotConnected)
	_, err = m.ListContainers(ctx, true)
	require.ErrorIs(t, err, ErrClientNotConnected)
	_, err = m.WaitContainer(ctx, "x")
	require.ErrorIs(t, err, ErrClientNotConnected)
}
