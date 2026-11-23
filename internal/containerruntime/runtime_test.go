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
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultRuntimeConfig(t *testing.T) {
	config := DefaultRuntimeConfig()

	require.NotNil(t, config)
	assert.NotEmpty(t, config.Root)
	assert.NotEmpty(t, config.RunRoot)
	assert.NotEmpty(t, config.TmpDir)
	assert.NotEmpty(t, config.GraphDriverName)
	assert.NotEmpty(t, config.StaticDir)
	assert.NotEmpty(t, config.VolumePath)
	assert.NotEmpty(t, config.ImageStorePath)
}

func TestRuntimeConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		config    *RuntimeConfig
		expectErr bool
		errField  string
	}{
		{
			name:      "valid config",
			config:    DefaultRuntimeConfig(),
			expectErr: false,
		},
		{
			name: "missing root",
			config: &RuntimeConfig{
				Root:            "",
				RunRoot:         DefaultRunRoot,
				TmpDir:          DefaultTmpDir,
				StaticDir:       "/var/lib/containers/storage/libpod",
				VolumePath:      "/var/lib/containers/storage/volumes",
				ImageStorePath:  "/var/lib/containers/storage/images",
				GraphDriverName: DefaultGraphDriver,
			},
			expectErr: true,
			errField:  "root",
		},
		{
			name: "missing run_root",
			config: &RuntimeConfig{
				Root:            DefaultStorageRoot,
				RunRoot:         "",
				TmpDir:          DefaultTmpDir,
				StaticDir:       "/var/lib/containers/storage/libpod",
				VolumePath:      "/var/lib/containers/storage/volumes",
				ImageStorePath:  "/var/lib/containers/storage/images",
				GraphDriverName: DefaultGraphDriver,
			},
			expectErr: true,
			errField:  "run_root",
		},
		{
			name: "invalid graph driver",
			config: &RuntimeConfig{
				Root:            DefaultStorageRoot,
				RunRoot:         DefaultRunRoot,
				TmpDir:          DefaultTmpDir,
				StaticDir:       "/var/lib/containers/storage/libpod",
				VolumePath:      "/var/lib/containers/storage/volumes",
				ImageStorePath:  "/var/lib/containers/storage/images",
				GraphDriverName: "invalid_driver",
			},
			expectErr: true,
			errField:  "graph_driver",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectErr {
				require.Error(t, err)
				if tt.errField != "" {
					assert.Contains(t, err.Error(), tt.errField)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRuntimeError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *RuntimeError
		expected string
	}{
		{
			name: "with wrapped error",
			err: &RuntimeError{
				Operation: "start",
				Err:       ErrContainerNotFound,
			},
			expected: "runtime start: container not found",
		},
		{
			name: "without wrapped error",
			err: &RuntimeError{
				Operation: "initialize",
			},
			expected: "runtime initialize",
		},
		{
			name: "with custom error",
			err: &RuntimeError{
				Operation: "stop",
				Err:       errors.New("custom error"),
			},
			expected: "runtime stop: custom error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
		})
	}
}

func TestRuntimeError_Unwrap(t *testing.T) {
	wrapped := ErrContainerNotFound
	err := &RuntimeError{
		Operation: "start",
		Err:       wrapped,
	}

	assert.Equal(t, wrapped, err.Unwrap())
	assert.ErrorIs(t, err, wrapped)
}

// testRootfs returns a rootfs whose /etc/passwd and /etc/group name a
// "nobody" user and group, and an "app" user in two supplementary groups.
func testRootfs(t *testing.T) string {
	t.Helper()

	rootfs := t.TempDir()
	etc := filepath.Join(rootfs, "etc")
	require.NoError(t, os.MkdirAll(etc, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(etc, "passwd"), []byte(
		"root:x:0:0:root:/root:/bin/sh\n"+
			"nobody:x:65534:65534:nobody:/nonexistent:/bin/false\n"+
			"app:x:1000:1000:app:/home/app:/bin/sh\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(etc, "group"), []byte(
		"root:x:0:\n"+
			"nogroup:x:65534:\n"+
			"app:x:1000:\n"+
			"video:x:44:app\n"+
			"audio:x:29:app\n"), 0o644))
	return rootfs
}

func TestParseUserSpec(t *testing.T) {
	rootfs := testRootfs(t)

	tests := []struct {
		name         string
		spec         string
		expectedUID  uint32
		expectedGID  uint32
		expectedGids []uint32
	}{
		{name: "empty spec", spec: "", expectedUID: 0, expectedGID: 0},
		{name: "uid only", spec: "1000", expectedUID: 1000, expectedGID: 1000, expectedGids: []uint32{44, 29}},
		{name: "uid:gid", spec: "1000:1000", expectedUID: 1000, expectedGID: 1000},
		{name: "different uid and gid", spec: "1000:2000", expectedUID: 1000, expectedGID: 2000},
		{name: "root user", spec: "0:0", expectedUID: 0, expectedGID: 0},
		{name: "uid absent from passwd", spec: "4242", expectedUID: 4242, expectedGID: 0},
		{name: "name spec", spec: "nobody:nogroup", expectedUID: 65534, expectedGID: 65534},
		{name: "user name only", spec: "app", expectedUID: 1000, expectedGID: 1000, expectedGids: []uint32{44, 29}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseUserSpec(tt.spec, rootfs)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedUID, got.UID)
			assert.Equal(t, tt.expectedGID, got.GID)
			assert.ElementsMatch(t, tt.expectedGids, got.AdditionalGids)
		})
	}
}

