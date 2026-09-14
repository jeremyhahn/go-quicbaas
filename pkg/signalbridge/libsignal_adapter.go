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
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jeremyhahn/go-libsignal/pkg/ecc"
	groupRecord "github.com/jeremyhahn/go-libsignal/pkg/groups/state/record"
	"github.com/jeremyhahn/go-libsignal/pkg/kem"
	"github.com/jeremyhahn/go-libsignal/pkg/keys/identity"
	"github.com/jeremyhahn/go-libsignal/pkg/keys/prekey"
	"github.com/jeremyhahn/go-libsignal/pkg/protocol"
	"github.com/jeremyhahn/go-libsignal/pkg/serialize"
	"github.com/jeremyhahn/go-libsignal/pkg/session"
	"github.com/jeremyhahn/go-libsignal/pkg/state/record"
	"github.com/jeremyhahn/go-libsignal/pkg/state/store"
	"github.com/jeremyhahn/go-libsignal/pkg/util/keyhelper"
	"github.com/jeremyhahn/go-libsignal/pkg/util/optional"
)

// LibSignalAdapter is the production ProtocolAdapter. It wraps
// go-libsignal primitives (keyhelper.GenerateIdentityKeyPair,
// keyhelper.GeneratePreKeys, keyhelper.GenerateSignedPreKey,
// keyhelper.GenerateKyberPreKey, session.Builder, session.Cipher) and
// maintains the six Signal Protocol stores in memory. Persistence
// belongs in a separate layer (StateStore) because the go-libsignal
// types are not themselves JSON-friendly.
//
// The adapter is thread-safe: all store access is serialized under a
// single mutex because session.Cipher is explicitly documented as
// NOT safe for concurrent use by multiple goroutines.
type LibSignalAdapter struct {
	serializer *serialize.Serializer

	mu sync.Mutex

	identityKeyPair *identity.KeyPair
	registrationID  uint32

	sessionStore      *inMemSessionStore
	preKeyStore       *inMemPreKeyStore
	signedPreKeyStore *inMemSignedPreKeyStore
	identityStore     *inMemIdentityStore
	kyberPreKeyStore  *inMemKyberPreKeyStore

	nextPreKeyID       uint32
	nextSignedPreKeyID uint32
	nextKyberPreKeyID  uint32
}

// NewLibSignalAdapter constructs an adapter with a fresh JSON
// serializer. Registration and pre-key generation proceed through
// GenerateIdentityKeyPair and GeneratePreKeyBundle.
func NewLibSignalAdapter() *LibSignalAdapter {
	ser := serialize.NewJSONSerializer()
	return &LibSignalAdapter{
		serializer:         ser,
		sessionStore:       newInMemSessionStore(ser),
		preKeyStore:        newInMemPreKeyStore(),
		signedPreKeyStore:  newInMemSignedPreKeyStore(),
		kyberPreKeyStore:   newInMemKyberPreKeyStore(),
		nextPreKeyID:       1,
		nextSignedPreKeyID: 1,
		nextKyberPreKeyID:  1,
	}
}

// Serializer exposes the adapter's serializer. Useful to tests that
// need to round-trip Signal Protocol messages.
func (a *LibSignalAdapter) Serializer() *serialize.Serializer {
	return a.serializer
}

// GenerateIdentityKeyPair implements ProtocolAdapter.
func (a *LibSignalAdapter) GenerateIdentityKeyPair() (IdentityKeyPairMaterial, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	ikp, err := keyhelper.GenerateIdentityKeyPair()
	if err != nil {
		return IdentityKeyPairMaterial{}, wrapError("signalbridge.GenerateIdentityKeyPair", ErrKindProtocol, err, "keyhelper")
	}
	a.identityKeyPair = ikp
	a.registrationID = keyhelper.GenerateRegistrationID()
	a.identityStore = newInMemIdentityStore(ikp, a.registrationID)

	return IdentityKeyPairMaterial{
		Serialized:       a.serializeIdentityKeyPair(ikp),
		PublicSerialized: ikp.PublicKey().Serialize(),
	}, nil
}

