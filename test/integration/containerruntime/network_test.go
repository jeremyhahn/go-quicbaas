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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/internal/containerruntime"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestJobRuntime_HostNetworkSharesTheHostNamespace runs a host-network job on
// the in-process job runtime. The job joins the agent's network namespace and
// sees the host's resolv.conf and hosts read-only, as `docker run --network
// host` gives them; a "none" job keeps a namespace of its own, and published
// ports on the host network are refused.
func TestJobRuntime_HostNetworkSharesTheHostNamespace(t *testing.T) {
	ctx := testContext(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "policy.json"), []byte(acceptAnythingPolicy), 0o600))
	config := leftoverRuntimeConfig(dir, registryHost(t))
	rt, err := containerruntime.NewRuntime(ctx, config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rt.Close()) })
	jr := containerruntime.NewJobRuntime(rt)

	image, _ := pushShellImage(t, "containerruntime/host-network")

	hostNS, err := os.Readlink("/proc/self/ns/net")
	require.NoError(t, err)

	start := func(network string) string {
		t.Helper()
		id, err := jr.Run(ctx, &orchestration.ContainerSpec{
			Network: network,
			Image:   image,
			Command: []string{"/bin/sh", "-c", "exec /bin/busybox sleep 600"},
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = jr.Remove(context.Background(), id) })
		return id
	}
	sh := func(id, script string) *orchestration.ExecResult {
		t.Helper()
		res, err := jr.Exec(ctx, id, &orchestration.ExecSpec{Cmd: []string{"/bin/sh", "-c", script}})
		require.NoError(t, err)
		return res
	}

	// The host-network job is in the agent's network namespace.
	hostID := start("host")
	res := sh(hostID, "/bin/busybox readlink /proc/self/ns/net")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, hostNS, strings.TrimSpace(res.Stdout), "the host-network job has a network namespace of its own")

	// It sees the host's resolver files, and cannot change them.
	for _, p := range []string{"/etc/resolv.conf", "/etc/hosts"} {
		want, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		res = sh(hostID, "/bin/busybox cat "+p)
		require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
		assert.Equal(t, string(want), res.Stdout, "%s is not the host's", p)
		res = sh(hostID, "/bin/busybox touch "+p)
		assert.NotEqual(t, 0, res.ExitCode, "%s is writable in the container", p)
	}

	// The control: a "none" job keeps a network namespace of its own.
	noneID := start("none")
	res = sh(noneID, "/bin/busybox readlink /proc/self/ns/net")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.NotEqual(t, hostNS, strings.TrimSpace(res.Stdout), "the none-network job shares the host's network namespace")

	// Published ports are refused on the host network too.
	_, err = jr.Run(ctx, &orchestration.ContainerSpec{
		Network: "host",
		Image:   image,
		Command: []string{"/bin/sh", "-c", "exec /bin/busybox sleep 600"},
		Ports:   []string{"8080:80"},
	})
	require.ErrorIs(t, err, containerruntime.ErrNetworkUnsupported)
}
