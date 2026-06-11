// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package iam

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// stubProviderLister is a minimal ProviderLister for the single-provider
// shortcut tests. It returns a fixed provider set (optionally an error) for any
// tenant.
type stubProviderLister struct {
	providers []*ExternalProvider
	err       error
}

func (s stubProviderLister) ListProviders(_ context.Context, _ string, _ ListOptions) ([]*ExternalProvider, error) {
	return s.providers, s.err
}

func newHRDWithTenant(t *testing.T) (*HomeRealmDirectory, context.Context) {
	t.Helper()
	dir, err := NewHomeRealmDirectory(newMockKVStore())
	if err != nil {
		t.Fatalf("NewHomeRealmDirectory: %v", err)
	}
	ctx := context.Background()
	if err := dir.RegisterDomain(ctx, "tenant-acme", "acme.example"); err != nil {
		t.Fatalf("RegisterDomain: %v", err)
	}
	return dir, ctx
}

// Single-provider shortcut: a tenant with exactly one enabled provider resolves
// unambiguously even with no explicit per-domain or default mapping.
func TestResolveProvider_SingleProviderShortcut(t *testing.T) {
	dir, ctx := newHRDWithTenant(t)
	lister := stubProviderLister{providers: []*ExternalProvider{
		{ID: "okta-1", TenantID: "tenant-acme", Enabled: true},
	}}

	realm, err := dir.ResolveProvider(ctx, RealmHints{Email: "alice@acme.example"}, lister)
	if err != nil {
		t.Fatalf("ResolveProvider: %v", err)
	}
	if realm.TenantID != "tenant-acme" || realm.ProviderID != "okta-1" {
		t.Fatalf("realm = %+v, want tenant-acme/okta-1", realm)
	}
	if realm.IsLocalRealm() {
		t.Error("did not expect local realm")
	}
}

// Disabled providers are excluded from the single-provider shortcut count.
func TestResolveProvider_SingleProviderShortcutIgnoresDisabled(t *testing.T) {
	dir, ctx := newHRDWithTenant(t)
	lister := stubProviderLister{providers: []*ExternalProvider{
		{ID: "okta-1", TenantID: "tenant-acme", Enabled: true},
		{ID: "old-idp", TenantID: "tenant-acme", Enabled: false},
	}}

	realm, err := dir.ResolveProvider(ctx, RealmHints{Email: "alice@acme.example"}, lister)
	if err != nil {
		t.Fatalf("ResolveProvider: %v", err)
	}
	if realm.ProviderID != "okta-1" {
		t.Fatalf("ProviderID = %q, want okta-1", realm.ProviderID)
	}
}

// Per-domain provider mapping wins over the default/shortcut and routes a
// specific domain to a specific IdP.
func TestResolveProvider_EmailDomainMapping(t *testing.T) {
	dir, ctx := newHRDWithTenant(t)
	// Two providers configured => shortcut would be ambiguous; the per-domain
	// mapping must take priority.
	if err := dir.RegisterDomainProvider(ctx, "tenant-acme", "acme.example", "azure-ad"); err != nil {
		t.Fatalf("RegisterDomainProvider: %v", err)
	}
	lister := stubProviderLister{providers: []*ExternalProvider{
		{ID: "okta-1", TenantID: "tenant-acme", Enabled: true},
		{ID: "azure-ad", TenantID: "tenant-acme", Enabled: true},
	}}

	realm, err := dir.ResolveProvider(ctx, RealmHints{Email: "bob@acme.example"}, lister)
	if err != nil {
		t.Fatalf("ResolveProvider: %v", err)
	}
	if realm.ProviderID != "azure-ad" {
		t.Fatalf("ProviderID = %q, want azure-ad (per-domain mapping)", realm.ProviderID)
	}
}