// LoadIdentityKeyPair implements ProtocolAdapter.
func (a *LibSignalAdapter) LoadIdentityKeyPair(serialized []byte, registrationID uint32) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(serialized) != 64 {
		return newError("signalbridge.LoadIdentityKeyPair", ErrKindInvalidInput, "serialized length must be 64 bytes (pub 32 || priv 32)")
	}
	var pub [32]byte
	var priv [32]byte
	copy(pub[:], serialized[:32])
	copy(priv[:], serialized[32:])

	pubKey := identity.NewKey(ecc.NewDjbECPublicKey(pub))
	privKey := ecc.NewDjbECPrivateKey(priv)
	ikp := identity.NewKeyPair(pubKey, privKey)
	a.identityKeyPair = ikp
	a.registrationID = registrationID
	a.identityStore = newInMemIdentityStore(ikp, registrationID)
	return nil
}

// serializeIdentityKeyPair concatenates public || private (32 || 32).
// go-libsignal doesn't expose a built-in serializer for KeyPair so we
// provide one that mirrors the wire format used by the upstream Java /
// Rust implementations.
func (a *LibSignalAdapter) serializeIdentityKeyPair(ikp *identity.KeyPair) []byte {
	// PublicKey().Serialize() returns 33 bytes (type-prefix + 32). We
	// strip the type byte before packing so the 64-byte wire format is
	// [pub32 || priv32], matching the upstream IdentityKeyPair layout.
	buf := make([]byte, 64)
	pub := ikp.PublicKey().Serialize()
	copy(buf[:32], pub[1:])
	priv := ikp.PrivateKey().Serialize()
	copy(buf[32:], priv[:])
	return buf
}

// GeneratePreKeyBundle implements ProtocolAdapter.
func (a *LibSignalAdapter) GeneratePreKeyBundle(account SignalAccount, numOneTime int, numKyber int) (PreKeyBundleUpload, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.identityKeyPair == nil {
		return PreKeyBundleUpload{}, newError("signalbridge.GeneratePreKeyBundle", ErrKindInvalidInput, "identity keypair not loaded")
	}
	if numOneTime <= 0 {
		numOneTime = 100
	}
	if numKyber <= 0 {
		numKyber = 25
	}

	preKeys, err := keyhelper.GeneratePreKeys(int(a.nextPreKeyID), numOneTime, a.serializer.PreKeyRecord)
	if err != nil {
		return PreKeyBundleUpload{}, wrapError("signalbridge.GeneratePreKeyBundle", ErrKindProtocol, err, "generate one-time prekeys")
	}
	a.nextPreKeyID += uint32(numOneTime)

	oneTimeMaterial := make([]PreKeyMaterial, 0, len(preKeys))
	for _, pk := range preKeys {
		stored := record.NewPreKey(pk.ID().Value, pk.KeyPair(), a.serializer.PreKeyRecord)
		if err := a.preKeyStore.StorePreKey(pk.ID().Value, stored); err != nil {
			return PreKeyBundleUpload{}, wrapError("signalbridge.GeneratePreKeyBundle", ErrKindProtocol, err, "store one-time prekey")
		}
		oneTimeMaterial = append(oneTimeMaterial, PreKeyMaterial{
			ID:     pk.ID().Value,
			Public: pk.KeyPair().PublicKey().Serialize(),
		})
	}

	spk, err := keyhelper.GenerateSignedPreKey(a.identityKeyPair, a.nextSignedPreKeyID, a.serializer.SignedPreKeyRecord)
	if err != nil {
		return PreKeyBundleUpload{}, wrapError("signalbridge.GeneratePreKeyBundle", ErrKindProtocol, err, "generate signed prekey")
	}
	if err := a.signedPreKeyStore.StoreSignedPreKey(
		spk.ID(),
		record.NewSignedPreKey(
			spk.ID(),
			spk.Timestamp(),
			spk.KeyPair(),
			spk.Signature(),
			a.serializer.SignedPreKeyRecord,
		),
	); err != nil {
		return PreKeyBundleUpload{}, wrapError("signalbridge.GeneratePreKeyBundle", ErrKindProtocol, err, "store signed prekey")
	}
	a.nextSignedPreKeyID++

	kyberMaterial := make([]KyberPreKeyMaterial, 0, numKyber)
	lastResortKyberID := uint32(0)
	for i := 0; i < numKyber; i++ {
		kpk, kerr := keyhelper.GenerateKyberPreKey(a.identityKeyPair.PrivateKey(), a.nextKyberPreKeyID, &noopKyberSerializer{})
		if kerr != nil {
			return PreKeyBundleUpload{}, wrapError("signalbridge.GeneratePreKeyBundle", ErrKindProtocol, kerr, "generate kyber prekey")
		}
		if kerr := a.kyberPreKeyStore.StoreKyberPreKey(a.nextKyberPreKeyID, kpk); kerr != nil {
			return PreKeyBundleUpload{}, wrapError("signalbridge.GeneratePreKeyBundle", ErrKindProtocol, kerr, "store kyber prekey")
		}
		sig := kpk.Signature()
		kyberMaterial = append(kyberMaterial, KyberPreKeyMaterial{
			ID: a.nextKyberPreKeyID,
			// go-libsignal v0.2.0 removed the raw KeyPair accessor: the secret
			// half is guarded now and only GuardedKeyPair hands it out.
			// SerializedPublicKey is the intended replacement and is literally
			// keyPair.Public.Serialize() upstream, so this is the same bytes by
			// the supported route rather than a workaround.
			Public:    kpk.SerializedPublicKey(),
			Signature: sig,
		})
		lastResortKyberID = a.nextKyberPreKeyID
		a.nextKyberPreKeyID++
	}

	// We need the signed prekey signature as bytes.
	spkSig := spk.Signature()
	return PreKeyBundleUpload{
		IdentityKey:       a.identityKeyPair.PublicKey().Serialize(),
		SignedPreKeyID:    spk.ID(),
		SignedPreKeyPub:   spk.KeyPair().PublicKey().Serialize(),
		SignedPreKeySig:   spkSig[:],
		OneTimePreKeys:    oneTimeMaterial,
		KyberPreKeys:      kyberMaterial,
		LastResortKyberID: lastResortKyberID,
	}, nil
}

