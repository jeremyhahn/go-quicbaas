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

package service

import (
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
)

// newServiceForScopeTest builds a real secretsService over a real
// *secrets.Manager with an in-memory backend — the same wiring every
// transport reaches, with no mock in the path. The tests below assert on
// what actually lands in the backend, not on what the service claims.
func newServiceForScopeTest(t *testing.T) *secretsService {
	t.Helper()
	mgr, err := secrets.NewManager(&secrets.ManagerOptions{
		Backend: secrets.NewMemoryBackend(),
	})
	if err != nil {
		t.Fatalf("secrets.NewManager: %v", err)
	}
	return newSecretsService(mgr, nil)
}

// secretsScopeCtx returns a context bound to tenantID the way every authenticated
// transport binds it (iam.ContextWithTenant).
func secretsScopeCtx(tenantID string) context.Context {
	return iam.ContextWithTenant(context.Background(), tenantID)
}

// requireCrossTenantRefusal asserts err is the service's cross-tenant
// sentinel rather than any other failure, so a test cannot pass because the
// operation broke for an unrelated reason.
func requireCrossTenantRefusal(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: want cross-tenant refusal, got nil error", what)
	}
	var qb *qberrors.QBError
	if !errors.As(err, &qb) {
		t.Fatalf("%s: want *errors.QBError, got %T: %v", what, err, err)
	}
	if qb.Code != qberrors.ErrCrossTenantAccess {
		t.Fatalf("%s: want code %v, got %v (%v)", what, qberrors.ErrCrossTenantAccess, qb.Code, err)
	}
}

// =============================================================================
// The issue #534 matrix: identity x requested tenant x observed namespace.
// =============================================================================

// TestStoreSecret_TenantWriteLandsInOwnNamespace pins the positive half of
// the fix: a tenant identity storing a secret under its OWN tenant succeeds,
// the record lands under that tenant's scope (the same scope GetSecret
// computes), and a second tenant cannot see it.
func TestStoreSecret_TenantWriteLandsInOwnNamespace(t *testing.T) {
	svc := newServiceForScopeTest(t)
	const (
		owner  = "automatethethings"
		other  = "burnin"
		path   = "sdlc/forge/go-truststrap/token"
		secret = "forge-token-value"
	)

	for _, requested := range []string{"", owner} {
		storeResp, err := svc.StoreSecret(secretsScopeCtx(owner), &StoreSecretRequest{
			Name:     path,
			Data:     []byte(secret),
			TenantID: requested,
		})
		if err != nil {
			t.Fatalf("StoreSecret(tenant=%q, req.TenantID=%q): %v", owner, requested, err)
		}
		if storeResp.SecretID == "" {
			t.Fatalf("StoreSecret returned an empty secret id")
		}

		// The owning tenant reads it back through the ordinary read path.
		getResp, err := svc.GetSecret(secretsScopeCtx(owner), &GetSecretRequest{SecretID: path})
		if err != nil {
			t.Fatalf("GetSecret(tenant=%q) after store with req.TenantID=%q: %v", owner, requested, err)
		}
		if string(getResp.Data) != secret {
			t.Fatalf("round trip mismatch: want %q, got %q", secret, getResp.Data)
		}

		// A different tenant computes a different scope and must not see it.
		if _, err := svc.GetSecret(secretsScopeCtx(other), &GetSecretRequest{SecretID: path}); err == nil {
			t.Fatalf("tenant %q read tenant %q's secret — namespaces are not isolated", other, owner)
		}
	}
}

// TestStoreSecret_TenantCannotWriteAnotherTenant is the negative that makes
// the fix safe: honouring req.TenantID on the write path must not turn
// "cannot write anywhere" into "can write everywhere". A tenant identity
// naming another tenant is refused, and nothing is written.
func TestStoreSecret_TenantCannotWriteAnotherTenant(t *testing.T) {
	svc := newServiceForScopeTest(t)
	const (
		attacker = "burnin"
		victim   = "automatethethings"
		path     = "sdlc/forge/go-truststrap/token"
	)

	_, err := svc.StoreSecret(secretsScopeCtx(attacker), &StoreSecretRequest{
		Name:     path,
		Data:     []byte("attacker-controlled"),
		TenantID: victim,
	})
	requireCrossTenantRefusal(t, err, "StoreSecret across tenants")

	// Nothing may exist in the victim's namespace.
	if _, err := svc.GetSecret(secretsScopeCtx(victim), &GetSecretRequest{SecretID: path}); err == nil {
		t.Fatalf("a refused cross-tenant StoreSecret still wrote into tenant %q", victim)
	}
}

