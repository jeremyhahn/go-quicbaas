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
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// dummyQRDBClient is the zero-value sentinel we hand the factory in
// configuration-error tests where no real qrdb backend is required.
// Construction never reaches qrdb when the factory rejects the config
// up-front.
var dummyQRDBClient = &qrdbsdk.Client{}

// TestNewWebAuthnServiceFactory_RejectsMissingQRDB verifies that the
// factory refuses construction when no qrdb client is supplied.
func TestNewWebAuthnServiceFactory_RejectsMissingQRDB(t *testing.T) {
	_, err := NewWebAuthnServiceFactory(WebAuthnFactoryParams{
		Config: WebAuthnFactoryConfig{
			RPID:          "example.com",
			RPDisplayName: "Example",
			RPOrigins:     []string{"https://example.com"},
		},
		QRDB:                nil,
		AttestationRecorder: recorderOf(&fakeUserAuthFactorClient{}),
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var cfgErr *WebAuthnFactoryConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected *WebAuthnFactoryConfigError, got %T: %v", err, err)
	}
	if cfgErr.Field != "QRDB" {
		t.Fatalf("expected field QRDB, got %q", cfgErr.Field)
	}
}

// TestNewWebAuthnServiceFactory_RejectsRequiredFields exercises every
// required-field check on WebAuthnFactoryConfig.
func TestNewWebAuthnServiceFactory_RejectsRequiredFields(t *testing.T) {
	cases := []struct {
		name      string
		cfg       WebAuthnFactoryConfig
		wantField string
	}{
		{
			name:      "missing rp id",
			cfg:       WebAuthnFactoryConfig{RPDisplayName: "X", RPOrigins: []string{"https://x"}},
			wantField: "Config.RPID",
		},
		{
			name:      "missing display name",
			cfg:       WebAuthnFactoryConfig{RPID: "x.com", RPOrigins: []string{"https://x"}},
			wantField: "Config.RPDisplayName",
		},
		{
			name:      "missing origins",
			cfg:       WebAuthnFactoryConfig{RPID: "x.com", RPDisplayName: "X"},
			wantField: "Config.RPOrigins",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewWebAuthnServiceFactory(WebAuthnFactoryParams{
				Config:              tc.cfg,
				QRDB:                dummyQRDBClient,
				AttestationRecorder: recorderOf(&fakeUserAuthFactorClient{}),
			})
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			var cfgErr *WebAuthnFactoryConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("expected *WebAuthnFactoryConfigError, got %T", err)
			}
			if cfgErr.Field != tc.wantField {
				t.Fatalf("expected field %q, got %q", tc.wantField, cfgErr.Field)
			}
		})
	}
}

