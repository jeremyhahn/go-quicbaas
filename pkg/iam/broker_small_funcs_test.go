//go:build iam

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

package iam

import (
	"context"
	"testing"
	"time"
)

// claimEmailVerified handles bool, string, missing, and unrecognised shapes.
func TestClaimEmailVerified(t *testing.T) {
	cases := []struct {
		name   string
		claims map[string]interface{}
		want   bool
	}{
		{"bool-true", map[string]interface{}{"email_verified": true}, true},
		{"bool-false", map[string]interface{}{"email_verified": false}, false},
		{"string-true", map[string]interface{}{"email_verified": "true"}, true},
		{"string-True", map[string]interface{}{"email_verified": " True "}, true},
		{"string-false", map[string]interface{}{"email_verified": "false"}, false},
		{"missing", map[string]interface{}{}, false},
		{"other-type", map[string]interface{}{"email_verified": 1}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := claimEmailVerified(tc.claims); got != tc.want {
				t.Fatalf("claimEmailVerified(%v) = %v, want %v", tc.claims, got, tc.want)
			}
		})
	}
}

// emailDomain extracts the normalized domain, returning "" for malformed input.
func TestEmailDomain(t *testing.T) {
	cases := map[string]string{
		"alice@Example.COM": "example.com",
		"no-at-sign":        "",
		"trailing@":         "",
		"u@ Sub.Example ":   "sub.example",
	}
	for in, want := range cases {
		if got := emailDomain(in); got != want {
			t.Fatalf("emailDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

// emailDomainAllowed enforces the allowlist, permitting all when empty.
func TestEmailDomainAllowed(t *testing.T) {
	if !emailDomainAllowed(nil, "anyone@anywhere.com") {
		t.Fatal("empty allowlist must permit all domains")
	}
	allowed := []string{"Example.com", "trusted.org"}
	if !emailDomainAllowed(allowed, "u@example.com") {
		t.Fatal("listed domain must be allowed (case-insensitive)")
	}
	if emailDomainAllowed(allowed, "u@evil.com") {
		t.Fatal("unlisted domain must be rejected")
	}
	if emailDomainAllowed(allowed, "no-domain") {
		t.Fatal("an address with no resolvable domain must be rejected when an allowlist exists")
	}
}

// groupMatches handles exact, prefix, case-insensitive, and empty cases.
func TestGroupMatches(t *testing.T) {
	if !groupMatches(GroupRoleMapping{Group: "Admins"}, "admins") {
		t.Fatal("exact case-insensitive match expected")
	}
	if groupMatches(GroupRoleMapping{Group: "Admins"}, "admins-extra") {
		t.Fatal("exact match must not match a longer value")
	}
	if !groupMatches(GroupRoleMapping{Group: "okta-", Prefix: true}, "okta-devs") {
		t.Fatal("prefix match expected")
	}
	if groupMatches(GroupRoleMapping{Group: ""}, "x") {
		t.Fatal("empty mapping group must never match")
	}
	if groupMatches(GroupRoleMapping{Group: "x"}, "") {
		t.Fatal("empty idp value must never match")
	}
}

// tenant_provisioner accessors round-trip their configured values.
func TestTenantManager_ProvisionerAccessors(t *testing.T) {
	mgr := newTestTenantManager()
	if mgr.Provisioner() != nil {
		t.Fatal("Provisioner() should be nil by default")
	}
	if mgr.QRDBProvisioner() != nil {
		t.Fatal("QRDBProvisioner() should be nil by default")
	}
	// SetQRDBTenantProvisioner accepts nil without panicking and is reflected.
	mgr.SetQRDBTenantProvisioner(nil)
	if mgr.QRDBProvisioner() != nil {
		t.Fatal("QRDBProvisioner() should remain nil after setting nil")
	}
}

// observerRecorder captures OnTenantSettingsChanged notifications.
type observerRecorder struct{ tenants []string }

func (o *observerRecorder) OnTenantSettingsChanged(tenantID string) {
	o.tenants = append(o.tenants, tenantID)
}

// AddSettingsObserver registers an observer that fires on UpdateTenant; a nil
// observer is ignored.
func TestTenantManager_AddSettingsObserver(t *testing.T) {
	ctx := context.Background()
	mgr := newTestTenantManager()

	mgr.AddSettingsObserver(nil) // no-op, must not register anything

	rec := &observerRecorder{}
	mgr.AddSettingsObserver(rec)

	tenant, err := mgr.CreateTenant(ctx, CreateTenantOptions{Name: "Obs"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	newName := "Obs-Renamed"
	if _, err := mgr.UpdateTenant(ctx, tenant.ID, UpdateTenantOptions{Name: &newName}); err != nil {
		t.Fatalf("UpdateTenant: %v", err)
	}
	if len(rec.tenants) == 0 || rec.tenants[len(rec.tenants)-1] != tenant.ID {
		t.Fatalf("observer not notified for %s; got %v", tenant.ID, rec.tenants)
	}
}

// WithFederationClock ignores a nil clock and installs a usable one otherwise.
func TestWithFederationClock(t *testing.T) {
	m := &DefaultFederationManager{}
	WithFederationClock(nil)(m)
	if m.now != nil {
		t.Fatal("nil clock must be ignored")
	}
	fixed := time.Unix(1700000000, 0)
	WithFederationClock(func() time.Time { return fixed })(m)
	if m.now == nil {
		t.Fatal("clock should be installed")
	}
	if !m.now().Equal(fixed) {
		t.Fatalf("installed clock returned %v, want %v", m.now(), fixed)
	}
}

// WebAuthnLoginSessionError renders a default message for nil/empty reason.
func TestWebAuthnLoginSessionError_DefaultMessage(t *testing.T) {
	var nilErr *WebAuthnLoginSessionError
	if nilErr.Error() == "" {
		t.Fatal("nil receiver must still render a message")
	}
	if (&WebAuthnLoginSessionError{}).Error() == "" {
		t.Fatal("empty reason must render the default message")
	}
	if got := (&WebAuthnLoginSessionError{Reason: "boom"}).Error(); got == "" {
		t.Fatalf("reason message empty: %q", got)
	}
}
