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
	"archive/tar"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/containerruntime"
)

// testTimeout bounds every container operation a test waits on.
const testTimeout = 2 * time.Minute

// acceptAnythingPolicy is the signature policy for the test registry, whose
// images are unsigned.
const acceptAnythingPolicy = `{"default":[{"type":"insecureAcceptAnything"}]}`

// testManager is a manager over real backends and the directories it owns.
type testManager struct {
	containerruntime.PodmanManager
	config *containerruntime.RuntimeConfig
}

// newTestManager opens a manager whose runtime keeps everything under a
// fresh temporary directory, using the vfs storage driver.
func newTestManager(t *testing.T) *testManager {
	t.Helper()

	dir := t.TempDir()
	policyPath := filepath.Join(dir, "policy.json")
	require.NoError(t, os.WriteFile(policyPath, []byte(acceptAnythingPolicy), 0o600))

	config := &containerruntime.RuntimeConfig{
		Root:                filepath.Join(dir, "root"),
		RunRoot:             filepath.Join(dir, "run"),
		TmpDir:              filepath.Join(dir, "tmp"),
		VolumePath:          filepath.Join(dir, "volumes"),
		GraphDriverName:     "vfs",
		InsecureRegistries:  []string{registryHost(t)},
		SignaturePolicyPath: policyPath,
	}
	manager, err := containerruntime.NewPodmanManager(&containerruntime.ManagerConfig{
		Runtime:            config,
		MaxContainers:      10,
		DefaultStopTimeout: 1,
	})
	require.NoError(t, err)
	require.NoError(t, manager.Initialize(context.Background()))
	t.Cleanup(func() { require.NoError(t, manager.Close()) })

	return &testManager{PodmanManager: manager, config: config}
}

// layerDirs lists the vfs layer directories: one per image layer and one
// per container root filesystem.
func (m *testManager) layerDirs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(m.config.Root, "storage", "vfs", "dir"))
	require.NoError(t, err)

	dirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		dirs = append(dirs, filepath.Join(m.config.Root, "storage", "vfs", "dir", entry.Name()))
	}
	return dirs
}

// bundleDir is where the runtime keeps a container's OCI bundle and logs.
func (m *testManager) bundleDir(id string) string {
	return filepath.Join(m.config.RunRoot, "bundles", id)
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	t.Cleanup(cancel)
	return ctx
}

// A container runs to its exit, reports its real exit code and its output,
// and deleting it removes its root filesystem and bundle.
func TestContainerLifecycle_ExitCodes(t *testing.T) {
	for _, exitCode := range []int{0, 5} {
		t.Run(fmt.Sprintf("exit %d", exitCode), func(t *testing.T) {
			ctx := testContext(t)
			manager := newTestManager(t)
			image, _ := pushShellImage(t, fmt.Sprintf("containerruntime/exit-%d", exitCode))

			require.NoError(t, manager.PullImage(ctx, image))
			imageLayers := manager.layerDirs(t)

			container, err := manager.CreateContainer(ctx, &containerruntime.ContainerConfig{
				Name:  fmt.Sprintf("exit-%d", exitCode),
				Image: image,
				Command: []string{"/bin/sh", "-c",
					fmt.Sprintf("echo out-%d; echo err-%d >&2; exit %d", exitCode, exitCode, exitCode)},
			})
			require.NoError(t, err)
			assert.Equal(t, containerruntime.ContainerStateCreated, container.State)

			// Create unpacked the image into a new root filesystem.
			layers := manager.layerDirs(t)
			require.Len(t, layers, len(imageLayers)+1)
			rootfs := newDir(layers, imageLayers)
			assert.FileExists(t, filepath.Join(rootfs, "bin", "busybox"))
			assert.DirExists(t, manager.bundleDir(container.ID))

			require.NoError(t, manager.StartContainer(ctx, container.ID))
			code, err := manager.WaitContainer(ctx, container.ID)
			require.NoError(t, err)
			assert.Equal(t, exitCode, code)

			inspected, err := manager.InspectContainer(ctx, container.ID)
			require.NoError(t, err)
			assert.Equal(t, containerruntime.ContainerStateExited, inspected.State)
			assert.Equal(t, exitCode, inspected.ExitCode)
			assert.NotNil(t, inspected.FinishedAt)

			logs, err := manager.GetContainerLogs(ctx, container.ID, false, nil)
			require.NoError(t, err)
			assert.Equal(t, fmt.Sprintf("out-%d\n", exitCode), string(logs.Stdout))
			assert.Equal(t, fmt.Sprintf("err-%d\n", exitCode), string(logs.Stderr))

			require.NoError(t, manager.RemoveContainer(ctx, container.ID, false))
			assert.NoDirExists(t, rootfs)
			assert.NoDirExists(t, manager.bundleDir(container.ID))
			assert.ElementsMatch(t, imageLayers, manager.layerDirs(t))

			_, err = manager.InspectContainer(ctx, container.ID)
			require.ErrorIs(t, err, containerruntime.ErrContainerNotFound)
		})
	}
}

