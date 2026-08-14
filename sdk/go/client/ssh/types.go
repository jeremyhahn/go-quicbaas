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

// Package ssh is the QuicBaaS SDK client for SSH key, known-hosts, and
// authorized-keys management.
//
// COMPOSITION, NOT REIMPLEMENTATION: every DTO and sub-client contract in
// this package is a type alias for the canonical go-xkms SSH SDK contract
// (go-xkms/sdk/go/client/ssh). QuicBaaS adds exactly one thing — a
// transport-backed implementation that speaks the QuicBaaS multi-protocol
// wire (embedded, REST, gRPC, QUIC, MCP, Unix) instead of talking to an
// xkmsd directly. Consumers therefore never import go-xkms.
//
// The single canonical entry point is quicbaas.SDK().Security().SSH().
package ssh

import (
	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"
)

// =============================================================================
// Contract aliases — the xkms SSH SDK is the canonical definition
// =============================================================================

type (
	// Client is the top-level SSH surface: Keys / KnownHosts /
	// AuthorizedKeys / Session / Console / Close.
	Client = xkmsssh.Client

	// KeysClient manages SSH key material.
	KeysClient = xkmsssh.KeysClient

	// KnownHostsClient manages pinned host-key fingerprints.
	KnownHostsClient = xkmsssh.KnownHostsClient

	// AuthorizedKeysClient manages authorized_keys records.
	AuthorizedKeysClient = xkmsssh.AuthorizedKeysClient

	// SessionClient opens outbound SSH sessions.
	SessionClient = xkmsssh.SessionClient

	// Session is an established outbound SSH connection.
	Session = xkmsssh.Session

	// ConsoleSession is an interactive PTY console.
	ConsoleSession = xkmsssh.ConsoleSession

	// ConsoleConfig configures an interactive console.
	ConsoleConfig = xkmsssh.ConsoleConfig

	// ConsoleConnectRequest identifies the key and target of a console.
	ConsoleConnectRequest = xkmsssh.ConsoleConnectRequest
)

type (
	// Key is the transport-safe representation of an SSH key (public
	// material and metadata only).
	Key = xkmsssh.Key

	// PublicKey is the authorized_keys-format export of a public key.
	PublicKey = xkmsssh.PublicKey

	// Pin is a pinned host-key fingerprint.
	Pin = xkmsssh.Pin

	// AuthorizedEntry is a stored authorized_keys entry.
	AuthorizedEntry = xkmsssh.AuthorizedEntry

	// GenerateRequest carries the inputs for Keys().Generate.
	GenerateRequest = xkmsssh.GenerateRequest

	// ImportRequest carries the inputs for Keys().Import.
	ImportRequest = xkmsssh.ImportRequest

	// GetKeyRequest identifies a single key.
	GetKeyRequest = xkmsssh.GetKeyRequest

	// ListKeysRequest enumerates keys by (scope, owner).
	ListKeysRequest = xkmsssh.ListKeysRequest

	// DeleteKeyRequest removes a single key.
	DeleteKeyRequest = xkmsssh.DeleteKeyRequest

	// ExportRequest identifies the key whose public half to export.
	ExportRequest = xkmsssh.ExportRequest

	// AddKnownHostRequest persists a host-key pin.
	AddKnownHostRequest = xkmsssh.AddKnownHostRequest

	// ListKnownHostsRequest enumerates host-key pins.
	ListKnownHostsRequest = xkmsssh.ListKnownHostsRequest

	// RemoveKnownHostRequest removes a host-key pin.
	RemoveKnownHostRequest = xkmsssh.RemoveKnownHostRequest

	// AddAuthorizedKeyRequest persists an authorized_keys entry.
	AddAuthorizedKeyRequest = xkmsssh.AddAuthorizedKeyRequest

	// ListAuthorizedKeysRequest enumerates authorized_keys entries.
	ListAuthorizedKeysRequest = xkmsssh.ListAuthorizedKeysRequest

	// EmitRequest formats a public key into an authorized_keys line.
	EmitRequest = xkmsssh.EmitRequest

	// SessionDialRequest requests an outbound SSH session.
	SessionDialRequest = xkmsssh.SessionDialRequest
)

