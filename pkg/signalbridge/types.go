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

import "time"

// ConversationKind discriminates DMs from group chats.
type ConversationKind uint8

const (
	// ConversationDM is a direct message thread between the bot and one
	// operator.
	ConversationDM ConversationKind = 1
	// ConversationGroup is a group chat the bot has been added to.
	ConversationGroup ConversationKind = 2
)

// String returns the canonical lowercase name for the kind.
func (k ConversationKind) String() string {
	switch k {
	case ConversationDM:
		return "dm"
	case ConversationGroup:
		return "group"
	default:
		return "unknown"
	}
}

// SignalAccount identifies a registered Signal account a SignalClient
// operates under. The ACI (account identifier) is the canonical Signal
// UUID; PNI is the phone number identifier. DeviceID distinguishes the
// bot's device from the operator's other linked devices when the bot
// runs as a secondary device. The identity key pair is the long-lived
// Signal Protocol key serialized by go-libsignal.
type SignalAccount struct {
	ACI        string `json:"aci"`
	PNI        string `json:"pni,omitempty"`
	Number     string `json:"number,omitempty"`
	DeviceID   uint32 `json:"device_id"`
	DeviceName string `json:"device_name,omitempty"`
	// RegistrationID is the Signal registration ID (1..16380) generated
	// at install time. Persisted across restarts.
	RegistrationID uint32 `json:"registration_id"`
	// IdentityKeyPairSerialized is the go-libsignal serialized identity
	// key pair. Held here so Login can restore the client without a new
	// verification ceremony.
	IdentityKeyPairSerialized []byte `json:"identity_key_pair,omitempty"`
	// Password is the HTTP Basic credential returned by the server on
	// successful registration.
	Password string `json:"password,omitempty"`
}

// RegisterOptions carries the parameters for Register.
type RegisterOptions struct {
	// Number is the E.164 phone number to register, e.g. "+15551234567".
	Number string
	// DeviceName is the human-visible device name shown in the operator's
	// device list.
	DeviceName string
	// Captcha is an optional proof-of-work token if the server demands
	// one during verification.
	Captcha string
	// VerificationCode is the SMS/voice verification code. When empty the
	// Register call returns ErrKindVerificationRequired after sending the
	// code, and the caller loops after collecting the code from the
	// operator.
	VerificationCode string
	// SessionID is the server-assigned verification session ID. The first
	// Register call with VerificationCode empty returns a fresh session;
	// subsequent calls with a code must echo it back.
	SessionID string
	// Password, when non-empty, is used as the HTTP Basic credential for
	// the new account. Empty means the client generates a random 24-byte
	// password.
	Password string
}

// LoginOptions carries parameters for re-establishing an existing
// account on SignalClient startup.
type LoginOptions struct {
	// Account is the persisted account record loaded by the caller (from
	// the StateStore).
	Account SignalAccount
	// Subscribe controls whether Login opens the realtime WebSocket for
	// inbound message delivery. Set false for out-of-band admin tasks.
	Subscribe bool
}

// OutboundMessage is a message the bot wants to send. For DMs,
// ConversationID is the recipient's ACI UUID. For groups, ConversationID
// is the opaque group ID and the transport expands it to all members.
type OutboundMessage struct {
	ConversationID   string
	Kind             ConversationKind
	Body             string
	Timestamp        int64
	QuoteTimestamp   int64
	QuoteAuthorACI   string
	Attachments      []Attachment
	TypingOnly       bool
	ReadReceipt      bool
	SessionResetFlag bool
}

// Attachment describes a binary payload sent alongside a Signal message.
// Large attachments (> 2MB) uploaded to the attachment CDN and referenced
// by Location; smaller ones can be inlined as Bytes.
type Attachment struct {
	ContentType string
	Filename    string
	Bytes       []byte
	Location    string
	Size        int64
	Digest      []byte
}