func TestParseUserSpec_Invalid(t *testing.T) {
	rootfs := testRootfs(t)

	for _, spec := range []string{"notanumber", "1000:notanumber", "-1"} {
		t.Run(spec, func(t *testing.T) {
			_, err := parseUserSpec(spec, rootfs)
			require.Error(t, err)
		})
	}
}

func TestParseUserSpec_NoPasswdFiles(t *testing.T) {
	// A numeric user needs no passwd or group file.
	got, err := parseUserSpec("1000:1000", t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, ociUser{UID: 1000, GID: 1000}, got)

	_, err = parseUserSpec("app", t.TempDir())
	require.Error(t, err)
}

func TestRemoveString(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		remove   string
		expected []string
	}{
		{
			name:     "remove existing element",
			slice:    []string{"a", "b", "c"},
			remove:   "b",
			expected: []string{"a", "c"},
		},
		{
			name:     "remove first element",
			slice:    []string{"a", "b", "c"},
			remove:   "a",
			expected: []string{"b", "c"},
		},
		{
			name:     "remove last element",
			slice:    []string{"a", "b", "c"},
			remove:   "c",
			expected: []string{"a", "b"},
		},
		{
			name:     "element not in slice",
			slice:    []string{"a", "b", "c"},
			remove:   "d",
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "empty slice",
			slice:    []string{},
			remove:   "a",
			expected: []string{},
		},
		{
			name:     "single element slice",
			slice:    []string{"a"},
			remove:   "a",
			expected: []string{},
		},
		{
			name:     "duplicate elements - removes all",
			slice:    []string{"a", "b", "a", "c"},
			remove:   "a",
			expected: []string{"b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeString(tt.slice, tt.remove)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestContainerState_Fields(t *testing.T) {
	state := &containerState{
		ID:       "abc123",
		Name:     "test-container",
		Image:    "alpine:latest",
		State:    ContainerStateRunning,
		PID:      12345,
		ExitCode: 0,
	}

	assert.Equal(t, "abc123", state.ID)
	assert.Equal(t, "test-container", state.Name)
	assert.Equal(t, "alpine:latest", state.Image)
	assert.Equal(t, ContainerStateRunning, state.State)
	assert.Equal(t, 12345, state.PID)
	assert.Equal(t, 0, state.ExitCode)
}

func TestOCIConfigStructs(t *testing.T) {
	// Test OCI config structure serialization/fields
	cfg := &ociConfig{
		OCIVersion: "1.0.0",
		Root: &ociRoot{
			Path:     "/rootfs",
			Readonly: true,
		},
		Process: &ociProcess{
			Terminal: false,
			User: ociUser{
				UID: 1000,
				GID: 1000,
			},
			Args: []string{"sleep", "infinity"},
			Env:  []string{"PATH=/usr/bin"},
			Cwd:  "/app",
		},
		Hostname: "testhost",
		Mounts: []ociMount{
			{
				Destination: "/proc",
				Type:        "proc",
				Source:      "proc",
			},
		},
	}

	assert.Equal(t, "1.0.0", cfg.OCIVersion)
	require.NotNil(t, cfg.Root)
	assert.Equal(t, "/rootfs", cfg.Root.Path)
	assert.True(t, cfg.Root.Readonly)
	require.NotNil(t, cfg.Process)
	assert.False(t, cfg.Process.Terminal)
	assert.Equal(t, uint32(1000), cfg.Process.User.UID)
	assert.Equal(t, []string{"sleep", "infinity"}, cfg.Process.Args)
	assert.Equal(t, "testhost", cfg.Hostname)
	assert.Len(t, cfg.Mounts, 1)
}

func TestOCIResources(t *testing.T) {
	memLimit := int64(1073741824) // 1GB
	cpuShares := uint64(1024)
	cpuQuota := int64(100000)
	cpuPeriod := uint64(100000)

	resources := &ociResources{
		Memory: &ociMemory{
			Limit: &memLimit,
		},
		CPU: &ociCPU{
			Shares: &cpuShares,
			Quota:  &cpuQuota,
			Period: &cpuPeriod,
		},
		Pids: &ociPids{
			Limit: 100,
		},
	}

	require.NotNil(t, resources.Memory)
	require.NotNil(t, resources.Memory.Limit)
	assert.Equal(t, int64(1073741824), *resources.Memory.Limit)

	require.NotNil(t, resources.CPU)
	require.NotNil(t, resources.CPU.Shares)
	assert.Equal(t, uint64(1024), *resources.CPU.Shares)
	require.NotNil(t, resources.CPU.Quota)
	assert.Equal(t, int64(100000), *resources.CPU.Quota)
	require.NotNil(t, resources.CPU.Period)
	assert.Equal(t, uint64(100000), *resources.CPU.Period)

	require.NotNil(t, resources.Pids)
	assert.Equal(t, int64(100), resources.Pids.Limit)
}

func TestOCICapabilities(t *testing.T) {
	caps := &ociCapabilities{
		Bounding:    []string{"CAP_NET_BIND_SERVICE"},
		Effective:   []string{"CAP_NET_BIND_SERVICE"},
		Inheritable: []string{},
		Permitted:   []string{"CAP_NET_BIND_SERVICE"},
		Ambient:     []string{},
	}

	assert.Contains(t, caps.Bounding, "CAP_NET_BIND_SERVICE")
	assert.Contains(t, caps.Effective, "CAP_NET_BIND_SERVICE")
	assert.Empty(t, caps.Inheritable)
	assert.Contains(t, caps.Permitted, "CAP_NET_BIND_SERVICE")
	assert.Empty(t, caps.Ambient)
}

func TestOCINamespace(t *testing.T) {
	namespaces := []ociNamespace{
		{Type: "pid"},
		{Type: "network"},
		{Type: "ipc"},
		{Type: "uts"},
		{Type: "mount"},
	}

	assert.Len(t, namespaces, 5)
	types := make([]string, len(namespaces))
	for i, ns := range namespaces {
		types[i] = ns.Type
	}
	assert.Contains(t, types, "pid")
	assert.Contains(t, types, "network")
	assert.Contains(t, types, "ipc")
	assert.Contains(t, types, "uts")
	assert.Contains(t, types, "mount")
}

func TestOCILinuxDevice(t *testing.T) {
	device := &ociLinuxDevice{
		Path:  "/dev/tpm0",
		Type:  "c",
		Major: 10,
		Minor: 224,
	}

	assert.Equal(t, "/dev/tpm0", device.Path)
	assert.Equal(t, "c", device.Type)
	assert.Equal(t, int64(10), device.Major)
	assert.Equal(t, int64(224), device.Minor)
}

func TestRuntimeConfig_Fields(t *testing.T) {
	config := &RuntimeConfig{
		Root:            "/var/lib/containers/storage",
		RunRoot:         "/run/containers/storage",
		TmpDir:          "/run/libpod",
		GraphDriverName: "overlay",
		StaticDir:       "/var/lib/containers/storage/libpod",
		VolumePath:      "/var/lib/containers/storage/volumes",
		ImageStorePath:  "/var/lib/containers/storage/images",
	}

	assert.Equal(t, "/var/lib/containers/storage", config.Root)
	assert.Equal(t, "/run/containers/storage", config.RunRoot)
	assert.Equal(t, "/run/libpod", config.TmpDir)
	assert.Equal(t, "overlay", config.GraphDriverName)
	assert.Equal(t, "/var/lib/containers/storage/libpod", config.StaticDir)
	assert.Equal(t, "/var/lib/containers/storage/volumes", config.VolumePath)
	assert.Equal(t, "/var/lib/containers/storage/images", config.ImageStorePath)
}

func TestOCIMount_Fields(t *testing.T) {
	mount := ociMount{
		Destination: "/data",
		Type:        "bind",
		Source:      "/host/data",
		Options:     []string{"rbind", "rw"},
	}

	assert.Equal(t, "/data", mount.Destination)
	assert.Equal(t, "bind", mount.Type)
	assert.Equal(t, "/host/data", mount.Source)
	assert.Contains(t, mount.Options, "rbind")
	assert.Contains(t, mount.Options, "rw")
}

func TestOCILinux_Fields(t *testing.T) {
	linux := &ociLinux{
		Namespaces: []ociNamespace{
			{Type: "pid"},
			{Type: "mount"},
		},
		CgroupsPath:   "/containers/test",
		MaskedPaths:   []string{"/proc/kcore"},
		ReadonlyPaths: []string{"/proc/bus"},
	}

	assert.Len(t, linux.Namespaces, 2)
	assert.Equal(t, "/containers/test", linux.CgroupsPath)
	assert.Contains(t, linux.MaskedPaths, "/proc/kcore")
	assert.Contains(t, linux.ReadonlyPaths, "/proc/bus")
}

func TestOCIRlimit_Fields(t *testing.T) {
	rlimit := ociRlimit{
		Type: "RLIMIT_NOFILE",
		Hard: 1048576,
		Soft: 1048576,
	}

	assert.Equal(t, "RLIMIT_NOFILE", rlimit.Type)
	assert.Equal(t, uint64(1048576), rlimit.Hard)
	assert.Equal(t, uint64(1048576), rlimit.Soft)
}

// startAutoRemoveTest creates and starts a container, with AutoRemove as
// given, and returns its id and bundle directory.
func startAutoRemoveTest(t *testing.T, rt *Runtime, name string, autoRemove bool) (string, string) {
	t.Helper()
	state, err := rt.CreateContainer(context.Background(), &ContainerConfig{
		Name: name, Image: "alpine:latest", AutoRemove: autoRemove,
	})
	require.NoError(t, err)
	require.NoError(t, rt.StartContainer(context.Background(), state.ID))
	return state.ID, state.BundleDir
}

// requireAutoRemoved waits for an exited auto-remove container to be gone:
// from the runtime, the engine, its root filesystem, its bundle and its logs.
func requireAutoRemoved(t *testing.T, rt *Runtime, doubles *testBackends, id, name, bundleDir string) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, err := rt.InspectContainer(context.Background(), id)
		return errors.Is(err, ErrContainerNotFound)
	}, 5*time.Second, 10*time.Millisecond, "the auto-remove container is still registered")
	_, err := rt.InspectContainer(context.Background(), name)
	require.ErrorIs(t, err, ErrContainerNotFound)

	doubles.engine.mu.Lock()
	assert.NotContains(t, doubles.engine.created, id, "the engine state outlived the container")
	assert.Equal(t, []string{id}, doubles.engine.destroyed)
	doubles.engine.mu.Unlock()
	doubles.rootfs.mu.Lock()
	assert.Equal(t, []string{id}, doubles.rootfs.removed)
	doubles.rootfs.mu.Unlock()
	assert.NoDirExists(t, filepath.Join(doubles.rootfs.root, id), "the writable layer outlived the container")
	assert.NoFileExists(t, filepath.Join(bundleDir, "stdout.log"), "the log outlived the container")
	assert.NoDirExists(t, bundleDir, "the bundle outlived the container")
}

