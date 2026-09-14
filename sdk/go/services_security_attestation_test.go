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
	"context"
	"errors"
	"strings"
	"testing"

	pkgiam "github.com/jeremyhahn/go-quicbaas/pkg/iam"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestValidateAuthFactorMetadata_RefusesCallerSuppliedAttestation is the
// bypass guard on the direct auth-factor admin API.
//
// The WebAuthn ceremony now records what it PROVED about an authenticator, and
// login policy reads that record. This API writes a factor row without running
// any ceremony, so a caller-supplied attest_level would be a forged claim
// wearing the same field name — the token-level forgery this work removed,
// reappearing one layer down.
func TestValidateAuthFactorMetadata_RefusesCallerSuppliedAttestation(t *testing.T) {
	for _, key := range []string{pkgiam.AuthFactorMetadataAttestLevel, pkgiam.AuthFactorMetadataAttestAAGUID} {
		t.Run(key, func(t *testing.T) {
			err := validateAuthFactorMetadata("authfactors.Add", &AuthFactorEnrollRequest{
				FactorName:   "fido2",
				Subject:      "user-1",
				MethodParams: map[string]any{key: "hardware"},
			})
			if err == nil {
				t.Fatalf("a caller-supplied %s was accepted", key)
			}
			if !strings.Contains(err.Error(), key) {
				t.Errorf("the refusal does not name the field: %v", err)
			}
		})
	}
}

// TestValidateAuthFactorMetadata_AllowsOrdinaryMetadata keeps the refusal
// narrow: everything that is not an attestation field still passes.
func TestValidateAuthFactorMetadata_AllowsOrdinaryMetadata(t *testing.T) {
	if err := validateAuthFactorMetadata("authfactors.Add", &AuthFactorEnrollRequest{
		FactorName:   "fido2",
		MethodParams: map[string]any{"label": "ops key", "issued_by": "helpdesk"},
	}); err != nil {
		t.Fatalf("ordinary metadata was refused: %v", err)
	}
	if err := validateAuthFactorMetadata("authfactors.Add", nil); err != nil {
		t.Errorf("a nil request was refused: %v", err)
	}
	if err := validateAuthFactorMetadata("authfactors.Add", &AuthFactorEnrollRequest{}); err != nil {
		t.Errorf("a request with no MethodParams was refused: %v", err)
	}
}

// TestAuthFactorMetadata_LeavesTheAttestationNamespaceToTheServer pins that
// a direct enrollment writes nothing under attest_*: since go-qrdb
// v0.16.2-alpha the SERVER stamps attest_level=none on every plain Add/Rotate
// and refuses a caller — this SDK included — that writes into that namespace.
func TestAuthFactorMetadata_LeavesTheAttestationNamespaceToTheServer(t *testing.T) {
	meta := authFactorMetadata(&AuthFactorEnrollRequest{
		FactorName:   "fido2",
		MethodParams: map[string]any{"credential_id": "abc", "label": "ops key", "site": "hq"},
	})

	if got, present := meta[pkgiam.AuthFactorMetadataAttestLevel]; present {
		t.Errorf("the SDK wrote %s = %q; that key is the server's", pkgiam.AuthFactorMetadataAttestLevel, got)
	}
	// Lookup-key and label params are consumed elsewhere and must not be
	// duplicated into metadata.
	for _, reserved := range []string{"credential_id", "label"} {
		if _, ok := meta[reserved]; ok {
			t.Errorf("%s leaked into factor metadata", reserved)
		}
	}
	if meta["site"] != "hq" {
		t.Errorf("ordinary metadata was dropped: %+v", meta)
	}

	// No MethodParams at all means no metadata at all — not an empty map that
	// a reader could mistake for "the server wrote nothing".
	if got := authFactorMetadata(nil); got != nil {
		t.Errorf("a nil request produced %v, want nil", got)
	}
	if got := authFactorMetadata(&AuthFactorEnrollRequest{MethodParams: map[string]any{"credential_id": "abc"}}); got != nil {
		t.Errorf("only-reserved params produced %v, want nil", got)
	}
}

// TestValidateAuthFactorMetadata_AppliesToRotate is the "one verb along" guard:
// Rotate writes a NEW factor, so it must refuse a caller-asserted attestation
// exactly as Add does.
func TestValidateAuthFactorMetadata_AppliesToRotate(t *testing.T) {
	// Rotate calls the same validator, so asserting the validator is what both
	// verbs share; the verb-level wiring is asserted by the refusal below
	// reaching it before any credential derivation.
	err := validateAuthFactorMetadata("authfactors.Rotate", &AuthFactorEnrollRequest{
		FactorName:   "fido2",
		Subject:      "user-1",
		MethodParams: map[string]any{pkgiam.AuthFactorMetadataAttestLevel: "hardware"},
	})
	if err == nil {
		t.Fatal("Rotate's validator accepted a caller-supplied attest_level")
	}
}

// TestAuthFactorsRotate_RefusesCallerSuppliedAttestation exercises the PUBLIC
// Rotate method rather than the helper.
//
// The previous test called validateAuthFactorMetadata directly, so it passed
// while Rotate itself was never wired to it — a test that asserts a helper
// works proves nothing about whether the verb calls it.
func TestAuthFactorsRotate_RefusesCallerSuppliedAttestation(t *testing.T) {
	svc := &authFactorsImpl{}

	_, err := svc.Rotate(context.Background(), "old-factor", &AuthFactorEnrollRequest{
		FactorName:   "fido2",
		Subject:      "user-1",
		MethodParams: map[string]any{pkgiam.AuthFactorMetadataAttestLevel: "hardware"},
	})
	if err == nil {
		t.Fatal("Rotate accepted a caller-supplied attest_level")
	}
	if !strings.Contains(err.Error(), pkgiam.AuthFactorMetadataAttestLevel) {
		t.Errorf("the refusal does not name the field: %v", err)
	}
	// The refusal must name the operation the caller actually invoked. Both
	// verbs share one validator, and it used to report "authfactors.Add" for
	// every refusal — sending whoever reads the error to a call site that was
	// never involved.
	var qberr *sdkerrors.QBError
	if !errors.As(err, &qberr) {
		t.Fatalf("want *errors.QBError, got %T: %v", err, err)
	}
	if qberr.Op != "authfactors.Rotate" {
		t.Errorf("Op = %q, want %q", qberr.Op, "authfactors.Rotate")
	}
}

// TestAuthFactorsAdd_NamesItsOwnOperation is the paired control: sharing the
// validator must not make Add report Rotate either.
func TestAuthFactorsAdd_NamesItsOwnOperation(t *testing.T) {
	svc := &authFactorsImpl{}

	_, err := svc.Add(context.Background(), &AuthFactorEnrollRequest{
		FactorName:   "fido2",
		Subject:      "user-1",
		MethodParams: map[string]any{pkgiam.AuthFactorMetadataAttestLevel: "hardware"},
	})
	if err == nil {
		t.Fatal("Add accepted a caller-supplied attest_level")
	}
	var qberr *sdkerrors.QBError
	if !errors.As(err, &qberr) {
		t.Fatalf("want *errors.QBError, got %T: %v", err, err)
	}
	if qberr.Op != "authfactors.Add" {
		t.Errorf("Op = %q, want %q", qberr.Op, "authfactors.Add")
	}
}
