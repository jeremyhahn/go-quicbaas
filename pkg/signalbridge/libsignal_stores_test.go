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
	"reflect"
	"sort"
	"testing"

	"github.com/jeremyhahn/go-libsignal/pkg/protocol"
	"github.com/jeremyhahn/go-libsignal/pkg/serialize"
)

// TestSessionStore_LoadCreatesAndContains verifies LoadSession
// auto-creates a record, ContainsSession then reports true, and
// DeleteSession removes it.
func TestSessionStore_LoadCreatesAndContains(t *testing.T) {
	s := newInMemSessionStore(serialize.NewJSONSerializer())
	addr := protocol.NewSignalAddress("alice", 1)
	if ok, _ := s.ContainsSession(addr); ok {
		t.Fatalf("should not contain session before load")
	}
	if _, err := s.LoadSession(addr); err != nil {
		t.Fatalf("load: %v", err)
	}
	if ok, _ := s.ContainsSession(addr); !ok {
		t.Fatalf("should contain session after load")
	}
	if err := s.DeleteSession(addr); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if ok, _ := s.ContainsSession(addr); ok {
		t.Fatalf("session should be gone after delete")
	}
}

// TestSessionStore_GetSubDeviceSessions verifies the parser returns every
// sub-device (deviceID != 1) for the named identity and excludes the primary
// device and other identities. Keys are "name::deviceID"; the parser splits
// on the final "::". (This previously had a bug: it used the unsupported
// fmt scanset verb "%[^::]" which never matched, always returning empty.)
func TestSessionStore_GetSubDeviceSessions(t *testing.T) {
	s := newInMemSessionStore(serialize.NewJSONSerializer())
	for _, dev := range []uint32{1, 2, 3} {
		if _, err := s.LoadSession(protocol.NewSignalAddress("alice", dev)); err != nil {
			t.Fatalf("load %d: %v", dev, err)
		}
	}
	if _, err := s.LoadSession(protocol.NewSignalAddress("bob", 5)); err != nil {
		t.Fatalf("load bob: %v", err)
	}
	ids, err := s.GetSubDeviceSessions("alice")
	if err != nil {
		t.Fatalf("subdevices returned error: %v", err)
	}
	// Sub-devices are everything except the primary (device 1); bob is a
	// different identity and must not appear.
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	want := []uint32{2, 3}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("GetSubDeviceSessions = %v; want %v", ids, want)
	}
}

// TestSessionStore_Keys and DeleteAllSessions clear every record.
func TestSessionStore_DeleteAllSessions(t *testing.T) {
	s := newInMemSessionStore(serialize.NewJSONSerializer())
	_, _ = s.LoadSession(protocol.NewSignalAddress("a", 1))
	_, _ = s.LoadSession(protocol.NewSignalAddress("b", 1))
	if len(s.keys()) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(s.keys()))
	}
	if err := s.DeleteAllSessions(); err != nil {
		t.Fatalf("delete all: %v", err)
	}
	if len(s.keys()) != 0 {
		t.Fatalf("expected 0 keys after delete all, got %d", len(s.keys()))
	}
}

// TestPreKeyStore_ContainsAndRemove covers Contains/Remove on the
// one-time pre-key store.
func TestPreKeyStore_ContainsAndRemove(t *testing.T) {
	s := newInMemPreKeyStore()
	if ok, _ := s.ContainsPreKey(1); ok {
		t.Fatalf("empty store should not contain key 1")
	}
	if err := s.StorePreKey(1, nil); err != nil {
		t.Fatalf("store: %v", err)
	}
	if ok, _ := s.ContainsPreKey(1); !ok {
		t.Fatalf("store should contain key 1")
	}
	if err := s.RemovePreKey(1); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if ok, _ := s.ContainsPreKey(1); ok {
		t.Fatalf("key 1 should be removed")
	}
}

