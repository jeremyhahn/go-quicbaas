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
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// saveKeyStoreCertificate initialises the go-xkms service over memory storage
// and stores a self-signed certificate, one no CA issued for a tenant, under
// keyID.
func saveKeyStoreCertificate(t *testing.T, keyID string, certStorage xkmssdk.StorageBackend) {
	t.Helper()
	xkmssdk.ServiceReset()
	t.Cleanup(xkmssdk.ServiceReset)

	swBackend, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{KeyStorage: xkmssdk.NewStorageMemory()})
	require.NoError(t, err)
	backend, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{Backend: swBackend, CertStorage: certStorage})
	require.NoError(t, err)
	require.NoError(t, xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"software": backend},
		DefaultBackend: "software",
	}))

	cert := newSelfSignedCertificate(t, 42)
	require.NoError(t, xkmssdk.ServiceSaveCertificateByID(context.Background(), keyID, cert))
}

// newSelfSignedCertificate returns a self-signed certificate with serial.
func newSelfSignedCertificate(t *testing.T, serial int64) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "key-store-cert"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

// storedCertificateSerial returns the serial of the certificate the go-xkms
// key store holds under keyID.
func storedCertificateSerial(t *testing.T, keyID string) int64 {
	t.Helper()
	cert, err := xkmssdk.ServiceCertificateByID(context.Background(), keyID)
	require.NoError(t, err)
	return cert.SerialNumber.Int64()
}

// TestXKMSSaveCert_RequiresSystemKeyStoreWriter pins that xkms.saveCert and
// xkms.saveCertChain refuse every caller but the system tenant, as REST does:
// the key store records no tenant, so a tenant that could overwrite another
// tenant's entry with a certificate issued for itself would pass the issuing
// tenant check on every later read and delete of that key ID.
func TestXKMSSaveCert_RequiresSystemKeyStoreWriter(t *testing.T) {
	const keyID = "globex-cert"
	saveKeyStoreCertificate(t, keyID, xkmssdk.NewStorageMemory())
	svc := certificateTenantService{
		QuicBaaS: newMinimalMockService(),
		certs:    issuingTenantCertificates{tenants: map[int64]string{42: "globex", 7: "acme"}},
	}
	registry := NewToolRegistry(svc, newTestLogger(t))
	acme := tenantctx.WithTenant(context.Background(), "acme")
	system := tenantctx.WithSystem(context.Background())
	acmeCertPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: newSelfSignedCertificate(t, 7).Raw}))
	keyArgs := map[string]interface{}{"key_id": keyID}

	requireWriteRefused := func(t *testing.T, err error, operation string) {
		t.Helper()
		var opErr *XKMSOperationError
		require.True(t, errors.As(err, &opErr), "err = %v, want *XKMSOperationError", err)
		require.Equal(t, operation, opErr.Operation)
		var refused *service.KeyStoreWriteRefusedError
		require.True(t, errors.As(err, &refused), "err = %v, want *service.KeyStoreWriteRefusedError", err)
	}

	t.Run("tenant cannot overwrite an entry with saveCert", func(t *testing.T) {
		_, err := registry.handleXKMSSaveCert(acme, map[string]interface{}{"key_id": keyID, "cert_pem": acmeCertPEM})
		requireWriteRefused(t, err, "saveCert")
		require.Equal(t, int64(42), storedCertificateSerial(t, keyID))
	})
	t.Run("tenant cannot overwrite an entry with saveCertChain", func(t *testing.T) {
		_, err := registry.handleXKMSSaveCertChain(acme, map[string]interface{}{"key_id": keyID, "chain_pems": []interface{}{acmeCertPEM}})
		requireWriteRefused(t, err, "saveCertChain")
		require.Equal(t, int64(42), storedCertificateSerial(t, keyID))
	})
	t.Run("after the refused overwrite the tenant still cannot read or delete the entry", func(t *testing.T) {
		_, err := registry.handleXKMSGetCert(acme, keyArgs)
		var opErr *XKMSOperationError
		require.True(t, errors.As(err, &opErr), "getCert err = %v, want *XKMSOperationError", err)
		_, err = registry.handleXKMSDeleteCert(acme, keyArgs)
		require.True(t, errors.As(err, &opErr), "deleteCert err = %v, want *XKMSOperationError", err)
		require.Equal(t, int64(42), storedCertificateSerial(t, keyID))
	})
	t.Run("system caller saves the certificate", func(t *testing.T) {
		_, err := registry.handleXKMSSaveCert(system, map[string]interface{}{"key_id": keyID, "cert_pem": acmeCertPEM})
		require.NoError(t, err)
		require.Equal(t, int64(7), storedCertificateSerial(t, keyID))
	})
}