// Default provider fallback: no per-domain mapping, multiple providers, but a
// registered default resolves cleanly.
func TestResolveProvider_DefaultFallback(t *testing.T) {
	dir, ctx := newHRDWithTenant(t)
	if err := dir.SetDefaultProvider(ctx, "tenant-acme", "okta-1"); err != nil {
		t.Fatalf("SetDefaultProvider: %v", err)
	}
	lister := stubProviderLister{providers: []*ExternalProvider{
		{ID: "okta-1", TenantID: "tenant-acme", Enabled: true},
		{ID: "azure-ad", TenantID: "tenant-acme", Enabled: true},
	}}

	realm, err := dir.ResolveProvider(ctx, RealmHints{Email: "carol@acme.example"}, lister)
	if err != nil {
		t.Fatalf("ResolveProvider: %v", err)
	}
	if realm.ProviderID != "okta-1" {
		t.Fatalf("ProviderID = %q, want okta-1 (default)", realm.ProviderID)
	}
}

// Ambiguous fail-closed: multiple enabled providers, no per-domain mapping, no
// default => the directory refuses to guess.
func TestResolveProvider_AmbiguousFailClosed(t *testing.T) {
	dir, ctx := newHRDWithTenant(t)
	lister := stubProviderLister{providers: []*ExternalProvider{
		{ID: "okta-1", TenantID: "tenant-acme", Enabled: true},
		{ID: "azure-ad", TenantID: "tenant-acme", Enabled: true},
	}}

	_, err := dir.ResolveProvider(ctx, RealmHints{Email: "dave@acme.example"}, lister)
	if !errors.As(err, new(*HRDProviderAmbiguousError)) {
		t.Fatalf("expected HRDProviderAmbiguousError, got %v", err)
	}
}

// A nil lister with no routing entries is also ambiguous (fail-closed).
func TestResolveProvider_NilListerAmbiguous(t *testing.T) {
	dir, ctx := newHRDWithTenant(t)
	_, err := dir.ResolveProvider(ctx, RealmHints{Email: "e@acme.example"}, nil)
	if !errors.As(err, new(*HRDProviderAmbiguousError)) {
		t.Fatalf("expected HRDProviderAmbiguousError, got %v", err)
	}
}

// A lister error is treated as ambiguous (fail-closed), never as a silent
// success.
func TestResolveProvider_ListerErrorFailClosed(t *testing.T) {
	dir, ctx := newHRDWithTenant(t)
	lister := stubProviderLister{err: errors.New("store down")}
	_, err := dir.ResolveProvider(ctx, RealmHints{Email: "f@acme.example"}, lister)
	if !errors.As(err, new(*HRDProviderAmbiguousError)) {
		t.Fatalf("expected HRDProviderAmbiguousError, got %v", err)
	}
}

// Unresolvable tenant: ResolveProvider fails closed with the tenant-level
// HRDNotFoundError before any provider selection.
func TestResolveProvider_TenantNotFound(t *testing.T) {
	dir, ctx := newHRDWithTenant(t)
	_, err := dir.ResolveProvider(ctx, RealmHints{Email: "g@unknown.example"}, nil)
	if !errors.As(err, new(*HRDNotFoundError)) {
		t.Fatalf("expected HRDNotFoundError, got %v", err)
	}
}

// Local realm: a tenant routes its domain to the local-factor path via the
// LocalRealmProviderID sentinel; IsLocalRealm reports true.
func TestResolveProvider_LocalRealmViaDomain(t *testing.T) {
	dir, ctx := newHRDWithTenant(t)
	if err := dir.RegisterDomainProvider(ctx, "tenant-acme", "acme.example", LocalRealmProviderID); err != nil {
		t.Fatalf("RegisterDomainProvider(local): %v", err)
	}

	realm, err := dir.ResolveProvider(ctx, RealmHints{Email: "h@acme.example"}, nil)
	if err != nil {
		t.Fatalf("ResolveProvider: %v", err)
	}
	if !realm.IsLocalRealm() || realm.ProviderID != LocalRealmProviderID {
		t.Fatalf("realm = %+v, want local realm", realm)
	}
}

