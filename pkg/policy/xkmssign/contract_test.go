package xkmssign_test

import (
	"context"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	qbcrypto "github.com/jeremyhahn/go-quicbaas/pkg/crypto"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/policy"
	"github.com/jeremyhahn/go-quicbaas/pkg/policy/sandbox"
	"github.com/jeremyhahn/go-quicbaas/pkg/policy/xkmssign"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// nopLogger is a no-op interfaces.Logger (copied from pkg/kms's test mock).
type nopLogger struct{}

func (l *nopLogger) Debug(msg string, args ...any)                              {}
func (l *nopLogger) Debugf(format string, args ...interface{})                  {}
func (l *nopLogger) Info(msg string, args ...any)                               {}
func (l *nopLogger) Infof(format string, args ...interface{})                   {}
func (l *nopLogger) Warn(msg string, args ...any)                               {}
func (l *nopLogger) Warnf(format string, args ...interface{})                   {}
func (l *nopLogger) Error(msg string, args ...any)                              {}
func (l *nopLogger) Errorf(format string, args ...interface{})                  {}
func (l *nopLogger) Fatal(msg string)                                           {}
func (l *nopLogger) Fatalf(format string, args ...interface{})                  {}
func (l *nopLogger) WithField(key string, value interface{}) interfaces.Logger  { return l }
func (l *nopLogger) WithFields(fields map[string]interface{}) interfaces.Logger { return l }
func (l *nopLogger) WithError(err error) interfaces.Logger                      { return l }

// realKMSManagerForTest builds a KMS manager over the real go-xkms software
// backend (copied from pkg/service/kms_import_through_test.go).
func realKMSManagerForTest(t *testing.T) (*kms.KMSService, func()) {
	t.Helper()

	dbxkms.Reset()

	keyStorage, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)

	certStorage, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)

	softwareBackend, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{
		KeyStorage: keyStorage,
	})
	require.NoError(t, err)

	serviceBackend, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
		Backend:     softwareBackend,
		CertStorage: certStorage,
	})
	require.NoError(t, err)

	require.NoError(t, dbxkms.Initialize(&dbxkms.Config{
		DefaultBackend: "software",
		Backends:       map[string]xkmssdk.ServiceBackend{"software": serviceBackend},
		Logger:         &nopLogger{},
	}))

	backend, err := kms.NewXKMSBackend(&kms.XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &nopLogger{},
	})
	require.NoError(t, err)

	registry := kms.NewBackendRegistry(nil)
	require.NoError(t, registry.Register("software", backend, 100))

	manager, err := kms.NewKMSService(&kms.KMSServiceOptions{Registry: registry})
	require.NoError(t, err)

	return manager, func() { dbxkms.Reset() }
}

// TestXKMSAdapterContract drives policy.Sign and Engine.Load through the
// adapter over pkg/crypto and a real go-xkms software backend, one EC P-256
// signing key per tenant.
func TestXKMSAdapterContract(t *testing.T) {
	manager, cleanup := realKMSManagerForTest(t)
	defer cleanup()
	ctx := tenantctx.WithSystem(context.Background())

	tenants := []string{"acme", "beta"}
	for _, tn := range tenants {
		_, err := manager.GenerateKey(ctx, &xkmssdk.KeyAttributes{
			CN:            xkmssign.DefaultKeyFor(tn),
			KeyType:       xkmssdk.KeyTypeSigning,
			KeyAlgorithm:  x509.ECDSA,
			ECCAttributes: &xkmssdk.ECCAttributes{Curve: elliptic.P256()},
		})
		require.NoError(t, err, "generate %s's key", tn)
	}

	adapter, err := xkmssign.New(qbcrypto.NewMemoryCryptoManager(manager), nil)
	require.NoError(t, err)
	engine, err := policy.NewEngine(adapter, sandbox.Evaluator{})
	require.NoError(t, err)

	doc := func(tn string) policy.Document {
		return policy.Document{
			Kind: policy.KindSandbox, Name: "sandbox", Tenant: tn, Version: 1,
			Spec: json.RawMessage(`{"policy":"opt-in"}`),
		}
	}

	signed := map[string]policy.SignedDocument{}
	for _, tn := range tenants {
		sd, err := policy.Sign(ctx, adapter, doc(tn))
		require.NoError(t, err)
		require.Equal(t, xkmssign.DefaultKeyFor(tn), sd.KeyID)
		require.NotEmpty(t, sd.Signature)

		loaded, err := engine.Load(ctx, tn, sd)
		require.NoError(t, err, "load %s's document", tn)
		require.Equal(t, tn, loaded.Document().Tenant)
		signed[tn] = sd
	}

	t.Run("tampered spec is refused", func(t *testing.T) {
		sd := signed["acme"]
		sd.Document.Spec = json.RawMessage(`{"policy":"disabled"}`)
		_, err := engine.Load(ctx, "acme", sd)
		require.ErrorIs(t, err, policy.ErrBadSignature)
		require.False(t, errors.Is(err, policy.ErrWrongTenant))
	})

	t.Run("tampered signature is refused", func(t *testing.T) {
		sd := signed["acme"]
		sd.Signature = append([]byte(nil), sd.Signature...)
		sd.Signature[len(sd.Signature)-1] ^= 0xff
		_, err := engine.Load(ctx, "acme", sd)
		require.ErrorIs(t, err, policy.ErrBadSignature)
	})

	t.Run("another tenant's key is refused", func(t *testing.T) {
		// acme's document signed with beta's real key.
		d := doc("acme")
		payload, err := d.Payload()
		require.NoError(t, err)
		keyID, sig, err := adapter.Sign(ctx, "beta", payload)
		require.NoError(t, err)
		_, err = engine.Load(ctx, "acme", policy.SignedDocument{Document: d, KeyID: keyID, Signature: sig})
		require.ErrorIs(t, err, policy.ErrWrongTenant)
	})

	t.Run("another tenant's signature under the tenant's key id is refused", func(t *testing.T) {
		// beta's signature over acme's payload, relabelled with acme's key id.
		d := doc("acme")
		payload, err := d.Payload()
		require.NoError(t, err)
		_, sig, err := adapter.Sign(ctx, "beta", payload)
		require.NoError(t, err)
		_, err = engine.Load(ctx, "acme", policy.SignedDocument{
			Document: d, KeyID: xkmssign.DefaultKeyFor("acme"), Signature: sig,
		})
		require.ErrorIs(t, err, policy.ErrBadSignature)
	})
}
