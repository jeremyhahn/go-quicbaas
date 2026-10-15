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
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// otherImageDigest is a well-formed digest no test image has.
const otherImageDigest = "sha256:" +
	"fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"

// runToExit starts the container and waits for its init process to exit.
func runToExit(t *testing.T, rt *Runtime, id string) int32 {
	t.Helper()
	require.NoError(t, rt.StartContainer(context.Background(), id))
	code, err := rt.WaitContainer(context.Background(), id)
	require.NoError(t, err)
	return code
}

func TestRuntime_ExitCodeIsTheProcessExitCode(t *testing.T) {
	for _, want := range []int{0, 5} {
		rt, doubles := newTestRuntimeWithBackends(t)
		doubles.engine.initResult = &fakeResult{exitCode: want}
		id := createTestContainer(t, rt, "exit-code")

		assert.Equal(t, int32(want), runToExit(t, rt, id))

		state, err := rt.InspectContainer(context.Background(), id)
		require.NoError(t, err)
		assert.Equal(t, ContainerStateExited, state.State)
		assert.Equal(t, want, state.ExitCode)
		assert.Zero(t, state.PID)
		assert.NotNil(t, state.FinishedAt)
	}
}

func TestRuntime_LogsHoldTheProcessOutput(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	doubles.engine.initResult = &fakeResult{stdout: "hello\n", stderr: "warning\n"}
	id := createTestContainer(t, rt, "logs")
	runToExit(t, rt, id)

	stdout, stderr, err := rt.Logs(id)
	require.NoError(t, err)
	assert.Equal(t, "hello\n", string(stdout))
	assert.Equal(t, "warning\n", string(stderr))
}

func TestRuntime_LogsBeforeStartAreEmpty(t *testing.T) {
	rt := newTestRuntime(t)
	id := createTestContainer(t, rt, "logs-empty")

	stdout, stderr, err := rt.Logs(id)
	require.NoError(t, err)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestRuntime_LogsOfUnknownContainer(t *testing.T) {
	rt := newTestRuntime(t)
	_, _, err := rt.Logs("ghost")
	require.ErrorIs(t, err, ErrContainerNotFound)
}

func TestRuntime_StartRunsTheConfiguredProcess(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	state, err := rt.CreateContainer(context.Background(), &ContainerConfig{
		Name:        "start",
		Image:       "alpine:latest",
		Command:     []string{"/app", "--serve"},
		Environment: map[string]string{"MODE": "test"},
		WorkDir:     "/srv",
	})
	require.NoError(t, err)
	require.NoError(t, rt.StartContainer(context.Background(), state.ID))

	inspected, err := rt.InspectContainer(context.Background(), state.ID)
	require.NoError(t, err)
	assert.Equal(t, ContainerStateRunning, inspected.State)
	assert.Equal(t, doubles.engine.lastProcess().Pid(), inspected.PID)
	assert.NotNil(t, inspected.StartedAt)

	spec := doubles.engine.lastStart()
	assert.Equal(t, []string{"/app", "--serve"}, spec.Args)
	assert.Contains(t, spec.Env, "MODE=test")
	assert.Equal(t, "/srv", spec.Cwd)
	assert.NotNil(t, spec.Stdout)
	assert.NotNil(t, spec.Stderr)
}

func TestRuntime_UserReachesTheContainerProcess(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	state, err := rt.CreateContainer(context.Background(), &ContainerConfig{
		Name:  "user",
		Image: "alpine:latest",
		User:  "1000:2000",
	})
	require.NoError(t, err)

	want := ociUser{UID: 1000, GID: 2000}
	assert.Equal(t, want, doubles.engine.created[state.ID].Process.User)

	require.NoError(t, rt.StartContainer(context.Background(), state.ID))
	assert.Equal(t, want, doubles.engine.lastStart().User)
}

func TestRuntime_CreateRejectsAnUnknownUser(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	_, err := rt.CreateContainer(context.Background(), &ContainerConfig{
		Name:  "bad-user",
		Image: "alpine:latest",
		User:  "nosuchuser",
	})
	require.Error(t, err)

	// The rootfs made for the container is removed again.
	require.Len(t, doubles.rootfs.removed, 1)
	assert.Empty(t, doubles.engine.created)
}

func TestRuntime_CreateFailsWhenTheImageIsNotStored(t *testing.T) {
	rt := newTestRuntime(t)
	_, err := rt.CreateContainer(context.Background(), &ContainerConfig{Name: "no-image", Image: "busybox:latest"})
	require.ErrorIs(t, err, ErrImageNotFound)
}

func TestRuntime_CreateFailsWhenTheRootfsCannotBeUnpacked(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	doubles.rootfs.createErr = errTestBackend

	_, err := rt.CreateContainer(context.Background(), &ContainerConfig{Name: "unpack", Image: "alpine:latest"})
	require.ErrorIs(t, err, errTestBackend)
	assertNoBundles(t, rt)
}

func TestRuntime_CreateFailsWhenTheEngineRefuses(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	doubles.engine.createErr = errTestBackend

	_, err := rt.CreateContainer(context.Background(), &ContainerConfig{Name: "engine", Image: "alpine:latest"})
	require.ErrorIs(t, err, errTestBackend)

	// Nothing the failed create made is left behind.
	require.Len(t, doubles.rootfs.removed, 1)
	assertNoBundles(t, rt)
	_, err = rt.InspectContainer(context.Background(), "engine")
	require.ErrorIs(t, err, ErrContainerNotFound)
}

// assertNoBundles fails unless the runtime holds no container bundles.
func assertNoBundles(t *testing.T, rt *Runtime) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(rt.config.RunRoot, "bundles"))
	if os.IsNotExist(err) {
		return
	}
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestRuntime_CreateChecksThePinnedDigest(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	pinned := "registry.local/app@" + testImageDigest
	doubles.images.images[pinned] = testImageRecord(pinned, []string{testImageDigest})

	_, err := rt.CreateContainer(context.Background(), &ContainerConfig{Name: "pinned", Image: pinned})
	require.NoError(t, err)

	moved := "registry.local/app@" + otherImageDigest
	doubles.images.images[moved] = testImageRecord(moved, []string{testImageDigest})
	_, err = rt.CreateContainer(context.Background(), &ContainerConfig{Name: "moved", Image: moved})
	require.ErrorIs(t, err, orchestration.ErrImageDigestMismatch)
}

