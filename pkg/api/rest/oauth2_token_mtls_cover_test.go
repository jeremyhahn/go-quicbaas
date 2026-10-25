//go:build rest

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

package rest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

const mtlsTrustDomain = "broker.example.com"

type mtlsCoverService struct {
	*testutil.MockQuicBaaS
	provider *iam.OIDCProvider
}

func (m *mtlsCoverService) GetOIDCProvider(_ context.Context) (*iam.OIDCProvider, error) {
	return m.provider, nil
}

func mtlsSpiffeCert(t *testing.T, tenant string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	require.NoError(t, err)
	spiffeURI, err := url.Parse("spiffe://" + tenant + "/svc/agent")
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(123),
		Subject:      pkix.Name{CommonName: "agent", OrganizationalUnit: []string{tenant}},
		URIs:         []*url.URL{spiffeURI},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(cryptorand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

// TokenMTLS issues a cnf-bound token for an mTLS client whose self-signed cert
// thumbprint is registered. Exercises authenticateMTLSClient + resolveAttest +
// provider.TokenMTLS.
func TestTokenMTLS_Success(t *testing.T) {
	const tenant = "tenant-mtls"
	const clientID = "app_tenant-mtls_agent"

	cert := mtlsSpiffeCert(t, tenant)

	memKV := newIntrospectMemKV()
	backend := iam.NewBackendStorage(memKV)
	require.NoError(t, backend.CreateApp(context.Background(), &iam.Application{
		ClientID:                clientID,
		TenantID:                tenant,
		GrantTypes:              []string{iam.GrantTypeClientCredentials},
		TokenEndpointAuthMethod: iam.AuthMethodSelfSignedTLSClientAuth,
		MTLSCertThumbprint:      iam.CertThumbprintS256(cert),
	}))

	privateKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	require.NoError(t, err)
	provider, err := iam.NewOIDCProvider(&iam.OIDCConfig{
		Issuer:            "https://" + mtlsTrustDomain,
		SigningAlgorithm:  "RS256",
		SigningKeyManager: testOIDCKeyManager(t, privateKey),
		AccessTokenTTL:    15 * time.Minute,
		RefreshTokenTTL:   24 * time.Hour,
		IDTokenTTL:        15 * time.Minute,
		AuthCodeTTL:       10 * time.Minute,
	}, iam.OIDCDependencies{
		IdentityStore: iam.NewIdentityStore(iam.IdentityStoreOptions{
			UserStorage: iam.NewMemoryStorage(),
			RoleStorage: iam.NewMemoryStorage(),
		}),
		AppStorage:   backend,
		TokenStorage: backend,
	})
	require.NoError(t, err)

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mockSvc := &mtlsCoverService{MockQuicBaaS: testutil.NewMockQuicBaaS(), provider: provider}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	cfg.SPIFFETrustDomain = mtlsTrustDomain
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc

	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})
	handler.selfSignedAuth = iam.NewSelfSignedClientAuth(backend)

	form := url.Values{
		"grant_type": {"client_credentials"},
		"client_id":  {clientID},
		"scope":      {"openid"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token-mtls", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	w := httptest.NewRecorder()
	handler.TokenMTLS(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "access_token")
}

// authenticateMTLSClient returns the no-cert sentinel when no authenticator and
// no app-storage getter are wired.
func TestAuthenticateMTLSClient_NoAuthenticators(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	cert := mtlsSpiffeCert(t, "t")
	err := handler.authenticateMTLSClient(context.Background(), cert, "app_t_x")
	assert.Error(t, err)
}

// resolveAttest returns the manual method when no TPM resolver is configured.
func TestResolveAttest_Manual(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	cert := mtlsSpiffeCert(t, "t")
	assert.Equal(t, iam.AttestMethodManual, handler.resolveAttest(cert))
}

// TokenMTLS rejects a client_id whose encoded tenant differs from the cert's
// SPIFFE tenant.
func TestTokenMTLS_ClientIDTenantMismatch(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()
	handler.server.config.SPIFFETrustDomain = mtlsTrustDomain

	cert := mtlsSpiffeCert(t, "tenant-a")
	form := url.Values{
		"grant_type": {"client_credentials"},
		"client_id":  {"app_tenant-b_x"}, // tenant-b != cert tenant-a
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token-mtls", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	w := httptest.NewRecorder()
	handler.TokenMTLS(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TokenMTLS fails closed when the certificate's trust domain is foreign so the
// tenant cannot be resolved.
func TestTokenMTLS_TenantUnresolved(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()
	handler.server.config.SPIFFETrustDomain = "operator.example.com"

	// Cert carries a SPIFFE SAN for a DIFFERENT (foreign) trust domain.
	cert := mtlsSpiffeCert(t, "tenant-a") // trust domain = mtlsTrustDomain
	form := url.Values{
		"grant_type": {"client_credentials"},
		"client_id":  {"app_tenant-a_x"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token-mtls", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	w := httptest.NewRecorder()
	handler.TokenMTLS(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// setupMTLSFullHandler builds a handler with a real OIDC provider and a
// self-signed mTLS app registered for the given cert.
func setupMTLSFullHandler(t *testing.T, tenant, clientID string, cert *x509.Certificate) *IAMHandler {
	t.Helper()
	backend := iam.NewBackendStorage(newIntrospectMemKV())
	require.NoError(t, backend.CreateApp(context.Background(), &iam.Application{
		ClientID:                clientID,
		TenantID:                tenant,
		GrantTypes:              []string{iam.GrantTypeClientCredentials},
		TokenEndpointAuthMethod: iam.AuthMethodSelfSignedTLSClientAuth,
		MTLSCertThumbprint:      iam.CertThumbprintS256(cert),
	}))
	privateKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	require.NoError(t, err)
	provider, err := iam.NewOIDCProvider(&iam.OIDCConfig{
		Issuer:            "https://" + mtlsTrustDomain,
		SigningAlgorithm:  "RS256",
		SigningKeyManager: testOIDCKeyManager(t, privateKey),
		AccessTokenTTL:    15 * time.Minute,
		RefreshTokenTTL:   24 * time.Hour,
		IDTokenTTL:        15 * time.Minute,
		AuthCodeTTL:       10 * time.Minute,
	}, iam.OIDCDependencies{
		IdentityStore: iam.NewIdentityStore(iam.IdentityStoreOptions{
			UserStorage: iam.NewMemoryStorage(), RoleStorage: iam.NewMemoryStorage(),
		}),
		AppStorage: backend, TokenStorage: backend,
	})
	require.NoError(t, err)
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := &mtlsCoverService{MockQuicBaaS: testutil.NewMockQuicBaaS(), provider: provider}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	cfg.SPIFFETrustDomain = mtlsTrustDomain
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc
	h := NewIAMHandler(IAMHandlerDependencies{Server: server})
	h.selfSignedAuth = iam.NewSelfSignedClientAuth(backend)
	return h
}

// An unsupported grant_type surfaces a provider OAuthError mapped to a 4xx.
func TestTokenMTLS_ProviderOAuthError(t *testing.T) {
	const tenant = "tenant-err"
	const clientID = "app_tenant-err_agent"
	cert := mtlsSpiffeCert(t, tenant)
	handler := setupMTLSFullHandler(t, tenant, clientID, cert)

	form := url.Values{
		"grant_type": {"urn:bogus:grant"},
		"client_id":  {clientID},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token-mtls", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	w := httptest.NewRecorder()
	handler.TokenMTLS(w, req)

	assert.GreaterOrEqual(t, w.Code, 400)
}