// Encrypt implements ProtocolAdapter. For brevity this implementation
// supports only DM messages (ConversationDM). Group messaging would
// layer a sender-key distribution step on top and is intentionally
// out of scope for the initial bridge iteration — groups are handled
// by expanding to N DM sends at the bot layer via the
// EventTranslator's group resolver callback.
func (a *LibSignalAdapter) Encrypt(ctx context.Context, account SignalAccount, msg OutboundMessage, fetchBundle FetchBundleFunc) ([]OutboundEnvelope, error) {
	const op = "signalbridge.LibSignalAdapter.Encrypt"
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.identityKeyPair == nil {
		return nil, newError(op, ErrKindInvalidInput, "identity keypair not loaded")
	}
	// Session-reset envelopes are plaintext 'null' messages with the
	// reset flag set; remote peers drop their session on receipt.
	if msg.SessionResetFlag {
		return []OutboundEnvelope{{
			DestinationACI:    msg.ConversationID,
			DestinationDevice: 1,
			EnvelopeType:      int32(protocol.PREKEY_TYPE),
			Content:           nil,
			Timestamp:         msg.Timestamp,
			Urgent:            true,
		}}, nil
	}

	deviceID := uint32(1)
	addr := protocol.NewSignalAddress(msg.ConversationID, deviceID)

	// Build session if we don't have one.
	hasSession, err := a.sessionStore.ContainsSession(addr)
	if err != nil {
		return nil, newError(op, ErrKindProtocol, err.Error())
	}
	if !hasSession {
		if fetchBundle == nil {
			return nil, newError(op, ErrKindProtocol, "no session and no bundle fetcher")
		}
		bundle, err := fetchBundle(msg.ConversationID, deviceID)
		if err != nil {
			return nil, wrapError(op, ErrKindTransport, err, "fetch bundle")
		}
		if err := a.buildOutgoingSession(addr, bundle); err != nil {
			return nil, err
		}
	}

	builder := session.NewBuilderWithKyber(
		a.sessionStore,
		a.preKeyStore,
		a.signedPreKeyStore,
		a.identityStore,
		a.kyberPreKeyStore,
		addr,
		a.serializer,
	)
	localAddr := protocol.NewSignalAddress(account.ACI, account.DeviceID)
	cipher := session.NewCipherWithLocalAddress(builder, localAddr, addr)
	ciphertext, err := cipher.Encrypt([]byte(msg.Body))
	if err != nil {
		return nil, wrapError(op, ErrKindProtocol, err, "encrypt")
	}
	serialized, err := ciphertext.Serialize()
	if err != nil {
		return nil, wrapError(op, ErrKindProtocol, err, "serialize ciphertext")
	}
	envType := int32(protocol.WHISPER_TYPE)
	if ciphertext.Type() == protocol.PREKEY_TYPE {
		envType = int32(protocol.PREKEY_TYPE)
	}
	return []OutboundEnvelope{{
		DestinationACI:    msg.ConversationID,
		DestinationDevice: deviceID,
		EnvelopeType:      envType,
		Content:           serialized,
		Timestamp:         msg.Timestamp,
		Urgent:            true,
	}}, nil
}

