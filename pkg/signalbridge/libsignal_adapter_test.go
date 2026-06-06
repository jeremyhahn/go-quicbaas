// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package signalbridge

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-libsignal/pkg/keys/identity"
	"github.com/jeremyhahn/go-libsignal/pkg/protocol"
	"github.com/jeremyhahn/go-libsignal/pkg/state/store"
)

// TestLibSignalAdapter_GenerateIdentityKeyPair produces a 64-byte
// serialized keypair and registers it in the identity store.
func TestLibSignalAdapter_GenerateIdentityKeyPair(t *testing.T) {
	a := NewLibSignalAdapter()
	mat, err := a.GenerateIdentityKeyPair()
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	if len(mat.Serialized) != 64 {
		t.Fatalf("serialized len: %d", len(mat.Serialized))
	}
	if len(mat.PublicSerialized) != 33 { // DjbType prefix + 32 bytes
		t.Fatalf("public len: %d", len(mat.PublicSerialized))
	}
}

// TestLibSignalAdapter_LoadIdentityKeyPair_BadInput rejects sizes
// other than 64 bytes.
func TestLibSignalAdapter_LoadIdentityKeyPair_BadInput(t *testing.T) {
	a := NewLibSignalAdapter()
	if err := a.LoadIdentityKeyPair(make([]byte, 10), 1); err == nil {
		t.Fatalf("expected error")
	}
}

// TestLibSignalAdapter_LoadIdentityKeyPair_RoundTrip verifies a
// generated keypair can be serialized out and loaded back.
func TestLibSignalAdapter_LoadIdentityKeyPair_RoundTrip(t *testing.T) {
	a := NewLibSignalAdapter()
	mat, err := a.GenerateIdentityKeyPair()
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	b := NewLibSignalAdapter()
	if err := b.LoadIdentityKeyPair(mat.Serialized, 12345); err != nil {
		t.Fatalf("load: %v", err)
	}
}

// TestLibSignalAdapter_GeneratePreKeyBundle produces upload material.
func TestLibSignalAdapter_GeneratePreKeyBundle(t *testing.T) {
	a := NewLibSignalAdapter()
	if _, err := a.GenerateIdentityKeyPair(); err != nil {
		t.Fatalf("gen: %v", err)
	}
	bundle, err := a.GeneratePreKeyBundle(SignalAccount{ACI: "x"}, 5, 2)
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}
	if len(bundle.OneTimePreKeys) != 5 {
		t.Fatalf("onetime: %d", len(bundle.OneTimePreKeys))
	}
	if len(bundle.KyberPreKeys) != 2 {
		t.Fatalf("kyber: %d", len(bundle.KyberPreKeys))
	}
	if len(bundle.SignedPreKeySig) != 64 {
		t.Fatalf("signed sig: %d", len(bundle.SignedPreKeySig))
	}
}

// TestLibSignalAdapter_GeneratePreKeyBundle_NoIdentity rejects when
// no identity keypair has been loaded.
func TestLibSignalAdapter_GeneratePreKeyBundle_NoIdentity(t *testing.T) {
	a := NewLibSignalAdapter()
	if _, err := a.GeneratePreKeyBundle(SignalAccount{}, 1, 1); err == nil {
		t.Fatalf("expected error")
	}
}

// TestLibSignalAdapter_SessionReset clears state without error.
func TestLibSignalAdapter_SessionReset(t *testing.T) {
	a := NewLibSignalAdapter()
	if err := a.ResetSession(SignalAccount{}, "peer", 1); err != nil {
		t.Fatalf("reset: %v", err)
	}
}