// TestNewWebAuthnServiceFactory_HappyPath confirms that a factory with
// valid configuration constructs successfully. The factory is a thin
// caching layer; we don't exercise ServiceFor here because that
// requires a live qrdb embedded transport.
func TestNewWebAuthnServiceFactory_HappyPath(t *testing.T) {
	f, err := NewWebAuthnServiceFactory(WebAuthnFactoryParams{
		Config: WebAuthnFactoryConfig{
			RPID:          "example.com",
			RPDisplayName: "Example",
			RPOrigins:     []string{"https://example.com"},
		},
		QRDB:                dummyQRDBClient,
		AttestationRecorder: recorderOf(&fakeUserAuthFactorClient{}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f == nil {
		t.Fatalf("expected non-nil factory")
	}
}

// TestWebAuthnServiceFactory_ServiceFor_RejectsEmptyTenantID verifies
// the per-tenant lookup refuses an empty tenant id.
func TestWebAuthnServiceFactory_ServiceFor_RejectsEmptyTenantID(t *testing.T) {
	f, err := NewWebAuthnServiceFactory(WebAuthnFactoryParams{
		Config: WebAuthnFactoryConfig{
			RPID:          "example.com",
			RPDisplayName: "Example",
			RPOrigins:     []string{"https://example.com"},
		},
		QRDB:                dummyQRDBClient,
		AttestationRecorder: recorderOf(&fakeUserAuthFactorClient{}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := f.ServiceFor(context.Background(), ""); err == nil {
		t.Fatalf("expected error for empty tenant id")
	}
}

// TestWebAuthnFactoryConfigError_Message verifies the error string
// contains the offending field name when set, and a generic fallback
// when empty.
func TestWebAuthnFactoryConfigError_Message(t *testing.T) {
	withField := &WebAuthnFactoryConfigError{Field: "RPID"}
	if got := withField.Error(); got == "" {
		t.Fatalf("empty error string for field RPID")
	}
	empty := &WebAuthnFactoryConfigError{}
	if got := empty.Error(); got == "" {
		t.Fatalf("empty error string for empty field")
	}
}

// TestNewXKMSWebAuthnManager_RequiresFactory ensures construction fails
// without a factory.
func TestNewXKMSWebAuthnManager_RequiresFactory(t *testing.T) {
	_, err := NewXKMSWebAuthnManager(nil)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var cfgErr *WebAuthnFactoryConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected *WebAuthnFactoryConfigError, got %T", err)
	}
	if cfgErr.Field != "factory" {
		t.Fatalf("expected field factory, got %q", cfgErr.Field)
	}
}

// TestWebAuthnServiceFactory_GateCarriesConfiguredPolicy verifies the factory
// hands its assurance policy and AAGUID allowlist to the gate the per-tenant
// credential store applies.
//
// The allowlist used to live on XKMSWebAuthnManager and be consulted only by
// that type's FinishRegistration, which the live registration route does not
// call. Asserting it on the factory is asserting it where every ceremony
// reaches it.
func TestWebAuthnServiceFactory_GateCarriesConfiguredPolicy(t *testing.T) {
	allowlist := NewAAGUIDAllowlist([]string{"00000000000000000000000000000000"}, AAGUIDPolicyStrict)
	f, err := NewWebAuthnServiceFactory(WebAuthnFactoryParams{
		Config: WebAuthnFactoryConfig{
			RPID:          "example.com",
			RPDisplayName: "Example",
			RPOrigins:     []string{"https://example.com"},
		},
		QRDB:                dummyQRDBClient,
		AttestationRecorder: recorderOf(&fakeUserAuthFactorClient{}),
		Assurance:           WebAuthnAssuranceAny,
		AAGUIDAllowlist:     allowlist,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gate := f.gate()
	if gate.Assurance() != WebAuthnAssuranceAny {
		t.Fatalf("gate assurance = %v, want any", gate.Assurance())
	}
	if gate.allowlist != allowlist {
		t.Fatal("the factory did not hand its allowlist to the gate")
	}
}

// TestWebAuthnServiceFactory_GateDefaultsToHardware is the zero-value contract:
// a factory built with no assurance and no allowlist produces a gate that
// refuses, not one that waves everything through.
func TestWebAuthnServiceFactory_GateDefaultsToHardware(t *testing.T) {
	f, err := NewWebAuthnServiceFactory(WebAuthnFactoryParams{
		Config: WebAuthnFactoryConfig{
			RPID:          "example.com",
			RPDisplayName: "Example",
			RPOrigins:     []string{"https://example.com"},
		},
		QRDB:                dummyQRDBClient,
		AttestationRecorder: recorderOf(&fakeUserAuthFactorClient{}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gate := f.gate()
	if gate.Assurance() != WebAuthnAssuranceHardware {
		t.Fatalf("gate assurance = %v, want hardware", gate.Assurance())
	}
	if _, evalErr := gate.Evaluate(context.Background(), "t1", "op@example.com", "basic_full", make([]byte, 16)); evalErr == nil {
		t.Fatal("a factory with no configured allowlist produced a gate that accepts")
	}
}

// TestWebAuthnServiceFactory_AttestationConveyance proves the relying party
// asks for "direct" whenever it requires hardware. Requesting the default
// "none" conveyance would strip the attestation statement in the browser, so a
// hardware requirement could only ever refuse.
func TestWebAuthnServiceFactory_AttestationConveyance(t *testing.T) {
	for _, tc := range []struct {
		name       string
		assurance  WebAuthnAssurance
		configured string
		want       string
	}{
		{"hardware raises none to direct", WebAuthnAssuranceHardware, "none", "direct"},
		{"hardware raises empty to direct", WebAuthnAssuranceHardware, "", "direct"},
		{"hardware raises indirect to direct", WebAuthnAssuranceHardware, "indirect", "direct"},
		{"hardware keeps enterprise", WebAuthnAssuranceHardware, "enterprise", "enterprise"},
		{"hardware keeps direct", WebAuthnAssuranceHardware, "direct", "direct"},
		// "any" still holds ADMINISTRATORS to hardware, so it must request
		// attestation too — otherwise the browser strips the statement the gate
		// then demands and no administrator can ever enroll.
		{"any still raises, because admins need hardware", WebAuthnAssuranceAny, "none", "direct"},
		{"the insecure test policy leaves the operator value alone",
			WebAuthnAssuranceInsecureAllowSoftwareAdministrators, "none", "none"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := NewWebAuthnServiceFactory(WebAuthnFactoryParams{
				Config: WebAuthnFactoryConfig{
					RPID:          "example.com",
					RPDisplayName: "Example",
					RPOrigins:     []string{"https://example.com"},
				},
				QRDB:                dummyQRDBClient,
				AttestationRecorder: recorderOf(&fakeUserAuthFactorClient{}),
				Assurance:           tc.assurance,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := f.attestationConveyance(tc.configured); got != tc.want {
				t.Errorf("attestationConveyance(%q) = %q, want %q", tc.configured, got, tc.want)
			}
		})
	}
}

// TestXKMSWebAuthnConfig_PublicRegistrationClosed proves the per-tenant
// relying party never lets a ceremony create an account (ruling 189, #482):
// self-service registration creates the account in the broker and the
// ceremony only binds a credential to it. The setting is written as an
// explicit false rather than left to go-xkms's nil default.
func TestXKMSWebAuthnConfig_PublicRegistrationClosed(t *testing.T) {
	cfg := xkmsWebAuthnConfig(WebAuthnFactoryConfig{
		RPID:          "example.com",
		RPDisplayName: "Example",
		RPOrigins:     []string{"https://example.com"},
	})
	if cfg.AllowPublicRegistration == nil {
		t.Fatal("AllowPublicRegistration is nil; quicbaas must write the closed setting explicitly")
	}
	if *cfg.AllowPublicRegistration || cfg.PublicRegistrationAllowed() {
		t.Error("public registration is open; a ceremony must never create an account")
	}
	if cfg.RPID != "example.com" || len(cfg.RPOrigins) != 1 {
		t.Errorf("relying-party identity was not carried: %+v", cfg)
	}
}

// TestPublicRegistrationClosed_FreshValue: each call returns its own false,
// so a caller that writes through one config's pointer cannot open another's.
func TestPublicRegistrationClosed_FreshValue(t *testing.T) {
	first := publicRegistrationClosed()
	second := publicRegistrationClosed()
	if first == second {
		t.Fatal("publicRegistrationClosed returned a shared pointer")
	}
	*first = true
	if *second {
		t.Error("writing through one closed setting opened another")
	}
}
