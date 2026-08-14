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

package grpc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	gossh "golang.org/x/crypto/ssh"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/sshfake"
)

// newSSHTestServer builds an SSHServer backed by an in-memory SSH store.
func newSSHTestServer(client xkmsssh.Client) *SSHServer {
	return NewSSHServer(service.NewSSHService(client), newTestLogger())
}

// =============================================================================
// Happy paths
// =============================================================================

func TestSSHServer_KeyLifecycle(t *testing.T) {
	fake := sshfake.NewClient()
	srv := newSSHTestServer(fake)
	ctx := context.Background()

	genResp, err := srv.GenerateSSHKey(ctx, &pb.SSHGenerateKeyRequest{
		Scope: "user", Owner: "alice", Name: "laptop", Algorithm: "ed25519", RsaBits: 0,
	})
	require.NoError(t, err)
	require.NotNil(t, genResp.GetKey())
	assert.Equal(t, "laptop", genResp.GetKey().GetName())

	getResp, err := srv.GetSSHKey(ctx, &pb.SSHGetKeyRequest{Scope: "user", Owner: "alice", Name: "laptop"})
	require.NoError(t, err)
	assert.Equal(t, "laptop", getResp.GetKey().GetName())

	listResp, err := srv.ListSSHKeys(ctx, &pb.SSHListKeysRequest{Scope: "user", Owner: "alice"})
	require.NoError(t, err)
	require.Len(t, listResp.GetKeys(), 1)

	expResp, err := srv.ExportSSHPublicKey(ctx, &pb.SSHExportPublicKeyRequest{
		Scope: "user", Owner: "alice", Name: "laptop",
	})
	require.NoError(t, err)
	assert.Contains(t, expResp.GetLine(), "ssh-ed25519")

	delResp, err := srv.DeleteSSHKey(ctx, &pb.SSHDeleteKeyRequest{Scope: "user", Owner: "alice", Name: "laptop"})
	require.NoError(t, err)
	assert.True(t, delResp.GetSuccess())
}

func TestSSHServer_ImportCarriesPassphrase(t *testing.T) {
	fake := sshfake.NewClient()
	srv := newSSHTestServer(fake)

	resp, err := srv.ImportSSHKey(context.Background(), &pb.SSHImportKeyRequest{
		Scope: "user", Owner: "alice", Name: "imported",
		Pem:           []byte("pem"),
		PassphraseB64: base64.StdEncoding.EncodeToString([]byte("s3cret")),
	})
	require.NoError(t, err)
	assert.Equal(t, "imported", resp.GetKey().GetName())
	assert.Equal(t, "s3cret", fake.LastImportPassphrase)
}

func TestSSHServer_SignDigest(t *testing.T) {
	fake := sshfake.NewClient()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	sshPub, err := gossh.NewPublicKey(pub)
	require.NoError(t, err)
	fake.SignerPublicKey = sshPub

	srv := newSSHTestServer(fake)
	resp, err := srv.SignSSHDigest(context.Background(), &pb.SSHSignDigestRequest{
		Scope: "user", Owner: "alice", Name: "laptop", Digest: []byte("digest"),
	})
	require.NoError(t, err)
	assert.Equal(t, "ssh-ed25519", resp.GetFormat())
	assert.Equal(t, []byte("sig"), resp.GetBlob())
}