func TestRuntime_AutoRemoveAfterStop(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	id, bundleDir := startAutoRemoveTest(t, rt, "auto-remove-stop", true)
	require.FileExists(t, filepath.Join(bundleDir, "stdout.log"))

	require.NoError(t, rt.StopContainer(context.Background(), id, 10))
	requireAutoRemoved(t, rt, doubles, id, "auto-remove-stop", bundleDir)
}

func TestRuntime_AutoRemoveAfterNaturalExit(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	doubles.engine.initResult = &fakeResult{exitCode: 0}
	id, bundleDir := startAutoRemoveTest(t, rt, "auto-remove-exit", true)
	requireAutoRemoved(t, rt, doubles, id, "auto-remove-exit", bundleDir)
}

func TestRuntime_NoAutoRemoveKeepsExited(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	doubles.engine.initResult = &fakeResult{exitCode: 0}
	id, bundleDir := startAutoRemoveTest(t, rt, "keep-exited", false)
	_, err := rt.WaitContainer(context.Background(), id)
	require.NoError(t, err)

	// Give a removal that should not happen the time it would take.
	time.Sleep(100 * time.Millisecond)
	state, err := rt.InspectContainer(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, ContainerStateExited, state.State)
	assert.DirExists(t, bundleDir)
	doubles.engine.mu.Lock()
	assert.Empty(t, doubles.engine.destroyed)
	doubles.engine.mu.Unlock()
}

