//go:build mcp

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

package mcp

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gossh "golang.org/x/crypto/ssh"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/sshfake"
	sshclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
)

// sshMockService exposes an SSH client to the tool registry.
type sshMockService struct {
	mockService
	ssh xkmsssh.Client
}

func (m *sshMockService) SSHClient() xkmsssh.Client { return m.ssh }

// newSSHRegistry builds a ToolRegistry whose SSH tools talk to client.
func newSSHRegistry(t *testing.T, client xkmsssh.Client) *ToolRegistry {
	t.Helper()
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	return NewToolRegistry(&sshMockService{ssh: client}, log)
}

// =============================================================================
// Registration + authorization
// =============================================================================

// TestSSHToolsAreRegistered proves every SSH tool reaches tools/list. A tool
// that is dispatched but not registered is invisible to MCP clients.
func TestSSHToolsAreRegistered(t *testing.T) {
	registry := newSSHRegistry(t, sshfake.NewClient())
	registered := map[string]bool{}
	for _, tool := range registry.tools {
		registered[tool.Name] = true
	}
	for name := range sshToolHandlers {
		assert.True(t, registered[name], "%s is dispatchable but never registered", name)
	}
}

// TestSSHToolsHaveQRDBPermissions is the authz guard. authz_qrdb.go is
// default-deny AND filters tools/list, so an unmapped tool ships dead. The
// (resource, action) pairs must match the REST PEP, gRPC, and QUIC maps.
func TestSSHToolsHaveQRDBPermissions(t *testing.T) {
	want := map[string][2]string{
		"quicbaas_ssh_key_generate":      {"keys", "create"},
		"quicbaas_ssh_key_import":        {"keys", "import"},
		"quicbaas_ssh_key_get":           {"keys", "get"},
		"quicbaas_ssh_key_list":          {"keys", "list"},
		"quicbaas_ssh_key_delete":        {"keys", "delete"},
		"quicbaas_ssh_key_export":        {"keys", "get"},
		"quicbaas_ssh_key_sign":          {"keys", "sign"},
		"quicbaas_ssh_known_host_add":    {"keys", "create"},
		"quicbaas_ssh_known_host_list":   {"keys", "list"},
		"quicbaas_ssh_known_host_remove": {"keys", "delete"},
		"quicbaas_ssh_authorized_add":    {"keys", "create"},
		"quicbaas_ssh_authorized_list":   {"keys", "list"},
		"quicbaas_ssh_authorized_remove": {"keys", "delete"},
		"quicbaas_ssh_authorized_emit":   {"keys", "get"},
	}
	for name := range sshToolHandlers {
		pair, expected := want[name]
		require.True(t, expected, "%s has no expected permission in this test", name)

		perm, ok := qrdbToolExactMapping[name]
		require.True(t, ok, "%s is unmapped in authz_qrdb.go and therefore default-denied", name)
		assert.Equal(t, pair[0], perm.resource, "%s resource", name)
		assert.Equal(t, pair[1], perm.action, "%s action", name)
	}
}

// =============================================================================
// Happy paths
// =============================================================================

func TestSSHTools_KeyLifecycle(t *testing.T) {
	fake := sshfake.NewClient()
	registry := newSSHRegistry(t, fake)
	ctx := context.Background()

	res, err := registry.executeSSHTool(ctx, "quicbaas_ssh_key_generate", map[string]interface{}{
		"scope": "user", "owner": "alice", "name": "laptop", "algorithm": "ed25519",
	})
	require.NoError(t, err)
	assert.Equal(t, "laptop", res.(*xkmsssh.Key).Name)

	res, err = registry.executeSSHTool(ctx, "quicbaas_ssh_key_list", map[string]interface{}{
		"scope": "user", "owner": "alice",
	})
	require.NoError(t, err)
	require.Len(t, res.(*sshclient.ListKeysResponse).Items, 1)

	res, err = registry.executeSSHTool(ctx, "quicbaas_ssh_key_export", map[string]interface{}{
		"scope": "user", "owner": "alice", "name": "laptop",
	})
	require.NoError(t, err)
	assert.Contains(t, res.(*xkmsssh.PublicKey).Line, "ssh-ed25519")

	res, err = registry.executeSSHTool(ctx, "quicbaas_ssh_key_delete", map[string]interface{}{
		"scope": "user", "owner": "alice", "name": "laptop",
	})
	require.NoError(t, err)
	assert.True(t, res.(*sshclient.AckResponse).Success)
}

func TestSSHTools_ImportDecodesBase64Arguments(t *testing.T) {
	fake := sshfake.NewClient()
	registry := newSSHRegistry(t, fake)

	_, err := registry.executeSSHTool(context.Background(), "quicbaas_ssh_key_import", map[string]interface{}{
		"scope": "user", "owner": "alice", "name": "imported",
		"pem_b64":        base64.StdEncoding.EncodeToString([]byte("pem-bytes")),
		"passphrase_b64": base64.StdEncoding.EncodeToString([]byte("s3cret")),
	})
	require.NoError(t, err)
	assert.Equal(t, "s3cret", fake.LastImportPassphrase)
}

