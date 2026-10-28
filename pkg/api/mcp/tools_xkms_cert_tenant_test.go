//go:build mcp

package mcp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// saveKeyStoreCertificate initialises the go-xkms service over memory storage
// and stores a self-signed certificate, one no CA issued for a tenant, under
// keyID.
func saveKeyStoreCertificate(t *testing.T, keyID string) {
	t.Helper()
	xkmssdk.ServiceReset()
	t.Cleanup(xkmssdk.ServiceReset)

	swBackend, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{KeyStorage: xkmssdk.NewStorageMemory()})
	require.NoError(t, err)
	backend, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{Backend: swBackend, CertStorage: xkmssdk.NewStorageMemory()})
	require.NoError(t, err)
	require.NoError(t, xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"software": backend},
		DefaultBackend: "software",
	}))

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{CommonName: "key-store-cert"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	require.NoError(t, xkmssdk.ServiceSaveCertificateByID(context.Background(), keyID, cert))
}

// TestXKMSCertByKeyID_BindsCallerToIssuingTenant covers xkms.getCert,
// xkms.certExists and xkms.deleteCert. The go-xkms key store records no
// tenant, so a tenant caller reaches a certificate only when the CA issued it
// for that tenant.
func TestXKMSCertByKeyID_BindsCallerToIssuingTenant(t *testing.T) {
	const keyID = "tenant-bound-cert"
	saveKeyStoreCertificate(t, keyID)
	registry := NewToolRegistry(newMinimalMockService(), newTestLogger(t))
	args := map[string]interface{}{"key_id": keyID}
	acme := tenantctx.WithTenant(context.Background(), "acme")
	system := tenantctx.WithSystem(context.Background())

	exists := func(t *testing.T, ctx context.Context) bool {
		t.Helper()
		result, err := registry.handleXKMSCertExists(ctx, args)
		require.NoError(t, err)
		raw, err := json.Marshal(result)
		require.NoError(t, err)
		var got struct {
			Exists bool `json:"exists"`
		}
		require.NoError(t, json.Unmarshal(raw, &got))
		return got.Exists
	}
	requireRefused := func(t *testing.T, err error) {
		t.Helper()
		var opErr *XKMSOperationError
		require.True(t, errors.As(err, &opErr), "err = %v, want *XKMSOperationError", err)
		require.ErrorIs(t, err, qrdbsdk.ErrTenantMissing)
	}

	t.Run("tenant cannot read a certificate not issued for it", func(t *testing.T) {
		_, err := registry.handleXKMSGetCert(acme, args)
		requireRefused(t, err)
	})
	t.Run("tenant cannot probe a certificate not issued for it", func(t *testing.T) {
		require.False(t, exists(t, acme))
	})
	t.Run("tenant cannot delete a certificate not issued for it", func(t *testing.T) {
		_, err := registry.handleXKMSDeleteCert(acme, args)
		requireRefused(t, err)
		require.True(t, exists(t, system))
	})
	t.Run("system reads and deletes the certificate", func(t *testing.T) {
		_, err := registry.handleXKMSGetCert(system, args)
		require.NoError(t, err)
		_, err = registry.handleXKMSDeleteCert(system, args)
		require.NoError(t, err)
		require.False(t, exists(t, system))
	})
}
