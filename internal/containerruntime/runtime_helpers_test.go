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
	"strconv"
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
	assert.Equal(t, []string{"rbind", "ro", "rro"}, config.Options, "a read-only bind mount must be bound, recursively read-only")
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
	assert.Nil(t, oci.Linux.Seccomp)
}

func TestGenerateOCIConfig_SeccompProfile(t *testing.T) {
	rt := newTestRuntime(t)
	oci := rt.generateOCIConfig(&ContainerConfig{}, "/rootfs")
	require.NotNil(t, oci.Linux.Seccomp)
	assert.Equal(t, "SCMP_ACT_ERRNO", oci.Linux.Seccomp.DefaultAction)
	assert.False(t, allowed(oci.Linux.Seccomp, "mount"))

	admin := rt.generateOCIConfig(&ContainerConfig{CapAdd: []string{"SYS_ADMIN"}}, "/rootfs")
	require.NotNil(t, admin.Linux.Seccomp)
	assert.True(t, allowed(admin.Linux.Seccomp, "mount"))
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

func TestGenerateOCIConfig_SandboxDropsAllCapabilities(t *testing.T) {
	spec := &orchestration.ContainerSpec{Network: "none", Image: "alpine:latest", User: "1000:1000"}
	require.NoError(t, orchestration.SandboxSecurity{}.Apply(spec))
	config, err := jobContainerConfig(spec)
	require.NoError(t, err)
	oci := newTestRuntime(t).generateOCIConfig(config, "/rootfs")

	want := []string{"CAP_CHOWN", "CAP_DAC_OVERRIDE", "CAP_FOWNER", "CAP_SETGID", "CAP_SETUID"}
	caps := oci.Process.Capabilities
	require.NotNil(t, caps)
	assert.ElementsMatch(t, want, caps.Bounding)
	assert.ElementsMatch(t, want, caps.Effective)
	assert.ElementsMatch(t, want, caps.Permitted)
	assert.Empty(t, caps.Inheritable)
	assert.Empty(t, caps.Ambient)
	for _, dropped := range []string{"CAP_NET_RAW", "CAP_MKNOD", "CAP_SETFCAP", "CAP_KILL"} {
		assert.NotContains(t, caps.Bounding, dropped)
	}
	assert.False(t, oci.Process.NoNewPrivileges, "no_new_privs would stop a setuid sudo")
	assert.Equal(t, "1000:1000", config.User)
}

func TestGenerateOCIConfig_SandboxSecuritySeccomp(t *testing.T) {
	ociFor := func(policy orchestration.SandboxSecurity) *ociConfig {
		t.Helper()
		spec := &orchestration.ContainerSpec{Network: "none", Image: "alpine:latest", User: "1000:1000"}
		require.NoError(t, policy.Apply(spec))
		config, err := jobContainerConfig(spec)
		require.NoError(t, err)
		oci := newTestRuntime(t).generateOCIConfig(config, "/rootfs")
		require.NotNil(t, oci.Linux.Seccomp)
		return oci
	}

	// CAP_SYS_CHROOT and CAP_SYS_ADMIN are dropped, so are their syscalls.
	sandboxed := ociFor(orchestration.SandboxSecurity{})
	assert.False(t, allowed(sandboxed.Linux.Seccomp, "chroot"))
	assert.False(t, allowed(sandboxed.Linux.Seccomp, "mount"))

	withChroot := ociFor(orchestration.SandboxSecurity{CapAdd: []string{"SYS_CHROOT"}})
	assert.True(t, allowed(withChroot.Linux.Seccomp, "chroot"))
}

func TestCapabilitySet(t *testing.T) {
	defaults := []string{"CAP_CHOWN", "CAP_KILL", "CAP_NET_RAW"}
	assert.Equal(t, defaults, capabilitySet(defaults, nil, nil))
	assert.Equal(t, []string{"CAP_SETUID"}, capabilitySet(defaults, []string{"setuid"}, []string{"all"}))
	assert.Equal(t, []string{"CAP_CHOWN", "CAP_SYS_ADMIN"},
		capabilitySet(defaults, []string{"SYS_ADMIN", "CAP_CHOWN"}, []string{"KILL", "CAP_NET_RAW"}))
	assert.Empty(t, capabilitySet(defaults, nil, []string{"ALL"}))
	assert.Equal(t, []string{"CAP_CHOWN", "CAP_KILL", "CAP_NET_RAW"}, defaults, "the defaults were changed")
}

func TestGenerateOCIConfig_NoNewPrivilegesIsExplicit(t *testing.T) {
	rt := newTestRuntime(t)
	assert.True(t, rt.generateOCIConfig(&ContainerConfig{}, "/rootfs").Process.NoNewPrivileges)
	assert.False(t, rt.generateOCIConfig(&ContainerConfig{AllowNewPrivileges: true}, "/rootfs").Process.NoNewPrivileges)
	assert.False(t, rt.generateOCIConfig(&ContainerConfig{Privileged: true}, "/rootfs").Process.NoNewPrivileges)
}

// TestGenerateOCIConfig_HostNetworkSharesTheHostNamespace pins that a
// host-network container joins the agent's network namespace and sees the
// host's resolver files read-only, while every other network mode keeps a
// namespace of its own.
func TestGenerateOCIConfig_HostNetworkSharesTheHostNamespace(t *testing.T) {
	rt := newTestRuntime(t)
	namespaceTypes := func(oci *ociConfig) []string {
		var types []string
		for _, ns := range oci.Linux.Namespaces {
			types = append(types, ns.Type)
		}
		return types
	}
	resolverFiles := []string{"/etc/resolv.conf", "/etc/hosts"}
	hasDestination := func(oci *ociConfig, dest string) bool {
		for _, m := range oci.Mounts {
			if m.Destination == dest {
				return true
			}
		}
		return false
	}

	for _, mode := range []string{"", "none", "bridge"} {
		t.Run("network="+mode, func(t *testing.T) {
			oci := rt.generateOCIConfig(&ContainerConfig{NetworkMode: mode}, "/rootfs")
			assert.Equal(t, []string{"pid", "network", "ipc", "uts", "mount"}, namespaceTypes(oci))
			for _, p := range resolverFiles {
				assert.False(t, hasDestination(oci, p), "%s is mounted", p)
			}
		})
	}

	t.Run("network=host", func(t *testing.T) {
		oci := rt.generateOCIConfig(&ContainerConfig{NetworkMode: "host"}, "/rootfs")
		assert.Equal(t, []string{"pid", "ipc", "uts", "mount"}, namespaceTypes(oci))
		for _, p := range resolverFiles {
			if _, err := os.Stat(p); err == nil {
				assert.Contains(t, oci.Mounts, ociMount{Destination: p, Type: "bind", Source: p, Options: []string{"rbind", "ro", "rro"}},
					"the host's %s is bound read-only", p)
			} else {
				assert.False(t, hasDestination(oci, p), "%s is mounted though the host has none", p)
			}
		}
	})
}

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

	mounts := newTestRuntime(t).generateOCIConfig(config, "/rootfs").Mounts
	require.GreaterOrEqual(t, len(mounts), 3)
	mounts = mounts[len(mounts)-3:]
	assert.Equal(t, ociMount{Destination: "/workspace", Type: "bind", Source: "/srv/workspace", Options: []string{"rbind"}}, mounts[0])
	assert.Equal(t, ociMount{Destination: "/home/agent/.creds", Type: "bind", Source: "/srv/creds", Options: []string{"rbind", "ro", "rro"}}, mounts[1])
	assert.Equal(t, "tmpfs", mounts[2].Type)
	assert.NotContains(t, mounts[2].Options, "rbind")
}

