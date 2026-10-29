//go:build rest

package rest

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	transportTypes "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// issuingTenantCertificates is a certificate service whose CA reports the
// tenant each serial was issued for.
type issuingTenantCertificates struct {
	service.CertificateService
	tenants map[string]string
}

// unissuedSerialError reports a serial the CA did not issue.
type unissuedSerialError struct{ serial string }

func (e *unissuedSerialError) Error() string { return "serial " + e.serial + " was not issued" }

func (c issuingTenantCertificates) CertificateTenant(_ context.Context, serial *big.Int) (string, error) {
	tenant, ok := c.tenants[serial.String()]
	if !ok {
		return "", &unissuedSerialError{serial: serial.String()}
	}
	return tenant, nil
}

// TestXKMSCertsKeyStore_TenantScope pins #1998 on REST /api/v1/xkms/certs: a
// tenant lists only the certificates issued for it and cannot write the key
// store; the system tenant lists and writes everything.
func TestXKMSCertsKeyStore_TenantScope(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, mockService, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	acmePEM, globexPEM := keystoreTestCertPEM(t, 101), keystoreTestCertPEM(t, 102)
	mockService.CertificateSvc = issuingTenantCertificates{tenants: map[string]string{
		"101": "acme",
		"102": "globex",
	}}

	acme := tenantctx.WithTenant(context.Background(), "acme")
	system := tenantctx.WithSystem(context.Background())
	serve := func(ctx context.Context, method, target, certPEM string) *httptest.ResponseRecorder {
		t.Helper()
		var body []byte
		if certPEM != "" {
			var err error
			body, err = json.Marshal(map[string]string{"certificate_pem": certPEM})
			require.NoError(t, err)
		}
		req := httptest.NewRequest(method, target, bytes.NewReader(body)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		return w
	}
	listIDs := func(ctx context.Context) []string {
		t.Helper()
		w := serve(ctx, http.MethodGet, "/api/v1/xkms/certs", "")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp transportTypes.ListCertificatesResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		ids := make([]string, 0, len(resp.Certificates))
		for _, cert := range resp.Certificates {
			ids = append(ids, cert.KeyID)
		}
		return ids
	}

	t.Run("tenant cannot write the key store", func(t *testing.T) {
		w := serve(acme, http.MethodPost, "/api/v1/xkms/certs?key_id=acme-planted", acmePEM)
		require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		require.NotContains(t, listIDs(system), "acme-planted")
	})
	t.Run("caller without a tenant cannot write the key store", func(t *testing.T) {
		w := serve(context.Background(), http.MethodPost, "/api/v1/xkms/certs?key_id=anonymous", acmePEM)
		require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	})
	t.Run("system writes the key store", func(t *testing.T) {
		require.Equal(t, http.StatusOK, serve(system, http.MethodPost, "/api/v1/xkms/certs?key_id=acme-leaf", acmePEM).Code)
		require.Equal(t, http.StatusOK, serve(system, http.MethodPost, "/api/v1/xkms/certs?key_id=globex-leaf", globexPEM).Code)
	})
	t.Run("tenant lists only its own certificates", func(t *testing.T) {
		ids := listIDs(acme)
		require.Contains(t, ids, "acme-leaf")
		require.NotContains(t, ids, "globex-leaf")
	})
	t.Run("system lists every certificate", func(t *testing.T) {
		ids := listIDs(system)
		require.Contains(t, ids, "acme-leaf")
		require.Contains(t, ids, "globex-leaf")
	})
	t.Run("caller without a tenant cannot list", func(t *testing.T) {
		w := serve(context.Background(), http.MethodGet, "/api/v1/xkms/certs", "")
		require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	})
	// Codex 8293 P1: an ownership store outage is a server error, not a
	// refusal the client could act on.
	t.Run("ownership store failure is a server error", func(t *testing.T) {
		mockService.CertificateSvc = ownershipStoreDownCertificates{}
		w := serve(acme, http.MethodGet, "/api/v1/xkms/certs", "")
		require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	})
	t.Run("ownership store failure on the bind path is a server error", func(t *testing.T) {
		mockService.CertificateSvc = ownershipStoreDownCertificates{}
		for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodHead} {
			w := serve(acme, method, "/api/v1/xkms/certs/acme-leaf", "")
			require.Equal(t, http.StatusInternalServerError, w.Code, "%s: %s", method, w.Body.String())
		}
		require.Contains(t, listIDs(system), "acme-leaf", "a failed bind must not delete the certificate")
	})
}

func TestXKMSCertificateLookupStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "ownership read failure is a server error", err: &qberrors.QBError{Code: qberrors.ErrInternal, Op: "certificate.bind_issuing_tenant", Err: ownershipStoreDownError{}}, want: http.StatusInternalServerError},
		{name: "tenant refusal answers like a missing certificate", err: &service.CertificateTenantError{CallerTenantID: "acme", IssuingTenantID: "globex"}, want: http.StatusNotFound},
		{name: "other QBError answers like a missing certificate", err: &qberrors.QBError{Code: qberrors.ErrNotFound}, want: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, xkmsCertificateLookupStatus(tt.err))
		})
	}
}

// ownershipStoreDownCertificates is a certificate service whose CA cannot
// reach its ownership store.
type ownershipStoreDownCertificates struct {
	service.CertificateService
}

// ownershipStoreDownError reports the unreachable ownership store.
type ownershipStoreDownError struct{}

func (ownershipStoreDownError) Error() string { return "ownership store unavailable" }

func (ownershipStoreDownCertificates) CertificateTenant(context.Context, *big.Int) (string, error) {
	return "", ownershipStoreDownError{}
}

// keystoreTestCertPEM returns a self-signed PEM certificate with the given
// serial, so the CA can tell the test certificates apart.
func keystoreTestCertPEM(t *testing.T, serial int64) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "keystore-test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