// buildOutgoingSession processes a fetched bundle to seed a new session.
func (a *LibSignalAdapter) buildOutgoingSession(addr *protocol.SignalAddress, bundle PreKeyBundleFetch) error {
	const op = "signalbridge.LibSignalAdapter.buildOutgoingSession"
	builder := session.NewBuilderWithKyber(
		a.sessionStore,
		a.preKeyStore,
		a.signedPreKeyStore,
		a.identityStore,
		a.kyberPreKeyStore,
		addr,
		a.serializer,
	)

	if len(bundle.IdentityKey) != 32 {
		return newError(op, ErrKindProtocol, "bundle identity key must be 32 bytes")
	}
	var idkBytes [32]byte
	copy(idkBytes[:], bundle.IdentityKey)
	theirIdentity := identity.NewKeyFromBytes(idkBytes, 0)

	if len(bundle.SignedPreKeyPub) != 32 {
		return newError(op, ErrKindProtocol, "signed prekey public must be 32 bytes")
	}
	var spkBytes [32]byte
	copy(spkBytes[:], bundle.SignedPreKeyPub)
	theirSigned := ecc.NewDjbECPublicKey(spkBytes)

	var spkSig [64]byte
	if len(bundle.SignedPreKeySig) != 64 {
		return newError(op, ErrKindProtocol, "signed prekey signature must be 64 bytes")
	}
	copy(spkSig[:], bundle.SignedPreKeySig)

	var preKeyID *optional.Uint32
	var theirOneTime ecc.ECPublicKeyable
	if len(bundle.PreKeyPub) == 32 {
		var otBytes [32]byte
		copy(otBytes[:], bundle.PreKeyPub)
		theirOneTime = ecc.NewDjbECPublicKey(otBytes)
		preKeyID = &optional.Uint32{Value: bundle.PreKeyID}
	}

	if len(bundle.KyberPreKeyPub) == 0 {
		return newError(op, ErrKindProtocol, "bundle missing kyber prekey (PQXDH required)")
	}
	// Kyber public key deserialization uses the kem package.
	kyberPub, err := kem.DeserializePublicKey(bundle.KyberPreKeyPub)
	if err != nil {
		return wrapError(op, ErrKindProtocol, err, "deserialize kyber public")
	}
	if len(bundle.KyberPreKeySig) != 64 {
		return newError(op, ErrKindProtocol, "kyber signature must be 64 bytes")
	}

	pkBundle := prekey.NewBundleWithKyber(
		bundle.RegistrationID,
		bundle.DeviceID,
		preKeyID,
		bundle.SignedPreKeyID,
		theirOneTime,
		theirSigned,
		spkSig,
		identity.NewKey(theirIdentity.PublicKey()),
		&optional.Uint32{Value: bundle.KyberPreKeyID},
		kyberPub,
		bundle.KyberPreKeySig,
	)
	if err := builder.ProcessBundle(pkBundle); err != nil {
		return wrapError(op, ErrKindProtocol, err, "process bundle")
	}
	return nil
}

