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

package sshfixture

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

// =============================================================================
// Known vector — the OpenSSH SHA256: form, rebuilt from the wire spec
// =============================================================================

// sshString encodes a value in the RFC 4251 §5 "string" form: a uint32
// length in network byte order followed by the raw bytes.
func sshString(b []byte) []byte {
	out := make([]byte, 4, 4+len(b))
	binary.BigEndian.PutUint32(out, uint32(len(b)))
	return append(out, b...)
}

// specFingerprint recomputes the OpenSSH "SHA256:" fingerprint of an
// ed25519 public key straight from the specifications, sharing no code with
// golang.org/x/crypto/ssh:
//
//   - RFC 8709 §4 fixes the ssh-ed25519 public key blob as the string
//     "ssh-ed25519" followed by the 32-byte key.
//   - OpenSSH's SHA256 fingerprint (the successor to the RFC 4716 MD5 form)
//     is "SHA256:" + unpadded standard base64 of SHA-256 over that blob.
//
// If gossh.FingerprintSHA256 ever stopped agreeing with this, every
// migrated SSH test in the repository would be asserting on the wrong
// string — which is exactly what this vector exists to catch.
func specFingerprint(pub ed25519.PublicKey) string {
	blob := append(sshString([]byte("ssh-ed25519")), sshString(pub)...)
	sum := sha256.Sum256(blob)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// TestFingerprintMatchesSSHSpec pins the production fingerprint function to
// an independently derived vector for each fixture key.
func TestFingerprintMatchesSSHSpec(t *testing.T) {
	for _, label := range []string{LabelHost, LabelHostAlt, LabelUser} {
		t.Run(label, func(t *testing.T) {
			seed := Seed(label)
			pub := ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey)

			want := specFingerprint(pub)
			if got := New(t, label).Fingerprint; got != want {
				t.Fatalf("fingerprint = %q, spec-derived vector = %q", got, want)
			}
		})
	}
}

// TestSpecFingerprintRejectsAMutatedKey proves the vector is sensitive to
// the key material: flipping a single bit of the public key must change the
// fingerprint. A comparison that ignored the key would survive this.
func TestSpecFingerprintRejectsAMutatedKey(t *testing.T) {
	seed := Seed(LabelHost)
	pub := ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey)

	mutated := make(ed25519.PublicKey, len(pub))
	copy(mutated, pub)
	mutated[0] ^= 0x01

	original := specFingerprint(pub)
	if specFingerprint(mutated) == original {
		t.Fatal("flipping a bit of the public key left the fingerprint unchanged")
	}

	sshMutated, err := gossh.NewPublicKey(mutated)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}
	if gossh.FingerprintSHA256(sshMutated) == original {
		t.Fatal("production fingerprint ignored a mutated public key")
	}
}

// =============================================================================
// Goldens
// =============================================================================

// TestGoldenFingerprintsMatchDerivedKeys is the link that makes every call
// site using a golden constant load-bearing: the constant is only correct
// while it equals the fingerprint the production function computes for the
// derived key.
func TestGoldenFingerprintsMatchDerivedKeys(t *testing.T) {
	cases := []struct {
		name   string
		key    Key
		golden string
	}{
		{"host", Host(t), HostFingerprint},
		{"host-alt", HostAlt(t), HostAltFingerprint},
		{"user", User(t), UserFingerprint},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.key.Fingerprint != tc.golden {
				t.Fatalf("derived fingerprint %q != golden %q", tc.key.Fingerprint, tc.golden)
			}
			if !strings.HasPrefix(tc.golden, "SHA256:") {
				t.Fatalf("golden %q lacks the OpenSSH SHA256: prefix", tc.golden)
			}
			// The base64 body must decode to a full SHA-256 digest;
			// go-xkms rejects anything shorter at the API boundary.
			body := strings.TrimPrefix(tc.golden, "SHA256:")
			digest, err := base64.RawStdEncoding.DecodeString(body)
			if err != nil {
				t.Fatalf("golden body is not raw std base64: %v", err)
			}
			if len(digest) != sha256.Size {
				t.Fatalf("golden decodes to %d bytes, want %d", len(digest), sha256.Size)
			}
		})
	}
}