// InboundMessage is a decrypted message delivered to the bot.
type InboundMessage struct {
	ConversationID string
	Kind           ConversationKind
	SenderACI      string
	SenderDeviceID uint32
	Body           string
	Timestamp      int64
	ReceivedAt     time.Time
	Attachments    []Attachment
	// QuoteTimestamp and QuoteAuthorACI reference a prior message this
	// one replies to. Used by the event translator to thread tool
	// results under their invocation.
	QuoteTimestamp int64
	QuoteAuthorACI string
	// SessionResetFlag is set when the sender's client signaled a fresh
	// double-ratchet reset (e.g. recovery from corruption).
	SessionResetFlag bool
}

// AuthMode controls how a Signal sender identity maps to a QuicBaaS
// operator identity.
type AuthMode string

const (
	// AuthModeImplicit treats the Signal identity as authoritative and
	// the bot acts under a per-tenant service account. Simple but
	// conflates Signal verification with QuicBaaS authorization.
	AuthModeImplicit AuthMode = "implicit"

	// AuthModeLinked requires an explicit signal↔quicbaas link established
	// via a one-time verification code issued by `quicbaas signalbridge
	// link` and completed through a Signal DM.
	AuthModeLinked AuthMode = "linked"

	// AuthModeGroupOnly makes DMs read-only; only chats pre-registered as
	// admin groups can issue commands. DMs receive help text and nothing
	// else.
	AuthModeGroupOnly AuthMode = "group-only"
)

// OperatorIdentity is the resolved QuicBaaS caller for an inbound Signal
// message. The bot threads this through orchestrator API calls as
// tenant + user metadata.
type OperatorIdentity struct {
	TenantID    string
	SubjectID   string
	DisplayName string
	// SignalID is the originating Signal ACI; kept for audit logging.
	SignalID string
	// Source records how the identity was resolved ("implicit", "link",
	// "group-admin", "service-account").
	Source string
}

// VerbosityPrefs are per-conversation presentation knobs. The event
// translator consults these to decide whether to stream chunks, include
// tool-call detail, and so on.
type VerbosityPrefs struct {
	// StreamTokens enables chunk-level rolling updates. When false, the
	// translator waits for stop_reason before emitting one final message.
	StreamTokens bool
	// ToolDetails toggles the 🔧 tool-invocation + → result messages.
	ToolDetails bool
	// FileOps toggles the 📝 file-op messages.
	FileOps bool
	// TypingIndicators toggles "bot is typing" presence while streaming.
	TypingIndicators bool
	// ReadReceipts toggles delivery acknowledgments for inbound messages.
	ReadReceipts bool
}

// DefaultVerbosityPrefs returns the out-of-the-box preferences used
// when a conversation has no overrides persisted.
func DefaultVerbosityPrefs() VerbosityPrefs {
	return VerbosityPrefs{
		StreamTokens:     true,
		ToolDetails:      true,
		FileOps:          true,
		TypingIndicators: true,
		ReadReceipts:     true,
	}
}

// ConversationState is the per-conversation record persisted by the
// SessionManager.
type ConversationState struct {
	ConversationID string           `json:"conversation_id"`
	Kind           ConversationKind `json:"kind"`
	AttachedSessionID string        `json:"attached_session_id,omitempty"`
	OperatorSubject   string        `json:"operator_subject,omitempty"`
	OperatorTenant    string        `json:"operator_tenant,omitempty"`
	Verbosity         VerbosityPrefs `json:"verbosity"`
	// LastDeliveredSeq is the highest session-event sequence number that
	// has been successfully forwarded to this conversation. Used for
	// exactly-once delivery on restart.
	LastDeliveredSeq uint64 `json:"last_delivered_seq"`
	// RollingMsgTimestamp is the Signal timestamp of the in-progress
	// assistant message being coalesced via Edit. Zero means no rolling
	// message active.
	RollingMsgTimestamp int64 `json:"rolling_msg_timestamp,omitempty"`
	// InvocationTimestamps maps an orchestrator invocation_id to the
	// Signal message timestamp used to send the 🔧 line, enabling
	// reply-threading of the → result.
	InvocationTimestamps map[string]int64 `json:"invocation_timestamps,omitempty"`
	UpdatedAt            time.Time        `json:"updated_at"`
}
