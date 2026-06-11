//go:build quic

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package quic

// =============================================================================
// Exported QUIC client wire types — Barrier + Ceremony.
//
// These types are the public-facing request/response envelopes used by the
// in-package Client (pkg/api/quic/client.go) to issue OpBarrier and
// OpCeremony requests. They share field tags with the unexported wire
// types declared in handler_barrier.go and handler_ceremony.go so the JSON
// encoding round-trips bit-for-bit across REST, gRPC, and QUIC clients.
//
// Keeping the request/response shape exported on the Client side (and
// unexported on the Handler side) preserves package-internal locality of
// server-side wire decoding while giving callers a stable, documented API.
// =============================================================================

// ---------------------------------------------------------------------------
// Barrier request types
// ---------------------------------------------------------------------------

// BarrierInitializeRequest is the request body for Client.BarrierInitialize.
type BarrierInitializeRequest struct {
	Secret string `json:"secret"`
}

// BarrierUnsealRequest is the request body for Client.BarrierUnseal.
type BarrierUnsealRequest struct {
	Secret string `json:"secret"`
}

// BarrierShamirInitializeRequest is the request body for
// Client.BarrierShamirInitialize.
type BarrierShamirInitializeRequest struct {
	Secret      string `json:"secret"`
	Threshold   int    `json:"threshold"`
	TotalShares int    `json:"total_shares"`
}

// BarrierShamirUnsealShareRequest is the request body for
// Client.BarrierShamirUnsealShare.
type BarrierShamirUnsealShareRequest struct {
	Share string `json:"share"`
}

// BarrierShamirUnsealSharesRequest is the request body for
// Client.BarrierShamirUnsealShares.
type BarrierShamirUnsealSharesRequest struct {
	Shares []string `json:"shares"`
}

// BarrierRekeyRequest is the request body for Client.BarrierRekey.
type BarrierRekeyRequest struct {
	Threshold int `json:"threshold"`
	Total     int `json:"total"`
}

// BarrierRecoveryGenerateRequest is the request body for
// Client.BarrierGenerateRecoveryKeys.
type BarrierRecoveryGenerateRequest struct {
	Threshold int `json:"threshold"`
	Total     int `json:"total"`
}

// BarrierRecoveryRecoverRequest is the request body for
// Client.BarrierRecoverWithKeys.
type BarrierRecoveryRecoverRequest struct {
	Keys []string `json:"keys"`
}

// BarrierRootTokenRequest is the request body for
// Client.BarrierGenerateRootToken.
type BarrierRootTokenRequest struct {
	Shares []string `json:"shares"`
}

// ---------------------------------------------------------------------------
// Barrier response types
// ---------------------------------------------------------------------------

// BarrierStatusResponse is the response from Client.BarrierStatus.
// InitializedAt is RFC3339 in UTC; empty when the barrier has never been
// initialised.
type BarrierStatusResponse struct {
	Sealed         bool   `json:"sealed"`
	Strategy       string `json:"strategy"`
	HardwareBacked bool   `json:"hardware_backed"`
	InitializedAt  string `json:"initialized_at,omitempty"`
}

// ShamirInitResponse is the response from Client.BarrierShamirInitialize
// and Client.BarrierRekey. Share material is never returned (qrdb
// share-free contract): officers claim their shares through the
// identity-gated claim endpoints.
type ShamirInitResponse struct {
	Threshold   int `json:"threshold"`
	TotalShares int `json:"total_shares"`
	ShareCount  int `json:"share_count"`
}

// UnsealShareProgressResponse is the response from
// Client.BarrierShamirUnsealShare reporting current quorum progress.
type UnsealShareProgressResponse struct {
	Required  int  `json:"required"`
	Submitted int  `json:"submitted"`
	Complete  bool `json:"complete"`
}

// RecoveryKeysResponse is the response from
// Client.BarrierGenerateRecoveryKeys. Recovery key material is never
// returned (qrdb share-free contract).
type RecoveryKeysResponse struct {
	Threshold  int `json:"threshold"`
	Total      int `json:"total"`
	ShareCount int `json:"share_count"`
}

// RecoveryStatusResponse is the response from
// Client.BarrierHasRecoveryKeys.
type RecoveryStatusResponse struct {
	HasRecoveryKeys bool `json:"has_recovery_keys"`
}

// RootTokenResponse is the response from
// Client.BarrierGenerateRootToken. CreatedAt is RFC3339 in UTC; empty when
// zero.
type RootTokenResponse struct {
	Token     string `json:"token"`
	CreatedAt string `json:"created_at,omitempty"`
}

