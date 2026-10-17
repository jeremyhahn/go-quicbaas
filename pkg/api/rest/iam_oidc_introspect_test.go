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
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"

	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// introspectMemKV is a minimal in-memory KVStore for the introspection tests.
// It backs a real iam.BackendStorage so the OIDC provider mints, stores, and
// revokes tokens through its production code paths (no test-only shortcuts).
type introspectMemKV struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func newIntrospectMemKV() *introspectMemKV {
	return &introspectMemKV{data: make(map[string][]byte)}
}

func (k *introspectMemKV) Put(_ context.Context, key string, value []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	cp := make([]byte, len(value))
	copy(cp, value)
	k.data[key] = cp
	return nil
}

func (k *introspectMemKV) Get(_ context.Context, key string) ([]byte, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	v, ok := k.data[key]
	if !ok {
		return nil, &iam.IAMMessageError{Message: "not found"}
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

func (k *introspectMemKV) Delete(_ context.Context, key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.data, key)
	return nil
}

func (k *introspectMemKV) Scan(_ context.Context, prefix string, fn func(string, []byte) error) error {
	k.mu.RLock()
	snapshot := make(map[string][]byte, len(k.data))
	for key, v := range k.data {
		snapshot[key] = v
	}
	k.mu.RUnlock()
	for key, v := range snapshot {
		if strings.HasPrefix(key, prefix) {
			if err := fn(key, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (k *introspectMemKV) List(_ context.Context, prefix string) ([]string, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	var keys []string
	for key := range k.data {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (k *introspectMemKV) Exists(_ context.Context, key string) (bool, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	_, ok := k.data[key]
	return ok, nil
}

// introspectMockService exposes a real OIDC provider and audit logger.
type introspectMockService struct {
	*testutil.MockQuicBaaS
	provider    *iam.OIDCProvider
	auditLogger iam.AuditLogger
}

func (m *introspectMockService) GetOIDCProvider(_ context.Context) (*iam.OIDCProvider, error) {
	return m.provider, nil
}

func (m *introspectMockService) GetAuditLogger(_ context.Context) (iam.AuditLogger, error) {
	return m.auditLogger, nil
}

const (
	introspectTenant   = "tenant-a"
	introspectClientID = "app_tenant-a_rs"
	introspectSecret   = "rs-secret"
)

// setupIntrospectHandler builds a handler backed by a real OIDC provider and a
// real audit logger, and registers a confidential client_credentials client.
func setupIntrospectHandler(t *testing.T) (*IAMHandler, *iam.OIDCProvider, iam.AuditLogger, func()) {
	t.Helper()
	f := newIntrospectFixture(t)
	return f.handler, f.provider, f.auditLogger, f.cleanup
}

// introspectFixture is the introspection handler with the stores behind it,
// for tests that must seed a user before minting a user token.
type introspectFixture struct {
	handler       *IAMHandler
	provider      *iam.OIDCProvider
	auditLogger   iam.AuditLogger
	identityStore *iam.IdentityStore
	appStorage    *iam.BackendStorage
	cleanup       func()
}

func newIntrospectFixture(t *testing.T) *introspectFixture {
	t.Helper()

	privateKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	require.NoError(t, err)

	kv := newIntrospectMemKV()
	backend := iam.NewBackendStorage(kv)
	identityStore := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: iam.NewMemoryStorage(),
		RoleStorage: iam.NewMemoryStorage(),
	})

	provider, err := iam.NewOIDCProvider(&iam.OIDCConfig{
		Issuer:            "https://broker.example.com",
		SigningAlgorithm:  "RS256",
		SigningKeyManager: testOIDCKeyManager(t, privateKey),
		AccessTokenTTL:    15 * time.Minute,
		RefreshTokenTTL:   24 * time.Hour,
		IDTokenTTL:        15 * time.Minute,
		AuthCodeTTL:       10 * time.Minute,
	}, iam.OIDCDependencies{
		IdentityStore: identityStore,
		AppStorage:    backend,
		TokenStorage:  backend,
	})
	require.NoError(t, err)

	// The stored form is the PBKDF2 digest since the #37 migration; a bcrypt
	// digest here fails closed under GODEBUG=fips140=only, which this package
	// is gated on.
	introspectSec, err := secure.NewSecret([]byte(introspectSecret))
	require.NoError(t, err)
	hashed, err := iam.HashClientSecret(introspectSec)
	introspectSec.Destroy()
	require.NoError(t, err)
	require.NoError(t, backend.CreateApp(context.Background(), &iam.Application{
		ClientID:     introspectClientID,
		ClientSecret: hashed,
		TenantID:     introspectTenant,
		GrantTypes:   []string{iam.GrantTypeClientCredentials},
		// The grant this client holds. A client_credentials token carries the
		// permissions its application record was granted, never the ones the
		// client asked for (go-quicbaas#653), so a fixture that requests a
		// permission must be granted it.
		Scopes: iam.DefaultFleetRuntimeScopes(),
	}))

	auditLogger, err := iam.NewAuditLogger()
	require.NoError(t, err)

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mockSvc := &introspectMockService{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		provider:     provider,
		auditLogger:  auditLogger,
	}

	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc

	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

	cleanup := func() {
		_ = auditLogger.Close()
		server.Close()
	}
	return &introspectFixture{
		handler:       handler,
		provider:      provider,
		auditLogger:   auditLogger,
		identityStore: identityStore,
		appStorage:    backend,
		cleanup:       cleanup,
	}
}

// mintToken issues an access token through the public client_credentials grant.
func mintToken(t *testing.T, provider *iam.OIDCProvider) string {
	t.Helper()
	ctx := tenantctx.WithTenant(context.Background(), introspectTenant)
	resp, err := provider.Token(ctx, &iam.TokenRequest{
		GrantType:    iam.GrantTypeClientCredentials,
		ClientID:     introspectClientID,
		ClientSecret: introspectSecret,
		Scope:        []string{"openid", "agent:runtime:spawn"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.AccessToken)
	return resp.AccessToken
}

// introspectRequest builds a POST /oauth2/introspect with the token form field
// and, when withCert is true, a verified mTLS peer-cert chain (caller auth).
func introspectRequest(token string, withCert bool) *http.Request {
	form := url.Values{}
	form.Set("token", token)
	req := httptest.NewRequest(http.MethodPost, "/oauth2/introspect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if withCert {
		spiffeURI, _ := url.Parse("spiffe://broker.example.com/tenant/tenant-a/svc/resource-server")
		cert := &x509.Certificate{
			Subject:      pkix.Name{CommonName: "rs.example.com"},
			URIs:         []*url.URL{spiffeURI},
			SerialNumber: big.NewInt(7),
		}
		req.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{cert}}}
	}
	return req
}

// TestIntrospect_ActiveWithCertCaller verifies a valid token introspected by an
// mTLS-authenticated caller returns active=true with the standard claims.
func TestIntrospect_ActiveWithCertCaller(t *testing.T) {
	handler, provider, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	token := mintToken(t, provider)
	w := httptest.NewRecorder()
	handler.Introspect(w, introspectRequest(token, true))

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, true, resp["active"])
	assert.Equal(t, introspectClientID, resp["sub"])
	assert.Equal(t, introspectTenant, resp["tenant_id"])
	assert.Equal(t, "https://broker.example.com", resp["iss"])
	assert.Contains(t, resp, "exp")
	assert.Contains(t, resp, "jti")
}

// TestIntrospect_InactiveForInvalid verifies an invalid token returns
// active=false and leaks no claims.
func TestIntrospect_InactiveForInvalid(t *testing.T) {
	handler, _, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.Introspect(w, introspectRequest("garbage.token.value", true))

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, false, resp["active"])
	_, hasSub := resp["sub"]
	assert.False(t, hasSub, "inactive response must not leak sub")
}

// TestIntrospect_InactiveForRevoked verifies a revoked token returns
// active=false.
func TestIntrospect_InactiveForRevoked(t *testing.T) {
	handler, provider, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	token := mintToken(t, provider)

	// Revoke via the public RFC 7009 endpoint using client credentials.
	require.NoError(t, provider.Revoke(tenantctx.WithTenant(context.Background(), introspectTenant),
		&iam.RevokeRequest{
			Token:        token,
			ClientID:     introspectClientID,
			ClientSecret: introspectSecret,
		}))

	w := httptest.NewRecorder()
	handler.Introspect(w, introspectRequest(token, true))

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, false, resp["active"])
}

// TestIntrospect_RejectsUnauthenticatedCaller verifies an introspection request
// with no client certificate and no client credentials is rejected (401).
func TestIntrospect_RejectsUnauthenticatedCaller(t *testing.T) {
	handler, provider, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	token := mintToken(t, provider)
	w := httptest.NewRecorder()
	handler.Introspect(w, introspectRequest(token, false)) // no cert, no credentials

	require.Equal(t, http.StatusUnauthorized, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "invalid_client", resp["error"])
}

// TestIntrospect_DerivesTenantFromClaim verifies the response tenant comes from
// the token's tenant_id claim, with no tenant hint supplied by the caller.
func TestIntrospect_DerivesTenantFromClaim(t *testing.T) {
	handler, provider, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	token := mintToken(t, provider)
	w := httptest.NewRecorder()
	handler.Introspect(w, introspectRequest(token, true))

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, true, resp["active"])
	assert.Equal(t, introspectTenant, resp["tenant_id"])
}

// TestIntrospect_DualIdentityAudit verifies introspecting an active token emits
// an audit event carrying BOTH the service identity (cert SPIFFE) and the user
// identity (sub/tenant_id/scp), stamped with the tenant.
func TestIntrospect_DualIdentityAudit(t *testing.T) {
	handler, provider, auditLogger, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	token := mintToken(t, provider)
	w := httptest.NewRecorder()
	handler.Introspect(w, introspectRequest(token, true))
	require.Equal(t, http.StatusOK, w.Code)

	events, _, err := auditLogger.Query(context.Background(), &iam.AuditFilter{TenantID: introspectTenant})
	require.NoError(t, err)
	require.Len(t, events, 1)

	ev := events[0]
	assert.Equal(t, introspectTenant, ev.TenantID)
	require.NotNil(t, ev.Actor)
	assert.Equal(t, "spiffe://broker.example.com/tenant/tenant-a/svc/resource-server", ev.Actor.ServiceIdentity)
	assert.Equal(t, introspectClientID, ev.Actor.UserID)
	assert.Contains(t, ev.Actor.Scopes, "agent:runtime:spawn")
	assert.Equal(t, "oauth2.introspect", ev.Action)
}

// TestIntrospect_AuditCarriesNetworkSource verifies the introspection audit
// event records the network source of the introspecting caller (AU-3).
//
// The address is the TRANSPORT PEER, not X-Forwarded-For. This test used to
// assert the opposite -- that the header won -- which is the defect #156 was
// filed for: the caller chose what the audit trail said about it, and forged
// provenance is worse than an absent field because the record looks
// authoritative. A forwarded client is honoured only when the peer is a
// configured trusted proxy, which is resolved by ClientIPMiddleware; this test
// calls the handler directly, so no proxy is trusted and the header must lose.
func TestIntrospect_AuditCarriesNetworkSource(t *testing.T) {
	handler, provider, auditLogger, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	token := mintToken(t, provider)
	req := introspectRequest(token, true)
	req.RemoteAddr = "203.0.113.9:44321"
	req.Header.Set("X-Forwarded-For", "198.51.100.23, 10.0.0.1")
	req.Header.Set("User-Agent", "quicbaas-rs/2.0")

	w := httptest.NewRecorder()
	handler.Introspect(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	events, _, err := auditLogger.Query(context.Background(), &iam.AuditFilter{TenantID: introspectTenant})
	require.NoError(t, err)
	require.Len(t, events, 1)

	ev := events[0]
	assert.Equal(t, "203.0.113.9", ev.IPAddress,
		"the audit record must name the transport peer, not a header the caller chose")
	assert.NotEqual(t, "198.51.100.23", ev.IPAddress,
		"a forged X-Forwarded-For reached the audit trail")
	assert.Equal(t, "quicbaas-rs/2.0", ev.UserAgent)
}

// TestToken_AuditCarriesNetworkSource verifies the token-endpoint dual-identity
// audit event records the network source (AU-3) as the TRANSPORT PEER, not
// X-Real-IP. See TestIntrospect_AuditCarriesNetworkSource: the header is
// believed only from a configured trusted proxy, and this calls the handler
// directly, so none is trusted. The old assertion encoded the #156 defect.
//
// It records the user
// agent of the requesting client (AU-3 network source). It reuses the
// introspect harness because that registers a real client_credentials client
// against a real OIDC provider + audit logger.
func TestToken_AuditCarriesNetworkSource(t *testing.T) {
	handler, _, auditLogger, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	form := url.Values{}
	form.Set("grant_type", iam.GrantTypeClientCredentials)
	form.Set("client_id", introspectClientID)
	form.Set("client_secret", introspectSecret)

	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.9:44321"
	req.Header.Set("X-Real-IP", "192.0.2.55")
	req.Header.Set("User-Agent", "quicbaas-client/3.1")

	w := httptest.NewRecorder()
	handler.Token(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	events, _, err := auditLogger.Query(context.Background(), &iam.AuditFilter{TenantID: introspectTenant})
	require.NoError(t, err)
	require.Len(t, events, 1)

	ev := events[0]
	assert.Equal(t, "oauth2.token", ev.Action)
	assert.Equal(t, "203.0.113.9", ev.IPAddress,
		"the audit record must name the transport peer, not a header the caller chose")
	assert.NotEqual(t, "192.0.2.55", ev.IPAddress,
		"a forged X-Real-IP reached the audit trail")
	assert.Equal(t, "quicbaas-client/3.1", ev.UserAgent)
}
