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
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// agentAppOptions are the registration options POST /api/v1/fleet/enroll
// supplies for a daemon's client-credentials application.
func agentAppOptions(tenantID, commonName string) RegisterAppOptions {
	return RegisterAppOptions{
		Name:           FleetNodeAppName(tenantID, commonName),
		GrantTypes:     []string{GrantTypeClientCredentials},
		ResponseTypes:  []string{},
		ResourcePrefix: "agent-runtime",
	}
}

// newAgentAppManager builds an AppManager over a tenant-routed store with the
// tenant barrier-isolated, the production shape for a burn-in tenant.
func newAgentAppManager(t *testing.T, tenantID string) (*AppManager, *BackendStorage) {
	t.Helper()
	kv := newRoutingTestKV()
	kv.barrierTenant(tenantID)
	storage := NewBackendStorage(kv)
	return NewAppManager(storage, nil), storage
}

// TestAgentClientID_IsDerivedAndParseable pins the two properties the
// identifier has to hold at once: it is stable for an identity (so enrollment
// can be idempotent) and it names its tenant (so the token endpoint can scope
// its storage context before resolving the client).
func TestAgentClientID_IsDerivedAndParseable(t *testing.T) {
	const tenantID = "automatethethings"

	first := FleetNodeClientID(tenantID, "agent-gpu-host")
	if second := FleetNodeClientID(tenantID, "agent-gpu-host"); first != second {
		t.Fatalf("FleetNodeClientID is not stable: %q then %q", first, second)
	}
	if got := TenantFromClientID(first); got != tenantID {
		t.Fatalf("TenantFromClientID(%q) = %q, want %q", first, got, tenantID)
	}
	if other := FleetNodeClientID(tenantID, "agent-other"); other == first {
		t.Fatalf("two agents share the client id %q", first)
	}
	if crossTenant := FleetNodeClientID("acme", "agent-gpu-host"); crossTenant == first {
		t.Fatalf("two tenants share the client id %q", first)
	}
	suffix := strings.TrimPrefix(first, ClientIDPrefix+tenantID+ClientIDSeparator)
	if strings.Contains(suffix, ClientIDSeparator) {
		t.Fatalf("client id %q has a separator inside its suffix %q", first, suffix)
	}
}

// TestAgentClientID_DegenerateIdentityNamesNoTenant is the error-input half: a
// derivation is not a validation. An identity with no tenant produces an id
// that names no tenant, which no token endpoint could ever scope -- which is
// why RegisterAgentApp refuses the inputs before deriving from them.
func TestAgentClientID_DegenerateIdentityNamesNoTenant(t *testing.T) {
	for _, tc := range []struct {
		name       string
		tenantID   string
		commonName string
	}{
		{"no tenant", "", "agent-gpu-host"},
		{"no common name", "automatethethings", ""},
		{"neither", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clientID := FleetNodeClientID(tc.tenantID, tc.commonName)
			if got := TenantFromClientID(clientID); got != tc.tenantID {
				t.Fatalf("TenantFromClientID(%q) = %q, want %q", clientID, got, tc.tenantID)
			}
		})
	}
}

// TestRegisterAgentApp_IsIdempotent is the #644 regression at the manager
// seam: a repeat enrollment returns the SAME client with a usable secret, and
// leaves no orphan behind.
func TestRegisterAgentApp_IsIdempotent(t *testing.T) {
	const tenantID = "automatethethings"
	mgr, storage := newAgentAppManager(t, tenantID)

	// The register route is public, so its context names no tenant.
	first, err := mgr.RegisterAgentApp(context.Background(), tenantID, "agent-gpu-host", agentAppOptions(tenantID, "agent-gpu-host"))
	if err != nil {
		t.Fatalf("first RegisterAgentApp: %v", err)
	}
	second, err := mgr.RegisterAgentApp(context.Background(), tenantID, "agent-gpu-host", agentAppOptions(tenantID, "agent-gpu-host"))
	if err != nil {
		t.Fatalf("second RegisterAgentApp: %v", err)
	}

	if first.ClientID != second.ClientID {
		t.Fatalf("re-registration minted %q, orphaning %q", second.ClientID, first.ClientID)
	}
	if second.ClientSecret == "" {
		t.Fatal("re-registration returned no plaintext secret")
	}
	if second.ClientSecret == first.ClientSecret {
		t.Fatal("re-registration returned the same secret; it must be rotated")
	}

	// The token endpoint's context resolves the LATEST credential.
	tokenCtx := tenantctx.WithTenant(context.Background(), tenantID)
	ok, err := storage.ValidateClientSecret(tokenCtx, second.ClientID, second.ClientSecret)
	if err != nil {
		t.Fatalf("ValidateClientSecret: %v", err)
	}
	if !ok {
		t.Fatal("the credential from a repeat registration is not usable")
	}

	// The rotated-away secret stops working, which is what makes the rotation
	// a rotation and not a second live credential.
	if ok, _ := storage.ValidateClientSecret(tokenCtx, first.ClientID, first.ClientSecret); ok {
		t.Fatal("the superseded secret still authenticates")
	}

	apps, err := storage.ListApps(tokenCtx, tenantID, ListOptions{})
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("tenant holds %d applications after two registrations, want 1", len(apps))
	}
}

// TestRegisterAgentApp_RejectsIncompleteIdentity is the error path: without a
// tenant or a certificate common name there is no identity to key on, so the
// call must refuse rather than derive an identifier from an empty string.
func TestRegisterAgentApp_RejectsIncompleteIdentity(t *testing.T) {
	mgr, _ := newAgentAppManager(t, "automatethethings")

	for _, tc := range []struct {
		name       string
		tenantID   string
		commonName string
	}{
		{"no tenant", "", "agent-gpu-host"},
		{"no common name", "automatethethings", ""},
		{"neither", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, err := mgr.RegisterAgentApp(context.Background(), tc.tenantID, tc.commonName,
				agentAppOptions("automatethethings", "agent-gpu-host"))
			if err == nil {
				t.Fatalf("RegisterAgentApp(%q, %q) succeeded with %q", tc.tenantID, tc.commonName, app.ClientID)
			}
		})
	}
}

// TestRegisterAgentApp_RefusesSealedTenant covers the operational error path:
// a tenant whose barrier is sealed has nowhere to put the record, and the
// daemon must be told so rather than handed a credential that resolves to
// nothing.
func TestRegisterAgentApp_RefusesSealedTenant(t *testing.T) {
	const tenantID = "automatethethings"
	kv := newRoutingTestKV()
	kv.barrierTenant(tenantID)
	kv.sealTenant(tenantID)
	mgr := NewAppManager(NewBackendStorage(kv), nil)

	if _, err := mgr.RegisterAgentApp(context.Background(), tenantID, "agent-gpu-host",
		agentAppOptions(tenantID, "agent-gpu-host")); err == nil {
		t.Fatal("RegisterAgentApp into a sealed tenant returned nil; it must fail closed")
	}
}
