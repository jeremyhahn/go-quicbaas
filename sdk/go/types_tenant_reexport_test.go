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

package quicbaas

import (
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// The point of these re-exports is that a host forbidden from importing
// go-qrdb still gets the engine's own answer, so every assertion compares
// against the upstream symbol rather than against a hard-coded literal that
// could silently diverge.

func TestSystemTenantID_MatchesEngineConstant(t *testing.T) {
	if SystemTenantID != qrdbsdk.SystemTenantID {
		t.Fatalf("SystemTenantID = %q, engine says %q", SystemTenantID, qrdbsdk.SystemTenantID)
	}
	if SystemTenantID == "" {
		t.Fatal("SystemTenantID must not be empty; an empty constant would make every tenant look like the system tenant")
	}
}

func TestIsSystemTenant(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		want     bool
	}{
		{name: "the system tenant id", tenantID: SystemTenantID, want: true},
		{name: "unbound tenant resolves to system scope", tenantID: "", want: true},
		{name: "an ordinary tenant", tenantID: "acme", want: false},
		{name: "a tenant merely prefixed with the system id", tenantID: SystemTenantID + "-lookalike", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSystemTenant(tt.tenantID); got != tt.want {
				t.Errorf("IsSystemTenant(%q) = %v, want %v", tt.tenantID, got, tt.want)
			}
			if got, want := IsSystemTenant(tt.tenantID), qrdbsdk.IsSystemTenant(tt.tenantID); got != want {
				t.Errorf("IsSystemTenant(%q) = %v, engine says %v", tt.tenantID, got, want)
			}
		})
	}
}

func TestIdentityIsSystemTenant(t *testing.T) {
	tests := []struct {
		name     string
		identity *AuthIdentity
		want     bool
	}{
		{
			name:     "nil identity is an internal pipeline",
			identity: nil,
			want:     true,
		},
		{
			name:     "identity bound to the system tenant",
			identity: &AuthIdentity{TenantID: SystemTenantID},
			want:     true,
		},
		{
			name:     "identity with no tenant resolves to system scope",
			identity: &AuthIdentity{},
			want:     true,
		},
		{
			name:     "identity bound to an ordinary tenant",
			identity: &AuthIdentity{TenantID: "acme"},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IdentityIsSystemTenant(tt.identity); got != tt.want {
				t.Errorf("IdentityIsSystemTenant(%+v) = %v, want %v", tt.identity, got, tt.want)
			}
		})
	}
}

func TestTenantIsolationConstants_MatchEngine(t *testing.T) {
	tests := []struct {
		name  string
		local TenantIsolationType
		enfin TenantIsolationType
	}{
		{name: "prefix", local: TenantIsolationPrefix, enfin: qrdbsdk.TenantIsolationPrefix},
		{name: "barrier", local: TenantIsolationBarrier, enfin: qrdbsdk.TenantIsolationBarrier},
		{name: "volume", local: TenantIsolationVolume, enfin: qrdbsdk.TenantIsolationVolume},
		{name: "raft", local: TenantIsolationRaft, enfin: qrdbsdk.TenantIsolationRaft},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.local != tt.enfin {
				t.Errorf("isolation constant = %q, engine says %q", tt.local, tt.enfin)
			}
			if tt.local == "" {
				t.Error("an empty isolation constant would be indistinguishable from the default")
			}
		})
	}
}
