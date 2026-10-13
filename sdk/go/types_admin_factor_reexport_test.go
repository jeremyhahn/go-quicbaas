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

package quicbaas_test

import (
	"errors"
	"slices"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// The aliases and the qrdb types are one type, so a value crosses the SDK
// boundary without conversion in either direction.
var (
	_ *qrdbsdk.AdminEnrollmentPlan          = (*quicbaas.AdminEnrollmentPlan)(nil)
	_ *quicbaas.AdminEnrollmentPlan         = (*qrdbsdk.AdminEnrollmentPlan)(nil)
	_ *qrdbsdk.UnsupportedAdminFactorError  = (*quicbaas.UnsupportedAdminFactorError)(nil)
	_ *quicbaas.UnsupportedAdminFactorError = (*qrdbsdk.UnsupportedAdminFactorError)(nil)
)

// TestParseAdminFactors_AcceptsKnownFactors pins the SDK path a consumer uses
// to validate auth.admin_factors at config load: case and whitespace are
// normalised, comma lists and repeated values mix, and mTLS is enrolled first.
func TestParseAdminFactors_AcceptsKnownFactors(t *testing.T) {
	plan, err := quicbaas.ParseAdminFactors([]string{" FIDO2 , mtls", "fido2"})
	if err != nil {
		t.Fatalf("ParseAdminFactors: %v", err)
	}

	want := []quicbaas.AdminFactor{qrdbsdk.FactorMTLS, qrdbsdk.FactorFIDO2}
	if got := plan.Factors(); !slices.Equal(got, want) {
		t.Fatalf("Factors() = %v, want %v", got, want)
	}
}

// TestParseAdminFactors_EmptyYieldsMTLSBaseline covers a config that leaves
// auth.admin_factors unset.
func TestParseAdminFactors_EmptyYieldsMTLSBaseline(t *testing.T) {
	plan, err := quicbaas.ParseAdminFactors(nil)
	if err != nil {
		t.Fatalf("ParseAdminFactors(nil): %v", err)
	}
	if !plan.Includes(qrdbsdk.FactorMTLS) || len(plan.Factors()) != 1 {
		t.Fatalf("Factors() = %v, want only %q", plan.Factors(), qrdbsdk.FactorMTLS)
	}
}

// TestParseAdminFactors_RejectsUnknownFactor asserts the typed error a
// consumer matches through the SDK alias.
func TestParseAdminFactors_RejectsUnknownFactor(t *testing.T) {
	plan, err := quicbaas.ParseAdminFactors([]string{"mtls,password"})
	if err == nil {
		t.Fatalf("ParseAdminFactors accepted %q: plan %v", "password", plan.Factors())
	}

	var unsupported *quicbaas.UnsupportedAdminFactorError
	if !errors.As(err, &unsupported) {
		t.Fatalf("error %v (%T) is not *UnsupportedAdminFactorError", err, err)
	}
	if unsupported.Factor != quicbaas.AdminFactor("password") {
		t.Fatalf("Factor = %q, want %q", unsupported.Factor, "password")
	}
}