// =============================================================================
// Passphrase providers
// =============================================================================
//
// Keys().Import takes a PassphraseProvider so an encrypted PEM can be
// unlocked without the plaintext ever landing in a Go string. These
// re-exports let CLI and GUI callers build one without importing go-xkms.

// PassphraseProvider supplies a PEM passphrase in an mlocked buffer.
type PassphraseProvider = secure.PassphraseProvider

// NewTerminalPassphraseProvider reads the passphrase from the controlling
// terminal with echo disabled. Use it when stdin is a TTY.
func NewTerminalPassphraseProvider() PassphraseProvider {
	return secure.NewTerminalProvider()
}

// NewStdinPassphraseProvider reads the passphrase from stdin. Use it when
// stdin is piped (scripts, CI).
func NewStdinPassphraseProvider() PassphraseProvider {
	return secure.NewStdinProvider()
}

// =============================================================================
// Wire types — QuicBaaS-specific request/response envelopes
// =============================================================================
//
// These exist because the xkms contract expresses some operations as Go
// return values (a []*Key slice, a gossh.Signer) that need an explicit
// envelope to cross a QuicBaaS transport. Every transport handler — server
// and client — encodes exactly these shapes, so the six transports agree
// byte-for-byte.

// ServiceName is the transport-level service identifier for SSH calls.
// Transport dispatchers match on this prefix.
const ServiceName = "quicbaas.SSH"

// Method names carried on ServiceName. Declared as constants so the SDK
// client, every transport dispatcher, and the tests share one spelling.
const (
	MethodGenerateKey         = "GenerateKey"
	MethodImportKey           = "ImportKey"
	MethodGetKey              = "GetKey"
	MethodListKeys            = "ListKeys"
	MethodDeleteKey           = "DeleteKey"
	MethodExportPublicKey     = "ExportPublicKey"
	MethodSignDigest          = "SignDigest"
	MethodAddKnownHost        = "AddKnownHost"
	MethodListKnownHosts      = "ListKnownHosts"
	MethodRemoveKnownHost     = "RemoveKnownHost"
	MethodAddAuthorizedKey    = "AddAuthorizedKey"
	MethodListAuthorizedKeys  = "ListAuthorizedKeys"
	MethodRemoveAuthorizedKey = "RemoveAuthorizedKey"
	MethodEmitAuthorizedKey   = "EmitAuthorizedKey"
)

// ListKeysResponse is the envelope for Keys().List.
type ListKeysResponse struct {
	Items []*Key `json:"items"`
}

// ListKnownHostsResponse is the envelope for KnownHosts().List.
type ListKnownHostsResponse struct {
	Items []*Pin `json:"items"`
}

// ListAuthorizedKeysResponse is the envelope for AuthorizedKeys().List.
type ListAuthorizedKeysResponse struct {
	Items []*AuthorizedEntry `json:"items"`
}

// SignRequest asks the server to sign a raw digest with the named key.
// The private half never leaves the key store; only the digest and the
// resulting signature cross the wire.
type SignRequest struct {
	Scope  string `json:"scope"`
	Owner  string `json:"owner"`
	Name   string `json:"name"`
	Digest []byte `json:"digest"`
}

// SignResponse mirrors *golang.org/x/crypto/ssh.Signature.
type SignResponse struct {
	Format string `json:"format"`
	Blob   []byte `json:"blob"`
	Rest   []byte `json:"rest,omitempty"`
}

// RemoveAuthorizedKeyRequest removes one authorized_keys entry by name.
type RemoveAuthorizedKeyRequest struct {
	Scope string `json:"scope"`
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

// EmitResponse carries the formatted authorized_keys line.
type EmitResponse struct {
	Line string `json:"line"`
}

// AckResponse is the envelope for verbs whose only result is success
// (Delete, Add, Remove). A typed envelope keeps every transport's
// response non-nil, so callers never have to distinguish "no body" from
// "wrong type".
type AckResponse struct {
	Success bool `json:"success"`
}
