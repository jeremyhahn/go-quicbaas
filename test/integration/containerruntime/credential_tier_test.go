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
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/internal/containerruntime"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestJobRuntime_CredentialTierKeepsTheTokenOffDiskAndOutOfTheRecords runs a
// spec carrying the credential tier and the signing socket on the in-process
// job runtime. The tier is a sized tmpfs, the signing socket is a socket in
// the container, and a token delivered over an exec's stdin is an owner-only
// file on the tier whose bytes reach no inspect result, environment, log, host
// file or runtime directory, before and after revocation and removal.
func TestJobRuntime_CredentialTierKeepsTheTokenOffDiskAndOutOfTheRecords(t *testing.T) {
	ctx := testContext(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "policy.json"), []byte(acceptAnythingPolicy), 0o600))
	config := leftoverRuntimeConfig(dir, registryHost(t))
	rt, err := containerruntime.NewRuntime(ctx, config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rt.Close()) })
	jr := containerruntime.NewJobRuntime(rt)

	image, _ := pushShellImage(t, "containerruntime/credential-tier")

	// A unix socket path is limited to 108 bytes, which t.TempDir can exceed.
	sockDir, err := os.MkdirTemp("", "qsa-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	sockPath := filepath.Join(sockDir, "agent.sock")
	listener, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	raw := make([]byte, 32)
	_, err = rand.Read(raw)
	require.NoError(t, err)
	token := []byte("qbtok-" + hex.EncodeToString(raw))
	sum := sha256.Sum256(token)

	spec := &orchestration.ContainerSpec{
		Network: "none",
		Image:   image,
		Command: []string{"/bin/sh", "-c", "exec /bin/busybox sleep 600"},
		Env:     map[string]string{orchestration.WorkspaceSigningAgentEnv: orchestration.WorkspaceSigningAgentSocket},
		Mounts: []orchestration.ContainerMount{
			orchestration.WorkspaceCredentialMount(),
			{Source: sockPath, Target: orchestration.WorkspaceSigningAgentSocket, ReadOnly: true},
		},
	}
	id, err := jr.Run(ctx, spec)
	require.NoError(t, err)
	t.Cleanup(func() { _ = jr.Remove(context.Background(), id) })

	sh := func(script string) *orchestration.ExecResult {
		t.Helper()
		res, err := jr.Exec(ctx, id, &orchestration.ExecSpec{Cmd: []string{"/bin/sh", "-c", script}})
		require.NoError(t, err)
		return res
	}

	// The credential tier is a tmpfs of its own, sized to the tier.
	res := sh("/bin/busybox cat /proc/mounts")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	var credential []string
	for _, line := range strings.Split(res.Stdout, "\n") {
		if fields := strings.Fields(line); len(fields) >= 4 && fields[1] == orchestration.WorkspaceCredentialDir {
			credential = append(credential, line)
		}
	}
	require.Len(t, credential, 1, "mounts: %s", res.Stdout)
	fields := strings.Fields(credential[0])
	assert.Equal(t, "tmpfs", fields[2], credential[0])
	assert.Contains(t, strings.Split(fields[3], ","), "size=1024k", credential[0])
	t.Logf("credential mount: %s", credential[0])

	// The signing socket is a socket in the container, and SSH_AUTH_SOCK names it.
	res = sh("[ -S " + orchestration.WorkspaceSigningAgentSocket + " ]")
	assert.Equal(t, 0, res.ExitCode, "the signing socket is not a socket: %s", res.Stderr)
	res = sh("/bin/busybox tr '\\0' '\\n' < /proc/1/environ")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, orchestration.WorkspaceSigningAgentEnv+"="+orchestration.WorkspaceSigningAgentSocket)

	// Deliver the token as the runner does: on the exec's stdin only.
	file := orchestration.WorkspaceForgeCredentialFile
	res, err = jr.Exec(ctx, id, &orchestration.ExecSpec{
		Cmd: []string{
			"/bin/sh", "-c",
			"umask 077 && /bin/busybox rm -f -- \"$1\" && /bin/busybox cat > \"$1\" && /bin/busybox chmod 0400 \"$1\"",
			"sh", file,
		},
		Stdin: bytes.Clone(token),
	})
	require.NoError(t, err)
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.NotContains(t, res.Stdout, string(token), "the delivery's stdout carries the token")
	assert.NotContains(t, res.Stderr, string(token), "the delivery's stderr carries the token")

	// The file holds the token, checked by its digest so no output carries it.
	res = sh("/bin/busybox sha256sum " + file)
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	digest := strings.Fields(res.Stdout)
	require.NotEmpty(t, digest, "sha256sum: %s", res.Stdout)
	assert.Equal(t, hex.EncodeToString(sum[:]), digest[0])
	res = sh("/bin/busybox stat -c %a " + file)
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "400", strings.TrimSpace(res.Stdout), "the token file is not owner-only")

	// The token's bytes are in none of the runtime's records, and on no disk:
	// the vfs rootfs is on the host disk, so the walk also shows the tmpfs is
	// mounted only in the container's mount namespace.
	assertNoToken := func(stage string, running bool) {
		t.Helper()
		if running {
			state, err := rt.InspectContainer(ctx, id)
			require.NoError(t, err)
			inspected, err := json.Marshal(state)
			require.NoError(t, err)
			assert.False(t, bytes.Contains(inspected, token), "%s: the inspect result carries the token", stage)

			logs, err := jr.Logs(ctx, id)
			require.NoError(t, err)
			logBytes, err := io.ReadAll(logs)
			require.NoError(t, logs.Close())
			require.NoError(t, err)
			assert.False(t, bytes.Contains(logBytes, token), "%s: the container's logs carry the token", stage)

			res := sh("/bin/busybox tr '\\0' '\\n' < /proc/1/environ")
			require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
			assert.NotContains(t, res.Stdout, string(token), "%s: the container's environment carries the token", stage)
		}
		for _, root := range []string{config.Root, config.RunRoot, config.TmpDir, sockDir} {
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
				data, err := os.ReadFile(path)
				if err != nil {
					if errors.Is(err, fs.ErrNotExist) {
						return nil
					}
					return err
				}
				assert.False(t, bytes.Contains(data, token), "%s: the token is on disk at %s", stage, path)
				return nil
			})
			require.NoError(t, walkErr)
		}
	}
	assertNoToken("delivered", true)

	// Revoke as the runner does: remove the file.
	res = sh("/bin/busybox rm -f -- " + file)
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	res = sh("[ ! -e " + file + " ]")
	assert.Equal(t, 0, res.ExitCode, "the token file is still there after revocation")
	assertNoToken("revoked", true)

	require.NoError(t, jr.Remove(ctx, id))
	_, err = jr.Exec(ctx, id, &orchestration.ExecSpec{Cmd: []string{"/bin/busybox", "true"}})
	require.Error(t, err)
	assertNoToken("removed", false)
}
