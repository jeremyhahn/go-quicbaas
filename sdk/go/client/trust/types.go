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

// Package trust holds the wire-format request/response types for the
// quicbaas.Trust service (Security().Trust()). They live in this leaf
// package — not in sdk/go — so the transport implementations
// (rest/quic/grpc/embedded) can return and decode them without creating
// an import cycle back into the top-level SDK package, which imports
// every transport. The high-level Security().Trust() surface in sdk/go
// re-exports these types via aliases.
package trust

// ServiceName is the gRPC-style service identifier used by the SDK to
// route Trust requests across every transport.
const ServiceName = "quicbaas.Trust"

// CrossSignedRootPoolRequest carries no parameters; the cross-signed
// root pool is resolved server-side from the active CA's own root and
// cross-signer.
type CrossSignedRootPoolRequest struct{}

// CrossSignedRootPoolResponse is the wire-level response payload for
// TrustService.CrossSignedRootPool. Each element is a PEM-encoded (or,
// as a fallback, DER-encoded) certificate that the SDK assembles into a
// *x509.CertPool.
type CrossSignedRootPoolResponse struct {
	RootPEMs [][]byte `json:"root_pems"`
}

// AutoImportAIARequest carries the DER-encoded certificate chain whose
// AIA (AuthorityInformationAccess) extensions are walked and imported
// server-side.
type AutoImportAIARequest struct {
	ChainDER [][]byte `json:"chain_der"`
}

// AutoImportAIAResponse acknowledges a completed AIA import. It carries
// the count of certificates imported so callers can report progress; a
// zero count is valid (the supplied chain had no AIA references).
type AutoImportAIAResponse struct {
	Imported int `json:"imported"`
}

// PublishDANERequest carries the DER-encoded certificate plus the DNS name
// and port the TLSA records are published for. The server derives the
// TLSA records (via xkms BuildRecords / go-truststrap) and hands them to
// the registered DANEPublisher.
type PublishDANERequest struct {
	Name    string `json:"name"`
	Port    int    `json:"port"`
	CertDER []byte `json:"cert_der"`
}

// PublishDANEResponse acknowledges a completed DANE publication and reports
// how many TLSA records were published.
type PublishDANEResponse struct {
	Published int `json:"published"`
}
