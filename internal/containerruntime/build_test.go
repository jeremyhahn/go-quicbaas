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
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const scratchHeredoc = `FROM scratch
COPY <<EOF /etc/hello
hello
EOF
CMD ["/bin/true"]
`

func TestBuildImage_ScratchHeredoc(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	ctx := context.Background()

	built, err := rt.buildImage(ctx, []byte(scratchHeredoc), buildOptions{Tags: []string{"localhost/built:test"}})
	require.NoError(t, err)

	commits := doubles.commit.recorded()
	require.Len(t, commits, 1)
	hello := filepath.Join(commits[0].snapshot, "etc", "hello")
	data, err := os.ReadFile(hello)
	require.NoError(t, err)
	assert.Equal(t, "hello\n", string(data))
	info, err := os.Stat(hello)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
	assert.Equal(t, []string{"/bin/true"}, commits[0].config.Cmd)

	tagged, err := doubles.images.Inspect(ctx, "localhost/built:test")
	require.NoError(t, err)
	assert.Equal(t, commits[0].image.ID, tagged.ID)
	assert.Equal(t, tagged.ID, built.ID)
}

// buildTag is the tag the tests build.
const buildTag = "localhost/built:test"

// buildTagged builds dockerfile tagged buildTag.
func buildTagged(t *testing.T, rt *Runtime, dockerfile string) (imageRecord, error) {
	t.Helper()
	return rt.buildImage(context.Background(), []byte(dockerfile), buildOptions{Tags: []string{buildTag}})
}

// lastSnapshot is the root filesystem the build's last commit stored.
func lastSnapshot(t *testing.T, doubles *testBackends) string {
	t.Helper()
	commits := doubles.commit.recorded()
	require.NotEmpty(t, commits)
	return commits[len(commits)-1].snapshot
}

// writeBuildFile writes data to rel under dir with mode, creating parents.
func writeBuildFile(t *testing.T, dir, rel, data string, mode os.FileMode) {
	t.Helper()
	name := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(name), 0o755))
	require.NoError(t, os.WriteFile(name, []byte(data), 0o600))
	require.NoError(t, os.Chmod(name, mode))
}

func TestBuildImage_RunStep(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	doubles.engine.onStart = func(_ processSpec, rootfs string) fakeResult {
		writeBuildFile(t, rootfs, "work/out", "hi\n", 0o644)
		return fakeResult{}
	}

	_, err := buildTagged(t, rt, `FROM alpine:latest
ENV A=1
WORKDIR /work
USER 1000
RUN echo hi > out
`)
	require.NoError(t, err)

	require.Len(t, doubles.engine.starts, 1)
	spec := doubles.engine.starts[0]
	assert.Equal(t, []string{"/bin/sh", "-c", "echo hi > out"}, spec.Args)
	assert.Contains(t, spec.Env, "A=1")
	assert.Equal(t, "/work", spec.Cwd)
	assert.Equal(t, uint32(1000), spec.User.UID)
	require.Len(t, doubles.engine.startConfigs, 1)
	assert.Contains(t, doubles.engine.startConfigs[0].Linux.Namespaces, ociNamespace{Type: "network"},
		"a build container gets its own network namespace, not the host's")

	commits := doubles.commit.recorded()
	require.Len(t, commits, 1)
	assert.True(t, strings.HasPrefix(commits[0].createdBy, "RUN "), commits[0].createdBy)
	assert.Equal(t, []string{commits[0].containerID}, doubles.engine.destroyed, "the build container is removed")
	containers, err := rt.ListContainers(context.Background(), true)
	require.NoError(t, err)
	assert.Empty(t, containers)

	data, err := os.ReadFile(filepath.Join(commits[0].snapshot, "work", "out"))
	require.NoError(t, err)
	assert.Equal(t, "hi\n", string(data))
	assert.Contains(t, commits[0].config.Env, "A=1")
	assert.Equal(t, "1000", commits[0].config.User)
}

