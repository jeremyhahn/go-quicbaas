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
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateOCIConfig_UserMounts(t *testing.T) {
	rt := newTestRuntime(t)
	oci := rt.generateOCIConfig(&ContainerConfig{
		Mounts: []Mount{
			{Type: "bind", Source: "/data", Target: "/srv/data", Options: []string{"rbind"}},
			{Type: "bind", Source: "/etc/app", Target: "/etc/app", ReadOnly: true},
		},
	}, "/rootfs")

	data := oci.Mounts[len(oci.Mounts)-2]
	assert.Equal(t, ociMount{Destination: "/srv/data", Type: "bind", Source: "/data", Options: []string{"rbind"}}, data)
	config := oci.Mounts[len(oci.Mounts)-1]
	assert.Equal(t, "/etc/app", config.Destination)
	assert.Equal(t, []string{"ro"}, config.Options)
}

func TestGenerateOCIConfig_CapabilityChanges(t *testing.T) {
	rt := newTestRuntime(t)
	oci := rt.generateOCIConfig(&ContainerConfig{
		CapAdd:  []string{"SYS_ADMIN", "CAP_NET_ADMIN"},
		CapDrop: []string{"KILL", "CAP_NET_RAW"},
	}, "/rootfs")

	caps := oci.Process.Capabilities
	for _, set := range [][]string{caps.Bounding, caps.Effective, caps.Permitted} {
		assert.Contains(t, set, "CAP_SYS_ADMIN")
		assert.Contains(t, set, "CAP_NET_ADMIN")
		assert.NotContains(t, set, "CAP_KILL")
		assert.NotContains(t, set, "CAP_NET_RAW")
	}
	// Added capabilities are not inherited.
	assert.NotContains(t, caps.Inheritable, "CAP_SYS_ADMIN")
}

func TestGenerateOCIConfig_PrivilegedKeepsAllCapabilities(t *testing.T) {
	rt := newTestRuntime(t)
	oci := rt.generateOCIConfig(&ContainerConfig{Privileged: true, CapDrop: []string{"KILL"}}, "/rootfs")

	assert.Nil(t, oci.Process.Capabilities)
	assert.False(t, oci.Process.NoNewPrivileges)
}

func TestStopProcess_SignalFailure(t *testing.T) {
	process := newFakeProcess(42)
	process.exit(0)

	// The process is gone but its exit has not been observed yet.
	err := stopProcess(context.Background(), process, make(chan struct{}), 0)
	var runtimeErr *RuntimeError
	require.ErrorAs(t, err, &runtimeErr)
	assert.Equal(t, "signal SIGTERM", runtimeErr.Operation)
	require.ErrorIs(t, err, os.ErrProcessDone)
}

func TestStopProcess_AlreadyExited(t *testing.T) {
	process := newFakeProcess(42)
	process.exit(0)

	require.NoError(t, stopProcess(context.Background(), process, process.exited, 0))
	assert.Empty(t, process.receivedSignals())
}

func TestOpenContainerLogs(t *testing.T) {
	dir := t.TempDir()
	logs, err := openContainerLogs(dir)
	require.NoError(t, err)
	require.NoError(t, logs.close())
	assert.FileExists(t, filepath.Join(dir, stdoutLogName))
	assert.FileExists(t, filepath.Join(dir, stderrLogName))
}

func TestOpenContainerLogs_Failures(t *testing.T) {
	_, err := openContainerLogs(filepath.Join(t.TempDir(), "missing"))
	require.ErrorIs(t, err, os.ErrNotExist)

	// A directory in place of the stderr log fails after stdout is open.
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, stderrLogName), 0o700))
	_, err = openContainerLogs(dir)
	require.Error(t, err)
}

func TestNewRuntime_InvalidConfig(t *testing.T) {
	_, err := NewRuntime(context.Background(), &RuntimeConfig{})
	require.Error(t, err)
}

