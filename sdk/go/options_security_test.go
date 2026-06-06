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
)

// =============================================================================
// WithEmbeddedQRDB optional-field round-trip tests
// =============================================================================
//
// These tests assert that the optional servicer fields on QRDBEmbeddedServicers
// (Barrier, Ceremony, CeremonyCA, Credential, etc.) round-trip onto the
// clientOptions snapshot when supplied via WithEmbeddedQRDB. Reusing the
// stub*Service types defined in qrdb_embedded_test.go keeps this file lean.

func TestWithEmbeddedQRDB_PersistsBarrierServicer(t *testing.T) {
	opts := defaultOptions()
	if err := WithEmbeddedQRDB(QRDBEmbeddedServicers{
		KV:      stubKVService{},
		Admin:   stubAdminService{},
		Barrier: &stubBarrierService{},
	})(opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.embeddedQRDB == nil || opts.embeddedQRDB.Barrier == nil {
		t.Error("Barrier servicer should round-trip onto clientOptions")
	}
}

func TestWithEmbeddedQRDB_PersistsCeremonyServicer(t *testing.T) {
	opts := defaultOptions()
	if err := WithEmbeddedQRDB(QRDBEmbeddedServicers{
		KV:       stubKVService{},
		Admin:    stubAdminService{},
		Ceremony: &stubCeremonyService{},
	})(opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.embeddedQRDB == nil || opts.embeddedQRDB.Ceremony == nil {
		t.Error("Ceremony servicer should round-trip onto clientOptions")
	}
}

func TestWithEmbeddedQRDB_PersistsCredentialServicer(t *testing.T) {
	opts := defaultOptions()
	if err := WithEmbeddedQRDB(QRDBEmbeddedServicers{
		KV:         stubKVService{},
		Admin:      stubAdminService{},
		Credential: &stubCredentialService{},
	})(opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.embeddedQRDB == nil || opts.embeddedQRDB.Credential == nil {
		t.Error("Credential servicer should round-trip onto clientOptions")
	}
}

// =============================================================================
// MCP option round-trip tests
// =============================================================================

func TestWithMCPCredentials(t *testing.T) {
	opts := defaultOptions()
	if err := WithMCPCredentials("alice", "secret")(opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.mcpUsername != "alice" || opts.mcpPassword != "secret" {
		t.Errorf("got (%q, %q), want (\"alice\", \"secret\")", opts.mcpUsername, opts.mcpPassword)
	}
}

func TestWithMCPAuthToken(t *testing.T) {
	opts := defaultOptions()
	if err := WithMCPAuthToken("tok-1")(opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.mcpAuthToken != "tok-1" {
		t.Errorf("got %q, want %q", opts.mcpAuthToken, "tok-1")
	}
}