// Local realm via the tenant default selector.
func TestResolveProvider_LocalRealmViaDefault(t *testing.T) {
	dir, ctx := newHRDWithTenant(t)
	if err := dir.SetDefaultProvider(ctx, "tenant-acme", LocalRealmProviderID); err != nil {
		t.Fatalf("SetDefaultProvider(local): %v", err)
	}
	realm, err := dir.ResolveProvider(ctx, RealmHints{Email: "i@acme.example"}, nil)
	if err != nil {
		t.Fatalf("ResolveProvider: %v", err)
	}
	if !realm.IsLocalRealm() {
		t.Fatalf("realm = %+v, want local realm via default", realm)
	}
}

// SNI host / parent-domain routing for per-domain provider mapping.
func TestResolveProvider_SNIParentDomainMapping(t *testing.T) {
	dir := mustHRD(t)
	ctx := context.Background()
	if err := dir.RegisterDomain(ctx, "tenant-b", "tenant-b.io"); err != nil {
		t.Fatalf("RegisterDomain: %v", err)
	}
	if err := dir.RegisterDomainProvider(ctx, "tenant-b", "tenant-b.io", "okta-b"); err != nil {
		t.Fatalf("RegisterDomainProvider: %v", err)
	}

	realm, err := dir.ResolveProvider(ctx, RealmHints{SNIHost: "app.tenant-b.io"}, nil)
	if err != nil {
		t.Fatalf("ResolveProvider: %v", err)
	}
	if realm.ProviderID != "okta-b" {
		t.Fatalf("ProviderID = %q, want okta-b", realm.ProviderID)
	}
}

// RegisterDomainProvider input validation and ownership guards.
func TestRegisterDomainProvider_Validation(t *testing.T) {
	dir := mustHRD(t)
	ctx := context.Background()

	if err := dir.RegisterDomainProvider(ctx, "", "d", "p"); !errors.As(err, new(*HRDInvalidError)) {
		t.Fatalf("empty tenant: got %v", err)
	}
	// Domain not registered to the tenant yet.
	if err := dir.RegisterDomainProvider(ctx, "tenant-x", "x.example", "p"); !errors.As(err, new(*HRDNotFoundError)) {
		t.Fatalf("unregistered domain: got %v", err)
	}
	// Domain owned by a different tenant.
	if err := dir.RegisterDomain(ctx, "tenant-owner", "shared.example"); err != nil {
		t.Fatalf("RegisterDomain: %v", err)
	}
	if err := dir.RegisterDomainProvider(ctx, "tenant-intruder", "shared.example", "p"); !errors.As(err, new(*HRDConflictError)) {
		t.Fatalf("cross-tenant: got %v", err)
	}
}

func TestSetDefaultProvider_Validation(t *testing.T) {
	dir := mustHRD(t)
	ctx := context.Background()
	if err := dir.SetDefaultProvider(ctx, "", "p"); !errors.As(err, new(*HRDInvalidError)) {
		t.Fatalf("empty tenant: got %v", err)
	}
	if err := dir.SetDefaultProvider(ctx, "t", ""); !errors.As(err, new(*HRDInvalidError)) {
		t.Fatalf("empty provider: got %v", err)
	}
}

func TestHRDProviderAmbiguousError_Message(t *testing.T) {
	e := &HRDProviderAmbiguousError{TenantID: "t"}
	if e.Error() == "" {
		t.Fatal("empty error message")
	}
}

func mustHRD(t *testing.T) *HomeRealmDirectory {
	t.Helper()
	dir, err := NewHomeRealmDirectory(newMockKVStore())
	if err != nil {
		t.Fatalf("NewHomeRealmDirectory: %v", err)
	}
	return dir
}

