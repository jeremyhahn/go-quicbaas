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

// ServerTransport is the abstraction boundary between SignalClient and
// the go-signal REST + WebSocket server. Everything that goes over the
// wire flows through this interface.
//
// This is intentionally minimal and synchronous; robustness concerns
// (rate limiting, backoff, reconnect) are layered on top by SignalClient.
// The interface is implemented against go-signal's REST API in
// http_transport.go; tests substitute a FakeServerTransport that records
// calls and enqueues synthetic events.
type ServerTransport interface {
	// CreateVerificationSession starts a new phone-number verification
	// session on the server. Returns the server-assigned session ID.
	CreateVerificationSession(ctx context.Context, number string) (string, error)

	// RequestVerificationCode asks the server to deliver a code over the
	// given transport ("sms" or "voice"). Returns when the delivery
	// request has been accepted (not when the code has been received).
	RequestVerificationCode(ctx context.Context, sessionID, transport, captcha string) error

	// SubmitVerificationCode sends the code the operator received back to
	// the server to unlock the session for registration.
	SubmitVerificationCode(ctx context.Context, sessionID, code string) error

	// CompleteRegistration finalizes account creation using a verified
	// session. The server returns the ACI/PNI assigned to the account.
	CompleteRegistration(ctx context.Context, req RegistrationRequest) (RegistrationResponse, error)

	// UploadPreKeys publishes the bot's one-time pre-keys, signed
	// pre-key, and optional Kyber pre-keys to the server so other
	// clients can initiate sessions.
	UploadPreKeys(ctx context.Context, account SignalAccount, bundle PreKeyBundleUpload) error

	// PreKeyCount returns the server's current view of remaining
	// one-time pre-keys. The maintenance loop triggers rotation when
	// this drops below threshold.
	PreKeyCount(ctx context.Context, account SignalAccount) (int, error)

	// FetchPreKeyBundle returns a recipient's bundle so a new session
	// can be established.
	FetchPreKeyBundle(ctx context.Context, account SignalAccount, recipientACI string, deviceID uint32) (PreKeyBundleFetch, error)

	// SendEnvelope dispatches a single pre-encrypted envelope to a
	// destination device.
	SendEnvelope(ctx context.Context, account SignalAccount, env OutboundEnvelope) (SendResult, error)

	// OpenInbound establishes the realtime WebSocket connection that
	// delivers envelopes pushed by the server. The returned channel
	// closes when Close is called or the connection drops; the caller
	// is expected to reconnect with backoff.
	OpenInbound(ctx context.Context, account SignalAccount) (<-chan InboundEnvelope, func() error, error)

	// AckInbound acknowledges delivery of an inbound envelope so the
	// server removes it from the queue.
	AckInbound(ctx context.Context, account SignalAccount, envelopeID uint64) error
}

// RegistrationRequest carries the fields the server needs to create a
// new account after successful verification.
type RegistrationRequest struct {
	SessionID      string
	Number         string
	Password       string
	RegistrationID uint32
	PNIRegID       uint32
	DeviceName     string
	// IdentityPublic is the serialized identity public key advertised to
	// the server.
	IdentityPublic []byte
}

// RegistrationResponse is the server's reply on successful registration.
type RegistrationResponse struct {
	ACI      string
	PNI      string
	DeviceID uint32
}

// PreKeyBundleUpload bundles the key material being advertised to the
// server for asynchronous session initiation by remote clients.
type PreKeyBundleUpload struct {
	IdentityKey       []byte
	SignedPreKeyID    uint32
	SignedPreKeyPub   []byte
	SignedPreKeySig   []byte
	OneTimePreKeys    []PreKeyMaterial
	KyberPreKeys      []KyberPreKeyMaterial
	LastResortKyberID uint32
}

// PreKeyMaterial is a single one-time pre-key upload record.
type PreKeyMaterial struct {
	ID     uint32
	Public []byte
}

// KyberPreKeyMaterial is a Kyber/PQXDH pre-key upload record.
type KyberPreKeyMaterial struct {
	ID        uint32
	Public    []byte
	Signature []byte
}

// PreKeyBundleFetch is the server's response to a bundle request for a
// remote recipient.
type PreKeyBundleFetch struct {
	RecipientACI    string
	DeviceID        uint32
	RegistrationID  uint32
	IdentityKey     []byte
	SignedPreKeyID  uint32
	SignedPreKeyPub []byte
	SignedPreKeySig []byte
	PreKeyID        uint32
	PreKeyPub       []byte
	KyberPreKeyID   uint32
	KyberPreKeyPub  []byte
	KyberPreKeySig  []byte
}

// OutboundEnvelope is a pre-encrypted Signal Protocol message ready to
// be pushed to the server.
type OutboundEnvelope struct {
	DestinationACI    string
	DestinationDevice uint32
	EnvelopeType      int32
	Content           []byte
	Timestamp         int64
	Urgent            bool
	Story             bool
}

// InboundEnvelope is the raw encrypted envelope the server pushed over
// the realtime channel. SignalClient decrypts it via go-libsignal and
// yields an InboundMessage.
type InboundEnvelope struct {
	ID                uint64
	Type              int32
	SourceACI         string
	SourceDeviceID    uint32
	DestinationACI    string
	DestinationDevice uint32
	Timestamp         int64
	ServerTimestamp   int64
	Content           []byte
	ServerGUID        string
	Urgent            bool
	Story             bool
}

// SendResult carries any per-recipient diagnostics the server returns
// (e.g. "message too large", "unknown recipient", "stale devices").
// Most of the time this is empty and success is indicated by nil error
// from SendEnvelope.
type SendResult struct {
	NeedsSync    bool
	StaleDevices []uint32
}
