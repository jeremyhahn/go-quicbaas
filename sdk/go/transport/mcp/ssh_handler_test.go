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
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sshclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/test/sshfixture"
)

// TestIsMCPSSHService pins the routing predicate.
func TestIsMCPSSHService(t *testing.T) {
	assert.True(t, isMCPSSHService("SSH"))
	assert.True(t, isMCPSSHService(sshclient.ServiceName))
	assert.False(t, isMCPSSHService("quicbaas.Account"))
}

// TestSSHToolNames pins the SDK-method -> server-tool mapping. The tools are
// named after the resource and operation, so a drift here asks the server for
// a tool it does not register and the call fails at runtime.
func TestSSHToolNames(t *testing.T) {
	want := map[string]string{
		sshclient.MethodGenerateKey:         "quicbaas_ssh_key_generate",
		sshclient.MethodImportKey:           "quicbaas_ssh_key_import",
		sshclient.MethodGetKey:              "quicbaas_ssh_key_get",
		sshclient.MethodListKeys:            "quicbaas_ssh_key_list",
		sshclient.MethodDeleteKey:           "quicbaas_ssh_key_delete",
		sshclient.MethodExportPublicKey:     "quicbaas_ssh_key_export",
		sshclient.MethodSignDigest:          "quicbaas_ssh_key_sign",
		sshclient.MethodAddKnownHost:        "quicbaas_ssh_known_host_add",
		sshclient.MethodListKnownHosts:      "quicbaas_ssh_known_host_list",
		sshclient.MethodRemoveKnownHost:     "quicbaas_ssh_known_host_remove",
		sshclient.MethodAddAuthorizedKey:    "quicbaas_ssh_authorized_add",
		sshclient.MethodListAuthorizedKeys:  "quicbaas_ssh_authorized_list",
		sshclient.MethodRemoveAuthorizedKey: "quicbaas_ssh_authorized_remove",
		sshclient.MethodEmitAuthorizedKey:   "quicbaas_ssh_authorized_emit",
	}
	for method, tool := range want {
		got, ok := sshToolName(method)
		require.True(t, ok, "no tool registered for %s", method)
		assert.Equal(t, tool, got, "tool name for %s", method)
	}
	assert.Len(t, sshMethodTools, len(want), "sshMethodTools has entries not covered by this test")

	_, ok := sshToolName("Teleport")
	assert.False(t, ok, "an unknown method must not resolve to a tool")
}

// =============================================================================
// Request transforms
// =============================================================================

// TestSSHMCPHandler_ImportUsesBase64Argument pins the binary-argument
// convention: JSON has no byte type, so the PEM travels base64 under
// pem_b64. Sending it under "pem" would be silently dropped by the tool.
func TestSSHMCPHandler_ImportUsesBase64Argument(t *testing.T) {
	h := newSSHMCPHandler()
	args, err := h.transformRequest(sshclient.MethodImportKey, &sshclient.ImportKeyRequest{
		Scope: "user", Owner: "alice", Name: "laptop",
		PEM: []byte("pem-bytes"), PassphraseB64: "aHVudGVyMg==",
	})
	require.NoError(t, err)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("pem-bytes")), args["pem_b64"])
	assert.Equal(t, "aHVudGVyMg==", args["passphrase_b64"])
	assert.Equal(t, "laptop", args["name"])
	assert.NotContains(t, args, "pem", "raw pem must not be sent under the untagged name")
}

func TestSSHMCPHandler_SignUsesBase64Argument(t *testing.T) {
	h := newSSHMCPHandler()
	args, err := h.transformRequest(sshclient.MethodSignDigest, &sshclient.SignRequest{
		Scope: "user", Owner: "alice", Name: "laptop", Digest: []byte("digest"),
	})
	require.NoError(t, err)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("digest")), args["digest_b64"])
	assert.NotContains(t, args, "digest")
}

func TestSSHMCPHandler_GenericRequestUsesJSONTags(t *testing.T) {
	h := newSSHMCPHandler()
	args, err := h.transformRequest(sshclient.MethodAddKnownHost, &sshclient.AddKnownHostRequest{
		Scope: "user", Owner: "alice", Host: "git.example.com",
		PublicKey: sshfixture.Host(t).AuthorizedLine,
	})
	require.NoError(t, err)
	assert.Equal(t, "git.example.com", args["host"])
	assert.Equal(t, sshfixture.Host(t).AuthorizedLine, args["public_key"],
		"the pinned host key must reach the tool arguments unaltered")
}

