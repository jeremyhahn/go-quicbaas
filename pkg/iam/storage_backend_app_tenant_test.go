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
	"errors"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// go-quicbaas #644 — an OAuth2 application record must live in the namespace of
// the tenant it names, not in the namespace of whoever created it.
//
// The IAM KVStore routes every read and write by the tenant bound to the
// operation's CONTEXT (pkg/server/tenant_kvstore.go tenantFromAnyContext), and
// a barrier-isolated tenant has its own store. POST /api/v1/agents/register is
// a public route: it carries no identity, so its context names no tenant and
// the app landed in the SHARED store, while POST /oauth2/token scopes its
// context to the tenant encoded in the client_id and read the TENANT's store.
// The credential the register endpoint handed out was therefore rejected by the
// token endpoint on its very first use.
//
// These run over the real BackendStorage on top of routingTestKV, the double
// that mirrors production routing. A flat store cannot reproduce the defect.
// =============================================================================

// agentApp builds the application record POST /api/v1/agents/register creates.
func agentApp(tenantID, clientID string) *Application {
	return &Application{
		ClientID:     clientID,
		ClientSecret: "hashed-secret",
		Name:         AgentAppName(tenantID, "agent-gpu-host"),
		TenantID:     tenantID,
		GrantTypes:   []string{GrantTypeClientCredentials},
	}
}

// TestCreateApp_LandsInTheAppsOwnTenantNamespace is the #644 regression. The
// creating context names no tenant (a public route); the reading context names
// the app's tenant (the token endpoint). Both must reach the same record.
func TestCreateApp_LandsInTheAppsOwnTenantNamespace(t *testing.T) {
	for _, tc := range []struct {
		name       string
		tenantID   string
		barrier    bool
		clientID   string
		createCtx  func() context.Context
		readTenant string
	}{
		{
			name:       "barrier tenant, untenanted creator",
			tenantID:   "automatethethings",
			barrier:    true,
			clientID:   "app_automatethethings_QQnqeujdEo",
			createCtx:  context.Background,
			readTenant: "automatethethings",
		},
		{
			name:     "barrier tenant, system admin creator",
			tenantID: "automatethethings",
			barrier:  true,
			clientID: "app_automatethethings_QQnqeujdEo",
			createCtx: func() context.Context {
				return ContextWithTenant(context.Background(), "system")
			},
			readTenant: "automatethethings",
		},
		{
			name:       "client id that encodes no tenant (mTLS app)",
			tenantID:   "automatethethings",
			barrier:    true,
			clientID:   "agent-automatethethings-agent-gpu-host-mtls",
			createCtx:  context.Background,
			readTenant: "automatethethings",
		},
		{
			name:       "shared-store tenant",
			tenantID:   "acme",
			barrier:    false,
			clientID:   "app_acme_QQnqeujdEo",
			createCtx:  context.Background,
			readTenant: "acme",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kv := newRoutingTestKV()
			if tc.barrier {
				kv.barrierTenant(tc.tenantID)
			}
			storage := NewBackendStorage(kv)

			if err := storage.CreateApp(tc.createCtx(), agentApp(tc.tenantID, tc.clientID)); err != nil {
				t.Fatalf("CreateApp: %v", err)
			}

			// The token endpoint's context: scoped to the app's tenant.
			readCtx := ContextWithTenant(context.Background(), tc.readTenant)
			app, err := storage.GetApp(readCtx, tc.clientID)
			if err != nil {
				t.Fatalf("GetApp under tenant %q: %v (this is the #644 invalid_client)", tc.readTenant, err)
			}
			if app.ClientID != tc.clientID || app.TenantID != tc.tenantID {
				t.Fatalf("resolved app = (%q,%q), want (%q,%q)", app.ClientID, app.TenantID, tc.clientID, tc.tenantID)
			}
		})
	}
}

