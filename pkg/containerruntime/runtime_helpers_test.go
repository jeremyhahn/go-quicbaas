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
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
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
	assert.Equal(t, []string{"rbind", "ro", "rro"}, config.Options, "a bind mount without a bind option would not be bound")
}

func TestGenerateOCIConfig_ReadOnlyRbindIsRecursive(t *testing.T) {
	tests := []struct {
		name  string
		mount Mount
		want  []string
	}{
		{"ReadOnlyWithNoOptions", Mount{ReadOnly: true}, []string{"rbind", "ro", "rro"}},
		{"RoOptionOnAnRbind", Mount{Options: []string{"rbind", "ro"}}, []string{"rbind", "ro", "rro"}},
		{"ReadOnlyBindIsNotRecursive", Mount{Options: []string{"bind"}, ReadOnly: true}, []string{"bind", "ro"}},
		{"WritableRbind", Mount{Options: []string{"rbind"}}, []string{"rbind"}},
	}
	rt := newTestRuntime(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.mount
			m.Type = "bind"
			m.Source = "/srv/data"
			m.Target = "/data"
			oci := rt.generateOCIConfig(&ContainerConfig{Mounts: []Mount{m}}, "/rootfs")
			assert.Equal(t, tt.want, oci.Mounts[len(oci.Mounts)-1].Options)
		})
	}
}

