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

// Package acme holds the wire-format request/response types for the
// quicbaas.ACME service (Security().ACME().Server()). They live in this leaf
// package — not in sdk/go — so the transport implementations
// (rest/embedded) can return and decode them without creating an import
// cycle back into the top-level SDK package. The high-level
// Security().ACME() surface in sdk/go re-exports these via the SDK's own
// ACME* types.
//
// ACME is an HTTP-native protocol (RFC 8555). Only the server-side control
// verbs — RegisterChallenge and Issue — exchange these wire types over a
// quicbaas transport. The protocol-client verbs (NewOrder / FinalizeOrder /
// Renew) are JWS-signed exchanges performed by an ACME protocol client
// against the directory the server mounts; they are NOT quicbaas transport
// RPCs.
package acme

// ServiceName is the gRPC-style service identifier used by the SDK to route
// ACME server-control requests. ACME is mounted on REST + embedded only.
const ServiceName = "quicbaas.ACME"

// RegisterChallengeRequest names the challenge type to enable on the server.
// The verifier function itself is process-local to the SDK caller; the server
// only needs the name to install its canonical verifier and advertise the
// challenge in the directory.
type RegisterChallengeRequest struct {
	Name string `json:"name"`
}

// RegisterChallengeResponse reports whether the challenge was registered.
type RegisterChallengeResponse struct {
	Registered bool `json:"registered"`
}

// IssueRequest carries a PEM-encoded CSR plus the named challenge type and
// value to perform a server-side issuance.
type IssueRequest struct {
	CSRPEM         []byte `json:"csr_pem"`
	ChallengeType  string `json:"challenge_type"`
	ChallengeValue string `json:"challenge_value"`
}

// IssueResponse carries the issued certificate, the CA chain, and the leaf
// serial number.
type IssueResponse struct {
	CertificatePEM []byte `json:"certificate_pem"`
	ChainPEM       []byte `json:"chain_pem"`
	SerialNumber   string `json:"serial_number"`
}
