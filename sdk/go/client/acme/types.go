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
// quicbaas.ACME service (Security().ACME()). They live in this leaf package —
// not in sdk/go — so the transport implementations (rest/embedded) can return
// and decode them without creating an import cycle back into the top-level SDK
// package. The high-level Security().ACME() surface in sdk/go re-exports these
// via the SDK's own ACME* types.
//
// ACME is an HTTP-native protocol (RFC 8555). All quicbaas.ACME verbs —
// the server-side control verbs (RegisterChallenge, Issue) and the broker-as-CA
// protocol-client verbs (NewOrder, FinalizeOrder, Renew) — exchange these wire
// types over the REST or embedded transport only. In the broker-as-CA model
// QuicBaaS is itself the certificate authority, so the client verbs are
// in-process service calls, NOT JWS-signed exchanges against an external ACME
// directory.
package acme

// ServiceName is the gRPC-style service identifier used by the SDK to route
// ACME requests. ACME is mounted on REST + embedded only.
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

// OrderIdentifier is a single RFC 8555 order identifier (e.g. a DNS name).
type OrderIdentifier struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// NewOrderRequest carries the identifiers and challenge family for a new
// broker-as-CA order.
type NewOrderRequest struct {
	Identifiers   []OrderIdentifier `json:"identifiers"`
	ChallengeType string            `json:"challenge_type"`
}

// NewOrderResponse is the lightweight in-process order descriptor returned by
// NewOrder. The Finalize and URL fields carry the generated order ID.
type NewOrderResponse struct {
	OrderID     uint64            `json:"order_id"`
	Status      string            `json:"status"`
	Identifiers []OrderIdentifier `json:"identifiers"`
	Finalize    string            `json:"finalize"`
	URL         string            `json:"url"`
}

// FinalizeOrderRequest finalizes an order by signing the supplied CSR.
type FinalizeOrderRequest struct {
	OrderURL string `json:"order_url"`
	CSRPEM   []byte `json:"csr_pem"`
}

// FinalizeOrderResponse carries the issued certificate, the CA chain, and the
// leaf serial number.
type FinalizeOrderResponse struct {
	CertificatePEM []byte `json:"certificate_pem"`
	ChainPEM       []byte `json:"chain_pem"`
	SerialNumber   string `json:"serial_number"`
}

// RenewRequest identifies the certificate to renew by its directory URL (the
// hex serial number the CA assigned at issuance, optionally as an
// "/acme/cert/<serial>" path) and whether to revoke it.
type RenewRequest struct {
	CertURL string `json:"cert_url"`
	Revoke  bool   `json:"revoke"`
}

// RenewResponse reports the outcome of a renew attempt. CertificatePEM carries
// the resolved (existing) certificate; Renewable is false because the
// broker-as-CA holds no subscriber key and the request carries no CSR.
type RenewResponse struct {
	CertificatePEM []byte `json:"certificate_pem"`
	SerialNumber   string `json:"serial_number"`
	Revoked        bool   `json:"revoked"`
	Renewable      bool   `json:"renewable"`
}