// A running container is not deleted without force. Its init ignores
// SIGTERM, as PID 1 does without a handler, so stop escalates to SIGKILL.
func TestContainerLifecycle_StopAndDeleteRunning(t *testing.T) {
	ctx := testContext(t)
	manager := newTestManager(t)
	image, _ := pushShellImage(t, "containerruntime/sleep")
	require.NoError(t, manager.PullImage(ctx, image))

	container, err := manager.CreateContainer(ctx, &containerruntime.ContainerConfig{
		Name:    "sleeper",
		Image:   image,
		Command: []string{"/bin/sh", "-c", "exec /bin/busybox sleep 600"},
	})
	require.NoError(t, err)
	require.NoError(t, manager.StartContainer(ctx, container.ID))

	err = manager.RemoveContainer(ctx, container.ID, false)
	require.ErrorIs(t, err, containerruntime.ErrContainerRunning)

	grace := 1
	require.NoError(t, manager.StopContainer(ctx, container.ID, &grace))
	code, err := manager.WaitContainer(ctx, container.ID)
	require.NoError(t, err)
	assert.Equal(t, 128+9, code)

	require.NoError(t, manager.RemoveContainer(ctx, container.ID, false))
	assert.NoDirExists(t, manager.bundleDir(container.ID))
}

// A forced delete stops a running container before removing it.
func TestContainerLifecycle_ForcedDelete(t *testing.T) {
	ctx := testContext(t)
	manager := newTestManager(t)
	image, _ := pushShellImage(t, "containerruntime/forced")
	require.NoError(t, manager.PullImage(ctx, image))
	imageLayers := manager.layerDirs(t)

	container, err := manager.CreateContainer(ctx, &containerruntime.ContainerConfig{
		Name:    "forced",
		Image:   image,
		Command: []string{"/bin/sh", "-c", "exec /bin/busybox sleep 600"},
	})
	require.NoError(t, err)
	require.NoError(t, manager.StartContainer(ctx, container.ID))

	require.NoError(t, manager.RemoveContainer(ctx, container.ID, true))
	assert.ElementsMatch(t, imageLayers, manager.layerDirs(t))
	assert.NoDirExists(t, manager.bundleDir(container.ID))
}