// Decrypt implements ProtocolAdapter.
func (a *LibSignalAdapter) Decrypt(ctx context.Context, account SignalAccount, env InboundEnvelope) (InboundMessage, error) {
	const op = "signalbridge.LibSignalAdapter.Decrypt"
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.identityKeyPair == nil {
		return InboundMessage{}, newError(op, ErrKindInvalidInput, "identity keypair not loaded")
	}

	addr := protocol.NewSignalAddress(env.SourceACI, env.SourceDeviceID)
	builder := session.NewBuilderWithKyber(
		a.sessionStore,
		a.preKeyStore,
		a.signedPreKeyStore,
		a.identityStore,
		a.kyberPreKeyStore,
		addr,
		a.serializer,
	)
	localAddr := protocol.NewSignalAddress(account.ACI, account.DeviceID)
	cipher := session.NewCipherWithLocalAddress(builder, localAddr, addr)

	var plaintext []byte
	switch env.Type {
	case int32(protocol.PREKEY_TYPE):
		if len(env.Content) == 0 {
			// Session-reset marker with no payload. Clear local state.
			if err := a.sessionStore.DeleteSession(addr); err != nil {
				return InboundMessage{}, wrapError(op, ErrKindProtocol, err, "delete session")
			}
			return InboundMessage{
				ConversationID:   env.SourceACI,
				Kind:             ConversationDM,
				SenderACI:        env.SourceACI,
				SenderDeviceID:   env.SourceDeviceID,
				Timestamp:        env.Timestamp,
				ReceivedAt:       time.Unix(0, env.ServerTimestamp*int64(time.Millisecond)),
				SessionResetFlag: true,
			}, nil
		}
		msg, err := protocol.NewPreKeySignalMessageFromBytes(
			env.Content,
			a.serializer.PreKeySignalMessage,
			a.serializer.SignalMessage,
		)
		if err != nil {
			return InboundMessage{}, wrapError(op, ErrKindProtocol, err, "decode prekey signal message")
		}
		pt, _, err := cipher.DecryptPreKeyMessage(builder, msg)
		if err != nil {
			return InboundMessage{}, wrapError(op, ErrKindProtocol, err, "decrypt prekey message")
		}
		plaintext = pt
	case int32(protocol.WHISPER_TYPE):
		msg, err := protocol.NewSignalMessageFromBytes(env.Content, a.serializer.SignalMessage)
		if err != nil {
			return InboundMessage{}, wrapError(op, ErrKindProtocol, err, "decode signal message")
		}
		pt, err := cipher.Decrypt(msg)
		if err != nil {
			return InboundMessage{}, wrapError(op, ErrKindProtocol, err, "decrypt message")
		}
		plaintext = pt
	default:
		return InboundMessage{}, newError(op, ErrKindProtocol, fmt.Sprintf("unsupported envelope type %d", env.Type))
	}

	return InboundMessage{
		ConversationID: env.SourceACI,
		Kind:           ConversationDM,
		SenderACI:      env.SourceACI,
		SenderDeviceID: env.SourceDeviceID,
		Body:           string(plaintext),
		Timestamp:      env.Timestamp,
		ReceivedAt:     time.Now().UTC(),
	}, nil
}

// ResetSession implements ProtocolAdapter.
func (a *LibSignalAdapter) ResetSession(account SignalAccount, recipientACI string, deviceID uint32) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	addr := protocol.NewSignalAddress(recipientACI, deviceID)
	return a.sessionStore.DeleteSession(addr)
}

// =============================================================================
// In-memory store implementations backing the adapter.
// =============================================================================

// inMemSessionStore implements store.Session.
type inMemSessionStore struct {
	sessions   map[string]*record.Session
	serializer *serialize.Serializer
}

func newInMemSessionStore(ser *serialize.Serializer) *inMemSessionStore {
	return &inMemSessionStore{
		sessions:   make(map[string]*record.Session),
		serializer: ser,
	}
}

func addressKey(a *protocol.SignalAddress) string {
	return fmt.Sprintf("%s::%d", a.Name(), a.DeviceID())
}

