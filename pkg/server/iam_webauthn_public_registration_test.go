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

package server

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// The operator's iam.self_service_registration.enabled decision is what
// authorises a WebAuthn ceremony to CREATE an account for an unknown email.
// go-xkms v0.9.2-alpha closed that by default (v0.9.1 created the account
// implicitly and its own config test asserted "a nil Config must read as
// open"), so quicbaas must map the decision explicitly. Leaving it unmapped
// disables self-service registration on a node that has it switched on —
// silently, because the failure is a 403 from the ceremony rather than a
// startup error.
//
// Both relying parties on a composed node are covered, because a node that
// opened one lane and closed the other would put the two registration
// surfaces back into disagreement.

// TestWebAuthnFactoryConfig_CarriesTheSelfServiceDecision covers quicbaas's
// own per-tenant relying party.
func TestWebAuthnFactoryConfig_CarriesTheSelfServiceDecision(t *testing.T) {
	for _, allow := range []bool{true, false} {
		got, err := webauthnFactoryConfigFromOperator(
			config.IAMWebAuthnConfig{}, "localhost", "https://localhost:18443", allow)
		if err != nil {
			t.Fatalf("resolve operator config (allow=%v): %v", allow, err)
		}
		if got.AllowPublicRegistration != allow {
			t.Errorf("AllowPublicRegistration = %v, want %v", got.AllowPublicRegistration, allow)
		}
	}
}

// TestModuleWebAuthnConfig_CarriesTheSelfServiceDecision covers the composed
// go-xkms module's relying party, which serves the /api/v1/webauthn/sdk/*
// ceremony `xkey auth register` drives.
func TestModuleWebAuthnConfig_CarriesTheSelfServiceDecision(t *testing.T) {
	for _, allow := range []bool{true, false} {
		forwarded := quicbaasWebAuthnConfigToXKMS(
			config.IAMWebAuthnConfig{RPID: "localhost", RPOrigins: []string{"https://localhost:18443"}},
			allow)
		if forwarded.AllowPublicRegistration == nil {
			t.Fatalf("AllowPublicRegistration is nil for allow=%v; go-xkms reads an unset "+
				"value as closed, so the operator's decision would be discarded", allow)
		}
		if *forwarded.AllowPublicRegistration != allow {
			t.Errorf("AllowPublicRegistration = %v, want %v", *forwarded.AllowPublicRegistration, allow)
		}
	}
}

// TestBothRelyingPartiesAgreeOnPublicRegistration is the invariant the two
// tests above exist to protect: one operator decision, the same answer on
// both ceremony surfaces.
func TestBothRelyingPartiesAgreeOnPublicRegistration(t *testing.T) {
	operator := config.IAMWebAuthnConfig{RPID: "localhost", RPOrigins: []string{"https://localhost:18443"}}
	for _, allow := range []bool{true, false} {
		own, err := webauthnFactoryConfigFromOperator(operator, "localhost", "https://localhost:18443", allow)
		if err != nil {
			t.Fatalf("resolve operator config (allow=%v): %v", allow, err)
		}
		module := quicbaasWebAuthnConfigToXKMS(operator, allow)
		if module.AllowPublicRegistration == nil {
			t.Fatalf("module relying party discarded the decision (allow=%v)", allow)
		}
		if own.AllowPublicRegistration != *module.AllowPublicRegistration {
			t.Errorf("relying parties disagree: quicbaas=%v module=%v (operator said %v)",
				own.AllowPublicRegistration, *module.AllowPublicRegistration, allow)
		}
	}
}