// TestJobContainerConfig_TmpfsMountHasATmpfsSource pins that a job's tmpfs
// mount reaches the OCI configuration with its source naming the filesystem,
// as the default /dev mount does, and with only the size the spec gives it.
func TestJobContainerConfig_TmpfsMountHasATmpfsSource(t *testing.T) {
	config, err := jobContainerConfig(&orchestration.ContainerSpec{
		Network: "none",
		Image:   "alpine:latest",
		Mounts: []orchestration.ContainerMount{
			{Target: "/workspace", Type: "tmpfs", TmpfsSizeBytes: 8 << 20},
		},
	})
	require.NoError(t, err)

	mounts := newTestRuntime(t).generateOCIConfig(config, "/rootfs").Mounts
	require.NotEmpty(t, mounts)
	assert.Equal(t, ociMount{Destination: "/workspace", Type: "tmpfs", Source: "tmpfs", Options: []string{"size=8388608"}}, mounts[len(mounts)-1])
}

// TestJobContainerConfig_EphemeralWorkspaceTier runs a spec that
// orchestration.ApplyWorkspaceStorage put on the ephemeral tier through the
// job runtime's configuration: the checkout becomes a sized tmpfs with no host
// bind left, the shared module cache stays a bind, and the memory limit is the
// plan's.
func TestJobContainerConfig_EphemeralWorkspaceTier(t *testing.T) {
	newSpec := func() *orchestration.ContainerSpec {
		return &orchestration.ContainerSpec{
			Network: "none",
			Image:   "alpine:latest",
			Mounts: []orchestration.ContainerMount{
				{Source: "/srv/checkout", Target: "/workspace"},
				{Source: "/srv/gomodcache", Target: "/go/pkg/mod", Type: "bind"},
			},
		}
	}

	t.Run("the checkout is a sized tmpfs inside the planned memory limit", func(t *testing.T) {
		spec := newSpec()
		require.NoError(t, orchestration.ApplyWorkspaceStorage(spec, "/workspace", orchestration.ProjectStorage{
			WorkspaceTier:     orchestration.WorkspaceTierEphemeral,
			WorkspaceTmpfsMB:  64,
			ContainerMemoryMB: 1024 + 128,
		}.Plan()))

		config, err := jobContainerConfig(spec)
		require.NoError(t, err)
		oci := newTestRuntime(t).generateOCIConfig(config, "/rootfs")

		var workspace []ociMount
		for _, m := range oci.Mounts {
			assert.NotEqual(t, "/srv/checkout", m.Source, "the checkout's host directory is still mounted")
			if m.Destination == "/workspace" {
				workspace = append(workspace, m)
			}
		}
		require.Len(t, workspace, 1, "exactly one mount at the checkout target")
		assert.Equal(t, ociMount{Destination: "/workspace", Type: "tmpfs", Source: "tmpfs", Options: []string{"size=67108864"}}, workspace[0])

		assert.Contains(t, oci.Mounts, ociMount{Destination: "/go/pkg/mod", Type: "bind", Source: "/srv/gomodcache", Options: []string{"rbind"}},
			"the shared module cache stays a read-write bind")

		require.NotNil(t, oci.Linux.Resources)
		require.NotNil(t, oci.Linux.Resources.Memory)
		require.NotNil(t, oci.Linux.Resources.Memory.Limit)
		assert.Equal(t, int64(1024+128)<<20, *oci.Linux.Resources.Memory.Limit)
	})

	t.Run("a limit below the tmpfs plus the build headroom is refused before the runtime", func(t *testing.T) {
		spec := newSpec()
		err := orchestration.ApplyWorkspaceStorage(spec, "/workspace", orchestration.ProjectStorage{
			WorkspaceTier:     orchestration.WorkspaceTierEphemeral,
			WorkspaceTmpfsMB:  64,
			ContainerMemoryMB: int(orchestration.WorkspaceBuildHeadroomBytes>>20) + 63,
		}.Plan())
		var budget *orchestration.MemoryBudgetError
		require.ErrorAs(t, err, &budget)
		assert.Equal(t, int64(64)<<20, budget.TmpfsBytes)
		assert.Equal(t, orchestration.WorkspaceBuildHeadroomBytes, budget.HeadroomBytes)
	})
}