// mountAt returns the OCI mount the fake engine was given for target.
func mountAt(t *testing.T, doubles *testBackends, id, target string) ociMount {
	t.Helper()
	doubles.engine.mu.Lock()
	defer doubles.engine.mu.Unlock()
	oci, ok := doubles.engine.created[id]
	require.True(t, ok, "container %s was created", id)
	for _, m := range oci.Mounts {
		if m.Destination == target {
			return m
		}
	}
	require.Failf(t, "no mount", "container %s has no mount at %s", id, target)
	return ociMount{}
}

func TestRuntime_NamedVolumeIsBoundFromVolumePath(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	dir := filepath.Join(rt.Config().VolumePath, "cache")

	first, err := rt.CreateContainer(t.Context(), &ContainerConfig{
		Name: "volume-a", Image: "alpine:latest",
		Mounts: []Mount{{Type: "volume", Source: "cache", Target: "/cache"}},
	})
	require.NoError(t, err)
	m := mountAt(t, doubles, first.ID, "/cache")
	assert.Equal(t, "bind", m.Type)
	assert.Equal(t, dir, m.Source)
	assert.Contains(t, m.Options, "rbind")
	assert.NotContains(t, m.Options, "ro")
	info, err := os.Stat(dir)
	require.NoError(t, err, "the volume's directory is created")
	assert.True(t, info.IsDir())

	second, err := rt.CreateContainer(t.Context(), &ContainerConfig{
		Name: "volume-b", Image: "alpine:latest",
		Mounts: []Mount{{Type: "volume", Source: "cache", Target: "/cache", ReadOnly: true}},
	})
	require.NoError(t, err)
	m = mountAt(t, doubles, second.ID, "/cache")
	assert.Equal(t, "bind", m.Type)
	assert.Equal(t, dir, m.Source, "a second container naming the volume gets the same directory")
	assert.Contains(t, m.Options, "rbind")
	assert.Contains(t, m.Options, "ro", "a read-only volume is bound read-only")

	require.NoError(t, rt.RemoveContainer(t.Context(), first.ID, true))
	require.NoError(t, rt.RemoveContainer(t.Context(), second.ID, true))
	_, err = os.Stat(dir)
	require.NoError(t, err, "a volume outlives the containers that mount it")
}

