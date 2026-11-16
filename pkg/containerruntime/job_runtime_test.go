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

package containerruntime

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// The JobRuntime tests are the contract tests the row container-runtime names
// (docs/service-plane/architecture.md#container-runtime), run against the
// in-process runtime instead of ExecContainerRuntime: TestExecContainerRuntime_X
// here is TestJobRuntime_X.

func newTestJobRuntime(t *testing.T) (*JobRuntime, *testBackends) {
	t.Helper()
	rt, doubles := newTestRuntimeWithBackends(t)
	return NewJobRuntime(rt), doubles
}

func requireInputValidation(t *testing.T, err error, field string) {
	t.Helper()
	var validErr *orchestration.InputValidationError
	require.ErrorAs(t, err, &validErr)
	if field != "" {
		assert.True(t, strings.HasPrefix(validErr.Field, field), "field %q, want %q", validErr.Field, field)
	}
}

func TestJobRuntime_RunValidation(t *testing.T) {
	jr, _ := newTestJobRuntime(t)

	_, err := jr.Run(t.Context(), nil)
	requireInputValidation(t, err, "spec")

	_, err = jr.Run(t.Context(), &orchestration.ContainerSpec{})
	requireInputValidation(t, err, "")
}

func TestJobRuntime_ContainerIDValidation(t *testing.T) {
	jr, _ := newTestJobRuntime(t)
	ctx := t.Context()

	requireInputValidation(t, jr.Stop(ctx, ""), "containerID")
	requireInputValidation(t, jr.Remove(ctx, ""), "containerID")
	_, err := jr.Logs(ctx, "")
	requireInputValidation(t, err, "containerID")
	_, err = jr.IsRunning(ctx, "")
	requireInputValidation(t, err, "containerID")
	requireInputValidation(t, jr.Pull(ctx, ""), "image")
}

func TestJobRuntime_RunRejectsUnpinnedAtPinnedLevel(t *testing.T) {
	jr, doubles := newTestJobRuntime(t)
	spec := &orchestration.ContainerSpec{Image: "alpine:latest", TrustLevel: orchestration.TrustLevelPinned}

	_, err := jr.Run(t.Context(), spec)
	require.ErrorIs(t, err, orchestration.ErrImageNotPinned)
	_, err = jr.RunInteractive(t.Context(), spec)
	require.ErrorIs(t, err, orchestration.ErrImageNotPinned)
	assert.Empty(t, doubles.engine.created, "nothing is created for a refused spec")
}

func TestJobRuntime_VerifyPinnedImage(t *testing.T) {
	t.Run("MatchingImageRuns", func(t *testing.T) {
		jr, doubles := newTestJobRuntime(t)
		doubles.images.pullDigests = []string{testImageDigest}

		id, err := jr.Run(t.Context(), &orchestration.ContainerSpec{Image: "alpine@" + testImageDigest})
		require.NoError(t, err)
		assert.NotEmpty(t, id)
		assert.Equal(t, []string{"alpine@" + testImageDigest}, doubles.images.pulls)
	})

	t.Run("SubstitutedImageIsRejected", func(t *testing.T) {
		jr, doubles := newTestJobRuntime(t)
		const other = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		doubles.images.images["alpine@"+testImageDigest] = testImageRecord("alpine", []string{other})

		_, err := jr.Run(t.Context(), &orchestration.ContainerSpec{Image: "alpine@" + testImageDigest})
		var mismatch *orchestration.ImageDigestMismatchError
		require.ErrorAs(t, err, &mismatch)
		assert.Equal(t, other, mismatch.Actual)
		assert.Empty(t, doubles.engine.created)
	})
}

func TestJobRuntime_Lifecycle(t *testing.T) {
	jr, doubles := newTestJobRuntime(t)
	ctx := t.Context()
	require.True(t, jr.IsAvailable())

	id, err := jr.Run(ctx, &orchestration.ContainerSpec{
		Image:       "alpine:latest",
		Command:     []string{"sleep"},
		Args:        []string{"60"},
		Env:         map[string]string{"B": "2", "A": "1"},
		MemoryBytes: 64 << 20,
		PidsLimit:   32,
		Ports:       []string{"127.0.0.1:8080:80/udp"},
	})
	require.NoError(t, err)
	assert.Empty(t, doubles.images.pulls, "a stored image is not pulled again")

	start := doubles.engine.lastStart()
	assert.Equal(t, []string{"sleep", "60"}, start.Args)
	assert.Subset(t, start.Env, []string{"A=1", "B=2"})

	running, err := jr.IsRunning(ctx, id)
	require.NoError(t, err)
	assert.True(t, running)

	require.NoError(t, jr.Stop(ctx, id))
	running, err = jr.IsRunning(ctx, id)
	require.NoError(t, err)
	assert.False(t, running)

	logs, err := jr.Logs(ctx, id)
	require.NoError(t, err)
	_, err = io.ReadAll(logs)
	require.NoError(t, err)
	require.NoError(t, logs.Close())

	require.NoError(t, jr.Remove(ctx, id))
	var notFound *orchestration.ContainerNotFoundError
	_, err = jr.IsRunning(ctx, id)
	require.ErrorAs(t, err, &notFound)
	require.ErrorAs(t, jr.Stop(ctx, id), &notFound)
	require.ErrorAs(t, jr.Remove(ctx, id), &notFound)
	_, err = jr.Logs(ctx, id)
	require.ErrorAs(t, err, &notFound)
}

