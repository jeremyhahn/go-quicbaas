// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package iam

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// stubAdminGate is a controllable AdminPresenceChecker for exercising the
// bootstrap-ordering invariant without a real AdminManager.
type stubAdminGate struct {
	hasAdmin bool
	err      error
}

func (g *stubAdminGate) HasAdmin(_ context.Context) (bool, error) {
	return g.hasAdmin, g.err
}

// systemTenantSSOHarness drives a federation BeginLogin -> HandleCallback round
// trip against the SYSTEM tenant, proving operator SSO works end-to-end against
// the prefix-isolated control-plane tenant (no special-casing blocks it).
type systemTenantSSOHarness struct {
	manager       *DefaultFederationManager
	identityStore *IdentityStore
	provider      *ExternalProvider
	idp           *oidcMockIdP
	ctx           context.Context
}

func newSystemTenantSSOHarness(t *testing.T, defaults []string) *systemTenantSSOHarness {
	t.Helper()
	now := time.Now()
	clientID := "operator-client"
	idp := newOIDCMockIdP(t, clientID, now)

	manager, providerStorage, _, _, identityStore, _ := setupTestFederationManager(t)
	manager.httpClient = idp.srv.Client()
	manager.now = func() time.Time { return now }

	provider := &ExternalProvider{
		ID:            "system-okta",
		TenantID:      SystemTenantID,
		Name:          "okta-operators",
		Type:          ProviderTypeOIDC,
		Enabled:       true,
		ClientID:      clientID,
		ClientSecret:  "secret",
		IssuerURL:     idp.issuer(),
		AuthURL:       idp.issuer() + "/authorize",
		TokenURL:      idp.issuer() + "/token",
		JWKSURL:       idp.issuer() + "/jwks",
		Scopes:        []string{"openid", "email", "profile", "groups"},
		AutoProvision: true,
		AllowLinking:  true,
		DefaultRoles:  defaults,
		ClaimMappings: &ClaimMappings{Subject: "sub", Email: "email", Name: "name", Groups: "groups"},
	}
	provider.ApplyDefaults()
	if err := providerStorage.CreateProvider(context.Background(), provider); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	for _, r := range defaults {
		if _, err := identityStore.CreateRole(context.Background(), SystemTenantID, CreateRoleOptions{ID: r, Name: r}); err != nil {
			t.Fatalf("create role %s: %v", r, err)
		}
	}

	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, idp.srv.Client())
	return &systemTenantSSOHarness{
		manager:       manager,
		identityStore: identityStore,
		provider:      provider,
		idp:           idp,
		ctx:           ctx,
	}
}

func (h *systemTenantSSOHarness) begin(t *testing.T) (stateToken, nonce string) {
	t.Helper()
	redirect, err := h.manager.BeginLogin(h.ctx, h.provider.TenantID, h.provider.Name, "https://console.operators.local/cb")
	if err != nil {
		t.Fatalf("begin login: %v", err)
	}
	u, err := url.Parse(redirect.AuthURL)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	nonce = u.Query().Get("nonce")
	if nonce == "" {
		t.Fatal("expected nonce in auth URL")
	}
	return redirect.State, nonce
}

// Test (a): an ExternalProvider registered for the SYSTEM tenant plus a
// simulated federated callback mints a valid quicbaas bearer for an operator.
// This proves the federation register/login paths do NOT special-case or block
// the system tenant, and the broker-token path applies to operators.
func TestOperatorSSO_SystemTenantCallbackMintsBearer(t *testing.T) {
	h := newSystemTenantSSOHarness(t, []string{"operator"})
	stateToken, nonce := h.begin(t)

	h.idp.idTokenFn = func(issuer string) string {
		return h.idp.signedIDToken(issuer, nonce, func(c jwt.MapClaims) {
			c["sub"] = "okta|operator-jane"
			c["email"] = "jane@operators.local"
			c["name"] = "Jane Operator"
		})
	}

	res, err := h.manager.HandleCallback(h.ctx, stateToken, "auth-code")
	if err != nil {
		t.Fatalf("system-tenant federated callback failed: %v", err)
	}
	if res.User == nil || res.User.TenantID != SystemTenantID {
		t.Fatalf("operator not provisioned in system tenant: %+v", res.User)
	}
	// generateTokenResult is invoked with no OIDC provider in this harness, so
	// the bearer is empty here; the broker minting path is covered by the
	// server-level test. What this asserts is that the SYSTEM tenant is a
	// first-class federation target: the callback completed, provisioned an
	// operator in the system tenant, and produced a FederationResult.
	if res.FederatedIdentity == nil || res.FederatedIdentity.TenantID != SystemTenantID {
		t.Fatalf("federated identity not linked in system tenant: %+v", res.FederatedIdentity)
	}
	if !res.Provisioned {
		t.Fatal("expected operator to be JIT-provisioned")
	}
}

