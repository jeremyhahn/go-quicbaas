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

package acme_test

import (
	"testing"

	acmeentities "github.com/jeremyhahn/go-xkms/pkg/acme/entities"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/acme"
)

// Compile-time proof EABDeviceInfo is the go-xkms entity, assignable both
// ways, so a binding built through the SDK is the one the ACME server stores.
var (
	_ acmeentities.EABDeviceInfo = acme.EABDeviceInfo{}
	_ acme.EABDeviceInfo         = acmeentities.EABDeviceInfo{}
)

// TestEABConstants_AreTheGoXKMSValues pins every purpose and status to the
// go-xkms value: the consumer writes these into the EAB store, and the ACME
// server compares them, so a copy that drifted would mint bindings the server
// refuses.
func TestEABConstants_AreTheGoXKMSValues(t *testing.T) {
	for _, tc := range []struct {
		name      string
		local, up string
	}{
		{"PurposeAccountBinding", acme.EABPurposeAccountBinding, acmeentities.EABPurposeAccountBinding},
		{"PurposeEndorse01", acme.EABPurposeEndorse01, acmeentities.EABPurposeEndorse01},
		{"PurposeEnroll01", acme.EABPurposeEnroll01, acmeentities.EABPurposeEnroll01},
		{"StatusActive", acme.EABStatusActive, acmeentities.EABStatusActive},
		{"StatusUsed", acme.EABStatusUsed, acmeentities.EABStatusUsed},
		{"StatusRevoked", acme.EABStatusRevoked, acmeentities.EABStatusRevoked},
		{"StatusExpired", acme.EABStatusExpired, acmeentities.EABStatusExpired},
	} {
		if tc.local != tc.up {
			t.Errorf("%s = %q, want go-xkms %q", tc.name, tc.local, tc.up)
		}
	}
}

// TestEABStatus_DrivesTheEntityLifecycle proves the aliased status values are
// the ones the go-xkms entity's own lifecycle reads: a binding marked with
// EABStatusActive is usable, and one marked EABStatusUsed is not.
func TestEABStatus_DrivesTheEntityLifecycle(t *testing.T) {
	active := &acme.ACMEExternalAccountBinding{Status: acme.EABStatusActive}
	if !active.IsActive() {
		t.Fatal("a binding with EABStatusActive must be active")
	}
	used := &acme.ACMEExternalAccountBinding{Status: acme.EABStatusUsed}
	if used.IsActive() {
		t.Fatal("a binding with EABStatusUsed must not be active")
	}
}
