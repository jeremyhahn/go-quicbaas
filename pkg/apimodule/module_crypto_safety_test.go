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

package apimodule

import (
	"context"
	"slices"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

const safetyTenant = "tenant-safety"

// safetyRaftNode satisfies the builder's raft requirement; no test path
// reaches it.
type safetyRaftNode struct{ interfaces.RaftNode }

// reversingCryptoManager encrypts by reversing the plaintext.
type reversingCryptoManager struct{ interfaces.CryptoManager }

func (reversingCryptoManager) Encrypt(_ context.Context, _ string, plaintext, _ []byte) ([]byte, error) {
	out := slices.Clone(plaintext)
	slices.Reverse(out)
	return out, nil
}

func safetyContext(ctx context.Context) context.Context {
	ctx = tenantctx.WithTenant(ctx, safetyTenant)
	return auth.SubjectIntoContext(ctx, interfaces.Subject{ID: "alice", Authenticated: true})
}

// TestModule_Crypto_SafetyLimitEnforcedThroughFacade proves the node's
// Crypto service counts against the tracker the KMS service's SetKeySafety
// configures. The tracker refuses the operation that reaches MaxOperations,
// so with MaxOperations=2 the first Encrypt through the registered node
// crypto server succeeds and the second fails. Without the shared tracker
// Crypto counted nothing and both succeeded.
func TestModule_Crypto_SafetyLimitEnforcedThroughFacade(t *testing.T) {
	store := qrdbsdk.NewMemoryKVStore()
	svc, err := service.NewServiceBuilder().
		WithConfig(&service.ServiceConfig{NodeID: 1, RequestTimeout: 5 * time.Second}).
		WithRaftNode(&safetyRaftNode{}).
		WithKMSManager(stubKMSManager{}).
		WithCryptoManager(reversingCryptoManager{}).
		WithKVStoreProvider(func(uint64) interface{} { return store }).
		Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })

	_, err = svc.KMS().SetKeySafety(safetyContext(context.Background()), &service.SetKeySafetyRequest{
		KeyID: "k1", MaxOperations: 2,
	})
	require.NoError(t, err)

	m, err := NewModule(func() service.QuicBaaS { return svc }, testLogger(t))
	require.NoError(t, err)
	withTenant := grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(safetyContext(ctx), req)
	})
	client := pb.NewCryptoClient(startBufconnModule(t, m, withTenant))

	req := &pb.CryptoEncryptRequest{KeyId: "k1", Plaintext: []byte("p")}
	_, err = client.Encrypt(context.Background(), req)
	require.NoError(t, err, "first Encrypt is under MaxOperations=2")

	_, err = client.Encrypt(context.Background(), req)
	require.Error(t, err, "second Encrypt reaches MaxOperations=2")
	assert.NotEqual(t, codes.OK, status.Code(err))
	assert.Contains(t, status.Convert(err).Message(), "AEAD safety limit exceeded")
}