func TestRuntime_NamedVolumeBadNameRefused(t *testing.T) {
	for _, name := range []string{"../x", "a/b", ".hidden", "/abs"} {
		t.Run(name, func(t *testing.T) {
			rt, doubles := newTestRuntimeWithBackends(t)
			_, err := rt.CreateContainer(t.Context(), &ContainerConfig{
				Name: "bad-volume", Image: "alpine:latest",
				Mounts: []Mount{{Type: "volume", Source: name, Target: "/data"}},
			})
			require.ErrorIs(t, err, ErrInvalidVolumeName)
			doubles.engine.mu.Lock()
			defer doubles.engine.mu.Unlock()
			assert.Empty(t, doubles.engine.created, "the engine created nothing")
		})
	}
}

// seedImageData returns a rootfs seed that ships, as an image would, a data
// directory at /data holding a file with the given content, a private
// subdirectory and a symbolic link to the file.
func seedImageData(content string) func(dir string) error {
	return func(dir string) error {
		data := filepath.Join(dir, "data")
		if err := os.MkdirAll(filepath.Join(data, "sub"), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(data, "seed.txt"), []byte(content), 0o640); err != nil {
			return err
		}
		if err := os.Symlink("seed.txt", filepath.Join(data, "link")); err != nil {
			return err
		}
		for name, mode := range map[string]os.FileMode{"sub": 0o700, "seed.txt": 0o640, ".": 0o750} {
			if err := os.Chmod(filepath.Join(data, name), mode); err != nil {
				return err
			}
		}
		return nil
	}
}

func setRootfsSeed(doubles *testBackends, seed func(dir string) error) {
	doubles.rootfs.mu.Lock()
	defer doubles.rootfs.mu.Unlock()
	doubles.rootfs.seed = seed
}

