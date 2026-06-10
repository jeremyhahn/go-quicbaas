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
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// authFactorSPKICredentialFor — derives the optional renewal-stable SPKI
// lookup credential from MethodParams["spki_sha256"].
// =============================================================================

func TestAuthFactorSPKICredentialFor_MTLSValidHex(t *testing.T) {
	want := sha256.Sum256([]byte("spki-der-bytes"))
	req := &AuthFactorEnrollRequest{
		FactorName:   "mtls",
		Subject:      "alice",
		MethodParams: map[string]any{"spki_sha256": hex.EncodeToString(want[:])},
	}
	got, err := authFactorSPKICredentialFor("mtls", req)
	if err != nil {
		t.Fatalf("authFactorSPKICredentialFor(mtls) error: %v", err)
	}
	if !bytes.Equal(got, want[:]) {
		t.Fatalf("SPKI credential = %x, want %x", got, want[:])
	}
}

func TestAuthFactorSPKICredentialFor_PIVValidHex(t *testing.T) {
	want := sha256.Sum256([]byte("piv-spki-der"))
	req := &AuthFactorEnrollRequest{
		FactorName:   "piv",
		Subject:      "bob",
		MethodParams: map[string]any{"spki_sha256": hex.EncodeToString(want[:])},
	}
	got, err := authFactorSPKICredentialFor("piv", req)
	if err != nil {
		t.Fatalf("authFactorSPKICredentialFor(piv) error: %v", err)
	}
	if !bytes.Equal(got, want[:]) {
		t.Fatalf("SPKI credential = %x, want %x", got, want[:])
	}
}

func TestAuthFactorSPKICredentialFor_AbsentParamIsOptional(t *testing.T) {
	req := &AuthFactorEnrollRequest{
		FactorName:   "mtls",
		Subject:      "alice",
		MethodParams: map[string]any{"credential_sha256": "aa"},
	}
	got, err := authFactorSPKICredentialFor("mtls", req)
	if err != nil {
		t.Fatalf("absent spki_sha256 must not error, got: %v", err)
	}
	if got != nil {
		t.Fatalf("absent spki_sha256 must yield nil credential, got %x", got)
	}
}

func TestAuthFactorSPKICredentialFor_NilRequest(t *testing.T) {
	got, err := authFactorSPKICredentialFor("mtls", nil)
	if err != nil {
		t.Fatalf("nil request must not error, got: %v", err)
	}
	if got != nil {
		t.Fatalf("nil request must yield nil credential, got %x", got)
	}
}

func TestAuthFactorSPKICredentialFor_InvalidHexRejected(t *testing.T) {
	req := &AuthFactorEnrollRequest{
		FactorName:   "mtls",
		Subject:      "alice",
		MethodParams: map[string]any{"spki_sha256": "zz-not-hex"},
	}
	got, err := authFactorSPKICredentialFor("mtls", req)
	if err == nil {
		t.Fatal("invalid hex spki_sha256 must be rejected")
	}
	if got != nil {
		t.Fatalf("invalid hex must yield nil credential, got %x", got)
	}
	if sdkerrors.Code(err) != sdkerrors.CodeInvalidInput {
		t.Fatalf("error code = %v, want CodeInvalidInput", sdkerrors.Code(err))
	}
}

func TestAuthFactorSPKICredentialFor_FIDO2HasNoSPKI(t *testing.T) {
	sum := sha256.Sum256([]byte("anything"))
	req := &AuthFactorEnrollRequest{
		FactorName:   "fido2",
		Subject:      "alice",
		MethodParams: map[string]any{"spki_sha256": hex.EncodeToString(sum[:])},
	}
	got, err := authFactorSPKICredentialFor("fido2", req)
	if err != nil {
		t.Fatalf("fido2 must not error, got: %v", err)
	}
	if got != nil {
		t.Fatalf("fido2 has no SPKI concept; got %x", got)
	}
}

// authFactorMetadata must not leak the spki_sha256 credential parameter into
// the descriptive factor metadata.
func TestAuthFactorMetadata_SkipsSPKIParam(t *testing.T) {
	req := &AuthFactorEnrollRequest{
		FactorName: "mtls",
		Subject:    "alice",
		MethodParams: map[string]any{
			"spki_sha256":       "aabb",
			"credential_sha256": "ccdd",
			"cert_cn":           "alice.example",
		},
	}
	meta := authFactorMetadata(req)
	if _, ok := meta["spki_sha256"]; ok {
		t.Fatal("spki_sha256 must be excluded from factor metadata")
	}
	if meta["cert_cn"] != "alice.example" {
		t.Fatalf("descriptive metadata lost: %v", meta)
	}
}