func TestSSHTools_SignDecodesBase64Digest(t *testing.T) {
	fake := sshfake.NewClient()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	sshPub, err := gossh.NewPublicKey(pub)
	require.NoError(t, err)
	fake.SignerPublicKey = sshPub

	registry := newSSHRegistry(t, fake)
	res, err := registry.executeSSHTool(context.Background(), "quicbaas_ssh_key_sign", map[string]interface{}{
		"scope": "user", "owner": "alice", "name": "laptop",
		"digest_b64": base64.StdEncoding.EncodeToString([]byte("digest")),
	})
	require.NoError(t, err)
	assert.Equal(t, "ssh-ed25519", res.(*sshclient.SignResponse).Format)
}

func TestSSHTools_KnownHostsAndAuthorized(t *testing.T) {
	registry := newSSHRegistry(t, sshfake.NewClient())
	ctx := context.Background()

	_, err := registry.executeSSHTool(ctx, "quicbaas_ssh_known_host_add", map[string]interface{}{
		"scope": "user", "owner": "alice", "host": "git.example.com", "fingerprint": "SHA256:abc",
	})
	require.NoError(t, err)

	res, err := registry.executeSSHTool(ctx, "quicbaas_ssh_known_host_list", map[string]interface{}{
		"scope": "user", "owner": "alice",
	})
	require.NoError(t, err)
	require.Len(t, res.(*sshclient.ListKnownHostsResponse).Items, 1)

	_, err = registry.executeSSHTool(ctx, "quicbaas_ssh_authorized_add", map[string]interface{}{
		"scope": "user", "owner": "alice", "name": "deploy", "line": "ssh-ed25519 AAAA",
	})
	require.NoError(t, err)

	res, err = registry.executeSSHTool(ctx, "quicbaas_ssh_authorized_list", map[string]interface{}{
		"scope": "user", "owner": "alice",
	})
	require.NoError(t, err)
	require.Len(t, res.(*sshclient.ListAuthorizedKeysResponse).Items, 1)

	res, err = registry.executeSSHTool(ctx, "quicbaas_ssh_authorized_emit", map[string]interface{}{
		"public_key": "ssh-ed25519 AAAA", "comment": "alice",
	})
	require.NoError(t, err)
	assert.Equal(t, "ssh-ed25519 AAAA alice", res.(*sshclient.EmitResponse).Line)

	_, err = registry.executeSSHTool(ctx, "quicbaas_ssh_authorized_remove", map[string]interface{}{
		"scope": "user", "owner": "alice", "name": "deploy",
	})
	require.NoError(t, err)
}

// =============================================================================
// Error paths
// =============================================================================

func TestSSHTools_UnknownTool(t *testing.T) {
	registry := newSSHRegistry(t, sshfake.NewClient())
	_, err := registry.executeSSHTool(context.Background(), "quicbaas_ssh_teleport", nil)
	require.Error(t, err)
}

func TestSSHTools_InvalidBase64IsTyped(t *testing.T) {
	registry := newSSHRegistry(t, sshfake.NewClient())

	_, err := registry.executeSSHTool(context.Background(), "quicbaas_ssh_key_import", map[string]interface{}{
		"name": "k", "pem_b64": "!!!not base64!!!",
	})
	require.Error(t, err)
	var b64Err *SSHInvalidBase64Error
	require.ErrorAs(t, err, &b64Err)
	assert.Equal(t, "pem_b64", b64Err.Field)

	_, err = registry.executeSSHTool(context.Background(), "quicbaas_ssh_key_sign", map[string]interface{}{
		"name": "k", "digest_b64": "!!!not base64!!!",
	})
	require.ErrorAs(t, err, &b64Err)
	assert.Equal(t, "digest_b64", b64Err.Field)
}

func TestSSHTools_NotConfigured(t *testing.T) {
	// A nil SSH client makes every tool fail closed.
	registry := newSSHRegistry(t, nil)
	for name := range sshToolHandlers {
		t.Run(name, func(t *testing.T) {
			_, err := registry.executeSSHTool(context.Background(), name, map[string]interface{}{
				"scope": "user", "owner": "alice", "name": "k", "host": "h",
				"fingerprint": "f", "line": "l", "public_key": "p",
			})
			require.Error(t, err, "%s must fail closed when ssh is not configured", name)
		})
	}
}

func TestSSHTools_BackendErrorPropagates(t *testing.T) {
	fake := sshfake.NewClient()
	fake.Err = &xkmsssh.NotFoundError{Resource: "key", Message: "absent"}
	registry := newSSHRegistry(t, fake)

	_, err := registry.executeSSHTool(context.Background(), "quicbaas_ssh_key_get", map[string]interface{}{
		"scope": "user", "owner": "alice", "name": "absent",
	})
	require.Error(t, err)
	var notFound *xkmsssh.NotFoundError
	assert.ErrorAs(t, err, &notFound)
}