func TestRuntime_NamedVolumeIsInitializedFromTheImage(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	setRootfsSeed(doubles, seedImageData("from-image"))
	dir := filepath.Join(rt.Config().VolumePath, "dataset")

	first, err := rt.CreateContainer(t.Context(), &ContainerConfig{
		Name: "volume-init-a", Image: "alpine:latest",
		Mounts: []Mount{{Type: "volume", Source: "dataset", Target: "/data"}},
	})
	require.NoError(t, err)
	m := mountAt(t, doubles, first.ID, "/data")
	assert.Equal(t, "bind", m.Type)
	assert.Equal(t, dir, m.Source)

	content, err := os.ReadFile(filepath.Join(dir, "seed.txt"))
	require.NoError(t, err)
	assert.Equal(t, "from-image", string(content), "the volume holds the image's file")
	for name, mode := range map[string]os.FileMode{".": 0o750, "seed.txt": 0o640, "sub": 0o700} {
		info, err := os.Lstat(filepath.Join(dir, name))
		require.NoError(t, err)
		assert.Equal(t, mode, info.Mode().Perm(), "mode of %s", name)
	}
	target, err := os.Readlink(filepath.Join(dir, "link"))
	require.NoError(t, err)
	assert.Equal(t, "seed.txt", target)
	entries, err := os.ReadDir(rt.Config().VolumePath)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotEqual(t, '.', rune(e.Name()[0]), "a temporary directory was left: %s", e.Name())
	}

	setRootfsSeed(doubles, seedImageData("changed"))
	second, err := rt.CreateContainer(t.Context(), &ContainerConfig{
		Name: "volume-init-b", Image: "alpine:latest",
		Mounts: []Mount{{Type: "volume", Source: "dataset", Target: "/data"}},
	})
	require.NoError(t, err)
	content, err = os.ReadFile(filepath.Join(dir, "seed.txt"))
	require.NoError(t, err)
	assert.Equal(t, "from-image", string(content), "a volume with content is not filled again")

	require.NoError(t, rt.RemoveContainer(t.Context(), first.ID, true))
	require.NoError(t, rt.RemoveContainer(t.Context(), second.ID, true))
}

func TestRuntime_NamedVolumeWithoutImageContentIsEmpty(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	setRootfsSeed(doubles, seedImageData("from-image"))
	dir := filepath.Join(rt.Config().VolumePath, "scratch")

	_, err := rt.CreateContainer(t.Context(), &ContainerConfig{
		Name: "volume-empty", Image: "alpine:latest",
		Mounts: []Mount{{Type: "volume", Source: "scratch", Target: "/scratch"}},
	})
	require.NoError(t, err)
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	assert.Equal(t, os.FileMode(0o750), info.Mode().Perm())
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "the image has nothing at the target")
}

func TestRuntime_EmptyNamedVolumeIsFilledFromTheImage(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	setRootfsSeed(doubles, seedImageData("from-image"))
	dir := filepath.Join(rt.Config().VolumePath, "dataset")
	require.NoError(t, os.MkdirAll(dir, 0o750))

	_, err := rt.CreateContainer(t.Context(), &ContainerConfig{
		Name: "volume-prefilled", Image: "alpine:latest",
		Mounts: []Mount{{Type: "volume", Source: "dataset", Target: "/data"}},
	})
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(dir, "seed.txt"))
	require.NoError(t, err)
	assert.Equal(t, "from-image", string(content), "an empty volume is filled from the image")
}

func TestRuntime_NamedVolumeInitFailureRemovesTheRootfs(t *testing.T) {
	rt, doubles := newTestRuntimeWithBackends(t)
	setRootfsSeed(doubles, func(dir string) error {
		return os.MkdirAll(filepath.Join(dir, "data"), 0o750)
	})
	require.NoError(t, os.RemoveAll(rt.Config().VolumePath))
	require.NoError(t, os.WriteFile(rt.Config().VolumePath, []byte("x"), 0o600))

	_, err := rt.CreateContainer(t.Context(), &ContainerConfig{
		Name: "volume-init-fails", Image: "alpine:latest",
		Mounts: []Mount{{Type: "volume", Source: "dataset", Target: "/data"}},
	})
	require.Error(t, err)
	var rtErr *RuntimeError
	require.True(t, errors.As(err, &rtErr), "got %v", err)
	assert.Equal(t, "initialize volume dataset", rtErr.Operation)

	doubles.rootfs.mu.Lock()
	assert.Len(t, doubles.rootfs.removed, 1, "the unpacked rootfs is removed")
	doubles.rootfs.mu.Unlock()
	doubles.engine.mu.Lock()
	defer doubles.engine.mu.Unlock()
	assert.Empty(t, doubles.engine.created, "the engine created nothing")
}