func TestBuildImage_RunFailureStopsTheBuild(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	doubles.engine.onStart = func(processSpec, string) fakeResult {
		return fakeResult{stdout: "working\n", stderr: "boom\n", exitCode: 3}
	}

	_, err := buildTagged(t, rt, `FROM alpine:latest
RUN exit 3
RUN echo never
`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "line 2")
	assert.Contains(t, err.Error(), "code 3")
	assert.Contains(t, err.Error(), "boom")

	_, err = doubles.images.Inspect(context.Background(), buildTag)
	require.ErrorIs(t, err, ErrImageNotFound, "a failed build tags nothing")
	assert.Len(t, doubles.engine.starts, 1, "the step after the failure does not run")
	assert.Len(t, doubles.engine.destroyed, 1, "the failed step's container is removed")
	assert.Empty(t, doubles.commit.recorded())
}

func TestBuildImage_PullsAMissingBase(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)

	_, err := buildTagged(t, rt, `FROM busybox:latest AS one
LABEL a=b
FROM busybox:latest
LABEL c=d
`)
	require.NoError(t, err)
	assert.Equal(t, []string{"busybox:latest"}, doubles.images.pulls, "a missing base is pulled once")
	commits := doubles.commit.recorded()
	require.Len(t, commits, 2)
	assert.Equal(t, "image-busybox:latest", doubles.rootfs.created[commits[1].containerID])
}

func TestBuildImage_PullFlag(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)

	_, err := rt.buildImage(context.Background(), []byte("FROM alpine:latest\nLABEL a=b\n"),
		buildOptions{Tags: []string{buildTag}, Pull: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"alpine:latest"}, doubles.images.pulls, "Pull pulls a stored base too")

	doubles.images.pulls = nil
	_, err = buildTagged(t, rt, "FROM alpine:latest\nLABEL a=b\n")
	require.NoError(t, err)
	assert.Empty(t, doubles.images.pulls, "a stored base is not pulled without Pull")
}

func TestBuildImage_StageWithoutSteps(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)

	built, err := buildTagged(t, rt, "FROM alpine:latest\nLABEL a=b\n")
	require.NoError(t, err)

	commits := doubles.commit.recorded()
	require.Len(t, commits, 1)
	assert.Equal(t, "CONFIG", commits[0].createdBy)
	assert.Equal(t, "b", commits[0].config.Labels["a"])
	assert.Equal(t, "image-alpine:latest", doubles.rootfs.created[commits[0].containerID])
	assert.Contains(t, doubles.rootfs.removed, commits[0].containerID)
	assert.Equal(t, commits[0].image.ID, built.ID)
}

func TestBuildImage_Refusals(t *testing.T) {
	t.Run("no tags", func(t *testing.T) {
		rt, _ := newTestRuntimeWithBackends(t)
		_, err := rt.buildImage(context.Background(), []byte(scratchHeredoc), buildOptions{})
		require.Error(t, err)
	})
	t.Run("no committer", func(t *testing.T) {
		rt, _ := newTestRuntimeWithBackends(t)
		rt.backends.commit = nil
		_, err := buildTagged(t, rt, scratchHeredoc)
		require.ErrorIs(t, err, ErrRuntimeRequiresLinuxCgo)
	})
	t.Run("copy from the context", func(t *testing.T) {
		rt, doubles := newTestRuntimeWithBackends(t)
		_, err := buildTagged(t, rt, "FROM scratch\nCOPY app /app\n")
		var dockerfileErr *DockerfileError
		require.ErrorAs(t, err, &dockerfileErr)
		assert.Equal(t, 2, dockerfileErr.Line)
		assert.Empty(t, doubles.commit.recorded())
	})
	t.Run("commit error", func(t *testing.T) {
		rt, doubles := newTestRuntimeWithBackends(t)
		doubles.commit.commitErr = errTestBackend
		_, err := buildTagged(t, rt, scratchHeredoc)
		require.ErrorIs(t, err, errTestBackend)
		_, err = doubles.images.Inspect(context.Background(), buildTag)
		require.ErrorIs(t, err, ErrImageNotFound)
	})
}

