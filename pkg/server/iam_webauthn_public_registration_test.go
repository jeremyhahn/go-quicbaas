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

// A WebAuthn ceremony never creates an account (ruling 189, #482).
// Self-service registration creates the account in the broker and the
// ceremony only binds a credential to it. quicbaas used to forward the
// operator's iam.self_service_registration.enabled decision as go-xkms's
// AllowPublicRegistration, so a ceremony created the account itself — with the
// email as its username — and the broker's own create then collided on the
// email. quicbaas's own relying party is covered in pkg/iam
// (TestXKMSWebAuthnConfig_PublicRegistrationClosed).

// TestModuleWebAuthnConfig_PublicRegistrationClosed covers the composed
// go-xkms module's relying party, which serves the /api/v1/webauthn/sdk/*
// ceremony `xkey auth register` drives: it is closed, and closed explicitly
// rather than left to go-xkms's nil default.
func TestModuleWebAuthnConfig_PublicRegistrationClosed(t *testing.T) {
	forwarded := quicbaasWebAuthnConfigToXKMS(
		config.IAMWebAuthnConfig{RPID: "localhost", RPOrigins: []string{"https://localhost:18443"}})
	if forwarded.AllowPublicRegistration == nil {
		t.Fatal("AllowPublicRegistration is nil; quicbaas must write the closed setting explicitly")
	}
	if *forwarded.AllowPublicRegistration {
		t.Error("AllowPublicRegistration = true; a ceremony must never create an account")
	}
}

// TestModuleWebAuthnConfig_UnconfiguredIsClosed: a host with no WebAuthn
// section leaves the module's surface unmounted, and the setting still reads
// closed should anything mount it.
func TestModuleWebAuthnConfig_UnconfiguredIsClosed(t *testing.T) {
	forwarded := quicbaasWebAuthnConfigToXKMS(config.IAMWebAuthnConfig{})
	if forwarded.Enabled {
		t.Error("an unconfigured WebAuthn section must leave the module surface unmounted")
	}
	if forwarded.AllowPublicRegistration == nil || *forwarded.AllowPublicRegistration {
		t.Error("AllowPublicRegistration must be an explicit false")
	}
}