func TestRuntimeCreate_InvalidConfig(t *testing.T) {
	rt := newTestRuntime(t)

	_, err := rt.CreateContainer(context.Background(), nil)
	require.ErrorIs(t, err, ErrInvalidContainerConfig)
	_, err = rt.CreateContainer(context.Background(), &ContainerConfig{Image: "alpine:latest"})
	require.Error(t, err)
}

func TestRuntimeCreate_BundleFailure(t *testing.T) {
	rt := newTestRuntime(t)
	bundles := filepath.Join(rt.config.RunRoot, "bundles")
	require.NoError(t, os.RemoveAll(bundles))
	require.NoError(t, os.WriteFile(bundles, nil, 0o600))

	_, err := rt.CreateContainer(context.Background(), &ContainerConfig{Name: "bundle", Image: "alpine:latest"})
	var runtimeErr *RuntimeError
	require.ErrorAs(t, err, &runtimeErr)
	assert.Equal(t, "create bundle", runtimeErr.Operation)
}

func TestRuntimeLogs_UnreadableLog(t *testing.T) {
	for _, name := range []string{stdoutLogName, stderrLogName} {
		t.Run(name, func(t *testing.T) {
			rt := newTestRuntime(t)
			id := createTestContainer(t, rt, "unreadable")
			state, err := rt.InspectContainer(context.Background(), id)
			require.NoError(t, err)
			require.NoError(t, os.Mkdir(filepath.Join(state.BundleDir, name), 0o700))

			_, _, err = rt.Logs(id)
			var containerErr *ContainerError
			require.ErrorAs(t, err, &containerErr)
			assert.Equal(t, "logs", containerErr.Operation)
		})
	}
}

func TestReadCgroupUint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.current")
	require.NoError(t, os.WriteFile(path, []byte("4096\n"), 0o600))

	value, ok := readCgroupUint(path)
	assert.True(t, ok)
	assert.Equal(t, uint64(4096), value)
}

func TestReadCgroupUint_Unreadable(t *testing.T) {
	dir := t.TempDir()
	_, ok := readCgroupUint(filepath.Join(dir, "missing"))
	assert.False(t, ok)

	path := filepath.Join(dir, "garbage")
	require.NoError(t, os.WriteFile(path, []byte("max\n"), 0o600))
	_, ok = readCgroupUint(path)
	assert.False(t, ok)
}

func TestReadCPUUsageUsec(t *testing.T) {
	dir := t.TempDir()
	stat := "usage_usec 1500\nuser_usec 1000\nsystem_usec 500\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cpu.stat"), []byte(stat), 0o600))

	usage, ok := readCPUUsageUsec(dir)
	assert.True(t, ok)
	assert.Equal(t, uint64(1500), usage)
}

func TestReadCPUUsageUsec_Unreadable(t *testing.T) {
	_, ok := readCPUUsageUsec(t.TempDir())
	assert.False(t, ok)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cpu.stat"), []byte("usage_usec lots\n"), 0o600))
	_, ok = readCPUUsageUsec(dir)
	assert.False(t, ok)
}

func TestToContainerID(t *testing.T) {
	id, err := toContainerID(1000)
	require.NoError(t, err)
	assert.Equal(t, uint32(1000), id)

	id, err = toContainerID(math.MaxUint32)
	require.NoError(t, err)
	assert.Equal(t, uint32(math.MaxUint32), id)
}

func TestToContainerID_OutOfRange(t *testing.T) {
	for _, value := range []int{-1, math.MaxUint32 + 1} {
		_, err := toContainerID(value)
		require.ErrorIs(t, err, ErrInvalidUser, "value %d", value)
	}
}

func TestWrappedError_Unwrap(t *testing.T) {
	err := &WrappedError{Msg: "pull", Err: errTestBackend}
	require.ErrorIs(t, err, errTestBackend)
	assert.Nil(t, (&WrappedError{Msg: "bare"}).Unwrap())
}