func TestRuntime_StartFailure(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	id := createTestContainer(t, rt, "start-fail")
	doubles.engine.startErr = errTestBackend

	err := rt.StartContainer(context.Background(), id)
	require.ErrorIs(t, err, errTestBackend)

	state, err := rt.InspectContainer(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, ContainerStateCreated, state.State)
}

func TestRuntime_StartTwice(t *testing.T) {
	rt := newTestRuntime(t)
	id := createTestContainer(t, rt, "start-twice")
	require.NoError(t, rt.StartContainer(context.Background(), id))

	err := rt.StartContainer(context.Background(), id)
	require.ErrorIs(t, err, ErrContainerAlreadyRunning)
}

func TestRuntime_StartAfterExit(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	doubles.engine.initResult = &fakeResult{}
	id := createTestContainer(t, rt, "start-exited")
	runToExit(t, rt, id)

	err := rt.StartContainer(context.Background(), id)
	require.ErrorIs(t, err, ErrContainerExited)
}

func TestRuntime_StartUnknownContainer(t *testing.T) {
	rt := newTestRuntime(t)
	err := rt.StartContainer(context.Background(), "ghost")
	require.ErrorIs(t, err, ErrContainerNotFound)
}

func TestRuntime_StopSendsSIGTERM(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	id := createTestContainer(t, rt, "stop")
	require.NoError(t, rt.StartContainer(context.Background(), id))

	require.NoError(t, rt.StopContainer(context.Background(), id, 10))

	process := doubles.engine.lastProcess()
	assert.Equal(t, []os.Signal{syscall.SIGTERM}, process.receivedSignals())
	state, err := rt.InspectContainer(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, ContainerStateStopped, state.State)
	assert.Equal(t, 128+int(syscall.SIGTERM), state.ExitCode)
}

