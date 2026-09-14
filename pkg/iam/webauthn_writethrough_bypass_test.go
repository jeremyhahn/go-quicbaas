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

package iam

import (
	"encoding/hex"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// =============================================================================
// A ceremony's proof must not be stamped away by the raw-surface guard.
//
// Two paths write an auth factor. The WebAuthn ceremony writes what it PROVED;
// the raw admin surfaces (gRPC, MCP) write through a proxy that stamps
// attest_level=none, because those callers run no ceremony and may not assert
// an attestation. The guard and the proof are opposite answers to the same
// field, so the two paths must stay separate: if a verified enrollment were
// ever routed through the stamping proxy, an allowlisted hardware key would
// persist as unattested and every later login and token would lose the
// assurance this work exists to establish.
//
// Nothing routes it there today. This pins that, because the failure is silent
// — the enrollment still succeeds, the record simply says the credential
// proved nothing.
// =============================================================================

// TestWriteThrough_CeremonyProofSurvivesTheUnattestedStamp asserts the
// invariant behaviourally: what the ceremony persists, and what the stamp would
// have made of it.
//
// The contrast is the point. Asserting only that a verified enrollment records
// hardware would still pass if the stamp were a no-op; asserting only that the
// stamp writes none would say nothing about the ceremony. Together they show
// the two paths produce DIFFERENT records from the same credential, which is
// what "the ceremony does not go through the stamp" means in observable terms.
func TestWriteThrough_CeremonyProofSurvivesTheUnattestedStamp(t *testing.T) {
	factors := &fakeUserAuthFactorClient{}
	gate := NewWebAuthnAttestationGate(
		WebAuthnAssuranceAny,
		hardwareAllowlist(),
		&stubAdminResolver{admins: map[string]bool{}},
	)
	wt, users, _ := newGatedWriteThrough(t, gate, factors)
	seedUser(t, users, 7, "tenant@example.com", "tenant yubikey")

	cred := attestedCredential(7, []byte{9, 9, 9, 9}, "basic_full", yubiKey5AAGUID)
	if err := wt.Save(verifiedChainContext(yubiKey5AAGUID), cred); err != nil {
		t.Fatalf("a verified manufacturer chain was refused: %v", err)
	}
	if len(factors.added) != 1 {
		t.Fatalf("the ceremony wrote %d factors, want 1", len(factors.added))
	}
	persisted := factors.added[0].Metadata

	if got := persisted[AuthFactorMetadataAttestLevel]; got != string(AttestLevelHardware) {
		t.Fatalf("the ceremony persisted %s = %q, want %q — a verified hardware "+
			"enrollment lost its proof on the way to storage",
			AuthFactorMetadataAttestLevel, got, AttestLevelHardware)
	}

	// What the raw-surface guard would have made of the very same record. If
	// the ceremony were ever routed through it, THIS is what would be stored.
	// The proof travelled as a VerifiedAttestation to the recorder — the one
	// path that can write the reserved keys — not as caller metadata, which
	// the raw surfaces would have refused.
	if attested := factors.attestedRecords(); len(attested) != 1 || attested[0].Level != qrdbsdk.AttestationLevelHardware {
		t.Fatalf("the ceremony recorded %+v, want one hardware attestation", attested)
	}
	if err := ValidateAuthFactorMetadata(persisted); err == nil {
		t.Fatal("the persisted record carries no reserved key, so a raw surface replaying it " +
			"would not be refused; the ceremony's record and a caller's must stay distinguishable")
	}
}

// TestWriteThrough_ModelIsRecordedUnderTheReservedKey closes the other half of
// the reserved-namespace rule.
//
// The LEVEL was already written under a reserved key while the MODEL went to
// the generic "aaguid", which the direct-write guards do not protect. A caller
// could therefore Add or Rotate a factor carrying an "aaguid" of their
// choosing and produce a field a consumer could not tell from one a ceremony
// proved. Attested facts belong in one namespace, under one rule.
func TestWriteThrough_ModelIsRecordedUnderTheReservedKey(t *testing.T) {
	factors := &fakeUserAuthFactorClient{}
	gate := NewWebAuthnAttestationGate(
		WebAuthnAssuranceAny,
		hardwareAllowlist(),
		&stubAdminResolver{admins: map[string]bool{}},
	)
	wt, users, _ := newGatedWriteThrough(t, gate, factors)
	seedUser(t, users, 11, "model@example.com", "model yubikey")

	cred := attestedCredential(11, []byte{1, 2, 3, 4}, "basic_full", yubiKey5AAGUID)
	if err := wt.Save(verifiedChainContext(yubiKey5AAGUID), cred); err != nil {
		t.Fatalf("a verified manufacturer chain was refused: %v", err)
	}
	meta := factors.added[0].Metadata

	want := hex.EncodeToString(yubiKey5AAGUID)
	if got := meta[AuthFactorMetadataAttestAAGUID]; got != want {
		t.Errorf("%s = %q, want %q", AuthFactorMetadataAttestAAGUID, got, want)
	}
	// The generic key must no longer carry the proven model: leaving a copy
	// there would keep the forgeable field alive next to the protected one, and
	// a consumer reading the wrong one would be back where it started.
	if got, ok := meta["aaguid"]; ok {
		t.Errorf("the proven model is still written under the unreserved key \"aaguid\" = %q; "+
			"a direct-write caller can set that field, so a consumer cannot tell it from a proof", got)
	}

	// Both attested facts are refused from a direct-write caller, which is what
	// makes recording them under these names meaningful.
	for _, field := range []string{AuthFactorMetadataAttestLevel, AuthFactorMetadataAttestAAGUID} {
		if err := ValidateAuthFactorMetadata(map[string]string{field: "x"}); err == nil {
			t.Errorf("%s is not refused from a caller-supplied metadata map", field)
		}
	}
}