func (s *inMemSessionStore) LoadSession(a *protocol.SignalAddress) (*record.Session, error) {
	key := addressKey(a)
	if r, ok := s.sessions[key]; ok {
		return r, nil
	}
	r := record.NewSession(s.serializer.Session, s.serializer.State)
	s.sessions[key] = r
	return r, nil
}

func (s *inMemSessionStore) GetSubDeviceSessions(name string) ([]uint32, error) {
	var ids []uint32
	for _, k := range s.keys() {
		// Keys are formatted by addressKey as "name::deviceID". Split on the
		// final "::" so device names that themselves contain "::" still parse.
		idx := strings.LastIndex(k, "::")
		if idx < 0 {
			continue
		}
		n := k[:idx]
		d, err := strconv.ParseUint(k[idx+len("::"):], 10, 32)
		if err != nil {
			continue
		}
		if n == name && uint32(d) != 1 {
			ids = append(ids, uint32(d))
		}
	}
	return ids, nil
}

func (s *inMemSessionStore) keys() []string {
	out := make([]string, 0, len(s.sessions))
	for k := range s.sessions {
		out = append(out, k)
	}
	return out
}

func (s *inMemSessionStore) StoreSession(a *protocol.SignalAddress, r *record.Session) error {
	s.sessions[addressKey(a)] = r
	return nil
}

func (s *inMemSessionStore) ContainsSession(a *protocol.SignalAddress) (bool, error) {
	_, ok := s.sessions[addressKey(a)]
	return ok, nil
}

func (s *inMemSessionStore) DeleteSession(a *protocol.SignalAddress) error {
	delete(s.sessions, addressKey(a))
	return nil
}

func (s *inMemSessionStore) DeleteAllSessions() error {
	s.sessions = make(map[string]*record.Session)
	return nil
}

// inMemPreKeyStore implements store.PreKey.
type inMemPreKeyStore struct {
	keys map[uint32]*record.PreKey
}

func newInMemPreKeyStore() *inMemPreKeyStore {
	return &inMemPreKeyStore{keys: make(map[uint32]*record.PreKey)}
}

func (s *inMemPreKeyStore) LoadPreKey(id uint32) (*record.PreKey, error) {
	return s.keys[id], nil
}

func (s *inMemPreKeyStore) StorePreKey(id uint32, r *record.PreKey) error {
	s.keys[id] = r
	return nil
}

func (s *inMemPreKeyStore) ContainsPreKey(id uint32) (bool, error) {
	_, ok := s.keys[id]
	return ok, nil
}

func (s *inMemPreKeyStore) RemovePreKey(id uint32) error {
	delete(s.keys, id)
	return nil
}

// inMemSignedPreKeyStore implements store.SignedPreKey.
type inMemSignedPreKeyStore struct {
	keys map[uint32]*record.SignedPreKey
}

func newInMemSignedPreKeyStore() *inMemSignedPreKeyStore {
	return &inMemSignedPreKeyStore{keys: make(map[uint32]*record.SignedPreKey)}
}

func (s *inMemSignedPreKeyStore) LoadSignedPreKey(id uint32) (*record.SignedPreKey, error) {
	return s.keys[id], nil
}
func (s *inMemSignedPreKeyStore) LoadSignedPreKeys() ([]*record.SignedPreKey, error) {
	out := make([]*record.SignedPreKey, 0, len(s.keys))
	for _, v := range s.keys {
		out = append(out, v)
	}
	return out, nil
}
func (s *inMemSignedPreKeyStore) StoreSignedPreKey(id uint32, r *record.SignedPreKey) error {
	s.keys[id] = r
	return nil
}
func (s *inMemSignedPreKeyStore) ContainsSignedPreKey(id uint32) (bool, error) {
	_, ok := s.keys[id]
	return ok, nil
}
func (s *inMemSignedPreKeyStore) RemoveSignedPreKey(id uint32) error {
	delete(s.keys, id)
	return nil
}

// inMemIdentityStore implements store.IdentityKey.
type inMemIdentityStore struct {
	identity       *identity.KeyPair
	registrationID uint32
	trusted        map[string]*identity.Key
}

