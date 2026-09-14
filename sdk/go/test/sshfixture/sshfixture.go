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

// Package sshfixture derives deterministic, test-only SSH key material and
// the OpenSSH fingerprints that belong to it.
//
// Every SSH surface in QuicBaaS — pkg/service, the five transports and the
// CLI — carries host-key and authorized-key fingerprints. A test that
// asserts on a hand-written placeholder such as "SHA256:abc" asserts
// nothing: no production path recomputes that string, so a regression in
// fingerprint derivation or comparison would pass unnoticed. This package
// gives those tests a fingerprint that genuinely is
// gossh.FingerprintSHA256 of a real ed25519 public key.
//
// # Key material is derived, never stored
//
// The ed25519 seed is SHA-256 over a domain-separated label, so the
// repository holds no bytes that could be mistaken for a credential, and
// every caller naming the same label gets the same key. Nothing here is
// suitable for any purpose other than tests.
//
// # Golden fingerprints
//
// The exported HostFingerprint, HostAltFingerprint and UserFingerprint
// constants are the fingerprints of Host, HostAlt and User. Tests that need
// only the string — proto conversions, argument transforms, CLI output —
// use the constant; tests that need the key itself call the constructor.
// The package's own test asserts constant and constructor agree, which is
// what makes all thirteen call sites load-bearing on the production
// fingerprint function.
package sshfixture

import (
	"crypto/ed25519"
	"crypto/sha256"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

// seedDomain separates this package's derived seeds from every other use of
// a label. It is versioned so a deliberate change to the derivation is a
// visible edit rather than a silent drift of the golden constants.
const seedDomain = "go-quicbaas/sdk/go/test/sshfixture v1: "

// Labels naming the keys this package pre-derives.
const (
	LabelHost    = "known-host"
	LabelHostAlt = "known-host-alt"
	LabelUser    = "user-key"
)

// Golden OpenSSH SHA256 fingerprints of the pre-derived keys.
//
// These are checked in so a test needing only the string does not have to
// build a key, and so a change in fingerprint derivation fails loudly in
// TestGoldenFingerprintsMatchDerivedKeys rather than silently everywhere.
const (
	HostFingerprint    = "SHA256:TwvItxs1DBvmhfm788l+reOU8q4lB1YpYeBK4dZ2eTY"
	HostAltFingerprint = "SHA256:HLmc7D/vupo8vH4NtcrnuHPRbkXJCZY5vwoHlTZDx2M"
	UserFingerprint    = "SHA256:YW0wHzB9uY7X9dqxe0avehmQy09iLVNWp8hGgxadTOw"
)

// Key bundles a deterministic test key with the artifacts SSH tests assert
// on: the public key, its OpenSSH SHA256 fingerprint and an
// authorized_keys line carrying the label as its comment.
type Key struct {
	// Label is the name the key was derived from.
	Label string

	// Public is the ed25519 public key in go-crypto/ssh form.
	Public gossh.PublicKey

	// Signer signs with the matching private key.
	Signer gossh.Signer

	// Fingerprint is gossh.FingerprintSHA256(Public) — the same function
	// every production verify path uses.
	Fingerprint string

	// AuthorizedLine is the single-line authorized_keys form of Public.
	AuthorizedLine string
}

// Seed returns the ed25519 seed derived from label. Exported so a test can
// show the derivation is a pure function of the label.
func Seed(label string) [ed25519.SeedSize]byte {
	return sha256.Sum256([]byte(seedDomain + label))
}

// New derives the Key named by label. Derivation is deterministic and
// cannot fail for a well-formed ed25519 key; tb reports the impossible so
// callers never have to.
func New(tb testing.TB, label string) Key {
	tb.Helper()

	seed := Seed(label)
	priv := ed25519.NewKeyFromSeed(seed[:])

	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		tb.Fatalf("sshfixture: signer for %q: %v", label, err)
	}
	pub := signer.PublicKey()

	return Key{
		Label:          label,
		Public:         pub,
		Signer:         signer,
		Fingerprint:    gossh.FingerprintSHA256(pub),
		AuthorizedLine: strings.TrimSpace(string(gossh.MarshalAuthorizedKey(pub))) + " " + label,
	}
}

// Host is the key a known_hosts pin points at.
func Host(tb testing.TB) Key { tb.Helper(); return New(tb, LabelHost) }

// HostAlt is a second host key, distinct from Host, for mismatch paths.
func HostAlt(tb testing.TB) Key { tb.Helper(); return New(tb, LabelHostAlt) }

// User is the key an authorized_keys entry grants.
func User(tb testing.TB) Key { tb.Helper(); return New(tb, LabelUser) }