// TestCreateApp_RefusesSealedTenant is the error path: a barrier tenant whose
// store cannot be resolved must fail closed rather than silently writing the
// record into the shared store.
func TestCreateApp_RefusesSealedTenant(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("automatethethings")
	kv.sealTenant("automatethethings")
	storage := NewBackendStorage(kv)

	err := storage.CreateApp(context.Background(), agentApp("automatethethings", "app_automatethethings_QQnqeujdEo"))
	if err == nil {
		t.Fatal("CreateApp into a sealed tenant returned nil; it must fail closed")
	}
	if !errors.Is(err, errTenantSealedTest) {
		t.Fatalf("CreateApp error = %v, want the sealed-tenant refusal", err)
	}
	if kv.shared.keyCount() != 0 {
		t.Fatalf("sealed CreateApp wrote %d keys into the shared store; it must write none", kv.shared.keyCount())
	}
}

// TestValidateClientSecret_AcrossTheRegisterTokenBoundary is the credential
// round trip the burn-in exercised: register under one context, authenticate
// under the token endpoint's.
func TestValidateClientSecret_AcrossTheRegisterTokenBoundary(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("automatethethings")
	storage := NewBackendStorage(kv)

	const clientID = "app_automatethethings_QQnqeujdEo"
	issued, err := clientAuthTestSecret()
	if err != nil {
		t.Fatalf("generateClientSecret: %v", err)
	}
	app := agentApp("automatethethings", clientID)
	app.ClientSecret = issued.Hashed
	if err := storage.CreateApp(context.Background(), app); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	tokenCtx := ContextWithTenant(context.Background(), "automatethethings")
	ok, err := storage.ValidateClientSecret(tokenCtx, clientID, issued.Plaintext)
	if err != nil {
		t.Fatalf("ValidateClientSecret: %v (this is the #644 invalid_client)", err)
	}
	if !ok {
		t.Fatal("ValidateClientSecret rejected the secret the register endpoint issued")
	}

	// Error path: the wrong secret is still refused.
	ok, err = storage.ValidateClientSecret(tokenCtx, clientID, "sk_not-the-secret")
	if ok {
		t.Fatal("ValidateClientSecret accepted a wrong secret")
	}
	if err != nil && dberrors.IsNotFound(err) {
		t.Fatalf("wrong secret reported as not-found: %v", err)
	}
}

// TestUpdateDeleteListApp_UseTheAppsOwnNamespace covers the remaining app verbs
// across the same boundary: each must address the record where CreateApp put it.
func TestUpdateDeleteListApp_UseTheAppsOwnNamespace(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("automatethethings")
	storage := NewBackendStorage(kv)

	const clientID = "app_automatethethings_QQnqeujdEo"
	app := agentApp("automatethethings", clientID)
	if err := storage.CreateApp(context.Background(), app); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	app.Name = "renamed"
	if err := storage.UpdateApp(context.Background(), app); err != nil {
		t.Fatalf("UpdateApp from an untenanted context: %v", err)
	}

	apps, err := storage.ListApps(context.Background(), "automatethethings", ListOptions{})
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) != 1 || apps[0].Name != "renamed" {
		t.Fatalf("ListApps = %+v, want one app named renamed", apps)
	}

	if err := storage.DeleteApp(context.Background(), clientID); err != nil {
		t.Fatalf("DeleteApp: %v", err)
	}
	if _, err := storage.GetApp(ContextWithTenant(context.Background(), "automatethethings"), clientID); err == nil {
		t.Fatal("GetApp resolved a deleted app")
	}
}

// TestGetApp_DoesNotCrossTenants proves the fix does not turn the app store
// into a global namespace: a client id belonging to one tenant must not resolve
// out of another tenant's store.
func TestGetApp_DoesNotCrossTenants(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("tenant-a")
	kv.barrierTenant("tenant-b")
	storage := NewBackendStorage(kv)

	if err := storage.CreateApp(context.Background(), agentApp("tenant-a", "agent-tenant-a-node-mtls")); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	_, err := storage.GetAppByTenant(ContextWithTenant(context.Background(), "tenant-b"), "tenant-b", "agent-tenant-a-node-mtls")
	if err == nil {
		t.Fatal("tenant-b resolved tenant-a's application")
	}
}