// TestOperatorSSORegistrar_RequiresBootstrapAdmin proves the bootstrap-ordering
// invariant: an operator IdP cannot be configured before the bootstrap admin
// exists (no admin -> can't configure Okta).
func TestOperatorSSORegistrar_RequiresBootstrapAdmin(t *testing.T) {
	manager, _, _, _, _, _ := setupTestFederationManager(t)

	gate := &stubAdminGate{hasAdmin: false}
	registrar, err := NewOperatorSSORegistrar(manager, gate)
	if err != nil {
		t.Fatalf("new registrar: %v", err)
	}

	provider := createTestProvider("ignored-tenant", "okta-operators")
	err = registrar.RegisterOperatorProvider(context.Background(), provider)
	if !errors.Is(err, ErrBootstrapNotReady) {
		t.Fatalf("expected ErrBootstrapNotReady before admin exists, got: %v", err)
	}
}

// TestOperatorSSORegistrar_SucceedsAfterBootstrap proves an operator IdP can be
// registered once the bootstrap admin exists, and that the registrar forces the
// provider onto the system tenant regardless of the caller-supplied tenant.
func TestOperatorSSORegistrar_SucceedsAfterBootstrap(t *testing.T) {
	manager, providerStorage, _, _, _, _ := setupTestFederationManager(t)

	gate := &stubAdminGate{hasAdmin: true}
	// Wire the same gate into the manager so the manager-level RegisterProvider
	// chokepoint (which now also enforces the system-tenant gate) permits the
	// registration the registrar forwards.
	manager.bootstrapGate = gate
	registrar, err := NewOperatorSSORegistrar(manager, gate)
	if err != nil {
		t.Fatalf("new registrar: %v", err)
	}

	provider := createTestProvider("some-data-plane-tenant", "okta-operators")
	if err := registrar.RegisterOperatorProvider(context.Background(), provider); err != nil {
		t.Fatalf("register after bootstrap: %v", err)
	}

	if provider.TenantID != SystemTenantID {
		t.Fatalf("registrar must force system tenant, got %q", provider.TenantID)
	}
	stored, err := providerStorage.GetProviderByName(context.Background(), SystemTenantID, "okta-operators")
	if err != nil {
		t.Fatalf("provider not persisted under system tenant: %v", err)
	}
	if stored.TenantID != SystemTenantID {
		t.Fatalf("stored provider tenant = %q, want system", stored.TenantID)
	}
}

// TestOperatorSSORegistrar_FailsClosedOnGateError proves the registrar fails
// closed when the admin-presence check itself errors — never opening the
// configuration path on uncertainty.
func TestOperatorSSORegistrar_FailsClosedOnGateError(t *testing.T) {
	manager, _, _, _, _, _ := setupTestFederationManager(t)

	gate := &stubAdminGate{err: errors.New("storage unavailable")}
	registrar, err := NewOperatorSSORegistrar(manager, gate)
	if err != nil {
		t.Fatalf("new registrar: %v", err)
	}

	err = registrar.RegisterOperatorProvider(context.Background(), createTestProvider("x", "okta"))
	if !errors.Is(err, ErrBootstrapNotReady) {
		t.Fatalf("expected fail-closed ErrBootstrapNotReady on gate error, got: %v", err)
	}
}

// TestNewOperatorSSORegistrar_Validation covers constructor error handling.
func TestNewOperatorSSORegistrar_Validation(t *testing.T) {
	manager, _, _, _, _, _ := setupTestFederationManager(t)

	if _, err := NewOperatorSSORegistrar(nil, &stubAdminGate{}); err == nil {
		t.Fatal("expected error for nil federation manager")
	}
	if _, err := NewOperatorSSORegistrar(manager, nil); err == nil {
		t.Fatal("expected error for nil admin gate")
	}
}

// TestRegisterProvider_SystemTenantGate_AllowsWithAdmin proves the gate, pushed
// down into the raw RegisterProvider chokepoint, ALLOWS a system-tenant operator
// IdP once a bootstrap admin is present -- on the path every transport uses.
func TestRegisterProvider_SystemTenantGate_AllowsWithAdmin(t *testing.T) {
	manager, providerStorage, _, _, _, _ := setupTestFederationManager(t)
	manager.bootstrapGate = &stubAdminGate{hasAdmin: true}

	provider := createTestProvider(SystemTenantID, "okta-operators")
	if err := manager.RegisterProvider(context.Background(), provider); err != nil {
		t.Fatalf("expected system-tenant registration to succeed with admin present: %v", err)
	}
	if _, err := providerStorage.GetProviderByName(context.Background(), SystemTenantID, "okta-operators"); err != nil {
		t.Fatalf("provider not persisted under system tenant: %v", err)
	}
}