// A job's host mounts reach libcontainer as bind mounts that request MS_BIND:
// a typed one, and an untyped one, which defaults to bind.
func TestJobContainerConfig_HostMountsAreBound(t *testing.T) {
	config, err := jobContainerConfig(&orchestration.ContainerSpec{
		Network: "none",
		Image:   "alpine:latest",
		Mounts: []orchestration.ContainerMount{
			{Source: "/srv/workspace", Target: "/workspace"},
			{Source: "/srv/creds", Target: "/home/agent/.creds", Type: "bind", ReadOnly: true},
			{Target: "/tmp", Type: "tmpfs", TmpfsSizeBytes: 1024},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "bind", config.Mounts[0].Type, "an untyped host mount stayed untyped")

	rt := newTestRuntime(t)
	mounts := rt.generateOCIConfig(config, "/rootfs").Mounts
	mounts = mounts[len(mounts)-3:]
	assert.Equal(t, ociMount{Destination: "/workspace", Type: "bind", Source: "/srv/workspace", Options: []string{"rbind"}}, mounts[0])
	assert.Equal(t, ociMount{Destination: "/home/agent/.creds", Type: "bind", Source: "/srv/creds", Options: []string{"rbind", "ro", "rro"}}, mounts[1])
	assert.Equal(t, "tmpfs", mounts[2].Type)
	assert.NotContains(t, mounts[2].Options, "rbind")
}

func TestJobContainerConfig_Network(t *testing.T) {
	for _, network := range []string{"", "bridge", "custom-net"} {
		t.Run("Refuses"+network, func(t *testing.T) {
			_, err := jobContainerConfig(&orchestration.ContainerSpec{Network: network, Image: "alpine:latest"})
			var invalid *orchestration.InputValidationError
			require.ErrorAs(t, err, &invalid)
			assert.Equal(t, "network", invalid.Field)
			assert.ErrorContains(t, err, ErrJobSpecUnsupported.Error())
		})
	}

	t.Run("NoneKeepsItsOwnNamespace", func(t *testing.T) {
		config, err := jobContainerConfig(&orchestration.ContainerSpec{Network: "none", Image: "alpine:latest"})
		require.NoError(t, err)
		oci := newTestRuntime(t).generateOCIConfig(config, "/rootfs")
		assert.True(t, slices.ContainsFunc(oci.Linux.Namespaces, func(ns ociNamespace) bool { return ns.Type == "network" }))
		assert.False(t, slices.ContainsFunc(oci.Mounts, func(m ociMount) bool { return m.Destination == "/etc/resolv.conf" }))
	})

	t.Run("HostSharesTheHostNetwork", func(t *testing.T) {
		config, err := jobContainerConfig(&orchestration.ContainerSpec{Network: "host", Image: "alpine:latest", Ports: []string{"8080:8080", "53:53/udp"}})
		require.NoError(t, err)
		oci := newTestRuntime(t).generateOCIConfig(config, "/rootfs")
		assert.False(t, slices.ContainsFunc(oci.Linux.Namespaces, func(ns ociNamespace) bool { return ns.Type == "network" }))
		for _, file := range []string{"/etc/resolv.conf", "/etc/hosts"} {
			assert.Contains(t, oci.Mounts, ociMount{Destination: file, Type: "bind", Source: file, Options: []string{"rbind", "ro"}})
		}
	})

	t.Run("PortsNeedTheHostNetwork", func(t *testing.T) {
		_, err := jobContainerConfig(&orchestration.ContainerSpec{Network: "none", Image: "alpine:latest", Ports: []string{"127.0.0.1:8080:80/udp"}})
		var invalid *orchestration.InputValidationError
		require.ErrorAs(t, err, &invalid)
		assert.Equal(t, "ports[0]", invalid.Field)
		assert.ErrorContains(t, err, ErrJobSpecUnsupported.Error())
	})

	t.Run("AHostPortCannotBeRemapped", func(t *testing.T) {
		_, err := jobContainerConfig(&orchestration.ContainerSpec{Network: "host", Image: "alpine:latest", Ports: []string{"8080:80"}})
		var invalid *orchestration.InputValidationError
		require.ErrorAs(t, err, &invalid)
		assert.Equal(t, "ports[0]", invalid.Field)
	})

	t.Run("AHostPortCannotBeBoundToAnAddress", func(t *testing.T) {
		for _, port := range []string{"127.0.0.1:8080:8080", "0.0.0.0:8080:8080"} {
			_, err := jobContainerConfig(&orchestration.ContainerSpec{Network: "host", Image: "alpine:latest", Ports: []string{port}})
			var invalid *orchestration.InputValidationError
			require.ErrorAs(t, err, &invalid, port)
			assert.Equal(t, "ports[0]", invalid.Field, port)
			assert.ErrorContains(t, err, ErrJobSpecUnsupported.Error(), port)
		}
	})
}

// The sandbox policy drops ALL and adds back its set: the OCI configuration
// carries exactly that set, and nothing inheritable or ambient.
func TestGenerateOCIConfig_SandboxDropsAllCapabilities(t *testing.T) {
	spec := &orchestration.ContainerSpec{Network: "none", Image: "alpine:latest", User: "1000:1000"}
	require.NoError(t, orchestration.SandboxSecurity{}.Apply(spec))
	config, err := jobContainerConfig(spec)
	require.NoError(t, err)

	rt := newTestRuntime(t)
	oci := rt.generateOCIConfig(config, "/rootfs")
	want := []string{"CAP_CHOWN", "CAP_DAC_OVERRIDE", "CAP_FOWNER", "CAP_SETGID", "CAP_SETUID"}
	caps := oci.Process.Capabilities
	assert.ElementsMatch(t, want, caps.Bounding)
	assert.ElementsMatch(t, want, caps.Effective)
	assert.ElementsMatch(t, want, caps.Permitted)
	assert.Empty(t, caps.Inheritable)
	assert.Empty(t, caps.Ambient)
	for _, gone := range []string{"CAP_NET_RAW", "CAP_MKNOD", "CAP_SETFCAP", "CAP_KILL"} {
		assert.NotContains(t, caps.Bounding, gone)
	}

	// A non-root worker keeps sudo: no_new_privs is clear, and the bounding
	// set a setuid-root sudo gains holds SETUID and SETGID.
	assert.False(t, oci.Process.NoNewPrivileges, "no_new_privs would stop a setuid sudo")
	assert.Equal(t, "1000:1000", config.User)
}

func TestCapabilitySet(t *testing.T) {
	defaults := []string{"CAP_CHOWN", "CAP_KILL", "CAP_NET_RAW"}
	assert.Equal(t, []string{"CAP_CHOWN", "CAP_KILL", "CAP_NET_RAW"}, capabilitySet(defaults, nil, nil))
	assert.Equal(t, []string{"CAP_SETUID"}, capabilitySet(defaults, []string{"setuid"}, []string{"all"}))
	assert.Equal(t, []string{"CAP_CHOWN", "CAP_SYS_ADMIN"}, capabilitySet(defaults, []string{"SYS_ADMIN", "CAP_CHOWN"}, []string{"KILL", "CAP_NET_RAW"}))
	assert.Empty(t, capabilitySet(defaults, nil, []string{"ALL"}))
	assert.Equal(t, []string{"CAP_CHOWN", "CAP_KILL", "CAP_NET_RAW"}, defaults, "the defaults were changed")
}

func TestGenerateOCIConfig_NoNewPrivilegesIsExplicit(t *testing.T) {
	rt := newTestRuntime(t)
	assert.True(t, rt.generateOCIConfig(&ContainerConfig{}, "/rootfs").Process.NoNewPrivileges)
	assert.False(t, rt.generateOCIConfig(&ContainerConfig{AllowNewPrivileges: true}, "/rootfs").Process.NoNewPrivileges)
	assert.False(t, rt.generateOCIConfig(&ContainerConfig{Privileged: true}, "/rootfs").Process.NoNewPrivileges)
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

func TestStopProcess_AFinishedProcessWaitsForItsExitRecord(t *testing.T) {
	process := newFakeProcess(42)
	process.exit(0)

	// The process is gone but its exit has not been recorded yet: the stop
	// waits for the record rather than failing on the signal.
	exited := make(chan struct{})
	go func() {
		time.Sleep(10 * time.Millisecond)
		close(exited)
	}()
	require.NoError(t, stopProcess(context.Background(), process, exited, 0))

	// A record that never comes ends with the caller's context.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := stopProcess(ctx, process, make(chan struct{}), 0)
	require.ErrorIs(t, err, context.DeadlineExceeded)
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