func TestRuntime_StopEscalatesToSIGKILL(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	doubles.engine.ignoreSIGTERM = true
	id := createTestContainer(t, rt, "stop-kill")
	require.NoError(t, rt.StartContainer(context.Background(), id))

	require.NoError(t, rt.StopContainer(context.Background(), id, 0))

	process := doubles.engine.lastProcess()
	assert.Equal(t, []os.Signal{syscall.SIGTERM, syscall.SIGKILL}, process.receivedSignals())
	code, err := rt.WaitContainer(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, int32(128+int(syscall.SIGKILL)), code)
}

func TestRuntime_StopHonoursTheContext(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	doubles.engine.ignoreSIGTERM = true
	id := createTestContainer(t, rt, "stop-ctx")
	require.NoError(t, rt.StartContainer(context.Background(), id))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := rt.StopContainer(ctx, id, 60)
	require.ErrorIs(t, err, context.Canceled)
}

func TestRuntime_StopOfAnExitedContainer(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	doubles.engine.initResult = &fakeResult{exitCode: 3}
	id := createTestContainer(t, rt, "stop-exited")
	runToExit(t, rt, id)

	require.NoError(t, rt.StopContainer(context.Background(), id, 10))
	assert.Empty(t, doubles.engine.lastProcess().receivedSignals())
}

func TestRuntime_KillOfAMissingContainer(t *testing.T) {
	rt := newTestRuntime(t)
	err := rt.StopContainer(context.Background(), "ghost", 0)
	require.ErrorIs(t, err, ErrContainerNotFound)
}

func TestRuntime_WaitBeforeStart(t *testing.T) {
	rt := newTestRuntime(t)
	id := createTestContainer(t, rt, "wait-created")

	_, err := rt.WaitContainer(context.Background(), id)
	require.ErrorIs(t, err, ErrContainerNotRunning)
}

func TestRuntime_WaitHonoursTheContext(t *testing.T) {
	rt := newTestRuntime(t)
	id := createTestContainer(t, rt, "wait-ctx")
	require.NoError(t, rt.StartContainer(context.Background(), id))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := rt.WaitContainer(ctx, id)
	require.ErrorIs(t, err, context.Canceled)
}

func TestRuntime_DeleteOfARunningContainer(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	id := createTestContainer(t, rt, "delete-running")
	require.NoError(t, rt.StartContainer(context.Background(), id))

	err := rt.RemoveContainer(context.Background(), id, false)
	require.ErrorIs(t, err, ErrContainerRunning)
	assert.Empty(t, doubles.engine.destroyed)
	assert.Empty(t, doubles.rootfs.removed)
}

func TestRuntime_ForcedDeleteStopsAndRemovesEverything(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	id := createTestContainer(t, rt, "delete-forced")
	require.NoError(t, rt.StartContainer(context.Background(), id))
	state, err := rt.InspectContainer(context.Background(), id)
	require.NoError(t, err)

	require.NoError(t, rt.RemoveContainer(context.Background(), id, true))

	assert.Equal(t, []string{id}, doubles.engine.destroyed)
	assert.Equal(t, []string{id}, doubles.rootfs.removed)
	assert.NoDirExists(t, filepath.Join(doubles.rootfs.root, id))
	assert.NoDirExists(t, state.BundleDir)
	_, err = rt.InspectContainer(context.Background(), id)
	require.ErrorIs(t, err, ErrContainerNotFound)
	_, err = rt.InspectContainer(context.Background(), "delete-forced")
	require.ErrorIs(t, err, ErrContainerNotFound)
}

func TestRuntime_DeleteFailsWhenTheEngineCannotDestroy(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	id := createTestContainer(t, rt, "delete-fail")
	doubles.engine.destroyErr = errTestBackend

	err := rt.RemoveContainer(context.Background(), id, false)
	require.ErrorIs(t, err, errTestBackend)

	// The container is still known, so the delete can be retried.
	_, err = rt.InspectContainer(context.Background(), id)
	require.NoError(t, err)
}

func TestRuntime_DeleteFailsWhenTheRootfsCannotBeRemoved(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	id := createTestContainer(t, rt, "delete-rootfs")
	doubles.rootfs.removeErr = errTestBackend

	err := rt.RemoveContainer(context.Background(), id, false)
	require.ErrorIs(t, err, errTestBackend)
}