// TestEnsureSecret_TenantCannotWriteAnotherTenant covers the same hole on
// the generate-on-first-access path, which honours req.TenantID for the
// storage path and therefore reaches another tenant's namespace directly.
func TestEnsureSecret_TenantCannotWriteAnotherTenant(t *testing.T) {
	svc := newServiceForScopeTest(t)
	const (
		attacker = "burnin"
		victim   = "automatethethings"
		path     = "sdlc/webhook/go-truststrap/secret"
	)
	spec := func(tenantID string) *EnsureSecretRequest {
		return &EnsureSecretRequest{
			SecretID: path,
			Length:   16,
			Encoding: secrets.EncodingHex,
			TenantID: tenantID,
		}
	}

	_, err := svc.EnsureSecret(secretsScopeCtx(attacker), spec(victim))
	requireCrossTenantRefusal(t, err, "EnsureSecret across tenants")

	// The victim's namespace must still be empty: its own first call is the
	// one that generates. If the attacker's call had landed there, this
	// would report Generated=false and hand the attacker's value back.
	victimResp, err := svc.EnsureSecret(secretsScopeCtx(victim), spec(victim))
	if err != nil {
		t.Fatalf("victim EnsureSecret: %v", err)
	}
	if !victimResp.Generated {
		t.Fatalf("a refused cross-tenant EnsureSecret still populated tenant %q", victim)
	}
}

// TestEnsureSecret_TenantNamespaceIsolated pins that EnsureSecret resolves
// the same namespace as every other secrets verb for the same tenant: the
// value is stable within the tenant and a second tenant generates its own.
func TestEnsureSecret_TenantNamespaceIsolated(t *testing.T) {
	svc := newServiceForScopeTest(t)
	const (
		owner = "automatethethings"
		other = "burnin"
		path  = "sdlc/webhook/go-truststrap/secret"
	)
	spec := func(tenantID string) *EnsureSecretRequest {
		return &EnsureSecretRequest{
			SecretID: path,
			Length:   16,
			Encoding: secrets.EncodingHex,
			TenantID: tenantID,
		}
	}

	first, err := svc.EnsureSecret(secretsScopeCtx(owner), spec(owner))
	if err != nil {
		t.Fatalf("owner EnsureSecret: %v", err)
	}
	if !first.Generated {
		t.Fatalf("first EnsureSecret must report Generated=true")
	}

	// Same tenant, request tenant omitted: must resolve the same namespace.
	second, err := svc.EnsureSecret(secretsScopeCtx(owner), spec(""))
	if err != nil {
		t.Fatalf("owner EnsureSecret (implicit tenant): %v", err)
	}
	if second.Generated || string(second.Data) != string(first.Data) {
		t.Fatalf("explicit and implicit tenant resolved different namespaces: %q vs %q",
			first.Data, second.Data)
	}

	// A different tenant must not observe the owner's value.
	foreign, err := svc.EnsureSecret(secretsScopeCtx(other), spec(other))
	if err != nil {
		t.Fatalf("other EnsureSecret: %v", err)
	}
	if !foreign.Generated {
		t.Fatalf("tenant %q read tenant %q's generated secret", other, owner)
	}
	if string(foreign.Data) == string(first.Data) {
		t.Fatalf("two tenants share one EnsureSecret namespace")
	}
}