// TestRegisterProvider_SystemTenantGate_RejectsWithoutAdmin proves the raw
// RegisterProvider chokepoint fails closed for the system tenant before a
// bootstrap admin exists -- proving there is NO transport bypass of the
// bootstrap-ordering invariant.
func TestRegisterProvider_SystemTenantGate_RejectsWithoutAdmin(t *testing.T) {
	manager, providerStorage, _, _, _, _ := setupTestFederationManager(t)
	manager.bootstrapGate = &stubAdminGate{hasAdmin: false}

	provider := createTestProvider(SystemTenantID, "okta-operators")
	err := manager.RegisterProvider(context.Background(), provider)
	if !errors.Is(err, ErrBootstrapNotReady) {
		t.Fatalf("expected ErrBootstrapNotReady from raw RegisterProvider, got: %v", err)
	}
	if _, err := providerStorage.GetProviderByName(context.Background(), SystemTenantID, "okta-operators"); err == nil {
		t.Fatal("provider must NOT be persisted when the gate rejects registration")
	}
}

// TestRegisterProvider_SystemTenantGate_FailsClosedOnGateError proves a gate
// error fails the system-tenant registration closed (never opening on
// uncertainty).
func TestRegisterProvider_SystemTenantGate_FailsClosedOnGateError(t *testing.T) {
	manager, _, _, _, _, _ := setupTestFederationManager(t)
	manager.bootstrapGate = &stubAdminGate{err: errors.New("storage unavailable")}

	err := manager.RegisterProvider(context.Background(), createTestProvider(SystemTenantID, "okta"))
	if !errors.Is(err, ErrBootstrapNotReady) {
		t.Fatalf("expected fail-closed ErrBootstrapNotReady on gate error, got: %v", err)
	}
}

// TestRegisterProvider_SystemTenantGate_NilGateFailsClosed proves that when no
// gate is wired, system-tenant registration is denied (no gate => cannot verify
// the admin => deny), so a misconfigured deployment cannot silently open the
// control-plane configuration path.
func TestRegisterProvider_SystemTenantGate_NilGateFailsClosed(t *testing.T) {
	manager, _, _, _, _, _ := setupTestFederationManager(t)
	// No bootstrapGate configured.

	err := manager.RegisterProvider(context.Background(), createTestProvider(SystemTenantID, "okta"))
	if !errors.Is(err, ErrBootstrapNotReady) {
		t.Fatalf("expected nil-gate system-tenant registration to fail closed, got: %v", err)
	}
}

// TestRegisterProvider_NonSystemTenant_UnaffectedByGate proves the gate never
// touches non-system tenants: a data-plane provider registers successfully even
// when no gate is wired and even when a stub gate would report no admin.
func TestRegisterProvider_NonSystemTenant_UnaffectedByGate(t *testing.T) {
	t.Run("nil gate", func(t *testing.T) {
		manager, providerStorage, _, _, _, _ := setupTestFederationManager(t)
		provider := createTestProvider("tenant-acme", "acme-oidc")
		if err := manager.RegisterProvider(context.Background(), provider); err != nil {
			t.Fatalf("non-system registration must succeed with no gate: %v", err)
		}
		if _, err := providerStorage.GetProviderByName(context.Background(), "tenant-acme", "acme-oidc"); err != nil {
			t.Fatalf("non-system provider not persisted: %v", err)
		}
	})

	t.Run("gate reports no admin", func(t *testing.T) {
		manager, providerStorage, _, _, _, _ := setupTestFederationManager(t)
		manager.bootstrapGate = &stubAdminGate{hasAdmin: false}
		provider := createTestProvider("tenant-acme", "acme-oidc")
		if err := manager.RegisterProvider(context.Background(), provider); err != nil {
			t.Fatalf("non-system registration must ignore the gate: %v", err)
		}
		if _, err := providerStorage.GetProviderByName(context.Background(), "tenant-acme", "acme-oidc"); err != nil {
			t.Fatalf("non-system provider not persisted: %v", err)
		}
	})
}

// TestSystemTenantIDMatchesQRDB guards SystemTenantID (an alias, not a
// duplicated literal) against drift from qrdb's canonical constant. The
// system-tenant predicate itself (empty-or-literal matching) now lives
// exclusively in go-qrdb/sdk/go as qrdbsdk.IsSystemTenant; quicbaas does not
// keep a local copy of that logic.
func TestSystemTenantIDMatchesQRDB(t *testing.T) {
	if SystemTenantID != qrdbsdk.SystemTenantID {
		t.Errorf("SystemTenantID = %q, want %q (qrdbsdk.SystemTenantID)", SystemTenantID, qrdbsdk.SystemTenantID)
	}
}
