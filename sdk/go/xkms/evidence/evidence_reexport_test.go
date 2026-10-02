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

package evidence_test

import (
	"errors"
	"testing"

	xkmsevidence "github.com/jeremyhahn/go-xkms/sdk/go/evidence"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/evidence"
)

// Compile-time proof the aliases are the go-xkms types, so a verifier built on
// this package hands values to go-xkms without conversion.
var (
	_ xkmsevidence.QuoteInput           = evidence.QuoteInput{}
	_ evidence.QuoteInput               = xkmsevidence.QuoteInput{}
	_ xkmsevidence.AKCertificateOptions = evidence.AKCertificateOptions{}
	_ xkmsevidence.EKCertificateOptions = evidence.EKCertificateOptions{}
	_ xkmsevidence.CoRIMVerifyOptions   = evidence.CoRIMVerifyOptions{}
	_ xkmsevidence.ReferenceManifest    = evidence.ReferenceManifest{}
	_ xkmsevidence.ReferenceValue       = evidence.ReferenceValue{}
	_ *xkmsevidence.Quote               = (*evidence.Quote)(nil)
	_ *xkmsevidence.Error               = (*evidence.Error)(nil)
	_ *xkmsevidence.EventLog            = (*evidence.EventLog)(nil)
)

// TestParseQuote_ClassifiesGarbageAsForged drives the aliased primitive: bytes
// that are present but are no TPMS_ATTEST are a forgery, not an absence, and
// the classification survives the alias through errors.Is.
func TestParseQuote_ClassifiesGarbageAsForged(t *testing.T) {
	_, err := evidence.ParseQuote([]byte("not a tpms_attest"))
	if !errors.Is(err, evidence.ErrForged) {
		t.Fatalf("ParseQuote(garbage) = %v, want ErrForged", err)
	}
	if _, err := evidence.ParseQuote(nil); !errors.Is(err, evidence.ErrAbsent) {
		t.Fatalf("ParseQuote(nil) = %v, want ErrAbsent", err)
	}
}

// TestVerifySignedCoRIM_RequiresExactlyOneTrustSource: with neither a pinned
// key nor roots there is nothing to trust a signer by, so verification refuses
// before decoding anything (no trust on first use).
func TestVerifySignedCoRIM_RequiresExactlyOneTrustSource(t *testing.T) {
	if _, err := evidence.VerifySignedCoRIM([]byte{0xd2}, evidence.CoRIMVerifyOptions{}); !errors.Is(err, evidence.ErrCoRIMTrustSource) {
		t.Fatalf("VerifySignedCoRIM(no trust source) = %v, want ErrCoRIMTrustSource", err)
	}
}

// TestComparePCRs_DistinguishesDivergenceFromAbsence pins the two outcomes a
// relying party acts on differently: a differing value is forged, a missing
// reference is absent.
func TestComparePCRs_DistinguishesDivergenceFromAbsence(t *testing.T) {
	proven := map[int][]byte{7: make([]byte, 32)}
	diverged := map[int][]byte{7: append(make([]byte, 31), 0x01)}
	if err := evidence.ComparePCRs(proven, diverged); !errors.Is(err, evidence.ErrForged) {
		t.Fatalf("ComparePCRs(diverged) = %v, want ErrForged", err)
	}
	if err := evidence.ComparePCRs(proven, nil); !errors.Is(err, evidence.ErrAbsent) {
		t.Fatalf("ComparePCRs(no reference) = %v, want ErrAbsent", err)
	}
	if err := evidence.ComparePCRs(proven, map[int][]byte{7: make([]byte, 32)}); err != nil {
		t.Fatalf("ComparePCRs(equal) = %v, want nil", err)
	}
}
