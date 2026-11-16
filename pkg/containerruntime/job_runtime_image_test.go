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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func TestWithImageDefaults(t *testing.T) {
	image := imageConfig{
		Entrypoint: []string{"/entry"},
		Cmd:        []string{"serve"},
		Env:        []string{"PATH=/opt/bin", "MODE=image", "BARE"},
		WorkingDir: "/app",
		User:       "app",
		Labels:     map[string]string{"org": "image", "tier": "base"},
	}

	t.Run("UnsetFieldsComeFromTheImage", func(t *testing.T) {
		config := &ContainerConfig{Name: "c", Image: "i", Environment: map[string]string{"MODE": "job"}}
		got := withImageDefaults(config, image)
		assert.Equal(t, []string{"/entry", "serve"}, got.Command)
		assert.Nil(t, got.Entrypoint)
		assert.Equal(t, map[string]string{"PATH": "/opt/bin", "MODE": "job", "BARE": ""}, got.Environment)
		assert.Equal(t, "/app", got.WorkDir)
		assert.Equal(t, "app", got.User)
		assert.Equal(t, map[string]string{"org": "image", "tier": "base"}, got.Labels)
		assert.Equal(t, map[string]string{"MODE": "job"}, config.Environment, "the caller's config is not changed")
	})

	t.Run("CommandKeepsTheImageEntrypoint", func(t *testing.T) {
		got := withImageDefaults(&ContainerConfig{Command: []string{"check"}}, image)
		assert.Equal(t, []string{"/entry", "check"}, got.Command)
	})

	t.Run("EntrypointDropsTheImageCommand", func(t *testing.T) {
		got := withImageDefaults(&ContainerConfig{Entrypoint: []string{"/bin/sh", "-c"}}, image)
		assert.Equal(t, []string{"/bin/sh", "-c"}, got.Command)
	})

	t.Run("SetFieldsWin", func(t *testing.T) {
		got := withImageDefaults(&ContainerConfig{
			WorkDir: "/srv",
			User:    "1000:1000",
			Labels:  map[string]string{"org": "job"},
		}, image)
		assert.Equal(t, "/srv", got.WorkDir)
		assert.Equal(t, "1000:1000", got.User)
		assert.Equal(t, map[string]string{"org": "job", "tier": "base"}, got.Labels)
	})

	t.Run("AnEmptyImageChangesNothing", func(t *testing.T) {
		config := &ContainerConfig{Command: []string{"true"}, Environment: map[string]string{"A": "1"}}
		got := withImageDefaults(config, imageConfig{})
		assert.Equal(t, config.Command, got.Command)
		assert.Equal(t, config.Environment, got.Environment)
		assert.Nil(t, withImageDefaults(&ContainerConfig{}, imageConfig{}).Command)
	})

	// ACPBackend clears the image's entrypoint with Entrypoint: []string{} so
	// that its command runs directly; only a nil entrypoint is inherited.
	t.Run("AnExplicitlyEmptyEntrypointClearsTheImages", func(t *testing.T) {
		got := withImageDefaults(&ContainerConfig{Entrypoint: []string{}, Command: []string{"agent", "--acp"}}, image)
		assert.Equal(t, []string{"agent", "--acp"}, got.Command)

		spec := &orchestration.ContainerSpec{Image: "alpine:latest", Entrypoint: []string{}, Command: []string{"agent"}}
		config, err := jobContainerConfig(spec)
		require.NoError(t, err)
		require.NotNil(t, config.Entrypoint, "the job adapter lost the explicit clear")
		assert.Equal(t, []string{"agent"}, withImageDefaults(config, image).Command)

		spec.Entrypoint = nil
		config, err = jobContainerConfig(spec)
		require.NoError(t, err)
		assert.Equal(t, []string{"/entry", "agent"}, withImageDefaults(config, image).Command)
	})
}

