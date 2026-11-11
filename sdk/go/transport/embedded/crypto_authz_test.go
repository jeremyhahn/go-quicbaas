package embedded

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/kmstenant"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// countingCryptoManager counts the Encrypt and Decrypt calls that passed the
// key authorization gate and reached the backend.
type countingCryptoManager struct {
	interfaces.CryptoManager
	calls atomic.Int32
}

func (m *countingCryptoManager) Encrypt(_ context.Context, _ string, plaintext, _ []byte) ([]byte, error) {
	m.calls.Add(1)
	return plaintext, nil
}

func (m *countingCryptoManager) Decrypt(_ context.Context, _ string, ciphertext, _ []byte) ([]byte, error) {
	m.calls.Add(1)
	return ciphertext, nil
}

// unusedRaftNode satisfies the service builder; the crypto path never
// proposes through Raft.
type unusedRaftNode struct{ interfaces.RaftNode }

// newCryptoAuthzTransport builds a real service whose kmstenant.KeyName key
// belongs to kmstenant.OwnerTenant, sets allowed as that key's policy when it
// is non-nil, and returns the embedded transport over it.
func newCryptoAuthzTransport(t *testing.T, allowed []string) (*Transport, *countingCryptoManager) {
	t.Helper()
	cryptoMgr := &countingCryptoManager{}
	store := qrdbsdk.NewMemoryKVStore()
	svc, err := service.NewServiceBuilder().
		WithConfig(&service.ServiceConfig{NodeID: 1, RequestTimeout: 5 * time.Second}).
		WithRaftNode(&unusedRaftNode{}).
		WithKMSManager(kmstenant.NewManager()).
		WithCryptoManager(cryptoMgr).
		WithKVStoreProvider(func(uint64) interface{} { return store }).
		Build()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, svc.Close()) })

	owner := auth.SubjectIntoContext(
		tenantctx.WithTenant(context.Background(), kmstenant.OwnerTenant),
		interfaces.Subject{ID: "owner", Username: "owner", Authenticated: true},
	)
	_, err = svc.KMS().GenerateKey(owner, &service.GenerateKeyRequest{
		Name:          kmstenant.KeyName,
		KeyAttributes: &xkmssdk.KeyAttributes{CN: kmstenant.KeyName, SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM},
		Usage:         []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	})
	require.NoError(t, err)
	if allowed != nil {
		_, err = svc.KMS().SetKeyPolicy(owner, &service.SetKeyPolicyRequest{
			KeyID:  kmstenant.KeyName,
			Policy: &service.KeyPolicy{AllowedPrincipals: allowed},
		})
		require.NoError(t, err)
	}

	tr, err := New(svc)
	require.NoError(t, err)
	return tr, cryptoMgr
}

func ownerTenantContext() context.Context {
	return transport.ContextWithTenant(context.Background(), kmstenant.OwnerTenant)
}

func TestTransport_CryptoAuthzAllowsInProcessCaller(t *testing.T) {
	tests := []struct {
		name    string
		allowed []string
		ctx     context.Context
	}{
		{name: "no key policy, no subject", ctx: ownerTenantContext()},
		{
			name:    "policy allows the caller's own subject",
			allowed: []string{"alice"},
			ctx: auth.SubjectIntoContext(ownerTenantContext(),
				interfaces.Subject{ID: "alice", Username: "alice", Authenticated: true}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr, cryptoMgr := newCryptoAuthzTransport(t, tc.allowed)

			resp, err := tr.Request(tc.ctx, "quicbaas.Crypto", "Encrypt", &service.EncryptRequest{
				KeyID:     kmstenant.KeyName,
				Plaintext: []byte("payload"),
			})
			require.NoError(t, err)
			encrypted, ok := resp.(*service.EncryptResponse)
			require.True(t, ok)

			_, err = tr.Request(tc.ctx, "quicbaas.Crypto", "Decrypt", &service.DecryptRequest{
				KeyID:      kmstenant.KeyName,
				Ciphertext: encrypted.Ciphertext,
			})
			require.NoError(t, err)
			require.Equal(t, int32(2), cryptoMgr.calls.Load())
		})
	}
}

func TestTransport_CryptoAuthzKeyPolicyDeniesInProcessCaller(t *testing.T) {
	tr, cryptoMgr := newCryptoAuthzTransport(t, []string{"someone-else"})

	_, err := tr.Request(ownerTenantContext(), "quicbaas.Crypto", "Encrypt", &service.EncryptRequest{
		KeyID:     kmstenant.KeyName,
		Plaintext: []byte("payload"),
	})
	require.Error(t, err)
	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	require.Equal(t, dberrors.ErrForbidden, qbErr.Code)
	require.Zero(t, cryptoMgr.calls.Load())
}

func TestInProcessPrincipal(t *testing.T) {
	t.Run("binds the in-process subject when none is present", func(t *testing.T) {
		ctx := inProcessPrincipal(context.Background())
		subject, ok := auth.SubjectFromContext(ctx)
		require.True(t, ok)
		require.Equal(t, inProcessPrincipalID, subject.ID)
		require.True(t, subject.Authenticated)
		require.False(t, auth.IsSystemContext(ctx))
	})
	t.Run("keeps a subject the caller supplied", func(t *testing.T) {
		in := auth.SubjectIntoContext(context.Background(),
			interfaces.Subject{ID: "alice", Username: "alice", Authenticated: true})
		subject, ok := auth.SubjectFromContext(inProcessPrincipal(in))
		require.True(t, ok)
		require.Equal(t, "alice", subject.ID)
	})
}
