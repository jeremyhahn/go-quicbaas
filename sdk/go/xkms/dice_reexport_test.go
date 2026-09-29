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

package xkms_test

import (
	"errors"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms"
)

// Compile-time proof the DICE aliases are the go-xkms types, not lookalikes: a
// value of the upstream type must be assignable to the alias and back. A
// lookalike compiles, passes every test in this package, and fails only where
// a consumer hands a value across the boundary -- which is the one place it
// matters and the last place anyone looks.
var (
	_ xkmssdk.DICEEvidence         = xkms.DICEEvidence{}
	_ xkms.DICEEvidence            = xkmssdk.DICEEvidence{}
	_ xkmssdk.DICEVerifyOptions    = xkms.DICEVerifyOptions{}
	_ xkms.DICEVerifyOptions       = xkmssdk.DICEVerifyOptions{}
	_ xkmssdk.DICETcbInfo          = xkms.DICETcbInfo{}
	_ xkms.DICETcbInfo             = xkmssdk.DICETcbInfo{}
	_ xkmssdk.DICEFWID             = xkms.DICEFWID{}
	_ xkmssdk.DICEOperationalFlags = xkms.DICEOperationalFlags{}
	_ xkmssdk.DICEUeid             = xkms.DICEUeid{}
	_ xkmssdk.DICEClaimsSet        = xkms.DICEClaimsSet{}
)

// TestDICEErrorSentinels_AreTheUpstreamValues is why the sentinels are aliased
// one by one rather than collapsed. A consumer matches on them with errors.Is
// to decide what to do, and the decision that matters -- a claim that was
// ABSENT versus one that was WRONG -- is destroyed by collapsing them.
func TestDICEErrorSentinels_AreTheUpstreamValues(t *testing.T) {
	for _, tc := range []struct {
		name      string
		local, up error
	}{
		{"NilCertificate", xkms.ErrDICENilCertificate, xkmssdk.ErrDICENilCertificate},
		{"NilVerifyOptions", xkms.ErrDICENilVerifyOptions, xkmssdk.ErrDICENilVerifyOptions},
		{"TcbInfoNotFound", xkms.ErrDICETcbInfoNotFound, xkmssdk.ErrDICETcbInfoNotFound},
		{"UeidNotFound", xkms.ErrDICEUeidNotFound, xkmssdk.ErrDICEUeidNotFound},
		{"UccsEvidenceNotFound", xkms.ErrDICEUccsEvidenceNotFound, xkmssdk.ErrDICEUccsEvidenceNotFound},
		{"MalformedTcbInfo", xkms.ErrDICEMalformedTcbInfo, xkmssdk.ErrDICEMalformedTcbInfo},
		{"MalformedUccsEvidence", xkms.ErrDICEMalformedUccsEvidence, xkmssdk.ErrDICEMalformedUccsEvidence},
		{"NonceClaimMissing", xkms.ErrDICENonceClaimMissing, xkmssdk.ErrDICENonceClaimMissing},
		{"NonceMismatch", xkms.ErrDICENonceMismatch, xkmssdk.ErrDICENonceMismatch},
		{"NonceRequired", xkms.ErrDICENonceRequired, xkmssdk.ErrDICENonceRequired},
		{"EmptyChain", xkms.ErrDICEEmptyChain, xkmssdk.ErrDICEEmptyChain},
		{"ChainVerificationFailed", xkms.ErrDICEChainVerificationFailed, xkmssdk.ErrDICEChainVerificationFailed},
		{"UeidMismatch", xkms.ErrDICEUeidMismatch, xkmssdk.ErrDICEUeidMismatch},
		{"NoMeasurement", xkms.ErrDICENoMeasurement, xkmssdk.ErrDICENoMeasurement},
		{"OperationalStateNotPermitted", xkms.ErrDICEOperationalStateNotPermitted, xkmssdk.ErrDICEOperationalStateNotPermitted},
		{"CriticalEvidenceUnreadable", xkms.ErrDICECriticalEvidenceUnreadable, xkmssdk.ErrDICECriticalEvidenceUnreadable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.local, tc.up) {
				t.Fatalf("Err%s is not the upstream sentinel: %v vs %v", tc.name, tc.local, tc.up)
			}
		})
	}
}

// TestDICESentinels_StayDistinct pins the separation a verifier acts on. Two
// sentinels aliased to the same upstream value would make "the nonce was
// absent" indistinguishable from "the nonce was wrong", and a relying party
// cannot tell a device that never answered from evidence replayed out of
// another exchange.
func TestDICESentinels_StayDistinct(t *testing.T) {
	if errors.Is(xkms.ErrDICENonceClaimMissing, xkms.ErrDICENonceMismatch) {
		t.Fatal("a missing nonce and a mismatched nonce must not be the same sentinel")
	}
	if errors.Is(xkms.ErrDICETcbInfoNotFound, xkms.ErrDICEMalformedTcbInfo) {
		t.Fatal("an absent TcbInfo and a malformed one must not be the same sentinel")
	}
	if errors.Is(xkms.ErrDICEEmptyChain, xkms.ErrDICEChainVerificationFailed) {
		t.Fatal("an empty chain and a failed chain must not be the same sentinel")
	}
}

// TestDICEVerify_IsTheRealFunction proves the aliased entry point is go-xkms's
// own and fails closed. A wrapper that returned nil here would report a
// verified device from no evidence at all.
func TestDICEVerify_IsTheRealFunction(t *testing.T) {
	if _, err := xkms.DICEVerify(nil, nil); err == nil {
		t.Fatal("DICEVerify(nil, nil) must fail")
	}
	if _, err := xkms.DICEVerify(nil, &xkms.DICEVerifyOptions{}); err == nil {
		t.Fatal("DICEVerify with an empty chain must fail")
	}
}

// TestDICEReaders_RefuseNil proves the reading helpers are the real ones: a nil
// certificate is a typed refusal, never a zero value that reads as "no DICE
// extensions present".
func TestDICEReaders_RefuseNil(t *testing.T) {
	if _, err := xkms.DICEChallenge(nil); err == nil {
		t.Fatal("DICEChallenge(nil) must fail")
	}
	if _, err := xkms.DICEUccsFromCertificate(nil); err == nil {
		t.Fatal("DICEUccsFromCertificate(nil) must fail")
	}
	if _, err := xkms.DICETcbInfoFromCertificate(nil); err == nil {
		t.Fatal("DICETcbInfoFromCertificate(nil) must fail")
	}
	if _, err := xkms.DICEUeidFromCertificate(nil); err == nil {
		t.Fatal("DICEUeidFromCertificate(nil) must fail")
	}
	if xkms.DICEHasExtensions(nil) {
		t.Fatal("DICEHasExtensions(nil) must be false")
	}
}