func TestJobRuntime_RunFailures(t *testing.T) {
	t.Run("PullFails", func(t *testing.T) {
		jr, doubles := newTestJobRuntime(t)
		doubles.images.pullErr = errTestBackend

		_, err := jr.Run(t.Context(), &orchestration.ContainerSpec{Image: "busybox:latest"})
		var pullErr *orchestration.ContainerImagePullError
		require.ErrorAs(t, err, &pullErr)
		require.ErrorIs(t, err, errTestBackend)
	})

	t.Run("StartFailsAndIsCleanedUp", func(t *testing.T) {
		jr, doubles := newTestJobRuntime(t)
		doubles.engine.startErr = errTestBackend

		_, err := jr.Run(t.Context(), &orchestration.ContainerSpec{Image: "alpine:latest"})
		var startErr *orchestration.ContainerStartError
		require.ErrorAs(t, err, &startErr)
		require.ErrorIs(t, err, errTestBackend)
		assert.Len(t, doubles.engine.destroyed, 1, "the created container is removed")
	})

	t.Run("UnsupportedFieldsAreRefused", func(t *testing.T) {
		jr, _ := newTestJobRuntime(t)
		_, err := jr.Run(t.Context(), &orchestration.ContainerSpec{Image: "alpine:latest", GPUEnabled: true})
		requireInputValidation(t, err, "gpu_enabled")
		require.ErrorContains(t, err, ErrJobSpecUnsupported.Error())
	})

	t.Run("BadPortIsRefused", func(t *testing.T) {
		jr, _ := newTestJobRuntime(t)
		_, err := jr.Run(t.Context(), &orchestration.ContainerSpec{Image: "alpine:latest", Ports: []string{"80"}})
		requireInputValidation(t, err, "")
	})

	t.Run("PullFailureIsTyped", func(t *testing.T) {
		jr, doubles := newTestJobRuntime(t)
		doubles.images.pullErr = errTestBackend
		var pullErr *orchestration.ContainerImagePullError
		require.ErrorAs(t, jr.Pull(t.Context(), "busybox:latest"), &pullErr)
	})
}

func TestJobRuntime_RunAppliesUlimits(t *testing.T) {
	jr, doubles := newTestJobRuntime(t)
	id, err := jr.Run(t.Context(), &orchestration.ContainerSpec{
		Image:   "alpine:latest",
		Ulimits: []orchestration.ContainerUlimit{{Name: "nofile", Soft: 1024, Hard: 4096}},
	})
	require.NoError(t, err)
	assert.Equal(t, []ociRlimit{{Type: "RLIMIT_NOFILE", Soft: 1024, Hard: 4096}}, doubles.engine.created[id].Process.Rlimits)

	for _, bad := range []orchestration.ContainerUlimit{
		{Name: "bogus", Soft: 1, Hard: 1},
		{Name: "nofile", Soft: 2, Hard: 1},
		{Name: "nofile", Soft: -1, Hard: 1},
	} {
		_, err := jr.Run(t.Context(), &orchestration.ContainerSpec{
			Image:   "alpine:latest",
			Ulimits: []orchestration.ContainerUlimit{bad},
		})
		var validErr *orchestration.InputValidationError
		require.ErrorAs(t, err, &validErr, "%+v", bad)
	}
}

func TestParsePortMapping(t *testing.T) {
	port, err := parsePortMapping("8080:80")
	require.NoError(t, err)
	assert.Equal(t, PortMapping{HostPort: 8080, ContainerPort: 80}, port)

	port, err = parsePortMapping("10.0.0.1:53:53/udp")
	require.NoError(t, err)
	assert.Equal(t, PortMapping{HostIP: "10.0.0.1", HostPort: 53, ContainerPort: 53, Protocol: "udp"}, port)

	for _, bad := range []string{"80", "a:80", "80:b", "1:2:3:4", "70000:80"} {
		_, err := parsePortMapping(bad)
		assert.Error(t, err, bad)
	}
}