// TestJobContainerConfig_CredentialTier pins the credential tier and the
// signing socket on the job runtime: the tier is a sized tmpfs with no host
// source, and the signing socket is a read-only bind. Neither carries a
// secret in the configuration.
func TestJobContainerConfig_CredentialTier(t *testing.T) {
	spec := &orchestration.ContainerSpec{
		Network: "none",
		Image:   "alpine:latest",
		Env:     map[string]string{orchestration.WorkspaceSigningAgentEnv: orchestration.WorkspaceSigningAgentSocket},
		Mounts: []orchestration.ContainerMount{
			orchestration.WorkspaceCredentialMount(),
			{Source: "/run/q/s1/agent.sock", Target: orchestration.WorkspaceSigningAgentSocket, ReadOnly: true},
		},
	}

	config, err := jobContainerConfig(spec)
	require.NoError(t, err)
	oci := newTestRuntime(t).generateOCIConfig(config, "/rootfs")

	assert.Contains(t, oci.Mounts, ociMount{
		Destination: orchestration.WorkspaceCredentialDir,
		Type:        "tmpfs",
		Source:      "tmpfs",
		Options:     []string{"size=" + strconv.FormatInt(orchestration.WorkspaceCredentialTmpfsBytes, 10)},
	}, "the credential tier is a sized tmpfs with no host source")
	assert.Contains(t, oci.Mounts, ociMount{
		Destination: orchestration.WorkspaceSigningAgentSocket,
		Type:        "bind",
		Source:      "/run/q/s1/agent.sock",
		Options:     []string{"rbind", "ro", "rro"},
	}, "the signing socket is a read-only bind")

	credentialMounts := 0
	for _, m := range oci.Mounts {
		if m.Destination == orchestration.WorkspaceCredentialDir {
			credentialMounts++
		}
	}
	assert.Equal(t, 1, credentialMounts, "exactly one mount at the credential tier")
}

