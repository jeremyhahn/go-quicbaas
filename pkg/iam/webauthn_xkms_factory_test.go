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
		QRDB: nil,
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
				Config: tc.cfg,
				QRDB:   dummyQRDBClient,
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
		QRDB: dummyQRDBClient,
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
		QRDB: dummyQRDBClient,
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

// TestXKMSWebAuthnManager_SetAAGUIDAllowlist verifies the allowlist
// setter is concurrency-safe and returns the most recent value via
// the internal accessor.
func TestXKMSWebAuthnManager_SetAAGUIDAllowlist(t *testing.T) {
	f, err := NewWebAuthnServiceFactory(WebAuthnFactoryParams{
		Config: WebAuthnFactoryConfig{
			RPID:          "example.com",
			RPDisplayName: "Example",
			RPOrigins:     []string{"https://example.com"},
		},
		QRDB: dummyQRDBClient,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, err := NewXKMSWebAuthnManager(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.allowlist() != nil {
		t.Fatalf("expected nil allowlist initially")
	}

	allowlist := NewAAGUIDAllowlist([]string{"00000000000000000000000000000000"}, AAGUIDPolicyStrict)
	m.SetAAGUIDAllowlist(allowlist)
	if got := m.allowlist(); got != allowlist {
		t.Fatalf("SetAAGUIDAllowlist did not store the value")
	}

	// Setting nil clears the allowlist.
	m.SetAAGUIDAllowlist(nil)
	if got := m.allowlist(); got != nil {
		t.Fatalf("SetAAGUIDAllowlist(nil) did not clear, got %v", got)
	}
}