func newInMemIdentityStore(ikp *identity.KeyPair, regID uint32) *inMemIdentityStore {
	return &inMemIdentityStore{
		identity:       ikp,
		registrationID: regID,
		trusted:        make(map[string]*identity.Key),
	}
}

func (s *inMemIdentityStore) GetIdentityKeyPair() *identity.KeyPair { return s.identity }
func (s *inMemIdentityStore) GetLocalRegistrationId() uint32        { return s.registrationID }

// SaveIdentity records a remote client's identity key for the given
// protocol address. The returned IdentityChange mirrors the upstream
// libsignal contract:
//
//   - IdentityChangeNewOrUnchanged when no prior key existed for the
//     address, or the saved key matches the one being recorded
//     (compared by fingerprint, the same equality test used by
//     IsTrustedIdentity).
//   - IdentityChangeReplacedExisting when a different key was already
//     trusted for the address. Callers may treat this as a security
//     event (key rotation / safety-number change).
func (s *inMemIdentityStore) SaveIdentity(a *protocol.SignalAddress, k *identity.Key) (store.IdentityChange, error) {
	key := addressKey(a)
	prev, existed := s.trusted[key]
	s.trusted[key] = k
	if !existed {
		return store.IdentityChangeNewOrUnchanged, nil
	}
	if prev.Fingerprint() == k.Fingerprint() {
		return store.IdentityChangeNewOrUnchanged, nil
	}
	return store.IdentityChangeReplacedExisting, nil
}
func (s *inMemIdentityStore) IsTrustedIdentity(a *protocol.SignalAddress, k *identity.Key, _ store.Direction) (bool, error) {
	saved := s.trusted[addressKey(a)]
	return saved == nil || saved.Fingerprint() == k.Fingerprint(), nil
}
func (s *inMemIdentityStore) GetIdentity(a *protocol.SignalAddress) (*identity.Key, error) {
	return s.trusted[addressKey(a)], nil
}

// inMemKyberPreKeyStore implements store.KyberPreKey.
type inMemKyberPreKeyStore struct {
	keys   map[uint32]*record.KyberPreKey
	marked map[uint32]bool
}

func newInMemKyberPreKeyStore() *inMemKyberPreKeyStore {
	return &inMemKyberPreKeyStore{
		keys:   make(map[uint32]*record.KyberPreKey),
		marked: make(map[uint32]bool),
	}
}

func (s *inMemKyberPreKeyStore) LoadKyberPreKey(id uint32) (*record.KyberPreKey, error) {
	return s.keys[id], nil
}
func (s *inMemKyberPreKeyStore) StoreKyberPreKey(id uint32, r *record.KyberPreKey) error {
	s.keys[id] = r
	return nil
}
func (s *inMemKyberPreKeyStore) MarkKyberPreKeyUsed(id uint32, _ uint32, _ ecc.ECPublicKeyable) error {
	s.marked[id] = true
	return nil
}
func (s *inMemKyberPreKeyStore) ContainsKyberPreKey(id uint32) (bool, error) {
	_, ok := s.keys[id]
	return ok, nil
}

// Verify interface compliance at compile time.
var (
	_ store.Session      = (*inMemSessionStore)(nil)
	_ store.PreKey       = (*inMemPreKeyStore)(nil)
	_ store.SignedPreKey = (*inMemSignedPreKeyStore)(nil)
	_ store.IdentityKey  = (*inMemIdentityStore)(nil)
	_ store.KyberPreKey  = (*inMemKyberPreKeyStore)(nil)
)

// noopKyberSerializer is a minimal serializer for the KyberPreKey
// store — we do not persist Kyber keys across process restarts in this
// MVP; identity keys + pre-key rotation on startup keep the bot
// operational.
type noopKyberSerializer struct{}

func (n *noopKyberSerializer) Serialize(_ *record.KyberPreKeyStructure) ([]byte, error) {
	return nil, nil
}
func (n *noopKyberSerializer) Deserialize(_ []byte) (*record.KyberPreKeyStructure, error) {
	return nil, nil
}

// Reference to groupRecord to keep the import alive for future group
// message expansion work.
var _ = (*groupRecord.SenderKey)(nil)