// TestStoreSecret_SystemIdentityBehaviourPinned records exactly what the
// system tenant keeps at the service layer:
//
//   - a plain path stays in the system namespace (no tenant prefix), and
//   - the literal tenant-scoped key documented on #534 as the operator
//     workaround still resolves for the tenant that owns it.
//
// Neither is widened by this change; the literal-key side door is issue
// #534 scope item 4 and is left for a follow-up that can retire it without
// stranding a live node.
func TestStoreSecret_SystemIdentityBehaviourPinned(t *testing.T) {
	svc := newServiceForScopeTest(t)
	const target = "automatethethings"

	for _, systemID := range []string{"", qrdbsdk.SystemTenantID} {
		sysCtx := secretsScopeCtx(systemID)

		if _, err := svc.StoreSecret(sysCtx, &StoreSecretRequest{
			Name: "platform/root-token",
			Data: []byte("platform-value"),
		}); err != nil {
			t.Fatalf("system StoreSecret(tenant=%q): %v", systemID, err)
		}
		got, err := svc.GetSecret(sysCtx, &GetSecretRequest{SecretID: "platform/root-token"})
		if err != nil {
			t.Fatalf("system GetSecret(tenant=%q): %v", systemID, err)
		}
		if string(got.Data) != "platform-value" {
			t.Fatalf("system round trip mismatch: got %q", got.Data)
		}

		literal := secretsTenantPrefix + target + secretsTenantSeparator + "sdlc/forge/go-truststrap/token"
		if _, err := svc.StoreSecret(sysCtx, &StoreSecretRequest{
			Name: literal,
			Data: []byte("provisioned-by-operator"),
		}); err != nil {
			t.Fatalf("system literal-key StoreSecret(tenant=%q): %v", systemID, err)
		}
		tenantView, err := svc.GetSecret(secretsScopeCtx(target),
			&GetSecretRequest{SecretID: "sdlc/forge/go-truststrap/token"})
		if err != nil {
			t.Fatalf("tenant %q cannot read the operator-provisioned key: %v", target, err)
		}
		if string(tenantView.Data) != "provisioned-by-operator" {
			t.Fatalf("literal-key provisioning mismatch: got %q", tenantView.Data)
		}
	}
}

// TestStoreSecret_SystemCannotRedirectViaTenantID pins the behaviour change
// the issue's third and fourth table rows record: a system caller naming
// another tenant in req.TenantID used to silently append a version to the
// SYSTEM copy. It must not do that, and it must not silently acquire a
// cross-tenant write either — req.TenantID names the caller's own tenant or
// the request is refused.
func TestStoreSecret_SystemCannotRedirectViaTenantID(t *testing.T) {
	svc := newServiceForScopeTest(t)
	const (
		target = "automatethethings"
		path   = "sdlc/forge/go-truststrap/token"
	)

	_, err := svc.StoreSecret(secretsScopeCtx(qrdbsdk.SystemTenantID), &StoreSecretRequest{
		Name:     path,
		Data:     []byte("provisioned"),
		TenantID: target,
	})
	requireCrossTenantRefusal(t, err, "system StoreSecret naming another tenant")

	// It must not have landed in the system namespace either.
	if _, err := svc.GetSecret(secretsScopeCtx(qrdbsdk.SystemTenantID),
		&GetSecretRequest{SecretID: path}); err == nil {
		t.Fatalf("refused request still wrote into the system namespace")
	}
}

// TestSecretsScopedTenant_Table exercises the resolver directly across the
// full identity x requested-tenant matrix, including the system-tenant
// spellings ("" and "system") that must be treated as one namespace.
func TestSecretsScopedTenant_Table(t *testing.T) {
	tests := []struct {
		name      string
		ctxTenant string
		requested string
		want      string
		wantErr   bool
	}{
		{name: "tenant, no request", ctxTenant: "acme", requested: "", want: "acme"},
		{name: "tenant names itself", ctxTenant: "acme", requested: "acme", want: "acme"},
		{name: "tenant names another", ctxTenant: "acme", requested: "burnin", wantErr: true},
		{name: "tenant names system", ctxTenant: "acme", requested: qrdbsdk.SystemTenantID, wantErr: true},
		{name: "system, no request", ctxTenant: qrdbsdk.SystemTenantID, requested: "", want: qrdbsdk.SystemTenantID},
		{name: "unbound, no request", ctxTenant: "", requested: "", want: ""},
		{name: "unbound names system", ctxTenant: "", requested: qrdbsdk.SystemTenantID, want: ""},
		{name: "system names unbound spelling", ctxTenant: qrdbsdk.SystemTenantID, requested: "", want: qrdbsdk.SystemTenantID},
		{name: "system names a tenant", ctxTenant: qrdbsdk.SystemTenantID, requested: "acme", wantErr: true},
		{name: "unbound names a tenant", ctxTenant: "", requested: "acme", wantErr: true},
		{name: "malformed request", ctxTenant: "acme", requested: "acme/../burnin", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := secretsScopedTenant(secretsScopeCtx(tc.ctxTenant), tc.requested, "Test")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got tenant %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}
