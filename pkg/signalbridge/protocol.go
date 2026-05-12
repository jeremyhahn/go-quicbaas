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

import "context"

// ProtocolAdapter is the narrow interface SignalClient uses to invoke
// Signal Protocol primitives. The production implementation lives in
// libsignal_adapter.go and wraps go-libsignal types (IdentityKeyPair,
// session.Builder, session.Cipher, PreKeyBundle). Tests substitute a
// FakeProtocolAdapter that records calls and returns deterministic
// envelopes so the bot pipeline can be exercised without real crypto.
//
// Keeping this interface narrow insulates the rest of the package from
// changes in go-libsignal and keeps the test surface manageable. The
// one-function-per-primitive shape matches the guidance in CLAUDE.md to
// keep interfaces small.
type ProtocolAdapter interface {
	// GenerateIdentityKeyPair produces a fresh long-lived Signal identity
	// key pair. Returned in both serialized form (for persistence) and
	// public-only form (for the registration API upload).
	GenerateIdentityKeyPair() (IdentityKeyPairMaterial, error)

	// LoadIdentityKeyPair restores a previously generated identity
	// keypair into the adapter's in-memory key stores. Called by Login.
	LoadIdentityKeyPair(serialized []byte, registrationID uint32) error

	// GeneratePreKeyBundle creates numOneTime + numKyber new pre-keys
	// and returns them along with the signed pre-key and Kyber
	// signatures, all ready for upload to the server.
	GeneratePreKeyBundle(account SignalAccount, numOneTime int, numKyber int) (PreKeyBundleUpload, error)

	// Encrypt produces one or more transport envelopes for the given
	// outbound message. fetchBundle is called when there is no existing
	// session for a recipient device and a fresh one must be built.
	// Group messages expand to one envelope per member device inside
	// the adapter.
	Encrypt(ctx context.Context, account SignalAccount, msg OutboundMessage, fetchBundle FetchBundleFunc) ([]OutboundEnvelope, error)

	// Decrypt unpacks an inbound envelope into a plaintext
	// InboundMessage, driving the double ratchet. On decrypt failure
	// the adapter returns an error without mutating session state —
	// the caller then invokes ResetSession to recover.
	Decrypt(ctx context.Context, account SignalAccount, env InboundEnvelope) (InboundMessage, error)

	// ResetSession clears all local state for the remote
	// (recipientACI, deviceID) so the next inbound PreKeySignalMessage
	// initializes a fresh ratchet.
	ResetSession(account SignalAccount, recipientACI string, deviceID uint32) error
}

// FetchBundleFunc is the callback the adapter uses to retrieve a
// remote's pre-key bundle on demand (only when a new session must be
// bootstrapped). Provided by SignalClient so the adapter does not need
// to know anything about the underlying transport.
type FetchBundleFunc func(recipientACI string, deviceID uint32) (PreKeyBundleFetch, error)

// IdentityKeyPairMaterial is the result of GenerateIdentityKeyPair.
type IdentityKeyPairMaterial struct {
	// Serialized is the go-libsignal serialized representation of the
	// full keypair. Persisted so Login can reconstitute the keys.
	Serialized []byte
	// PublicSerialized is the serialized public key only, used for
	// registration upload.
	PublicSerialized []byte
}