func TestJobContainerConfig_UntypedNamedSourceIsAVolume(t *testing.T) {
	config, err := jobContainerConfig(&orchestration.ContainerSpec{
		Network: "none",
		Image:   "alpine:latest",
		Mounts: []orchestration.ContainerMount{
			{Source: "cache", Target: "/cache"},
			{Source: "/srv/x", Target: "/x"},
			{Source: "cache", Target: "/c2", Type: "bind"},
		},
	})
	require.NoError(t, err)
	require.Len(t, config.Mounts, 3)
	assert.Equal(t, "volume", config.Mounts[0].Type, "an untyped named source is a managed volume")
	assert.Equal(t, "bind", config.Mounts[1].Type, "an untyped absolute source is a host bind")
	assert.Equal(t, "bind", config.Mounts[2].Type, "an explicit type is kept")
}

func TestGenerateOCIConfig_ReadOnlyRbindIsRecursive(t *testing.T) {
	tests := []struct {
		name  string
		mount Mount
		want  []string
	}{
		{name: "ReadOnlyWithNoOptions", mount: Mount{ReadOnly: true}, want: []string{"rbind", "ro", "rro"}},
		{name: "RoOptionOnAnRbind", mount: Mount{Options: []string{"rbind", "ro"}}, want: []string{"rbind", "ro", "rro"}},
		{name: "ReadOnlyBindIsNotRecursive", mount: Mount{Options: []string{"bind"}, ReadOnly: true}, want: []string{"bind", "ro"}},
		{name: "WritableRbind", mount: Mount{Options: []string{"rbind"}}, want: []string{"rbind"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.mount
			m.Type, m.Source, m.Target = "bind", "/srv/data", "/data"
			mounts := newTestRuntime(t).generateOCIConfig(&ContainerConfig{Mounts: []Mount{m}}, "/rootfs").Mounts
			assert.Equal(t, tt.want, mounts[len(mounts)-1].Options)
		})
	}
}

func TestGenerateOCIConfig_MountOptionsAreNotShared(t *testing.T) {
	config := &ContainerConfig{Mounts: []Mount{{Type: "bind", Source: "/a", Target: "/b", Options: make([]string, 1, 4), ReadOnly: true}}}
	config.Mounts[0].Options[0] = "rbind"
	rt := newTestRuntime(t)
	rt.generateOCIConfig(config, "/rootfs")
	mounts := rt.generateOCIConfig(config, "/rootfs").Mounts
	assert.Equal(t, []string{"rbind"}, config.Mounts[0].Options, "the caller's mount options were changed")
	assert.Equal(t, []string{"rbind", "ro", "rro"}, mounts[len(mounts)-1].Options)
}