func TestExec_CancelKillsTheDescendantsOfAnExitedLeader(t *testing.T) {
	ctx := testContext(t)
	manager := newTestManager(t)
	image, _ := pushShellImage(t, "containerruntime/exec-cancel")
	require.NoError(t, manager.PullImage(ctx, image))

	container, err := manager.CreateContainer(ctx, &containerruntime.ContainerConfig{
		Name:    "exec-cancel",
		Image:   image,
		Command: []string{"/bin/sh", "-c", "exec /bin/busybox sleep 600"},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = manager.RemoveContainer(context.Background(), container.ID, true)
	})
	require.NoError(t, manager.StartContainer(ctx, container.ID))

	executor, ok := manager.PodmanManager.(containerruntime.Executor)
	require.True(t, ok)

	// The exec runs in its own sub-cgroup, not in its container's cgroup.
	res, err := executor.ExecContainer(ctx, container.ID, containerruntime.ExecOptions{
		Command: []string{"/bin/busybox", "cat", "/proc/self/cgroup"},
	})
	require.NoError(t, err)
	require.Equal(t, 0, res.ExitCode)
	require.Contains(t, string(res.Stdout), "/exec-")

	// The shell exits at once and is reaped; its subshell holds stdout, so
	// the exec runs until the deadline and its cancellation must reach the
	// subshell.
	cancelCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	start := time.Now()
	_, err = executor.ExecContainer(cancelCtx, container.ID, containerruntime.ExecOptions{
		Command: []string{"/bin/sh", "-c", "(/bin/busybox sleep 3; /bin/busybox touch /late) & exit 0"},
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NotErrorIs(t, err, containerruntime.ErrExecOutlivedItsKill)
	require.Less(t, time.Since(start), 5*time.Second)

	time.Sleep(4 * time.Second)
	res, err = executor.ExecContainer(ctx, container.ID, containerruntime.ExecOptions{
		Command: []string{"/bin/busybox", "test", "-e", "/late"},
	})
	require.NoError(t, err)
	require.Equal(t, 1, res.ExitCode, "the cancelled exec's child survived and wrote /late")
}

// TestContainer_SeccompRefusesAProfileDeniedSyscall shows that an
// unprivileged container runs under the default seccomp filter, which
// refuses a user namespace without CAP_SYS_ADMIN although no capability is
// needed for it.
func TestContainer_SeccompRefusesAProfileDeniedSyscall(t *testing.T) {
	ctx := testContext(t)
	manager := newTestManager(t)
	image, _ := pushShellImage(t, "containerruntime/seccomp")
	require.NoError(t, manager.PullImage(ctx, image))

	container, err := manager.CreateContainer(ctx, &containerruntime.ContainerConfig{
		Name:  "seccomp",
		Image: image,
		Command: []string{"/bin/sh", "-c",
			"/bin/busybox cat /proc/self/status; /bin/busybox unshare -U /bin/busybox true; echo unshare-rc=$?"},
	})
	require.NoError(t, err)

	require.NoError(t, manager.StartContainer(ctx, container.ID))
	code, err := manager.WaitContainer(ctx, container.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, code)

	logs, err := manager.GetContainerLogs(ctx, container.ID, false, nil)
	require.NoError(t, err)
	stdout, stderr := string(logs.Stdout), string(logs.Stderr)
	assert.Regexp(t, `(?m)^Seccomp:\s+2$`, stdout, "the process runs in seccomp filter mode")
	assert.Regexp(t, `unshare-rc=[1-9]`, stdout)
	assert.NotContains(t, stdout, "unshare-rc=0")
	assert.Contains(t, stderr, "Operation not permitted", "the profile's errno")

	require.NoError(t, manager.RemoveContainer(ctx, container.ID, false))
}

// TestNamedVolume_IsInitializedFromTheImageForItsUser checks that a new named
// volume holds the image's files at its target with their owner and mode, so
// the image's non-root user can read and write it.
func TestNamedVolume_IsInitializedFromTheImageForItsUser(t *testing.T) {
	ctx := testContext(t)
	manager := newTestManager(t)
	image, _ := pushShellImage(t, "containerruntime/volume-init",
		layerEntry{Header: tar.Header{Name: "data/", Typeflag: tar.TypeDir, Mode: 0o700, Uid: 1000, Gid: 1000}},
		layerEntry{Header: tar.Header{Name: "data/seed", Typeflag: tar.TypeReg, Mode: 0o600, Uid: 1000, Gid: 1000}, Body: []byte("from-image\n")},
	)
	require.NoError(t, manager.PullImage(ctx, image))

	container, err := manager.CreateContainer(ctx, &containerruntime.ContainerConfig{
		Name:    "volume-init",
		Image:   image,
		User:    "1000:1000",
		Mounts:  []containerruntime.Mount{{Type: "volume", Source: "dataset", Target: "/data"}},
		Command: []string{"/bin/sh", "-c", "/bin/busybox cat /data/seed && echo written > /data/new && echo ok"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = manager.RemoveContainer(context.Background(), container.ID, true) })

	require.NoError(t, manager.StartContainer(ctx, container.ID))
	code, err := manager.WaitContainer(ctx, container.ID)
	require.NoError(t, err)
	logs, err := manager.GetContainerLogs(ctx, container.ID, false, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, code, "stderr: %s", logs.Stderr)
	assert.Equal(t, "from-image\nok\n", string(logs.Stdout))

	dir := filepath.Join(manager.config.VolumePath, "dataset")
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	stat, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	assert.Equal(t, uint32(1000), stat.Uid)
	assert.Equal(t, uint32(1000), stat.Gid)
	_, err = os.Stat(filepath.Join(dir, "new"))
	require.NoError(t, err, "the image's user wrote into the volume")
}

// A digest-pinned reference pulls when the registry serves the pinned
// manifest, and fails when it does not.
func TestPullImage_PinnedDigest(t *testing.T) {
	ctx := testContext(t)
	manager := newTestManager(t)
	image, manifestDigest := pushShellImage(t, "containerruntime/pinned")
	repository := image[:len(image)-len(":latest")]

	require.NoError(t, manager.PullImage(ctx, repository+"@"+manifestDigest.String()))

	other := digest.FromString("not the pushed manifest")
	err := manager.PullImage(ctx, repository+"@"+other.String())
	require.ErrorIs(t, err, containerruntime.ErrImagePullFailed)
}

// Pulling an image the registry does not have fails, and a container
// cannot be created from an image that was never pulled.
func TestImageErrors(t *testing.T) {
	ctx := testContext(t)
	manager := newTestManager(t)
	missing := registryHost(t) + "/containerruntime/missing:latest"

	err := manager.PullImage(ctx, missing)
	require.ErrorIs(t, err, containerruntime.ErrImagePullFailed)

	_, err = manager.CreateContainer(ctx, &containerruntime.ContainerConfig{Name: "missing", Image: missing})
	require.ErrorIs(t, err, containerruntime.ErrImageNotFound)
}

// Operations on a container that does not exist fail with
// ErrContainerNotFound.
func TestContainerErrors_Missing(t *testing.T) {
	ctx := testContext(t)
	manager := newTestManager(t)

	grace := 1
	require.ErrorIs(t, manager.StartContainer(ctx, "ghost"), containerruntime.ErrContainerNotFound)
	require.ErrorIs(t, manager.StopContainer(ctx, "ghost", &grace), containerruntime.ErrContainerNotFound)
	require.ErrorIs(t, manager.RemoveContainer(ctx, "ghost", true), containerruntime.ErrContainerNotFound)
}

// newDir returns the one entry of after that is not in before.
func newDir(after, before []string) string {
	known := make(map[string]bool, len(before))
	for _, dir := range before {
		known[dir] = true
	}
	for _, dir := range after {
		if !known[dir] {
			return dir
		}
	}
	return ""
}
