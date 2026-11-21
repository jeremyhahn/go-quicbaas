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
// newCryptoFacadeClient builds a service over an in-memory KVStore, mounts the
// module on bufconn with the safety tenant in every call's context, and returns
// the service and a Crypto client of it.
func newCryptoFacadeClient(t *testing.T) (service.QuicBaaS, pb.CryptoClient) {
	t.Helper()
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

	m, err := NewModule(func() service.QuicBaaS { return svc }, testLogger(t))
	require.NoError(t, err)
	withTenant := grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(safetyContext(ctx), req)
	})
	return svc, pb.NewCryptoClient(startBufconnModule(t, m, withTenant))
}

func TestModule_Crypto_SafetyLimitEnforcedThroughFacade(t *testing.T) {
	svc, client := newCryptoFacadeClient(t)

	_, err := svc.KMS().SetKeySafety(safetyContext(context.Background()), &service.SetKeySafetyRequest{
		KeyID: "k1", MaxOperations: 2,
	})
	require.NoError(t, err)

	req := &pb.CryptoEncryptRequest{KeyId: "k1", Plaintext: []byte("p")}
	_, err = client.Encrypt(context.Background(), req)
	require.NoError(t, err, "first Encrypt is under MaxOperations=2")

	_, err = client.Encrypt(context.Background(), req)
	require.Error(t, err, "second Encrypt reaches MaxOperations=2")
	assert.NotEqual(t, codes.OK, status.Code(err))
	assert.Contains(t, status.Convert(err).Message(), "AEAD safety limit exceeded")
}

// TestModule_Crypto_DisabledCurrentVersionRefusesEncryption: once the key's
// current version is disabled, the mounted Crypto service seals no data and
// wraps no data key under it.
func TestModule_Crypto_DisabledCurrentVersionRefusesEncryption(t *testing.T) {
	svc, client := newCryptoFacadeClient(t)
	ctx := context.Background()

	_, err := client.Encrypt(ctx, &pb.CryptoEncryptRequest{KeyId: "k1", Plaintext: []byte("p")})
	require.NoError(t, err)

	_, err = svc.KMS().UpdateKeyStatus(safetyContext(ctx), &service.UpdateKeyStatusRequest{
		KeyID: "k1", Version: 1, Status: service.KeyStatusDisabled,
	})
	require.NoError(t, err)

	requireDisabled := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		assert.NotEqual(t, codes.OK, status.Code(err))
		assert.Contains(t, status.Convert(err).Message(), "k1 is disabled")
	}
	t.Run("Encrypt", func(t *testing.T) {
		_, err := client.Encrypt(ctx, &pb.CryptoEncryptRequest{KeyId: "k1", Plaintext: []byte("p")})
		requireDisabled(t, err)
	})
	t.Run("EncryptBatch", func(t *testing.T) {
		_, err := client.EncryptBatch(ctx, &pb.EncryptBatchRequest{
			KeyId: "k1", Items: []*pb.BatchEncryptItem{{Plaintext: []byte("p")}},
		})
		requireDisabled(t, err)
	})
	t.Run("GenerateDataKey", func(t *testing.T) {
		_, err := client.GenerateDataKey(ctx, &pb.GenerateDataKeyRequest{KeyId: "k1", KeySpec: &pb.KeySpec{KeySize: 32}})
		requireDisabled(t, err)
	})
	t.Run("GenerateWrappedDataKey", func(t *testing.T) {
		_, err := client.GenerateWrappedDataKey(ctx, &pb.GenerateWrappedDataKeyRequest{KeyId: "k1", KeySpec: &pb.KeySpec{KeySize: 32}})
		requireDisabled(t, err)
	})
}