// ResolveProviderForTenant selects within an already-known tenant without
// re-running tenant discovery (the hint carries no email/SNI).
func TestResolveProviderForTenant_SingleProvider(t *testing.T) {
	dir := mustHRD(t)
	ctx := context.Background()
	lister := stubProviderLister{providers: []*ExternalProvider{
		{ID: "okta-1", TenantID: "tenant-known", Enabled: true},
	}}
	realm, err := dir.ResolveProviderForTenant(ctx, "tenant-known", RealmHints{}, lister)
	if err != nil {
		t.Fatalf("ResolveProviderForTenant: %v", err)
	}
	if realm.ProviderID != "okta-1" {
		t.Fatalf("ProviderID = %q, want okta-1", realm.ProviderID)
	}
}

func TestResolveProviderForTenant_EmptyTenant(t *testing.T) {
	dir := mustHRD(t)
	_, err := dir.ResolveProviderForTenant(context.Background(), "", RealmHints{}, nil)
	if !errors.As(err, new(*HRDInvalidError)) {
		t.Fatalf("expected HRDInvalidError, got %v", err)
	}
}

// ResolveProvider honours an explicit tenant hint validated against a registered
// SNI host entry (anti-spoof: the hint must match a real routing entry).
func TestResolveProvider_ExplicitHintViaSNIHost(t *testing.T) {
	dir := mustHRD(t)
	ctx := context.Background()
	if err := dir.RegisterHost(ctx, "tenant-h", "portal.tenant-h.io"); err != nil {
		t.Fatalf("RegisterHost: %v", err)
	}
	lister := stubProviderLister{providers: []*ExternalProvider{
		{ID: "idp-h", TenantID: "tenant-h", Enabled: true},
	}}
	realm, err := dir.ResolveProvider(ctx,
		RealmHints{TenantHint: "tenant-h", SNIHost: "portal.tenant-h.io"}, lister)
	if err != nil {
		t.Fatalf("ResolveProvider: %v", err)
	}
	if realm.TenantID != "tenant-h" || realm.ProviderID != "idp-h" {
		t.Fatalf("realm = %+v, want tenant-h/idp-h", realm)
	}
}

// ResolveProvider honours an explicit tenant hint validated against a registered
// parent-domain entry reached from an SNI subdomain.
func TestResolveProvider_ExplicitHintViaSNIParentDomain(t *testing.T) {
	dir := mustHRD(t)
	ctx := context.Background()
	if err := dir.RegisterDomain(ctx, "tenant-p", "tenant-p.io"); err != nil {
		t.Fatalf("RegisterDomain: %v", err)
	}
	lister := stubProviderLister{providers: []*ExternalProvider{
		{ID: "idp-p", TenantID: "tenant-p", Enabled: true},
	}}
	realm, err := dir.ResolveProvider(ctx,
		RealmHints{TenantHint: "tenant-p", SNIHost: "app.tenant-p.io"}, lister)
	if err != nil {
		t.Fatalf("ResolveProvider: %v", err)
	}
	if realm.ProviderID != "idp-p" {
		t.Fatalf("ProviderID = %q, want idp-p", realm.ProviderID)
	}
}

