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

package ca_test

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/ca"
)

// Compile-time proof the profile-selection aliases are the go-xkms types.
var (
	_ xkmssdk.NameConstraints      = ca.NameConstraints{}
	_ ca.NameConstraints           = xkmssdk.NameConstraints{}
	_ xkmssdk.SubordinateCARequest = ca.SubordinateCARequest{}
	_ xkmssdk.AttestationEvidence  = ca.AttestationEvidence{}
	_ ca.AttestationEvidence       = xkmssdk.AttestationEvidence{}
)

// TestProfiles_CarryTheirRegistryNames proves the constructors are go-xkms's
// and name themselves with the constants a caller puts on
// SignOptions.Profile.
func TestProfiles_CarryTheirRegistryNames(t *testing.T) {
	if got := ca.NewIPsecVTEPProfile().Name(); got != ca.ProfileNameIPsecVTEP {
		t.Errorf("NewIPsecVTEPProfile().Name() = %q, want %q", got, ca.ProfileNameIPsecVTEP)
	}
	if got := ca.NewX509SVIDProfile().Name(); got != ca.ProfileNameX509SVID {
		t.Errorf("NewX509SVIDProfile().Name() = %q, want %q", got, ca.ProfileNameX509SVID)
	}
	if ca.ProfileNameIPsecVTEP != "ipsec-vtep" || ca.ProfileNameX509SVID != "x509-svid" {
		t.Errorf("profile names = %q, %q", ca.ProfileNameIPsecVTEP, ca.ProfileNameX509SVID)
	}
}

// TestAttestationEvidenceExtension_RoundTrips encodes evidence through the
// re-export and reads it back off a certificate, as the relying party that
// accepts an ipsec-vtep certificate does.
func TestAttestationEvidenceExtension_RoundTrips(t *testing.T) {
	in := ca.AttestationEvidence{
		VerdictID:          "verdict-1",
		PCRDigestAlgorithm: crypto.SHA256,
		PCRDigest:          bytes.Repeat([]byte{0xAB}, 32),
		AttestedAt:         time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
	}
	ext, err := ca.CreateAttestationEvidenceExtension(in)
	if err != nil {
		t.Fatalf("CreateAttestationEvidenceExtension: %v", err)
	}
	if !ext.Id.Equal(ca.OIDAttestationEvidence) {
		t.Fatalf("extension OID = %v, want %v", ext.Id, ca.OIDAttestationEvidence)
	}
	out, found, err := ca.ParseAttestationEvidenceExtension(&x509.Certificate{Extensions: []pkix.Extension{ext}})
	if err != nil || !found {
		t.Fatalf("ParseAttestationEvidenceExtension = %v, %v", found, err)
	}
	if out.VerdictID != in.VerdictID || !bytes.Equal(out.PCRDigest, in.PCRDigest) || !out.AttestedAt.Equal(in.AttestedAt) {
		t.Fatalf("round trip = %+v, want %+v", out, in)
	}
}

// TestAttestationEvidenceExtension_RefusesMissingVerdict matches the
// re-exported sentinel.
func TestAttestationEvidenceExtension_RefusesMissingVerdict(t *testing.T) {
	_, err := ca.CreateAttestationEvidenceExtension(ca.AttestationEvidence{
		PCRDigestAlgorithm: crypto.SHA256,
		PCRDigest:          make([]byte, 32),
		AttestedAt:         time.Now(),
	})
	if !errors.Is(err, ca.ErrCAInvalidAttestationEvidence) {
		t.Fatalf("CreateAttestationEvidenceExtension(no verdict) = %v, want ErrCAInvalidAttestationEvidence", err)
	}
}