func TestJobRuntime_RunUsesTheImageConfiguration(t *testing.T) {
	jr, doubles := newTestJobRuntime(t)
	record := doubles.images.images["alpine:latest"]
	record.Config = imageConfig{
		Entrypoint: []string{"/usr/local/bin/agent"},
		Cmd:        []string{"--acp"},
		Env:        []string{"PATH=/opt/agent/bin:/usr/bin"},
		WorkingDir: "/home/agent",
	}
	doubles.images.images["alpine:latest"] = record

	id, err := jr.Run(t.Context(), &orchestration.ContainerSpec{Image: "alpine:latest", Env: map[string]string{"TOKEN": "x"}})
	require.NoError(t, err)

	process := doubles.engine.created[id].Process
	assert.Equal(t, []string{"/usr/local/bin/agent", "--acp"}, process.Args, "the image's entrypoint runs, not /bin/sh")
	assert.Equal(t, "/home/agent", process.Cwd)
	assert.Equal(t, []string{"TERM=xterm", "PATH=/opt/agent/bin:/usr/bin", "TOKEN=x"}, process.Env,
		"the image's PATH replaces the default rather than shadowing it")
}

func TestJobRuntime_InspectImage(t *testing.T) {
	t.Run("ParsesIDLabelsAndDigests", func(t *testing.T) {
		jr, doubles := newTestJobRuntime(t)
		record := testImageRecord("docker.io/library/alpine:latest", []string{testImageDigest})
		record.ID = strings.Repeat("c", 64)
		record.Config.Labels = map[string]string{"org.opencontainers.image.source": "repo"}
		doubles.images.images["alpine:latest"] = record

		info, err := jr.InspectImage(t.Context(), "alpine:latest")
		require.NoError(t, err)
		assert.Equal(t, "sha256:"+strings.Repeat("c", 64), info.ID)
		assert.Equal(t, map[string]string{"org.opencontainers.image.source": "repo"}, info.Labels)
		assert.Equal(t, []string{"docker.io/library/alpine@" + testImageDigest}, info.RepoDigests)
	})

	t.Run("UnlabelledImageHasAnEmptyLabelMap", func(t *testing.T) {
		jr, _ := newTestJobRuntime(t)
		info, err := jr.InspectImage(t.Context(), "alpine:latest")
		require.NoError(t, err)
		assert.NotNil(t, info.Labels)
		assert.Empty(t, info.Labels)
	})

	t.Run("Errors", func(t *testing.T) {
		jr, _ := newTestJobRuntime(t)
		_, err := jr.InspectImage(t.Context(), " ")
		requireInputValidation(t, err, "image")

		_, err = jr.InspectImage(t.Context(), "missing:latest")
		var notFound *orchestration.ImageNotFoundError
		require.ErrorAs(t, err, &notFound)
	})
}

// TagImage is the in-process `docker tag`: the stored image takes the target
// name, whether the source names it by reference or by ID. Ported from the
// exec runtime's TestTagImage.
func TestJobRuntime_TagImage(t *testing.T) {
	t.Run("ByReference", func(t *testing.T) {
		jr, doubles := newTestJobRuntime(t)
		doubles.images.images["alpine:latest"] = testImageRecord("docker.io/library/alpine:latest", nil)

		require.NoError(t, jr.TagImage(t.Context(), "alpine:latest", "reg/o/w:1"))
		info, err := jr.InspectImage(t.Context(), "reg/o/w:1")
		require.NoError(t, err)
		assert.NotEmpty(t, info.ID)
	})

	t.Run("ByID", func(t *testing.T) {
		jr, doubles := newTestJobRuntime(t)
		record := testImageRecord("docker.io/library/alpine:latest", nil)
		record.ID = strings.Repeat("d", 64)
		doubles.images.images["alpine:latest"] = record

		require.NoError(t, jr.TagImage(t.Context(), "sha256:"+record.ID, "reg/o/w:1"))
		info, err := jr.InspectImage(t.Context(), "reg/o/w:1")
		require.NoError(t, err)
		assert.Equal(t, "sha256:"+record.ID, info.ID)
	})

	t.Run("Validation", func(t *testing.T) {
		jr, _ := newTestJobRuntime(t)
		requireInputValidation(t, jr.TagImage(t.Context(), " ", "t"), "source")
		requireInputValidation(t, jr.TagImage(t.Context(), "s", ""), "target")
	})

	t.Run("AMissingSourceIsATagError", func(t *testing.T) {
		jr, _ := newTestJobRuntime(t)
		err := jr.TagImage(t.Context(), "missing:latest", "reg/o/w:1")
		require.ErrorIs(t, err, orchestration.ErrImageTag)
		require.ErrorIs(t, err, ErrImageNotFound)
	})
}