func TestSSHServer_KnownHostsAndAuthorized(t *testing.T) {
	srv := newSSHTestServer(sshfake.NewClient())
	ctx := context.Background()

	_, err := srv.AddSSHKnownHost(ctx, &pb.SSHAddKnownHostRequest{
		Scope: "user", Owner: "alice", Host: "git.example.com", Fingerprint: "SHA256:abc",
	})
	require.NoError(t, err)

	pins, err := srv.ListSSHKnownHosts(ctx, &pb.SSHListKnownHostsRequest{Scope: "user", Owner: "alice"})
	require.NoError(t, err)
	require.Len(t, pins.GetPins(), 1)
	assert.Equal(t, "git.example.com", pins.GetPins()[0].GetHost())

	_, err = srv.RemoveSSHKnownHost(ctx, &pb.SSHRemoveKnownHostRequest{
		Scope: "user", Owner: "alice", Host: "git.example.com",
	})
	require.NoError(t, err)

	_, err = srv.AddSSHAuthorizedKey(ctx, &pb.SSHAddAuthorizedKeyRequest{
		Scope: "user", Owner: "alice", Name: "deploy", Line: "ssh-ed25519 AAAA",
	})
	require.NoError(t, err)

	entries, err := srv.ListSSHAuthorizedKeys(ctx, &pb.SSHListAuthorizedKeysRequest{Scope: "user", Owner: "alice"})
	require.NoError(t, err)
	require.Len(t, entries.GetEntries(), 1)

	emit, err := srv.EmitSSHAuthorizedKey(ctx, &pb.SSHEmitAuthorizedKeyRequest{
		PublicKey: "ssh-ed25519 AAAA", Comment: "alice",
	})
	require.NoError(t, err)
	assert.Equal(t, "ssh-ed25519 AAAA alice", emit.GetLine())

	_, err = srv.RemoveSSHAuthorizedKey(ctx, &pb.SSHRemoveAuthorizedKeyRequest{
		Scope: "user", Owner: "alice", Name: "deploy",
	})
	require.NoError(t, err)
}

// =============================================================================
// Error paths
// =============================================================================

