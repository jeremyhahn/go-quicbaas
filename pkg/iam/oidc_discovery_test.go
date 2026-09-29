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

package iam_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// newDiscoveryProvider builds a minimal OIDCProvider for discovery tests.
func newDiscoveryProvider(t *testing.T) *iam.OIDCProvider {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	cfg := &iam.OIDCConfig{
		Issuer:           "https://test.quicbaas.example.com",
		SigningAlgorithm: "RS256",
		SigningKey:       key,
		AccessTokenTTL:   time.Hour,
		RefreshTokenTTL:  24 * time.Hour,
		IDTokenTTL:       time.Hour,
		AuthCodeTTL:      10 * time.Minute,
	}

	memStorage := iam.NewMemoryStorage()
	identityStore := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: memStorage,
		RoleStorage: memStorage,
	})

	provider, err := iam.NewOIDCProvider(cfg, iam.OIDCDependencies{
		IdentityStore: identityStore,
		AppStorage:    newMemAppStorage(),
		TokenStorage:  newMemTokenStorage(),
	})
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}
	return provider
}

// TestGetConfiguration_MTLSAuthMethods verifies that the discovery document
// includes the two mTLS client auth methods (RFC 8705 §2).
func TestGetConfiguration_MTLSAuthMethods(t *testing.T) {
	provider := newDiscoveryProvider(t)
	doc := provider.GetConfiguration()

	methods, ok := doc["token_endpoint_auth_methods_supported"].([]string)
	if !ok {
		t.Fatalf("token_endpoint_auth_methods_supported not a []string")
	}

	want := map[string]bool{
		iam.AuthMethodClientSecretBasic:       true,
		iam.AuthMethodClientSecretPost:        true,
		iam.AuthMethodTLSClientAuth:           true,
		iam.AuthMethodSelfSignedTLSClientAuth: true,
	}

	for _, m := range methods {
		delete(want, m)
	}
	if len(want) > 0 {
		t.Fatalf("missing token_endpoint_auth_methods: %v", want)
	}
}

// TestGetConfiguration_TLSCertBoundTokens verifies that the discovery document
// advertises tls_client_certificate_bound_access_tokens: true (RFC 8705 §5.1).
func TestGetConfiguration_TLSCertBoundTokens(t *testing.T) {
	provider := newDiscoveryProvider(t)
	doc := provider.GetConfiguration()

	bound, ok := doc["tls_client_certificate_bound_access_tokens"].(bool)
	if !ok {
		t.Fatalf("tls_client_certificate_bound_access_tokens not a bool")
	}
	if !bound {
		t.Fatal("expected tls_client_certificate_bound_access_tokens to be true")
	}
}

// TestGetConfiguration_MTLSEndpointAlias verifies that the mTLS endpoint alias
// for token_endpoint is present (RFC 8705 §5).
func TestGetConfiguration_MTLSEndpointAlias(t *testing.T) {
	provider := newDiscoveryProvider(t)
	doc := provider.GetConfiguration()

	aliases, ok := doc["mtls_endpoint_aliases"].(map[string]string)
	if !ok {
		t.Fatalf("mtls_endpoint_aliases missing or wrong type")
	}

	if aliases["token_endpoint"] == "" {
		t.Fatal("mtls_endpoint_aliases.token_endpoint must not be empty")
	}
}

// TestGetOAuthASMetadata_TLSCertBoundTokens verifies the AS metadata endpoint
// also advertises mTLS support.
func TestGetOAuthASMetadata_TLSCertBoundTokens(t *testing.T) {
	provider := newDiscoveryProvider(t)
	meta := provider.GetOAuthASMetadata()

	if !meta.TLSClientCertificateBoundAccessTokens {
		t.Fatal("expected TLSClientCertificateBoundAccessTokens to be true")
	}
}

// TestGetOAuthASMetadata_MTLSAuthMethods verifies the AS metadata endpoint
// includes both mTLS auth methods.
func TestGetOAuthASMetadata_MTLSAuthMethods(t *testing.T) {
	provider := newDiscoveryProvider(t)
	meta := provider.GetOAuthASMetadata()

	want := map[string]bool{
		iam.AuthMethodTLSClientAuth:           true,
		iam.AuthMethodSelfSignedTLSClientAuth: true,
	}
	for _, m := range meta.TokenEndpointAuthMethodsSupported {
		delete(want, m)
	}
	if len(want) > 0 {
		t.Fatalf("missing mTLS auth methods in AS metadata: %v", want)
	}
}

// ---------------------------------------------------------------------------
// minimal in-memory TokenStorage for discovery tests
// ---------------------------------------------------------------------------

type memTokenStorage struct{}

func newMemTokenStorage() iam.TokenStorage {
	return &memTokenStorage{}
}

func (m *memTokenStorage) StoreAuthCode(_ context.Context, _ *iam.AuthCodeData) error { return nil }
func (m *memTokenStorage) GetAuthCode(_ context.Context, _ string) (*iam.AuthCodeData, error) {
	return nil, iam.ErrMTLSClientNotFound
}
func (m *memTokenStorage) DeleteAuthCode(_ context.Context, _ string) error { return nil }
func (m *memTokenStorage) StoreToken(_ context.Context, _ *iam.Token) error { return nil }
func (m *memTokenStorage) GetToken(_ context.Context, _ string) (*iam.Token, error) {
	return nil, iam.ErrMTLSClientNotFound
}
func (m *memTokenStorage) GetTokenByValue(_ context.Context, _ string) (*iam.Token, error) {
	return nil, iam.ErrMTLSClientNotFound
}
func (m *memTokenStorage) RevokeToken(_ context.Context, _ string) error { return nil }
func (m *memTokenStorage) IsTokenRevoked(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (m *memTokenStorage) DeleteExpiredTokens(_ context.Context) error { return nil }
func (m *memTokenStorage) ListTokensByUser(_ context.Context, _ string) ([]*iam.Token, error) {
	return nil, nil
}
func (m *memTokenStorage) RevokeUserTokens(_ context.Context, _ string) error { return nil }