// TestGoldenFingerprintsAreDistinct guards against a copy-paste that would
// silently make a mismatch test assert a match.
func TestGoldenFingerprintsAreDistinct(t *testing.T) {
	seen := map[string]string{}
	for name, fp := range map[string]string{
		"host":     HostFingerprint,
		"host-alt": HostAltFingerprint,
		"user":     UserFingerprint,
	} {
		if other, dup := seen[fp]; dup {
			t.Fatalf("%s and %s share fingerprint %q", name, other, fp)
		}
		seen[fp] = name
	}
}

// =============================================================================
// Derivation
// =============================================================================

// TestSeedIsDeterministicAndLabelScoped shows the derivation is a pure
// function of the label, so no key bytes need to be checked in.
func TestSeedIsDeterministicAndLabelScoped(t *testing.T) {
	first, second := Seed(LabelHost), Seed(LabelHost)
	if first != second {
		t.Fatal("Seed is not deterministic for a fixed label")
	}
	if Seed(LabelHost) == Seed(LabelHostAlt) {
		t.Fatal("different labels produced the same seed")
	}
	if len(first) != ed25519.SeedSize {
		t.Fatalf("seed is %d bytes, want %d", len(first), ed25519.SeedSize)
	}
}

// TestNewIsStableAcrossCalls guarantees two packages naming the same label
// agree on the key, which is what lets one fixture serve every transport.
func TestNewIsStableAcrossCalls(t *testing.T) {
	first, second := New(t, LabelUser), New(t, LabelUser)

	if first.Fingerprint != second.Fingerprint {
		t.Fatalf("fingerprint drifted: %q vs %q", first.Fingerprint, second.Fingerprint)
	}
	if first.AuthorizedLine != second.AuthorizedLine {
		t.Fatalf("authorized line drifted: %q vs %q", first.AuthorizedLine, second.AuthorizedLine)
	}
	if first.Public.Type() != gossh.KeyAlgoED25519 {
		t.Fatalf("key type = %q, want %q", first.Public.Type(), gossh.KeyAlgoED25519)
	}
}

// TestNewWithAnUnknownLabelStillYieldsAUsableKey covers the constructor's
// open-ended input: any label is valid and yields a distinct, usable key.
func TestNewWithAnUnknownLabelStillYieldsAUsableKey(t *testing.T) {
	key := New(t, "some-label-no-constant-names")

	if key.Fingerprint == HostFingerprint {
		t.Fatal("an unrelated label collided with the host fixture")
	}
	if key.Signer == nil {
		t.Fatal("Signer is nil")
	}
	if !strings.HasPrefix(key.AuthorizedLine, "ssh-ed25519 ") {
		t.Fatalf("authorized line %q is not an ssh-ed25519 entry", key.AuthorizedLine)
	}
}

// =============================================================================
// Artifacts round-trip through the production parsers
// =============================================================================

// TestAuthorizedLineParsesToTheSameKey is what the REST and SDK verify
// paths do: parse a stored authorized_keys line and fingerprint it. The
// line and the fingerprint must describe one key.
func TestAuthorizedLineParsesToTheSameKey(t *testing.T) {
	key := User(t)

	parsed, comment, _, rest, err := gossh.ParseAuthorizedKey([]byte(key.AuthorizedLine))
	if err != nil {
		t.Fatalf("ParseAuthorizedKey: %v", err)
	}
	if len(rest) != 0 {
		t.Fatalf("trailing bytes after the entry: %q", rest)
	}
	if comment != LabelUser {
		t.Fatalf("comment = %q, want %q", comment, LabelUser)
	}
	if got := gossh.FingerprintSHA256(parsed); got != key.Fingerprint {
		t.Fatalf("parsed line fingerprints to %q, want %q", got, key.Fingerprint)
	}
}

// TestSignerRoundTripsThroughItsPublicKey checks the private half matches
// the public half the fingerprint was taken over, so tests that sign with
// the fixture verify against the fingerprint they pinned.
func TestSignerRoundTripsThroughItsPublicKey(t *testing.T) {
	key := Host(t)

	sig, err := key.Signer.Sign(nil, []byte("quicbaas ssh fixture"))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := key.Public.Verify([]byte("quicbaas ssh fixture"), sig); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if err := key.Public.Verify([]byte("a different message"), sig); err == nil {
		t.Fatal("signature verified against the wrong message")
	}
}