func TestJobRuntime_PushImage(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)

	t.Run("ReturnsTheDigestAndPassesTheAuth", func(t *testing.T) {
		jr, doubles := newTestJobRuntime(t)
		doubles.images.images["reg/o/w:1"] = testImageRecord("reg/o/w:1", nil)
		doubles.images.pushDigest = digest
		auth := &orchestration.RegistryAuth{Username: "u", Secret: []byte("s")}

		got, err := jr.PushImage(t.Context(), "reg/o/w:1", auth)
		require.NoError(t, err)
		assert.Equal(t, digest, got)
		require.Equal(t, []fakePush{{ref: "reg/o/w:1", auth: auth}}, doubles.images.pushes)
	})

	t.Run("Validation", func(t *testing.T) {
		jr, _ := newTestJobRuntime(t)
		_, err := jr.PushImage(t.Context(), " ", nil)
		requireInputValidation(t, err, "image")
	})

	t.Run("AMissingImageIsAPushError", func(t *testing.T) {
		jr, _ := newTestJobRuntime(t)
		_, err := jr.PushImage(t.Context(), "reg/o/w:1", nil)
		require.ErrorIs(t, err, orchestration.ErrImagePush)
		require.ErrorIs(t, err, ErrImageNotFound)
	})

	t.Run("ARegistryFailureIsAPushError", func(t *testing.T) {
		jr, doubles := newTestJobRuntime(t)
		doubles.images.images["reg/o/w:1"] = testImageRecord("reg/o/w:1", nil)
		doubles.images.pushErr = errors.New("denied")
		_, err := jr.PushImage(t.Context(), "reg/o/w:1", nil)
		require.ErrorIs(t, err, orchestration.ErrImagePush)
	})

	t.Run("AnInvalidDigestIsAPushError", func(t *testing.T) {
		jr, doubles := newTestJobRuntime(t)
		doubles.images.images["reg/o/w:1"] = testImageRecord("reg/o/w:1", nil)
		doubles.images.pushDigest = "not-a-digest"
		_, err := jr.PushImage(t.Context(), "reg/o/w:1", nil)
		require.ErrorIs(t, err, orchestration.ErrImagePush)
	})
}

func TestImageRepository(t *testing.T) {
	for name, want := range map[string]string{
		"alpine":                                    "alpine",
		"docker.io/library/alpine:3.20":             "docker.io/library/alpine",
		"registry:5000/team/app":                    "registry:5000/team/app",
		"registry:5000/team/app:v1":                 "registry:5000/team/app",
		"registry:5000/team/app@" + testImageDigest: "registry:5000/team/app",
	} {
		assert.Equal(t, want, imageRepository(name), name)
	}
}

func runOwned(t *testing.T, jr *JobRuntime, owner string) string {
	t.Helper()
	spec := &orchestration.ContainerSpec{Image: "alpine:latest"}
	if owner != "" {
		spec.Labels = map[string]string{orchestration.ContainerOwnerLabel: owner}
	}
	id, err := jr.Run(t.Context(), spec)
	require.NoError(t, err)
	return id
}