func TestRuntime_DeleteOfAMissingContainer(t *testing.T) {
	rt := newTestRuntime(t)
	err := rt.RemoveContainer(context.Background(), "ghost", false)
	require.ErrorIs(t, err, ErrContainerNotFound)
}

func TestRuntime_ListContainers(t *testing.T) {
	rt := newTestRuntime(t)
	first := createTestContainer(t, rt, "list-a")
	second := createTestContainer(t, rt, "list-b")

	containers, err := rt.ListContainers(context.Background(), true)
	require.NoError(t, err)

	ids := make([]string, 0, len(containers))
	for _, c := range containers {
		ids = append(ids, c.ID)
	}
	assert.ElementsMatch(t, []string{first, second}, ids)
}

func TestRuntime_PullImage(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)

	require.NoError(t, rt.PullImage(context.Background(), "busybox:latest"))
	assert.Equal(t, []string{"busybox:latest"}, doubles.images.pulls)
	_, err := rt.CreateContainer(context.Background(), &ContainerConfig{Name: "pulled", Image: "busybox:latest"})
	require.NoError(t, err)
}

func TestRuntime_PullFailure(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	doubles.images.pullErr = errTestBackend

	err := rt.PullImage(context.Background(), "busybox:latest")
	require.ErrorIs(t, err, ErrImagePullFailed)
	require.ErrorIs(t, err, errTestBackend)
}

func TestRuntime_PullEmptyReference(t *testing.T) {
	rt := newTestRuntime(t)
	err := rt.PullImage(context.Background(), "")
	require.ErrorIs(t, err, ErrImageNotFound)
}

func TestRuntime_PullChecksThePinnedDigest(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)

	require.NoError(t, rt.PullImage(context.Background(), "registry.local/app@"+testImageDigest))

	err := rt.PullImage(context.Background(), "registry.local/app@"+otherImageDigest)
	var mismatch *orchestration.ImageDigestMismatchError
	require.ErrorAs(t, err, &mismatch)
	assert.Equal(t, otherImageDigest, mismatch.Expected)
	assert.Equal(t, testImageDigest, mismatch.Actual)

	doubles.images.pullDigests = []string{}
	err = rt.PullImage(context.Background(), "registry.local/app@"+testImageDigest)
	require.ErrorIs(t, err, orchestration.ErrImageDigestMismatch)

	err = rt.PullImage(context.Background(), "registry.local/app@sha256:short")
	require.Error(t, err)
}

func TestRuntime_RemoveImage(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)

	require.NoError(t, rt.RemoveImage(context.Background(), "alpine:latest", false))
	assert.Empty(t, doubles.images.images)

	err := rt.RemoveImage(context.Background(), "alpine:latest", false)
	require.ErrorIs(t, err, ErrImageNotFound)
	require.NoError(t, rt.RemoveImage(context.Background(), "alpine:latest", true))

	err = rt.RemoveImage(context.Background(), "", false)
	require.ErrorIs(t, err, ErrImageNotFound)
}

func TestRuntime_CloseReportsABackendFailure(t *testing.T) {
	dir := t.TempDir()
	config := &RuntimeConfig{
		Root:            filepath.Join(dir, "root"),
		RunRoot:         filepath.Join(dir, "run"),
		TmpDir:          filepath.Join(dir, "tmp"),
		GraphDriverName: "vfs",
		StaticDir:       filepath.Join(dir, "static"),
		VolumePath:      filepath.Join(dir, "volumes"),
		ImageStorePath:  filepath.Join(dir, "images"),
	}
	rt, err := NewRuntime(context.Background(), config, withBackends(&backends{
		engine: newFakeEngine(),
		images: newFakeImages(),
		rootfs: &fakeRootfs{root: dir},
		close:  func() error { return errTestBackend },
	}))
	require.NoError(t, err)

	require.ErrorIs(t, rt.Close(), errTestBackend)
	assert.False(t, rt.IsInitialized())
	require.NoError(t, rt.Close())
}