// TestLibSignalAdapter_Encrypt_NoIdentity rejects when identity not
// yet loaded.
func TestLibSignalAdapter_Encrypt_NoIdentity(t *testing.T) {
	a := NewLibSignalAdapter()
	_, err := a.Encrypt(context.Background(), SignalAccount{}, OutboundMessage{ConversationID: "peer"}, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

// TestLibSignalAdapter_Encrypt_SessionReset emits a plaintext
// session-reset marker envelope without touching crypto.
func TestLibSignalAdapter_Encrypt_SessionReset(t *testing.T) {
	a := NewLibSignalAdapter()
	if _, err := a.GenerateIdentityKeyPair(); err != nil {
		t.Fatalf("gen: %v", err)
	}
	envs, err := a.Encrypt(context.Background(), SignalAccount{}, OutboundMessage{
		ConversationID:   "peer",
		SessionResetFlag: true,
	}, nil)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if len(envs) != 1 || envs[0].DestinationACI != "peer" {
		t.Fatalf("unexpected: %+v", envs)
	}
}

// TestNewLibSignalAdapter_Serializer exposes the JSON serializer.
func TestNewLibSignalAdapter_Serializer(t *testing.T) {
	a := NewLibSignalAdapter()
	if a.Serializer() == nil {
		t.Fatal("nil serializer")
	}
}

// TestLibSignalAdapter_EncryptDecrypt_RoundTrip exercises a full
// Signal Protocol ratchet between two adapters: Alice fetches Bob's
// bundle, encrypts a message, Bob decrypts and recovers the
// plaintext. This covers the mainline crypto path end-to-end.
func TestLibSignalAdapter_EncryptDecrypt_RoundTrip(t *testing.T) {
	alice := NewLibSignalAdapter()
	bob := NewLibSignalAdapter()

	// ACI values are canonical Signal ServiceId UUIDs. libsignal rejects
	// address names that are not valid ServiceId strings when it serializes
	// the session, so the round-trip must use well-formed UUIDs.
	const (
		aliceACI = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		bobACI   = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	)

	// Both parties have identities.
	aliceMat, err := alice.GenerateIdentityKeyPair()
	if err != nil {
		t.Fatalf("alice id: %v", err)
	}
	_ = aliceMat
	if _, err := bob.GenerateIdentityKeyPair(); err != nil {
		t.Fatalf("bob id: %v", err)
	}
	bobBundle, err := bob.GeneratePreKeyBundle(SignalAccount{ACI: bobACI}, 5, 2)
	if err != nil {
		t.Fatalf("bob bundle: %v", err)
	}

	// Alice encrypts to Bob. fetchBundle returns Bob's bundle.
	fetch := func(aci string, dev uint32) (PreKeyBundleFetch, error) {
		return PreKeyBundleFetch{
			RecipientACI:    bobACI,
			DeviceID:        1,
			RegistrationID:  77,
			IdentityKey:     bob.identityKeyPair.PublicKey().Serialize()[1:],
			SignedPreKeyID:  bobBundle.SignedPreKeyID,
			SignedPreKeyPub: bobBundle.SignedPreKeyPub[1:],
			SignedPreKeySig: bobBundle.SignedPreKeySig,
			PreKeyID:        bobBundle.OneTimePreKeys[0].ID,
			PreKeyPub:       bobBundle.OneTimePreKeys[0].Public[1:],
			KyberPreKeyID:   bobBundle.KyberPreKeys[0].ID,
			KyberPreKeyPub:  bobBundle.KyberPreKeys[0].Public,
			KyberPreKeySig:  bobBundle.KyberPreKeys[0].Signature,
		}, nil
	}
	envs, err := alice.Encrypt(context.Background(), SignalAccount{ACI: aliceACI, DeviceID: 1}, OutboundMessage{
		ConversationID: bobACI,
		Body:           "hello bob",
		Timestamp:      12345,
	}, fetch)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if len(envs) != 1 {
		t.Fatalf("want 1 envelope got %d", len(envs))
	}

	// Bob decrypts what Alice sent.
	inbound := InboundEnvelope{
		ID:             1,
		Type:           envs[0].EnvelopeType,
		SourceACI:      aliceACI,
		SourceDeviceID: 1,
		Content:        envs[0].Content,
		Timestamp:      envs[0].Timestamp,
	}
	msg, err := bob.Decrypt(context.Background(), SignalAccount{ACI: bobACI, DeviceID: 1}, inbound)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if msg.Body != "hello bob" {
		t.Fatalf("body: %q", msg.Body)
	}
}

// newTestIdentityKey produces a deterministic *identity.Key from a
// single seed byte. Different seeds produce different fingerprints
// because identity.Key.Fingerprint hex-encodes the serialized public
// key bytes.
func newTestIdentityKey(seed byte) *identity.Key {
	var pub [32]byte
	for i := range pub {
		pub[i] = seed + byte(i)
	}
	k := identity.NewKeyFromBytes(pub, 0)
	return &k
}

// TestSaveIdentity_NewIdentity_ReturnsNew asserts that the first save
// for a protocol address returns IdentityChangeNewOrUnchanged (the
// "new" half of the upstream NewOrUnchanged enum value).
func TestSaveIdentity_NewIdentity_ReturnsNew(t *testing.T) {
	s := newInMemIdentityStore(nil, 1)
	addr := protocol.NewSignalAddress("alice", 1)

	got, err := s.SaveIdentity(addr, newTestIdentityKey(0x10))
	if err != nil {
		t.Fatalf("SaveIdentity: %v", err)
	}

	if got != store.IdentityChangeNewOrUnchanged {
		t.Fatalf("first save: got %d, want IdentityChangeNewOrUnchanged (%d)", got, store.IdentityChangeNewOrUnchanged)
	}
}

// TestSaveIdentity_SameIdentity_ReturnsNone asserts that re-saving an
// identity with the same fingerprint returns
// IdentityChangeNewOrUnchanged (the "unchanged" half of the enum
// value). A different *identity.Key pointer with identical key bytes
// must compare equal so this exercises the fingerprint path, not
// pointer identity.
func TestSaveIdentity_SameIdentity_ReturnsNone(t *testing.T) {
	s := newInMemIdentityStore(nil, 1)
	addr := protocol.NewSignalAddress("alice", 1)

	if got, err := s.SaveIdentity(addr, newTestIdentityKey(0x10)); err != nil || got != store.IdentityChangeNewOrUnchanged {
		t.Fatalf("first save: got %d err %v, want IdentityChangeNewOrUnchanged", got, err)
	}

	got, err := s.SaveIdentity(addr, newTestIdentityKey(0x10))
	if err != nil {
		t.Fatalf("SaveIdentity: %v", err)
	}
	if got != store.IdentityChangeNewOrUnchanged {
		t.Fatalf("resave same key: got %d, want IdentityChangeNewOrUnchanged", got)
	}
}

// TestSaveIdentity_RotatedKey_ReturnsReplacedExisting asserts that
// saving a different identity key for an address that already has a
// trusted key returns IdentityChangeReplacedExisting and that the new
// key replaces the prior one in the store.
func TestSaveIdentity_RotatedKey_ReturnsReplacedExisting(t *testing.T) {
	s := newInMemIdentityStore(nil, 1)
	addr := protocol.NewSignalAddress("alice", 1)

	original := newTestIdentityKey(0x10)
	rotated := newTestIdentityKey(0x20)

	if got, err := s.SaveIdentity(addr, original); err != nil || got != store.IdentityChangeNewOrUnchanged {
		t.Fatalf("first save: got %d err %v, want IdentityChangeNewOrUnchanged", got, err)
	}

	got, err := s.SaveIdentity(addr, rotated)
	if err != nil {
		t.Fatalf("SaveIdentity: %v", err)
	}
	if got != store.IdentityChangeReplacedExisting {
		t.Fatalf("rotated save: got %d, want IdentityChangeReplacedExisting (%d)", got, store.IdentityChangeReplacedExisting)
	}

	stored, err := s.GetIdentity(addr)
	if err != nil {
		t.Fatalf("GetIdentity: %v", err)
	}
	if stored.Fingerprint() != rotated.Fingerprint() {
		t.Fatalf("stored fingerprint = %s, want %s (rotated key)", stored.Fingerprint(), rotated.Fingerprint())
	}
}
