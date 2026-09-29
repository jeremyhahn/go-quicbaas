// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package iam

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"sync"
	"testing"
	"time"
)

// tenantRoutedSigningKey derives ONE RSA key for the whole file. Key generation
// is seconds under the race detector and the tests here are about tenant
// routing, not about the key.
var tenantRoutedSigningKey = sync.OnceValues(func() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 2048)
})

// =============================================================================
// go-quicbaas #644 — the whole grant, end to end, over a tenant-routed store.
//
// The agent registers through the AppManager under the PUBLIC route's context
// (no tenant), then presents the issued credential to the OIDC provider under
// the token endpoint's context (the tenant encoded in the client id). That is
// the exact pair of contexts the burn-in ran, and the pair that returned 401
// invalid_client on every attempt.
// =============================================================================

// newTenantRoutedOIDCProvider builds a provider whose app and token storage is
// the real BackendStorage over a routed KV, with tenantID barrier-isolated.
func newTenantRoutedOIDCProvider(t *testing.T, tenantID string) (*OIDCProvider, *AppManager, *BackendStorage) {
	t.Helper()

	key, err := tenantRoutedSigningKey()
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}

	kv := newRoutingTestKV()
	kv.barrierTenant(tenantID)
	storage := NewBackendStorage(kv)

	provider, err := NewOIDCProvider(&OIDCConfig{
		Issuer:           "https://burnin.example.test",
		SigningAlgorithm: "RS256",
		SigningKey:       key,
		AccessTokenTTL:   time.Hour,
		RefreshTokenTTL:  24 * time.Hour,
		IDTokenTTL:       time.Hour,
		AuthCodeTTL:      10 * time.Minute,
	}, OIDCDependencies{
		IdentityStore: NewIdentityStore(IdentityStoreOptions{
			UserStorage: NewMemoryStorage(),
			RoleStorage: NewMemoryStorage(),
		}),
		AppStorage:   storage,
		TokenStorage: storage,
	})
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	return provider, NewAppManager(storage, nil), storage
}

// TestClientCredentialsGrant_AcceptsTheCredentialRegistrationIssued is the
// #644 regression at the grant seam.
func TestClientCredentialsGrant_AcceptsTheCredentialRegistrationIssued(t *testing.T) {
	const tenantID = "automatethethings"
	provider, mgr, _ := newTenantRoutedOIDCProvider(t, tenantID)

	// Registration: the public route's context names no tenant.
	app, err := mgr.RegisterAgentApp(context.Background(), tenantID, "agent-gpu-host",
		agentAppOptions(tenantID, "agent-gpu-host"))
	if err != nil {
		t.Fatalf("RegisterAgentApp: %v", err)
	}

	// Token: the endpoint scopes the context to the client id's tenant.
	tokenCtx := ContextWithTenant(context.Background(), TenantFromClientID(app.ClientID))
	resp, err := provider.Token(tokenCtx, &TokenRequest{
		GrantType:    GrantTypeClientCredentials,
		ClientID:     app.ClientID,
		ClientSecret: app.ClientSecret,
	})
	if err != nil {
		t.Fatalf("client_credentials grant: %v (this is the #644 invalid_client)", err)
	}
	if resp.AccessToken == "" {
		t.Fatal("grant returned no access token")
	}
}

// TestClientCredentialsGrant_RefusesAWrongSecret is the error path: the same
// opaque invalid_client, so the response cannot distinguish a client that
// exists from one that does not.
func TestClientCredentialsGrant_RefusesAWrongSecret(t *testing.T) {
	const tenantID = "automatethethings"
	provider, mgr, _ := newTenantRoutedOIDCProvider(t, tenantID)

	app, err := mgr.RegisterAgentApp(context.Background(), tenantID, "agent-gpu-host",
		agentAppOptions(tenantID, "agent-gpu-host"))
	if err != nil {
		t.Fatalf("RegisterAgentApp: %v", err)
	}
	tokenCtx := ContextWithTenant(context.Background(), tenantID)

	wrongSecret, err := provider.Token(tokenCtx, &TokenRequest{
		GrantType:    GrantTypeClientCredentials,
		ClientID:     app.ClientID,
		ClientSecret: "sk_not-the-issued-secret",
	})
	if err == nil {
		t.Fatalf("a wrong secret minted %q", wrongSecret.AccessToken)
	}
	unknownClient, err2 := provider.Token(tokenCtx, &TokenRequest{
		GrantType:    GrantTypeClientCredentials,
		ClientID:     FleetNodeClientID(tenantID, "agent-that-never-enrolled"),
		ClientSecret: app.ClientSecret,
	})
	if err2 == nil {
		t.Fatalf("an unknown client minted %q", unknownClient.AccessToken)
	}

	// The two refusals must be indistinguishable to the caller: a response that
	// told them apart would enumerate the tenant's client ids.
	if err.Error() != err2.Error() {
		t.Fatalf("wrong secret answered %q but unknown client answered %q", err, err2)
	}
}
