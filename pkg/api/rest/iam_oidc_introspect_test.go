//go:build rest && iam

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
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
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

	privateKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	require.NoError(t, err)

	kv := newIntrospectMemKV()
	backend := iam.NewBackendStorage(kv)
	identityStore := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  iam.NewMemoryStorage(),
		GroupStorage: iam.NewMemoryStorage(),
		RoleStorage:  iam.NewMemoryStorage(),
	})

	provider, err := iam.NewOIDCProvider(&iam.OIDCConfig{
		Issuer:           "https://broker.example.com",
		SigningAlgorithm: "RS256",
		SigningKey:       privateKey,
		AccessTokenTTL:   15 * time.Minute,
		RefreshTokenTTL:  24 * time.Hour,
		IDTokenTTL:       15 * time.Minute,
		AuthCodeTTL:      10 * time.Minute,
	}, iam.OIDCDependencies{
		IdentityStore: identityStore,
		AppStorage:    backend,
		TokenStorage:  backend,
	})
	require.NoError(t, err)

	hashed, err := bcrypt.GenerateFromPassword([]byte(introspectSecret), bcrypt.DefaultCost)
	require.NoError(t, err)
	require.NoError(t, backend.CreateApp(context.Background(), &iam.Application{
		ClientID:     introspectClientID,
		ClientSecret: string(hashed),
		TenantID:     introspectTenant,
		GrantTypes:   []string{iam.GrantTypeClientCredentials},
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
	return handler, provider, auditLogger, cleanup
}

// mintToken issues an access token through the public client_credentials grant.
func mintToken(t *testing.T, provider *iam.OIDCProvider) string {
	t.Helper()
	ctx := iam.ContextWithTenant(context.Background(), introspectTenant)
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
	require.NoError(t, provider.Revoke(iam.ContextWithTenant(context.Background(), introspectTenant),
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