func startJob(t *testing.T, jr *JobRuntime) string {
	t.Helper()
	id, err := jr.Run(t.Context(), &orchestration.ContainerSpec{Image: "alpine:latest", WorkDir: "/srv"})
	require.NoError(t, err)
	return id
}

func TestJobRuntime_Exec_ReportsExitStatus(t *testing.T) {
	jr, doubles := newTestJobRuntime(t)
	id := startJob(t, jr)
	doubles.engine.execResult = fakeResult{stdout: "out", stderr: "err", exitCode: 3}

	result, err := jr.Exec(t.Context(), id, &orchestration.ExecSpec{Cmd: []string{"false"}})
	require.NoError(t, err)
	assert.Equal(t, &orchestration.ExecResult{ExitCode: 3, Stdout: "out", Stderr: "err"}, result)
}

func TestJobRuntime_Exec_WorkDirEnvAndStdin(t *testing.T) {
	jr, doubles := newTestJobRuntime(t)
	id := startJob(t, jr)

	_, err := jr.Exec(t.Context(), id, &orchestration.ExecSpec{
		Cmd:     []string{"git", "apply"},
		Env:     map[string]string{"TOKEN": "secret"},
		WorkDir: "/work",
		Stdin:   []byte("patch"),
	})
	require.NoError(t, err)

	exec := doubles.engine.lastExec()
	assert.Equal(t, []string{"git", "apply"}, exec.Args)
	assert.Equal(t, "/work", exec.Cwd)
	assert.Contains(t, exec.Env, "TOKEN=secret")
	require.NotNil(t, exec.Stdin)
	stdin, err := io.ReadAll(exec.Stdin)
	require.NoError(t, err)
	assert.Equal(t, "patch", string(stdin))

	_, err = jr.Exec(t.Context(), id, &orchestration.ExecSpec{Cmd: []string{"true"}})
	require.NoError(t, err)
	exec = doubles.engine.lastExec()
	assert.Equal(t, "/srv", exec.Cwd, "an exec without a work dir takes the container's")
	assert.Nil(t, exec.Stdin, "an exec without stdin has it closed")
}

func TestJobRuntime_Exec_MissingContainer(t *testing.T) {
	jr, _ := newTestJobRuntime(t)
	_, err := jr.Exec(t.Context(), "nope", &orchestration.ExecSpec{Cmd: []string{"true"}})
	var notFound *orchestration.ContainerNotFoundError
	require.ErrorAs(t, err, &notFound)
	require.ErrorIs(t, err, orchestration.ErrContainerNotFound)
}

func TestJobRuntime_Exec_StoppedContainer(t *testing.T) {
	jr, doubles := newTestJobRuntime(t)
	id := startJob(t, jr)
	require.NoError(t, jr.Stop(t.Context(), id))

	_, err := jr.Exec(t.Context(), id, &orchestration.ExecSpec{Cmd: []string{"true"}})
	var stopped *orchestration.ContainerNotRunningError
	require.ErrorAs(t, err, &stopped)
	require.ErrorIs(t, err, orchestration.ErrContainerNotRunning)
	assert.Empty(t, doubles.engine.execs)
}

func TestJobRuntime_Exec_RejectsUnrunnableRequests(t *testing.T) {
	cases := []struct {
		name        string
		containerID string
		spec        *orchestration.ExecSpec
		wantField   string
	}{
		{name: "EmptyContainerID", spec: &orchestration.ExecSpec{Cmd: []string{"git"}}, wantField: "containerID"},
		{name: "NilSpec", containerID: "ctr-1", wantField: "spec"},
		{name: "NilCmd", containerID: "ctr-1", spec: &orchestration.ExecSpec{}, wantField: "cmd"},
		{name: "EmptyCmd", containerID: "ctr-1", spec: &orchestration.ExecSpec{Cmd: []string{}}, wantField: "cmd"},
		{name: "BlankEntryPoint", containerID: "ctr-1", spec: &orchestration.ExecSpec{Cmd: []string{""}}, wantField: "cmd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jr, doubles := newTestJobRuntime(t)
			result, err := jr.Exec(t.Context(), tc.containerID, tc.spec)
			assert.Nil(t, result)
			requireInputValidation(t, err, tc.wantField)
			assert.Empty(t, doubles.engine.execs, "nothing runs for a refused request")
		})
	}
}

func TestJobRuntime_Exec_ContextCancelled(t *testing.T) {
	jr, doubles := newTestJobRuntime(t)
	id := startJob(t, jr)
	doubles.engine.execBlocks = true

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	result, err := jr.Exec(ctx, id, &orchestration.ExecSpec{Cmd: []string{"sleep", "60"}})
	assert.Nil(t, result)
	var execErr *orchestration.ContainerExecError
	require.ErrorAs(t, err, &execErr)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorIs(t, err, orchestration.ErrContainerExec)
}