// ---------------------------------------------------------------------------
// Ceremony request types
// ---------------------------------------------------------------------------

// CeremonyOfficer describes a single officer participating in the
// initialization ceremony.
type CeremonyOfficer struct {
	Username string `json:"username"`
	CSRPEM   string `json:"csr_pem"`
}

// CeremonyInitializeRequest is the request body for
// Client.CeremonyInitialize.
type CeremonyInitializeRequest struct {
	SOPin                  string            `json:"so_pin"`
	UserPin                string            `json:"user_pin"`
	CredentialSealStrategy string            `json:"credential_seal_strategy,omitempty"`
	Threshold              int               `json:"threshold"`
	Officers               []CeremonyOfficer `json:"officers,omitempty"`
	Hostname               string            `json:"hostname,omitempty"`
	SANs                   []string          `json:"sans,omitempty"`
}

// CeremonyClaimCertBeginRequest is the request body for
// Client.CeremonyClaimCertBegin.
type CeremonyClaimCertBeginRequest struct {
	Username string `json:"username"`
}

// CeremonyClaimCertCompleteRequest is the request body for
// Client.CeremonyClaimCertComplete. Signature is base64-encoded.
type CeremonyClaimCertCompleteRequest struct {
	Username  string `json:"username"`
	NonceHex  string `json:"nonce_hex"`
	Signature string `json:"signature"`
}

// CeremonyClaimShareRequest is the request body for
// Client.CeremonyClaimShare.
type CeremonyClaimShareRequest struct {
	Username string `json:"username"`
}

// CeremonySignCSRRequest is the request body for Client.CeremonySignCSR.
type CeremonySignCSRRequest struct {
	Username string `json:"username"`
	SOPin    string `json:"so_pin"`
	CSRPEM   string `json:"csr_pem"`
	Role     string `json:"role"`
}

// CeremonyEnrollExternalOfficerRequest is the request body for
// Client.CeremonyEnrollExternalOfficer. Exactly one of CertPEM or
// PubKeyPEM must be supplied.
type CeremonyEnrollExternalOfficerRequest struct {
	Username   string `json:"username"`
	CertPEM    string `json:"cert_pem,omitempty"`
	PubKeyPEM  string `json:"pubkey_pem,omitempty"`
	ShareIndex int    `json:"share_index"`
	AuthFactor string `json:"auth_factor"`
	ShareBytes []byte `json:"share_bytes,omitempty"`
}

// ---------------------------------------------------------------------------
// Ceremony response types
// ---------------------------------------------------------------------------

// CeremonyInitResponse is the response from Client.CeremonyInitialize.
type CeremonyInitResponse struct {
	SPKIPin    string `json:"spki_pin"`
	CACertPEM  string `json:"ca_cert_pem"`
	TLSCertPEM string `json:"tls_cert_pem"`
	State      string `json:"state"`
	ShareCount int    `json:"share_count"`
	SOCertPEM  string `json:"so_cert_pem,omitempty"`
	SOKeyPEM   string `json:"so_key_pem,omitempty"`
}

// CeremonyStatusResponse is the response from Client.CeremonyStatus.
type CeremonyStatusResponse struct {
	State   string `json:"state"`
	SPKIPin string `json:"spki_pin,omitempty"`
}

// CeremonyChallengeResponse is the response from
// Client.CeremonyClaimCertBegin. ExpiresAt is RFC3339 UTC; empty when zero.
type CeremonyChallengeResponse struct {
	NonceHex  string `json:"nonce_hex"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// CeremonyClaimCertResponse is the response from
// Client.CeremonyClaimCertComplete.
type CeremonyClaimCertResponse struct {
	CertPEM   string `json:"cert_pem"`
	CACertPEM string `json:"ca_cert_pem"`
}

// CeremonyClaimShareResponse is the response from Client.CeremonyClaimShare.
type CeremonyClaimShareResponse struct {
	Share string `json:"share"`
}

// CeremonySignCSRResponse is the response from Client.CeremonySignCSR.
type CeremonySignCSRResponse struct {
	CertPEM string `json:"cert_pem"`
}

// CeremonyListOfficersResponse is the response from
// Client.CeremonyListOfficers. The Officers slice is intentionally
// untyped so that the persistent ceremony-registry struct can evolve
// without forcing every QUIC client to recompile.
type CeremonyListOfficersResponse struct {
	Officers []interface{} `json:"officers"`
}

// CeremonyEnrollExternalOfficerResponse is the response from
// Client.CeremonyEnrollExternalOfficer.
type CeremonyEnrollExternalOfficerResponse struct {
	SealedShare string `json:"sealed_share,omitempty"`
}