func TestSSHMCPHandler_NilRequestYieldsEmptyArguments(t *testing.T) {
	h := newSSHMCPHandler()
	args, err := h.transformRequest(sshclient.MethodListKeys, nil)
	require.NoError(t, err)
	assert.Empty(t, args)
}

// =============================================================================
// Response transforms
// =============================================================================

func TestSSHMCPHandler_TransformResponses(t *testing.T) {
	h := newSSHMCPHandler()

	key, err := h.transformResponse(sshclient.MethodGetKey, map[string]interface{}{"name": "laptop"})
	require.NoError(t, err)
	assert.Equal(t, "laptop", key.(*sshclient.Key).Name)

	list, err := h.transformResponse(sshclient.MethodListKeys, map[string]interface{}{
		"items": []interface{}{map[string]interface{}{"name": "laptop"}},
	})
	require.NoError(t, err)
	require.Len(t, list.(*sshclient.ListKeysResponse).Items, 1)

	// A tool result with no items must decode to an empty slice, not nil, so
	// callers can range over it unconditionally.
	empty, err := h.transformResponse(sshclient.MethodListKnownHosts, map[string]interface{}{})
	require.NoError(t, err)
	assert.NotNil(t, empty.(*sshclient.ListKnownHostsResponse).Items)
	assert.Empty(t, empty.(*sshclient.ListKnownHostsResponse).Items)

	entries, err := h.transformResponse(sshclient.MethodListAuthorizedKeys, map[string]interface{}{
		"items": []interface{}{map[string]interface{}{"name": "deploy"}},
	})
	require.NoError(t, err)
	require.Len(t, entries.(*sshclient.ListAuthorizedKeysResponse).Items, 1)

	pub, err := h.transformResponse(sshclient.MethodExportPublicKey, map[string]interface{}{
		"line": "ssh-ed25519 AAAA",
	})
	require.NoError(t, err)
	assert.Equal(t, "ssh-ed25519 AAAA", pub.(*sshclient.PublicKey).Line)

	sig, err := h.transformResponse(sshclient.MethodSignDigest, map[string]interface{}{
		"format": "ssh-ed25519",
	})
	require.NoError(t, err)
	assert.Equal(t, "ssh-ed25519", sig.(*sshclient.SignResponse).Format)

	emit, err := h.transformResponse(sshclient.MethodEmitAuthorizedKey, map[string]interface{}{
		"line": "ssh-ed25519 AAAA alice",
	})
	require.NoError(t, err)
	assert.Equal(t, "ssh-ed25519 AAAA alice", emit.(*sshclient.EmitResponse).Line)

	ack, err := h.transformResponse(sshclient.MethodDeleteKey, map[string]interface{}{"success": true})
	require.NoError(t, err)
	assert.True(t, ack.(*sshclient.AckResponse).Success)
}

// =============================================================================
// Error paths
// =============================================================================

func TestSSHMCPHandler_UnknownMethodIsUnimplemented(t *testing.T) {
	h := newSSHMCPHandler()
	_, err := h.transformResponse("Teleport", map[string]interface{}{})
	require.Error(t, err)
	assert.Equal(t, errors.CodeUnimplemented, errors.Code(err))
}

func TestSSHMCPHandler_NilResultIsInternalError(t *testing.T) {
	h := newSSHMCPHandler()
	_, err := h.transformResponse(sshclient.MethodGetKey, nil)
	require.Error(t, err)
	assert.Equal(t, errors.CodeInternal, errors.Code(err))
}

func TestSSHMCPHandler_UndecodableResult(t *testing.T) {
	h := newSSHMCPHandler()
	// A JSON array cannot decode into the object-shaped Key envelope.
	_, err := h.transformResponse(sshclient.MethodGetKey, []interface{}{"nope"})
	require.Error(t, err)
	assert.Equal(t, errors.CodeInternal, errors.Code(err))
}