func TestJobRuntime_SweepOwned(t *testing.T) {
	t.Run("RemovesThisDaemonsOrphansAndNothingElse", func(t *testing.T) {
		jr, _ := newTestJobRuntime(t)
		first := runOwned(t, jr, "daemon-a")
		other := runOwned(t, jr, "daemon-b")
		unowned := runOwned(t, jr, "")
		second := runOwned(t, jr, "daemon-a")

		sweep, err := jr.SweepOwned(t.Context(), " daemon-a ")
		require.NoError(t, err)
		assert.Equal(t, "daemon-a", sweep.Owner)
		assert.ElementsMatch(t, []string{first, second}, sweep.Removed)

		for _, kept := range []string{other, unowned} {
			running, err := jr.IsRunning(t.Context(), kept)
			require.NoError(t, err)
			assert.True(t, running)
		}
		_, err = jr.IsRunning(t.Context(), first)
		require.ErrorIs(t, err, orchestration.ErrContainerNotFound)
	})

	t.Run("AHostWithNothingOfItsOwnIsANoOp", func(t *testing.T) {
		jr, _ := newTestJobRuntime(t)
		runOwned(t, jr, "daemon-b")
		sweep, err := jr.SweepOwned(t.Context(), "daemon-a")
		require.NoError(t, err)
		assert.Empty(t, sweep.Removed)
	})

	t.Run("RefusesAnEmptyOwner", func(t *testing.T) {
		jr, doubles := newTestJobRuntime(t)
		runOwned(t, jr, "")
		_, err := jr.SweepOwned(t.Context(), "  ")
		var refused *orchestration.ContainerSweepRefusedError
		require.ErrorAs(t, err, &refused)
		assert.Empty(t, doubles.engine.destroyed)
	})

	t.Run("ReportsAnOrphanItCouldNotRemove", func(t *testing.T) {
		jr, doubles := newTestJobRuntime(t)
		id := runOwned(t, jr, "daemon-a")
		doubles.engine.destroyErr = errTestBackend

		_, err := jr.SweepOwned(t.Context(), "daemon-a")
		var sweepErr *orchestration.ContainerSweepError
		require.ErrorAs(t, err, &sweepErr)
		assert.Equal(t, id, sweepErr.ContainerID)
		require.ErrorIs(t, err, errTestBackend)
	})
}