// TestXKMSCertByKeyID_BindsCallerToIssuingTenant covers xkms.getCert,
// xkms.certExists and xkms.deleteCert. The go-xkms key store records no
// tenant, so a tenant caller reaches a certificate only when the CA issued it
// for that tenant.
func TestXKMSCertByKeyID_BindsCallerToIssuingTenant(t *testing.T) {
	const keyID = "tenant-bound-cert"
	saveKeyStoreCertificate(t, keyID, xkmssdk.NewStorageMemory())
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

// errCertificateUnreadable is the read failure unreadableCertStorage injects.
var errCertificateUnreadable = errors.New("certificate storage unreadable")

// unreadableCertStorage is a certificate store whose entries exist but cannot
// be read once failGet is set.
type unreadableCertStorage struct {
	xkmssdk.StorageBackend
	failGet bool
}

func (s *unreadableCertStorage) Get(ctx context.Context, key string) ([]byte, error) {
	if s.failGet {
		return nil, errCertificateUnreadable
	}
	return s.StorageBackend.Get(ctx, key)
}

// issuingTenantCertificates is a certificate service that reports the tenant
// the CA issued each serial for.
type issuingTenantCertificates struct {
	service.CertificateService
	tenants map[int64]string
}

func (c issuingTenantCertificates) CertificateTenant(_ context.Context, serial *big.Int) (string, error) {
	return c.tenants[serial.Int64()], nil
}

// certificateTenantService serves the MCP test service with a certificate
// service that reports issuing tenants.
type certificateTenantService struct {
	service.QuicBaaS
	certs service.CertificateService
}

func (s certificateTenantService) Certificate() service.CertificateService {
	return s.certs
}

// TestXKMSCertExists_ReadFailureIsNotAbsence pins that once the key store
// reports a certificate present, only the tenant binding refusal reads as
// absence: a certificate that cannot be read is returned as an error.
func TestXKMSCertExists_ReadFailureIsNotAbsence(t *testing.T) {
	const keyID = "acme-cert"
	storage := &unreadableCertStorage{StorageBackend: xkmssdk.NewStorageMemory()}
	saveKeyStoreCertificate(t, keyID, storage)
	svc := certificateTenantService{
		QuicBaaS: newMinimalMockService(),
		certs:    issuingTenantCertificates{tenants: map[int64]string{42: "acme"}},
	}
	registry := NewToolRegistry(svc, newTestLogger(t))
	args := map[string]interface{}{"key_id": keyID}

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

	t.Run("owning tenant sees the certificate", func(t *testing.T) {
		require.True(t, exists(t, tenantctx.WithTenant(context.Background(), "acme")))
	})
	t.Run("another tenant reads it as absent", func(t *testing.T) {
		require.False(t, exists(t, tenantctx.WithTenant(context.Background(), "globex")))
	})
	t.Run("system caller gets the read failure, not absence", func(t *testing.T) {
		storage.failGet = true
		t.Cleanup(func() { storage.failGet = false })
		result, err := registry.handleXKMSCertExists(tenantctx.WithSystem(context.Background()), args)
		require.Nil(t, result)
		var opErr *XKMSOperationError
		require.True(t, errors.As(err, &opErr), "err = %v, want *XKMSOperationError", err)
		require.Equal(t, "certExists", opErr.Operation)
		require.ErrorIs(t, err, errCertificateUnreadable)
	})
}
