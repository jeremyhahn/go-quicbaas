// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/kmstenant"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// keyPolicyCryptoManager counts the crypto operations that reach the
// backend, so a test can prove a denied call never got there.
type keyPolicyCryptoManager struct {
	interfaces.CryptoManager
	calls atomic.Int32
}

func (m *keyPolicyCryptoManager) Encrypt(_ context.Context, _ string, plaintext, _ []byte) ([]byte, error) {
	m.calls.Add(1)
	return plaintext, nil
}

func (m *keyPolicyCryptoManager) Decrypt(_ context.Context, _ string, ciphertext, _ []byte) ([]byte, error) {
	m.calls.Add(1)
	return ciphertext, nil
}

func (m *keyPolicyCryptoManager) Sign(_ context.Context, _ string, input []byte, _ string, _ bool) ([]byte, error) {
	m.calls.Add(1)
	return input, nil
}

// cryptoKeyPolicyFixture is the production Crypto facade behind the gRPC
// Crypto server, with a tenant key whose policy the test persists.
type cryptoKeyPolicyFixture struct {
	srv       *CryptoServer
	cryptoMgr *keyPolicyCryptoManager
}

// newCryptoKeyPolicyFixture imports keyID as kmstenant.OwnerTenant and
// stores a policy allowing only allowed on it, both through svc.KMS().
func newCryptoKeyPolicyFixture(t *testing.T, keyID string, allowed []string) *cryptoKeyPolicyFixture {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	require.NoError(t, err)

	// The key exists only once ImportKey has run, as a real backend does.
	var imported atomic.Bool
	mgr := &mockKMSManager{
		importKeyFunc: func(_ context.Context, id string, _ []byte, _ string) (interface{}, error) {
			imported.Store(true)
			return &kms.KeyMetadata{KeyID: id, Algorithm: kms.AlgAES256GCM, State: kms.KeyStateEnabled, Version: 1}, nil
		},
		getKeyFunc: func(_ context.Context, id string) (interface{}, error) {
			if !imported.Load() {
				return nil, nil
			}
			return &kms.KeyMetadata{KeyID: id, Algorithm: kms.AlgAES256GCM, State: kms.KeyStateEnabled, Version: 1}, nil
		},
	}
	cryptoMgr := &keyPolicyCryptoManager{}
	store := qrdbsdk.NewMemoryKVStore()
	svc, err := service.NewServiceBuilder().
		WithConfig(&service.ServiceConfig{NodeID: 1, RequestTimeout: 5 * time.Second}).
		WithRaftNode(&importRaftNode{}).
		WithKMSManager(mgr).
		WithCryptoManager(cryptoMgr).
		WithKVStoreProvider(func(uint64) interface{} { return store }).
		Build()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, svc.Close()) })

	owner := importTenantContext()
	_, err = NewKMSServerWithService(mgr, svc.KMS(), log).ImportKey(owner, &pb.ImportKeyRequest{
		Name:        keyID,
		KeyMaterial: []byte("key-material"),
		KeySpec:     &pb.KeySpec{Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM},
	})
	require.NoError(t, err)
	_, err = svc.KMS().SetKeyPolicy(owner, &service.SetKeyPolicyRequest{
		KeyID:  keyID,
		Policy: &service.KeyPolicy{AllowedPrincipals: allowed},
	})
	require.NoError(t, err)

	return &cryptoKeyPolicyFixture{srv: NewCryptoServer(svc.Crypto(), log), cryptoMgr: cryptoMgr}
}

// principalContext authorizes subject as a caller of kmstenant.OwnerTenant,
// the tenant that owns the fixture's key.
func principalContext(subject string) context.Context {
	return contextWithAuthorizedSubject(context.Background(), &platformtoken.AuthorizedContext{
		Subject: subject, TenantID: kmstenant.OwnerTenant,
	})
}

// cryptoKeyPolicyRPC is one Crypto RPC that names a key.
type cryptoKeyPolicyRPC struct {
	name string
	call func(srv *CryptoServer, ctx context.Context, keyID string) error
}

func cryptoKeyPolicyRPCs() []cryptoKeyPolicyRPC {
	return []cryptoKeyPolicyRPC{
		{"Encrypt", func(srv *CryptoServer, ctx context.Context, keyID string) error {
			_, err := srv.Encrypt(ctx, &pb.CryptoEncryptRequest{KeyId: keyID, Plaintext: []byte("pt")})
			return err
		}},
		{"Decrypt", func(srv *CryptoServer, ctx context.Context, keyID string) error {
			_, err := srv.Decrypt(ctx, &pb.CryptoDecryptRequest{KeyId: keyID, Ciphertext: []byte("ct")})
			return err
		}},
		{"Sign", func(srv *CryptoServer, ctx context.Context, keyID string) error {
			_, err := srv.Sign(ctx, &pb.SignRequest{KeyId: keyID, Message: []byte("msg")})
			return err
		}},
	}
}

// TestCryptoServer_CryptoKeyPolicyDeniesPrincipal pins Codex review 8504
// finding 1 (#1993): the gRPC Crypto server delegates to the production
// facade, which enforces the persisted key policy, so a same-tenant caller
// the policy does not name is refused before the backend runs, while the
// named caller succeeds.
func TestCryptoServer_CryptoKeyPolicyDeniesPrincipal(t *testing.T) {
	for _, rpc := range cryptoKeyPolicyRPCs() {
		t.Run(rpc.name, func(t *testing.T) {
			f := newCryptoKeyPolicyFixture(t, "policy-key", []string{"alice"})

			err := rpc.call(f.srv, principalContext("bob"), "policy-key")
			assert.Equal(t, codes.PermissionDenied, status.Code(err), "error: %v", err)
			assert.Zero(t, f.cryptoMgr.calls.Load(), "a denied call reached the crypto backend")

			require.NoError(t, rpc.call(f.srv, principalContext("alice"), "policy-key"))
			assert.Equal(t, int32(1), f.cryptoMgr.calls.Load())
		})
	}
}

// TestCryptoServer_CryptoKeyPolicyNamesNeitherPrincipal proves a policy
// that names neither caller refuses both.
func TestCryptoServer_CryptoKeyPolicyNamesNeitherPrincipal(t *testing.T) {
	for _, rpc := range cryptoKeyPolicyRPCs() {
		t.Run(rpc.name, func(t *testing.T) {
			f := newCryptoKeyPolicyFixture(t, "closed-key", []string{"carol"})

			for _, subject := range []string{"alice", "bob"} {
				err := rpc.call(f.srv, principalContext(subject), "closed-key")
				assert.Equal(t, codes.PermissionDenied, status.Code(err), "%s: %v", subject, err)
			}
			assert.Zero(t, f.cryptoMgr.calls.Load(), "a denied call reached the crypto backend")
		})
	}
}