func TestSSHServer_NilRequestsRejected(t *testing.T) {
	srv := newSSHTestServer(sshfake.NewClient())
	ctx := context.Background()

	_, err := srv.GenerateSSHKey(ctx, nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = srv.ImportSSHKey(ctx, nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = srv.GetSSHKey(ctx, nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = srv.ListSSHKeys(ctx, nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = srv.DeleteSSHKey(ctx, nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = srv.ExportSSHPublicKey(ctx, nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = srv.SignSSHDigest(ctx, nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = srv.AddSSHKnownHost(ctx, nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = srv.ListSSHKnownHosts(ctx, nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = srv.RemoveSSHKnownHost(ctx, nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = srv.AddSSHAuthorizedKey(ctx, nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = srv.ListSSHAuthorizedKeys(ctx, nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = srv.RemoveSSHAuthorizedKey(ctx, nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = srv.EmitSSHAuthorizedKey(ctx, nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSSHServer_MissingRequiredFields(t *testing.T) {
	srv := newSSHTestServer(sshfake.NewClient())
	ctx := context.Background()

	_, err := srv.GenerateSSHKey(ctx, &pb.SSHGenerateKeyRequest{Scope: "user", Owner: "alice"})
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "name is required")

	_, err = srv.ImportSSHKey(ctx, &pb.SSHImportKeyRequest{Name: "k"})
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "pem is required")

	_, err = srv.SignSSHDigest(ctx, &pb.SSHSignDigestRequest{Name: "k"})
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "digest is required")

	_, err = srv.AddSSHKnownHost(ctx, &pb.SSHAddKnownHostRequest{Host: "h"})
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "fingerprint is required")

	_, err = srv.AddSSHAuthorizedKey(ctx, &pb.SSHAddAuthorizedKeyRequest{Name: "n"})
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "line is required")

	_, err = srv.EmitSSHAuthorizedKey(ctx, &pb.SSHEmitAuthorizedKeyRequest{})
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "public_key is required")
}

func TestSSHServer_NotConfiguredIsUnavailable(t *testing.T) {
	srv := newSSHTestServer(nil)
	_, err := srv.ListSSHKeys(context.Background(), &pb.SSHListKeysRequest{})
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestSSHServer_ErrorCodeMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"not found", &xkmsssh.NotFoundError{Resource: "key", Message: "absent"}, codes.NotFound},
		{"duplicate", &xkmsssh.DuplicateError{Resource: "key", Message: "exists"}, codes.AlreadyExists},
		{"invalid request", &xkmsssh.InvalidRequestError{Message: "bad"}, codes.InvalidArgument},
		{"nil input", &xkmsssh.NilInputError{Field: "scope"}, codes.InvalidArgument},
		{"mismatch", &xkmsssh.MismatchError{Host: "h"}, codes.FailedPrecondition},
		{"not configured", &service.SSHNotConfiguredError{}, codes.Unavailable},
		{"missing digest", &service.SSHMissingDigestError{}, codes.InvalidArgument},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := sshfake.NewClient()
			fake.Err = tc.err
			srv := newSSHTestServer(fake)
			_, err := srv.GetSSHKey(context.Background(), &pb.SSHGetKeyRequest{Name: "k"})
			assert.Equal(t, tc.want, status.Code(err))
		})
	}
}

// =============================================================================
// Authorization mapping
// =============================================================================

// TestSSHGRPCPermissionsMatchRESTVocabulary pins the SSH gRPC methods to the
// same keys:* (resource, action) pairs the REST PEP assigns. The gRPC authz
// gate is default-deny, so an unmapped method is silently unreachable.
func TestSSHGRPCPermissionsMatchRESTVocabulary(t *testing.T) {
	want := map[string][2]string{
		"/quicbaas.ssh.v1.SSHService/GenerateSSHKey":         {"keys", "create"},
		"/quicbaas.ssh.v1.SSHService/ImportSSHKey":           {"keys", "import"},
		"/quicbaas.ssh.v1.SSHService/GetSSHKey":              {"keys", "get"},
		"/quicbaas.ssh.v1.SSHService/ListSSHKeys":            {"keys", "list"},
		"/quicbaas.ssh.v1.SSHService/DeleteSSHKey":           {"keys", "delete"},
		"/quicbaas.ssh.v1.SSHService/ExportSSHPublicKey":     {"keys", "get"},
		"/quicbaas.ssh.v1.SSHService/SignSSHDigest":          {"keys", "sign"},
		"/quicbaas.ssh.v1.SSHService/AddSSHKnownHost":        {"keys", "create"},
		"/quicbaas.ssh.v1.SSHService/ListSSHKnownHosts":      {"keys", "list"},
		"/quicbaas.ssh.v1.SSHService/RemoveSSHKnownHost":     {"keys", "delete"},
		"/quicbaas.ssh.v1.SSHService/AddSSHAuthorizedKey":    {"keys", "create"},
		"/quicbaas.ssh.v1.SSHService/ListSSHAuthorizedKeys":  {"keys", "list"},
		"/quicbaas.ssh.v1.SSHService/RemoveSSHAuthorizedKey": {"keys", "delete"},
		"/quicbaas.ssh.v1.SSHService/EmitSSHAuthorizedKey":   {"keys", "get"},
	}
	for method, pair := range want {
		perm, ok := grpcMethodPermissions[method]
		require.True(t, ok, "%s is unmapped and therefore default-denied", method)
		assert.Equal(t, pair[0], perm.resource, "%s resource", method)
		assert.Equal(t, pair[1], perm.action, "%s action", method)
	}

	// Every RPC on the descriptor must be covered by the table above, so a
	// new RPC cannot ship unmapped.
	for _, m := range pb.SSHService_ServiceDesc.Methods {
		full := "/" + pb.SSHService_ServiceDesc.ServiceName + "/" + m.MethodName
		_, ok := want[full]
		assert.True(t, ok, "%s has no expected permission in this test", full)
	}
}

// TestSSHServiceIsRegisteredForAuthzGuard proves the SSH descriptor is in
// brokerServiceDescs so the exhaustive mapping guard covers it.
func TestSSHServiceIsRegisteredForAuthzGuard(t *testing.T) {
	found := false
	for _, desc := range brokerServiceDescs {
		if desc.ServiceName == pb.SSHService_ServiceDesc.ServiceName {
			found = true
			break
		}
	}
	assert.True(t, found, "SSHService must be listed in brokerServiceDescs")
}