func TestJobRuntime_RunImage(t *testing.T) {
	t.Run("ReportsTheExitAndOutputAndRemovesTheContainer", func(t *testing.T) {
		jr, doubles := newTestJobRuntime(t)
		doubles.engine.initResult = &fakeResult{stdout: "probe ok\n", stderr: "warn\n", exitCode: 2}

		result, err := jr.RunImage(t.Context(), &orchestration.ImageRunSpec{
			Image:       "alpine:latest",
			Command:     []string{"probe", "--once"},
			Env:         map[string]string{"KEY": "v"},
			User:        "1000",
			NetworkNone: true,
			Stdin:       []byte("input"),
		})
		require.NoError(t, err)
		assert.Equal(t, &orchestration.ExecResult{ExitCode: 2, Stdout: "probe ok\n", Stderr: "warn\n"}, result)

		start := doubles.engine.lastStart()
		assert.Equal(t, []string{"probe", "--once"}, start.Args)
		assert.Contains(t, start.Env, "KEY=v")
		require.NotNil(t, start.Stdin)
		stdin, err := io.ReadAll(start.Stdin)
		require.NoError(t, err)
		assert.Equal(t, "input", string(stdin))
		assert.Len(t, doubles.engine.destroyed, 1, "a completed run is removed")
	})

	t.Run("NoStdinLeavesItClosed", func(t *testing.T) {
		jr, doubles := newTestJobRuntime(t)
		doubles.engine.initResult = &fakeResult{}
		_, err := jr.RunImage(t.Context(), &orchestration.ImageRunSpec{Image: "alpine:latest", Command: []string{"true"}})
		require.NoError(t, err)
		assert.Nil(t, doubles.engine.lastStart().Stdin)
	})

	t.Run("SandboxedRunUsesTheSpawnsBoundary", func(t *testing.T) {
		jr, doubles := newTestJobRuntime(t)
		doubles.engine.initResult = &fakeResult{}
		// A failed removal is only logged, and it keeps the configuration
		// the run was created with in view.
		doubles.engine.destroyErr = errTestBackend
		id := ""
		_, err := jr.RunImage(t.Context(), &orchestration.ImageRunSpec{
			Image:    "alpine:latest",
			Command:  []string{"true"},
			Security: &orchestration.SandboxSecurity{Ulimits: []orchestration.ContainerUlimit{{Name: "nofile", Soft: 64, Hard: 64}}},
		})
		require.NoError(t, err)
		for created := range doubles.engine.created {
			id = created
		}
		require.NotEmpty(t, id)
		assert.Contains(t, doubles.engine.created[id].Process.Rlimits, ociRlimit{Type: "RLIMIT_NOFILE", Soft: 64, Hard: 64})
	})

	t.Run("Errors", func(t *testing.T) {
		jr, doubles := newTestJobRuntime(t)
		_, err := jr.RunImage(t.Context(), nil)
		requireInputValidation(t, err, "spec")
		_, err = jr.RunImage(t.Context(), &orchestration.ImageRunSpec{Command: []string{"true"}})
		requireInputValidation(t, err, "image")
		_, err = jr.RunImage(t.Context(), &orchestration.ImageRunSpec{Image: "alpine:latest"})
		requireInputValidation(t, err, "command")
		_, err = jr.RunImage(t.Context(), &orchestration.ImageRunSpec{
			Image: "alpine:latest", Command: []string{"true"}, Mounts: []orchestration.ContainerMount{{}},
		})
		requireInputValidation(t, err, "mounts[0]")
		assert.Empty(t, doubles.engine.created, "nothing is created for a refused run")

		doubles.engine.startErr = errTestBackend
		_, err = jr.RunImage(t.Context(), &orchestration.ImageRunSpec{Image: "alpine:latest", Command: []string{"true"}})
		var startErr *orchestration.ContainerStartError
		require.ErrorAs(t, err, &startErr)
		require.ErrorIs(t, err, errTestBackend)
	})

	t.Run("CancelledRunKillsAndRemovesItsContainer", func(t *testing.T) {
		jr, doubles := newTestJobRuntime(t)
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()

		result, err := jr.RunImage(ctx, &orchestration.ImageRunSpec{Image: "alpine:latest", Command: []string{"sleep", "60"}})
		assert.Nil(t, result)
		var startErr *orchestration.ContainerStartError
		require.ErrorAs(t, err, &startErr)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.NotEmpty(t, doubles.engine.lastProcess().receivedSignals())
		assert.Len(t, doubles.engine.destroyed, 1, "the hung run's container is removed")
	})

	// Port of TestRunImage_CompletedRunIsNotRemovedAgain: each completed run
	// is removed exactly once, and two runs never share a container.
	t.Run("CompletedRunIsNotRemovedAgain", func(t *testing.T) {
		jr, doubles := newTestJobRuntime(t)
		doubles.engine.initResult = &fakeResult{exitCode: 1}
		for range 2 {
			result, err := jr.RunImage(t.Context(), &orchestration.ImageRunSpec{Image: "alpine:latest", Command: []string{"false"}})
			require.NoError(t, err)
			assert.Equal(t, 1, result.ExitCode)
		}
		require.Len(t, doubles.engine.destroyed, 2, "a completed run was removed more or less than once")
		assert.NotEqual(t, doubles.engine.destroyed[0], doubles.engine.destroyed[1], "two runs shared a container")
		assert.Empty(t, doubles.engine.created, "a completed run's container outlived it")

		states, err := jr.runtime.ListContainers(t.Context(), true)
		require.NoError(t, err)
		assert.Empty(t, states, "a completed run is still registered")
	})

	// Port of TestRunImage_FailedRemovalKeepsTheRunsError: a removal that
	// fails does not replace the run's own failure.
	t.Run("FailedRemovalKeepsTheRunsError", func(t *testing.T) {
		jr, doubles := newTestJobRuntime(t)
		doubles.engine.destroyErr = errTestBackend
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()

		_, err := jr.RunImage(ctx, &orchestration.ImageRunSpec{Image: "alpine:latest", Command: []string{"sleep", "60"}})
		require.ErrorIs(t, err, context.DeadlineExceeded, "the run's error was lost")
		assert.NotErrorIs(t, err, errTestBackend, "the removal's error replaced the run's")
		assert.Empty(t, doubles.engine.destroyed, "the removal was refused, yet recorded")
	})
}
