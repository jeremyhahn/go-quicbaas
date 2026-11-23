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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	sshclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/test/sshfixture"
)

// TestIsSSHService pins the routing predicate.
func TestIsSSHService(t *testing.T) {
	assert.True(t, isSSHService(sshclient.ServiceName))
	assert.True(t, isSSHService(sshclient.ServiceName+"/ListKeys"))
	assert.False(t, isSSHService("quicbaas.Account"))
	assert.False(t, isSSHService("quicbaas.Certificate"))
}

// TestSSHHandler_MethodMapCoversSurface proves every SDK SSH method has a
// gRPC handler. A missing entry is a runtime "unknown ssh method" that the
// compiler cannot catch.
func TestSSHHandler_MethodMapCoversSurface(t *testing.T) {
	methods := []string{
		sshclient.MethodGenerateKey, sshclient.MethodImportKey, sshclient.MethodGetKey,
		sshclient.MethodListKeys, sshclient.MethodDeleteKey, sshclient.MethodExportPublicKey,
		sshclient.MethodSignDigest, sshclient.MethodAddKnownHost, sshclient.MethodListKnownHosts,
		sshclient.MethodRemoveKnownHost, sshclient.MethodAddAuthorizedKey,
		sshclient.MethodListAuthorizedKeys, sshclient.MethodRemoveAuthorizedKey,
		sshclient.MethodEmitAuthorizedKey,
	}
	for _, m := range methods {
		_, ok := sshMethodMap[m]
		assert.True(t, ok, "grpc transport is missing a handler for %s", m)
	}
	assert.Len(t, sshMethodMap, len(methods), "sshMethodMap has entries not covered by this test")

	// The handler set must match the proto service one-for-one, so a new RPC
	// cannot ship unreachable from the SDK.
	assert.Len(t, pb.SSHService_ServiceDesc.Methods, len(methods),
		"proto SSHService and the SDK method map have diverged")
}

// TestSSHHandler_UnknownMethod covers the dispatch miss.
func TestSSHHandler_UnknownMethod(t *testing.T) {
	h := newSSHHandler(nil)
	_, err := h.handleRequest(context.Background(), sshclient.ServiceName, "Teleport", nil)
	require.Error(t, err)
	assert.Equal(t, errors.CodeInvalidInput, errors.Code(err))
}

// TestSSHHandler_WrongRequestType proves every handler rejects a foreign
// request type before it touches the connection. The nil *grpc.ClientConn is
// deliberate: reaching it would panic, which is exactly the regression this
// test guards.
func TestSSHHandler_WrongRequestType(t *testing.T) {
	h := newSSHHandler(nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	for method := range sshMethodMap {
		t.Run(method, func(t *testing.T) {
			_, err := h.handleRequest(ctx, sshclient.ServiceName, method, &struct{ Nope string }{})
			require.Error(t, err)
			assert.Equal(t, errors.CodeInvalidInput, errors.Code(err))
			assert.Contains(t, err.Error(), "invalid request type")
		})
	}
}

// TestSSHKeyFromProto covers the conversion helper's happy and nil paths.
func TestSSHKeyFromProto(t *testing.T) {
	key, err := sshKeyFromProto("op", &pb.SSHKeyInfo{
		Scope: "user", Owner: "alice", Name: "laptop",
		Algorithm: "ed25519", PublicKey: "ssh-ed25519 AAAA",
		Fingerprint: sshfixture.UserFingerprint, CreatedAtUnix: 1700000000,
		Comment: "work", Backend: "tpm2", BackendKeyId: "ssh-user-laptop",
	})
	require.NoError(t, err)
	assert.Equal(t, "laptop", key.Name)
	assert.Equal(t, sshfixture.UserFingerprint, key.Fingerprint,
		"the fingerprint must survive the proto boundary byte for byte")
	assert.Equal(t, "tpm2", key.Backend)
	assert.Equal(t, "ssh-user-laptop", key.BackendKeyID)
	assert.Equal(t, int64(1700000000), key.CreatedAt.Unix())

	_, err = sshKeyFromProto("op", nil)
	require.Error(t, err)
	assert.Equal(t, errors.CodeInternal, errors.Code(err))
}

// recordingSSHSignServer records the last SignSSHDigest request it served.
type recordingSSHSignServer struct {
	pb.UnimplementedSSHServiceServer
	mu   sync.Mutex
	last *pb.SSHSignDigestRequest
}

func (s *recordingSSHSignServer) SignSSHDigest(_ context.Context, req *pb.SSHSignDigestRequest) (*pb.SSHSignDigestResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = req
	return &pb.SSHSignDigestResponse{Format: req.GetAlgorithm()}, nil
}

func (s *recordingSSHSignServer) lastAlgorithm() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last.GetAlgorithm()
}

func TestSSHHandler_SignDigestSendsAlgorithm(t *testing.T) {
	fake := &recordingSSHSignServer{}
	hs := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterSSHServiceServer(s, fake) })
	conn := hs.dial(t)

	_, err := handleSSHSignDigest(context.Background(), conn, &sshclient.SignRequest{
		Name: "rsa", Digest: []byte("digest"), Algorithm: "rsa-sha2-512",
	})
	require.NoError(t, err)
	assert.Equal(t, "rsa-sha2-512", fake.lastAlgorithm())

	_, err = handleSSHSignDigest(context.Background(), conn, &sshclient.SignRequest{
		Name: "rsa", Digest: []byte("digest"),
	})
	require.NoError(t, err)
	assert.Empty(t, fake.lastAlgorithm())
}
