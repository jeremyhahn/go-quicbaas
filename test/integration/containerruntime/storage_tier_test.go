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

//go:build integration && linux && cgo && containers_image_openpgp && exclude_graphdriver_btrfs

package containerruntime_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/internal/containerruntime"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// largeFileBytes is above the static busybox and below the file the test
// writes into the tmpfs, so a file this large on disk is the tmpfs's data.
const largeFileBytes = 4 << 20

// TestJobRuntime_EphemeralWorkspaceTierStaysInItsMemoryLimit runs a spec on
// the ephemeral workspace tier on the in-process job runtime: its checkout is
// a sized tmpfs with no host bind left, a write past the size fails, the
// container's memory cgroup has the planned limit and counts the tmpfs pages
// as shmem, and nothing written reaches the host or the runtime's directories.
func TestJobRuntime_EphemeralWorkspaceTierStaysInItsMemoryLimit(t *testing.T) {
	ctx := testContext(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "policy.json"), []byte(acceptAnythingPolicy), 0o600))
	config := leftoverRuntimeConfig(dir, registryHost(t))
	rt, err := containerruntime.NewRuntime(ctx, config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rt.Close()) })
	jr := containerruntime.NewJobRuntime(rt)

	image, _ := pushShellImage(t, "containerruntime/storage-tier")

	hostCheckout := t.TempDir()
	spec := &orchestration.ContainerSpec{
		Network: "none",
		Image:   image,
		Command: []string{"/bin/sh", "-c", "exec /bin/busybox sleep 600"},
		Mounts:  []orchestration.ContainerMount{{Source: hostCheckout, Target: "/workspace"}},
	}
	require.NoError(t, orchestration.ApplyWorkspaceStorage(spec, "/workspace", orchestration.ProjectStorage{
		WorkspaceTier:     orchestration.WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  16,
		ContainerMemoryMB: 1024 + 16,
	}.Plan()))
	const tmpfsBytes = 16 << 20
	wantMemory := int64(1024+16) << 20
	require.Equal(t, int64(tmpfsBytes)+orchestration.WorkspaceBuildHeadroomBytes, wantMemory,
		"the planned limit is the tmpfs plus the build headroom")

	id, err := jr.Run(ctx, spec)
	require.NoError(t, err)
	t.Cleanup(func() { _ = jr.Remove(context.Background(), id) })

	sh := func(script string) *orchestration.ExecResult {
		t.Helper()
		res, err := jr.Exec(ctx, id, &orchestration.ExecSpec{Cmd: []string{"/bin/sh", "-c", script}})
		require.NoError(t, err)
		return res
	}

	// The checkout is a tmpfs of the planned size, not the host directory.
	res := sh("/bin/busybox cat /proc/mounts")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	var workspace []string
	for _, line := range strings.Split(res.Stdout, "\n") {
		if fields := strings.Fields(line); len(fields) >= 4 && fields[1] == "/workspace" {
			workspace = append(workspace, line)
		}
	}
	require.Len(t, workspace, 1, "mounts: %s", res.Stdout)
	fields := strings.Fields(workspace[0])
	assert.Equal(t, "tmpfs", fields[2], workspace[0])
	assert.Contains(t, strings.Split(fields[3], ","), "size=16384k", workspace[0])
	t.Logf("workspace mount: %s", workspace[0])

	// A write past the tmpfs size fails.
	res = sh("/bin/busybox dd if=/dev/zero of=/workspace/a bs=1M count=8")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	res = sh("/bin/busybox dd if=/dev/zero of=/workspace/b bs=1M count=16")
	assert.NotEqual(t, 0, res.ExitCode)
	assert.Contains(t, res.Stderr, "No space left on device")

	// The init's cgroup is the container's; an exec runs in a sub-cgroup below it.
	state, err := rt.InspectContainer(ctx, id)
	require.NoError(t, err)
	require.Positive(t, state.PID)
	procCgroup, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(state.PID), "cgroup"))
	require.NoError(t, err)
	var cgroupPath string
	for _, line := range strings.Split(string(procCgroup), "\n") {
		if path, ok := strings.CutPrefix(line, "0::"); ok {
			cgroupPath = path
		}
	}
	require.NotEmpty(t, cgroupPath, "cgroup: %s", procCgroup)
	cg := filepath.Join("/sys/fs/cgroup", cgroupPath)

	memoryMax, err := os.ReadFile(filepath.Join(cg, "memory.max"))
	require.NoError(t, err)
	assert.Equal(t, strconv.FormatInt(wantMemory, 10), strings.TrimSpace(string(memoryMax)))

	memoryStat, err := os.ReadFile(filepath.Join(cg, "memory.stat"))
	require.NoError(t, err)
	shmem := int64(-1)
	for _, line := range strings.Split(string(memoryStat), "\n") {
		if value, ok := strings.CutPrefix(line, "shmem "); ok {
			shmem, err = strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			require.NoError(t, err)
		}
	}
	assert.GreaterOrEqual(t, shmem, int64(8<<20), "memory.stat: %s", memoryStat)

	// Nothing written reaches the host directory or the runtime's directories.
	entries, err := os.ReadDir(hostCheckout)
	require.NoError(t, err)
	assert.Empty(t, entries)
	assertNoLargeFile := func() {
		t.Helper()
		for _, root := range []string{config.Root, config.RunRoot, config.TmpDir} {
			walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					if errors.Is(err, fs.ErrNotExist) {
						return nil
					}
					return err
				}
				if !entry.Type().IsRegular() {
					return nil
				}
				info, err := entry.Info()
				if err != nil {
					if errors.Is(err, fs.ErrNotExist) {
						return nil
					}
					return err
				}
				assert.Less(t, info.Size(), int64(largeFileBytes), "a large file on disk: %s", path)
				return nil
			})
			require.NoError(t, walkErr)
		}
	}
	assertNoLargeFile()

	require.NoError(t, jr.Remove(ctx, id))
	_, err = jr.Exec(ctx, id, &orchestration.ExecSpec{Cmd: []string{"/bin/busybox", "true"}})
	require.Error(t, err)
	assertNoLargeFile()
}