// Resolve honours an explicit tenant hint validated against an exact SNI host
// entry (exercises the anti-spoof exact-host branch).
func TestResolve_ExplicitHintExactHost(t *testing.T) {
	dir := mustHRD(t)
	ctx := context.Background()
	if err := dir.RegisterHost(ctx, "tenant-x", "edge.tenant-x.io"); err != nil {
		t.Fatalf("RegisterHost: %v", err)
	}
	got, err := dir.Resolve(ctx, RealmHints{TenantHint: "tenant-x", SNIHost: "edge.tenant-x.io"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "tenant-x" {
		t.Fatalf("Resolve = %q, want tenant-x", got)
	}
}

// A spoofed explicit hint is NOT honoured: the hint validation fails (the hint
// owns no matching entry) and resolution falls through to the authoritative SNI
// host entry, never to the attacker-asserted tenant.
func TestResolve_SpoofedHintIgnored(t *testing.T) {
	dir := mustHRD(t)
	ctx := context.Background()
	if err := dir.RegisterHost(ctx, "tenant-real", "edge.tenant-real.io"); err != nil {
		t.Fatalf("RegisterHost: %v", err)
	}
	got, err := dir.Resolve(ctx, RealmHints{TenantHint: "tenant-attacker", SNIHost: "edge.tenant-real.io"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "tenant-real" {
		t.Fatalf("Resolve = %q, want tenant-real (spoofed hint must be ignored)", got)
	}
}

// RegisterDomain / RegisterHost reject empty inputs (fail-closed validation).
func TestRegister_EmptyInputsRejected(t *testing.T) {
	dir := mustHRD(t)
	ctx := context.Background()
	if err := dir.RegisterDomain(ctx, "", "d.example"); !errors.As(err, new(*HRDInvalidError)) {
		t.Fatalf("RegisterDomain empty tenant: got %v", err)
	}
	if err := dir.RegisterDomain(ctx, "t", "   "); !errors.As(err, new(*HRDInvalidError)) {
		t.Fatalf("RegisterDomain blank domain: got %v", err)
	}
	if err := dir.RegisterHost(ctx, "", "h.example"); !errors.As(err, new(*HRDInvalidError)) {
		t.Fatalf("RegisterHost empty tenant: got %v", err)
	}
	if err := dir.RegisterHost(ctx, "t", ""); !errors.As(err, new(*HRDInvalidError)) {
		t.Fatalf("RegisterHost empty host: got %v", err)
	}
}

// parentDomain returns "" for hosts with no routable parent (single label,
// trailing dot, or single-label parent like "com"), so HRD never routes on a
// non-domain.
func TestResolveProvider_NoRoutableParentDomain(t *testing.T) {
	dir := mustHRD(t)
	ctx := context.Background()
	// "localhost" has no parent label, and "host.com" has only the single-label
	// parent "com" — neither resolves a tenant, so HRD fails closed.
	for _, sni := range []string{"localhost", "host.com", "trailing."} {
		if _, err := dir.Resolve(ctx, RealmHints{SNIHost: sni}); !errors.As(err, new(*HRDNotFoundError)) {
			t.Fatalf("Resolve(%q) = %v, want HRDNotFoundError", sni, err)
		}
	}
}

// getErrorKVStore returns an error from Get so the directory's lookup treats a
// transient shared-store failure as a clean miss (never a wrong-tenant route).
type getErrorKVStore struct{ interfaces.KVStore }

func (getErrorKVStore) Get(context.Context, string) ([]byte, error) {
	return nil, errors.New("shared store unavailable")
}

// A shared-store Get error during resolution is a clean miss -> fail closed,
// never a silent route to a wrong tenant.
func TestResolve_SharedStoreErrorFailsClosed(t *testing.T) {
	dir, err := NewHomeRealmDirectory(getErrorKVStore{KVStore: newMockKVStore()})
	if err != nil {
		t.Fatalf("NewHomeRealmDirectory: %v", err)
	}
	_, err = dir.Resolve(context.Background(), RealmHints{Email: "u@whatever.example"})
	if !errors.As(err, new(*HRDNotFoundError)) {
		t.Fatalf("expected HRDNotFoundError on store error, got %v", err)
	}
}

// parentDomain strips a :port before computing the registrable parent so an
// SNI host carrying a port still routes via its registered parent domain.
func TestResolveProvider_SNIHostWithPort(t *testing.T) {
	dir := mustHRD(t)
	ctx := context.Background()
	if err := dir.RegisterDomain(ctx, "tenant-port", "tenant-port.io"); err != nil {
		t.Fatalf("RegisterDomain: %v", err)
	}
	got, err := dir.Resolve(ctx, RealmHints{SNIHost: "app.tenant-port.io:8443"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "tenant-port" {
		t.Fatalf("Resolve = %q, want tenant-port", got)
	}
}