func TestBuildImage_CopyFromStage(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	stamp := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	doubles.engine.onStart = func(_ processSpec, rootfs string) fakeResult {
		writeBuildFile(t, rootfs, "app/bin", "binary", 0o755)
		require.NoError(t, os.Chtimes(filepath.Join(rootfs, "app", "bin"), stamp, stamp))
		writeBuildFile(t, rootfs, "etc/conf", "conf", 0o640)
		writeBuildFile(t, rootfs, "app/tree/sub/file", "file", 0o600)
		require.NoError(t, os.Chmod(filepath.Join(rootfs, "app", "tree", "sub"), 0o750))
		require.NoError(t, os.Symlink("sub/file", filepath.Join(rootfs, "app", "tree", "link")))
		return fakeResult{}
	}

	_, err := buildTagged(t, rt, `FROM alpine:latest AS src
RUN make
FROM scratch
COPY --from=src /app/bin /etc/conf /out/
COPY --from=src /app/tree /tree
`)
	require.NoError(t, err)
	commits := doubles.commit.recorded()
	require.Len(t, commits, 3)
	assert.Equal(t, "COPY --from=src /app/bin /etc/conf /out/", commits[1].createdBy)
	snapshot := lastSnapshot(t, doubles)

	assertFile := func(rel, data string, mode os.FileMode) os.FileInfo {
		t.Helper()
		name := filepath.Join(snapshot, rel)
		got, err := os.ReadFile(name)
		require.NoError(t, err, rel)
		assert.Equal(t, data, string(got), rel)
		info, err := os.Lstat(name)
		require.NoError(t, err)
		assert.Equal(t, mode, info.Mode().Perm(), rel)
		return info
	}
	bin := assertFile("out/bin", "binary", 0o755)
	assert.True(t, bin.ModTime().Equal(stamp), "the modification time is kept")
	assertFile("out/conf", "conf", 0o640)
	assertFile("tree/sub/file", "file", 0o600)
	sub, err := os.Stat(filepath.Join(snapshot, "tree", "sub"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o750), sub.Mode().Perm())
	link, err := os.Readlink(filepath.Join(snapshot, "tree", "link"))
	require.NoError(t, err, "a symlink is copied as a symlink")
	assert.Equal(t, "sub/file", link)

	var sources int
	for id := range doubles.rootfs.created {
		if strings.HasPrefix(id, "build-from-") {
			sources++
			assert.Contains(t, doubles.rootfs.removed, id, "the source stage's root filesystem is removed")
			assert.NoDirExists(t, filepath.Join(doubles.rootfs.root, id))
		}
	}
	assert.Equal(t, 2, sources)
}

func TestBuildImage_CopyChmod(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)

	_, err := buildTagged(t, rt, "FROM scratch\nCOPY --chmod=0755 <<EOF /usr/local/bin/run\n#!/bin/sh\nEOF\n")
	require.NoError(t, err)
	snapshot := lastSnapshot(t, doubles)
	info, err := os.Stat(filepath.Join(snapshot, "usr", "local", "bin", "run"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
	dir, err := os.Stat(filepath.Join(snapshot, "usr", "local"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), dir.Mode().Perm())
}

func TestBuildImage_CopyChown(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	owner := strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid())

	_, err := buildTagged(t, rt, "FROM scratch\nCOPY --chown="+owner+" <<EOF /etc/owned\nx\nEOF\n")
	require.NoError(t, err)
	info, err := os.Stat(filepath.Join(lastSnapshot(t, doubles), "etc", "owned"))
	require.NoError(t, err)
	stat, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	assert.Equal(t, strconv.Itoa(os.Getuid()), strconv.FormatUint(uint64(stat.Uid), 10))
	assert.Equal(t, strconv.Itoa(os.Getgid()), strconv.FormatUint(uint64(stat.Gid), 10))
	assert.Equal(t, "COPY --chown="+owner+" <<heredoc> /etc/owned", doubles.commit.recorded()[0].createdBy)
}

func TestBuildImage_CopyStaysInsideTheRoot(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	outside := t.TempDir()
	doubles.rootfs.seed = func(dir string) error {
		return os.Symlink(outside, filepath.Join(dir, "etc"))
	}

	_, err := buildTagged(t, rt, "FROM alpine:latest\nCOPY <<EOF /etc/x\nx\nEOF\n")
	require.NoError(t, err)
	// The absolute link is resolved inside the root filesystem, as the
	// image sees it.
	_, err = os.Stat(filepath.Join(lastSnapshot(t, doubles), outside, "x"))
	require.NoError(t, err)
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing is written outside the root filesystem")
}