// TestSignedPreKeyStore_LoadContainsRemove covers LoadSignedPreKeys,
// ContainsSignedPreKey, and RemoveSignedPreKey.
func TestSignedPreKeyStore_LoadContainsRemove(t *testing.T) {
	s := newInMemSignedPreKeyStore()
	if got, _ := s.LoadSignedPreKeys(); len(got) != 0 {
		t.Fatalf("empty store should list 0 signed pre-keys")
	}
	if ok, _ := s.ContainsSignedPreKey(2); ok {
		t.Fatalf("should not contain signed pre-key 2")
	}
	if err := s.StoreSignedPreKey(2, nil); err != nil {
		t.Fatalf("store: %v", err)
	}
	if ok, _ := s.ContainsSignedPreKey(2); !ok {
		t.Fatalf("should contain signed pre-key 2")
	}
	if got, _ := s.LoadSignedPreKeys(); len(got) != 1 {
		t.Fatalf("expected 1 signed pre-key, got %d", len(got))
	}
	if err := s.RemoveSignedPreKey(2); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if ok, _ := s.ContainsSignedPreKey(2); ok {
		t.Fatalf("signed pre-key 2 should be removed")
	}
}

// TestKyberPreKeyStore_Contains covers ContainsKyberPreKey and the
// mark-used path.
func TestKyberPreKeyStore_Contains(t *testing.T) {
	s := newInMemKyberPreKeyStore()
	if ok, _ := s.ContainsKyberPreKey(3); ok {
		t.Fatalf("should not contain kyber key 3")
	}
	if err := s.StoreKyberPreKey(3, nil); err != nil {
		t.Fatalf("store: %v", err)
	}
	if ok, _ := s.ContainsKyberPreKey(3); !ok {
		t.Fatalf("should contain kyber key 3")
	}
	if err := s.MarkKyberPreKeyUsed(3, 0, nil); err != nil {
		t.Fatalf("mark used: %v", err)
	}
	if !s.marked[3] {
		t.Fatalf("key 3 should be marked used")
	}
}

// TestNoopKyberSerializer round-trips nils, documenting the MVP no-persist
// behavior.
func TestNoopKyberSerializer(t *testing.T) {
	n := &noopKyberSerializer{}
	b, err := n.Serialize(nil)
	if err != nil || b != nil {
		t.Fatalf("serialize = %v %v, want nil nil", b, err)
	}
	rec, err := n.Deserialize([]byte{1, 2, 3})
	if err != nil || rec != nil {
		t.Fatalf("deserialize = %v %v, want nil nil", rec, err)
	}
}

// TestLibSignalAdapter_Decrypt_NoIdentity rejects decrypt before an
// identity is loaded.
func TestLibSignalAdapter_Decrypt_NoIdentity(t *testing.T) {
	a := NewLibSignalAdapter()
	if _, err := a.Decrypt(context.Background(), SignalAccount{}, InboundEnvelope{}); !IsKind(err, ErrKindInvalidInput) {
		t.Fatalf("want invalid_input, got %v", err)
	}
}

// TestLibSignalAdapter_Decrypt_UnsupportedType rejects an unknown envelope
// type with a protocol error.
func TestLibSignalAdapter_Decrypt_UnsupportedType(t *testing.T) {
	a := NewLibSignalAdapter()
	if _, err := a.GenerateIdentityKeyPair(); err != nil {
		t.Fatalf("gen: %v", err)
	}
	env := InboundEnvelope{
		Type:           999,
		SourceACI:      "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		SourceDeviceID: 1,
		Content:        []byte{1},
	}
	if _, err := a.Decrypt(context.Background(), SignalAccount{ACI: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", DeviceID: 1}, env); !IsKind(err, ErrKindProtocol) {
		t.Fatalf("want protocol, got %v", err)
	}
}

// TestLibSignalAdapter_Decrypt_ResetMarker handles a zero-payload PREKEY
// envelope as a session-reset marker.
func TestLibSignalAdapter_Decrypt_ResetMarker(t *testing.T) {
	a := NewLibSignalAdapter()
	if _, err := a.GenerateIdentityKeyPair(); err != nil {
		t.Fatalf("gen: %v", err)
	}
	env := InboundEnvelope{
		Type:           int32(protocol.PREKEY_TYPE),
		SourceACI:      "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		SourceDeviceID: 1,
		Content:        nil, // empty payload => reset marker
	}
	msg, err := a.Decrypt(context.Background(), SignalAccount{ACI: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", DeviceID: 1}, env)
	if err != nil {
		t.Fatalf("reset marker decrypt: %v", err)
	}
	if !msg.SessionResetFlag {
		t.Fatalf("expected SessionResetFlag set on reset marker")
	}
}
