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

package service

import (
	"context"
	"crypto/x509"
	stderrors "errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TrustService exposes the cross-sign / AIA trust composition verbs that
// QuicBaaS surfaces under Security().Trust(). It is a pure composition
// layer over the xkms TrustedCA held by the CA adapter — it does NOT
// reimplement cross-signing or AIA walking; both already live in
// go-xkms/pkg/ca and are reached through the TrustCA capability below.
type TrustService interface {
	// CrossSignedRootPool returns the PEM-encoded cross-signed root set
	// for the active CA: the native root plus any cross-signed roots that
	// bridge to a foreign issuer. Callers assemble these into an
	// *x509.CertPool to verify chains that traverse a cross-signature
	// boundary.
	CrossSignedRootPool(ctx context.Context, req *CrossSignedRootPoolRequest) (*CrossSignedRootPoolResponse, error)

	// AutoImportAIA walks the AuthorityInformationAccess (AIA) extension
	// of each certificate in the supplied chain, downloads the referenced
	// issuing CA certificates, and imports them into the CA's certificate
	// store. Returns the number of chain certificates whose AIA was
	// processed.
	AutoImportAIA(ctx context.Context, req *AutoImportAIARequest) (*AutoImportAIAResponse, error)
}

// TrustCA is the narrow capability the TrustService needs from the
// xkms-composed CA. The pkg/server caServiceAdapter satisfies it by
// delegating to the embedded TrustedCA. Keeping this surface minimal
// (rather than depending on the full xkmssdk.TrustedCA) keeps the
// service layer transport- and xkms-type-agnostic, and trivially
// mockable in tests.
type TrustCA interface {
	// CrossSignedRootPEMs returns the cross-sign-aware root set for the
	// active CA as PEM-encoded certificates: the native root followed by
	// any cross-signed roots. The serialisation is performed by the
	// adapter so the service layer never touches xkms-internal types or
	// x509.CertPool internals.
	CrossSignedRootPEMs() ([][]byte, error)

	// ImportIssuingCAs recursively imports the issuing CA certificates
	// referenced by cert's AIA extension into the certificate store.
	ImportIssuingCAs(cert *x509.Certificate) error
}

// CrossSignedRootPoolRequest carries no parameters; the pool is derived
// from the active CA.
type CrossSignedRootPoolRequest struct{}

// CrossSignedRootPoolResponse returns the cross-signed root set as a
// slice of PEM-encoded certificates, the wire-portable form of an
// *x509.CertPool.
type CrossSignedRootPoolResponse struct {
	RootPEMs [][]byte `json:"root_pems"`
}

// AutoImportAIARequest carries the DER-encoded chain whose AIA
// extensions are walked and imported.
type AutoImportAIARequest struct {
	ChainDER [][]byte `json:"chain_der"`
}

// AutoImportAIAResponse reports how many chain certificates had their
// AIA processed.
type AutoImportAIAResponse struct {
	Imported int `json:"imported"`
}

// Typed sentinel errors for the trust service.
var (
	ErrTrustNilRequest = &errors.QBError{Code: errors.ErrInvalidInput, Op: "TrustService", Err: errTrustNilRequest}
	ErrTrustEmptyChain = &errors.QBError{Code: errors.ErrInvalidInput, Op: "TrustService", Err: errTrustEmptyChain}
	ErrTrustNilCertDER = &errors.QBError{Code: errors.ErrInvalidInput, Op: "TrustService", Err: errTrustNilCertDER}
	ErrTrustCAMissing  = &errors.QBError{Code: errors.ErrUnavailable, Op: "TrustService", Err: errTrustCAMissing}
)

var (
	errTrustNilRequest = stderrors.New("request must not be nil")
	errTrustEmptyChain = stderrors.New("certificate chain is required")
	errTrustNilCertDER = stderrors.New("certificate chain entry is empty")
	errTrustCAMissing  = stderrors.New("trust service has no backing certificate authority")
)

// trustService is the production TrustService backed by a TrustCA.
type trustService struct {
	ca TrustCA
}

// NewTrustService creates a TrustService backed by ca. Returns nil when
// ca is nil so the service registry can treat an absent CA as "Trust not
// configured" rather than panicking on a nil delegate.
func NewTrustService(ca TrustCA) TrustService {
	if ca == nil {
		return nil
	}
	return &trustService{ca: ca}
}

// CrossSignedRootPool returns the active CA's cross-sign-aware root set
// in PEM form.
func (s *trustService) CrossSignedRootPool(ctx context.Context, _ *CrossSignedRootPoolRequest) (*CrossSignedRootPoolResponse, error) {
	if s.ca == nil {
		return nil, ErrTrustCAMissing
	}
	if err := ctx.Err(); err != nil {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "TrustService.CrossSignedRootPool", Err: err}
	}

	roots, err := s.ca.CrossSignedRootPEMs()
	if err != nil {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "TrustService.CrossSignedRootPool", Err: err}
	}

	return &CrossSignedRootPoolResponse{RootPEMs: roots}, nil
}

// AutoImportAIA walks the AIA chain of each supplied certificate and
// imports the referenced issuing CAs.
func (s *trustService) AutoImportAIA(ctx context.Context, req *AutoImportAIARequest) (*AutoImportAIAResponse, error) {
	if s.ca == nil {
		return nil, ErrTrustCAMissing
	}
	if req == nil {
		return nil, ErrTrustNilRequest
	}
	if len(req.ChainDER) == 0 {
		return nil, ErrTrustEmptyChain
	}
	if err := ctx.Err(); err != nil {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "TrustService.AutoImportAIA", Err: err}
	}

	imported := 0
	for _, der := range req.ChainDER {
		if len(der) == 0 {
			return nil, ErrTrustNilCertDER
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, &errors.QBError{Code: errors.ErrInvalidInput, Op: "TrustService.AutoImportAIA", Err: err}
		}
		if err := s.ca.ImportIssuingCAs(cert); err != nil {
			return nil, &errors.QBError{Code: errors.ErrInternal, Op: "TrustService.AutoImportAIA", Err: err}
		}
		imported++
	}

	return &AutoImportAIAResponse{Imported: imported}, nil
}