func TestJobRuntime_Exec_EngineFailure(t *testing.T) {
	jr, doubles := newTestJobRuntime(t)
	id := startJob(t, jr)
	doubles.engine.execErr = errTestBackend

	_, err := jr.Exec(t.Context(), id, &orchestration.ExecSpec{Cmd: []string{"true"}})
	var execErr *orchestration.ContainerExecError
	require.ErrorAs(t, err, &execErr)
	require.ErrorIs(t, err, errTestBackend)
}

func TestJobRuntime_IsAvailable(t *testing.T) {
	var nilRuntime *JobRuntime
	assert.False(t, nilRuntime.IsAvailable())
	assert.False(t, NewJobRuntime(nil).IsAvailable())

	rt, _ := newTestRuntimeWithBackends(t)
	jr := NewJobRuntime(rt)
	assert.True(t, jr.IsAvailable())
	require.NoError(t, rt.Close())
	assert.False(t, jr.IsAvailable())
	assert.False(t, errors.Is(nil, ErrJobSpecUnsupported))
}

func TestJobRuntime_RunInteractive_StreamsStdoutAndReportsExit(t *testing.T) {
	jr, doubles := newTestJobRuntime(t)
	doubles.engine.initResult = &fakeResult{stdout: "{\"jsonrpc\":\"2.0\"}\n", stderr: "noise", exitCode: 7}

	proc, err := jr.RunInteractive(t.Context(), &orchestration.ContainerSpec{
		Image: "alpine:latest",
		Env:   map[string]string{"TOKEN": "secret"},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, proc.ContainerID)
	assert.Nil(t, proc.Cleanup, "the environment never lands in a file")

	start := doubles.engine.lastStart()
	assert.IsType(t, &os.File{}, start.Stdin, "stdin is a pipe the init process inherits")
	assert.IsType(t, &os.File{}, start.Stdout, "stdout is a pipe, not the log file")
	assert.Contains(t, start.Env, "TOKEN=secret")

	out, err := io.ReadAll(proc.Stdout)
	require.NoError(t, err)
	assert.Equal(t, "{\"jsonrpc\":\"2.0\"}\n", string(out), "stdout carries only the entrypoint's output")

	exit := proc.Wait(t.Context())
	assert.Equal(t, orchestration.ContainerExit{Known: true, Code: 7}, exit)
	assert.Equal(t, exit, proc.Wait(t.Context()), "Wait is safe to call again")

	_, err = jr.IsRunning(t.Context(), proc.ContainerID)
	require.ErrorIs(t, err, orchestration.ErrContainerNotFound, "an exited interactive container is removed")
	require.NoError(t, proc.Stdin.Close())
	require.NoError(t, proc.Stdout.Close())
}

func TestJobRuntime_RunInteractive_ContextEndKillsAndRemoves(t *testing.T) {
	jr, doubles := newTestJobRuntime(t)
	ctx, cancel := context.WithCancel(t.Context())

	proc, err := jr.RunInteractive(ctx, &orchestration.ContainerSpec{Image: "alpine:latest"})
	require.NoError(t, err)
	running, err := jr.IsRunning(t.Context(), proc.ContainerID)
	require.NoError(t, err)
	require.True(t, running, "the container outlives RunInteractive until its context ends")

	expired, expire := context.WithCancel(t.Context())
	expire()
	assert.Equal(t, orchestration.ContainerExit{}, proc.Wait(expired), "Wait gives up when its own ctx ends")

	cancel()
	assert.Equal(t, orchestration.ContainerExit{}, proc.Wait(t.Context()), "a killed container has no exit of its own")
	assert.NotEmpty(t, doubles.engine.lastProcess().receivedSignals(), "the container was signalled")
	_, err = jr.IsRunning(t.Context(), proc.ContainerID)
	require.ErrorIs(t, err, orchestration.ErrContainerNotFound)
	require.NoError(t, proc.Stdin.Close())
	require.NoError(t, proc.Stdout.Close())
}

func TestJobRuntime_RunInteractive_StartFailure(t *testing.T) {
	jr, doubles := newTestJobRuntime(t)
	doubles.engine.startErr = errTestBackend

	proc, err := jr.RunInteractive(t.Context(), &orchestration.ContainerSpec{Image: "alpine:latest"})
	assert.Nil(t, proc)
	var startErr *orchestration.ContainerStartError
	require.ErrorAs(t, err, &startErr)
	require.ErrorIs(t, err, errTestBackend)
	assert.Len(t, doubles.engine.destroyed, 1, "the created container is removed")
}
