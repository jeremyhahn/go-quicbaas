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

package server

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"strings"
	"sync"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// Typed errors for the CA service adapter. Returning typed sentinels makes
// adapter behaviour testable without string matching.
var (
	// ErrCAServiceAdapterNilCA is returned when the adapter is constructed
	// without a backing TrustedCA.
	ErrCAServiceAdapterNilCA = errors.New("server: ca service adapter requires a non-nil TrustedCA")

	// ErrCAServiceAdapterInvalidPEM is returned when input PEM data cannot
	// be decoded.
	ErrCAServiceAdapterInvalidPEM = errors.New("server: invalid PEM input")

	// ErrCAServiceAdapterInvalidSerial is returned when a serial number
	// cannot be parsed as a hexadecimal big.Int.
	ErrCAServiceAdapterInvalidSerial = errors.New("server: invalid certificate serial number")

	// ErrCAServiceAdapterNilRequest is returned when the caller passes a
	// nil request struct to a CA operation.
	ErrCAServiceAdapterNilRequest = errors.New("server: ca service adapter received nil request")
)

// caServiceAdapter wraps an xkmssdk.TrustedCA so it satisfies the
// xkmstransport.Client interface required by the QuicBaaS service builder's
// WithCAService option. The CertificateService inside the service builder
// only invokes the seven CAService methods, so the adapter implements those
// methods directly and embeds the wider xkmstransport.Client interface as
// a nil placeholder to satisfy the type assertion. Calls to any non-CA
// method would dereference the nil embedded interface, but the
// CertificateService never makes those calls.
//
// This pattern is identical to how mockCAService in
// pkg/service/certificate_service_test.go satisfies the same assertion.
type caServiceAdapter struct {
	xkmstransport.Client // intentionally nil; only CAService methods are invoked

	mu sync.RWMutex
	ca xkmssdk.TrustedCA
}

// newCAServiceAdapter constructs an adapter that exposes a TrustedCA via
// the xkmstransport CAService method set. Returns ErrCAServiceAdapterNilCA
// when ca is nil.
func newCAServiceAdapter(ca xkmssdk.TrustedCA) (*caServiceAdapter, error) {
	if ca == nil {
		return nil, ErrCAServiceAdapterNilCA
	}
	return &caServiceAdapter{ca: ca}, nil
}

// SetCA atomically replaces the Certificate Authority backing this adapter.
// All in-flight CA operations complete against the previous CA before the
// swap takes effect. Passing nil is a no-op; the existing CA is preserved.
func (a *caServiceAdapter) SetCA(ca xkmssdk.TrustedCA) {
	if ca == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ca = ca
}

// getCA returns the current Certificate Authority under a read lock.
func (a *caServiceAdapter) getCA() xkmssdk.TrustedCA {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.ca
}

// IssueCertificate issues a new certificate from the wrapped CA, generating
// a fresh keypair as part of the request.
func (a *caServiceAdapter) IssueCertificate(
	ctx context.Context,
	req *xkmstransport.IssueCertificateRequest,
) (*xkmstransport.IssueCertificateResponse, error) {

	if req == nil {
		return nil, ErrCAServiceAdapterNilRequest
	}

	caReq := &xkmssdk.TrustedCACertificateRequest{
		Subject: xkmssdk.TrustedCASubject{
			CommonName:   req.CommonName,
			Organization: req.Organization,
		},
		SANS:  parseTransportSANs(req.SANs),
		Valid: req.ValidityDays,
	}

	issued, err := a.getCA().IssueCertificateWithProfile(caReq, req.Profile)
	if err != nil {
		return nil, err
	}

	chain := issued.ChainPEM
	if len(chain) == 0 {
		chain = issued.CACertificatePEM
	}

	resp := &xkmstransport.IssueCertificateResponse{
		CertificatePEM: issued.CertificatePEM,
		ChainPEM:       chain,
		PrivateKeyPEM:  issued.PrivateKeyPEM,
	}
	if issued.SerialNumber != nil {
		resp.SerialNumber = issued.SerialNumber.Text(16)
	}
	return resp, nil
}

// SignCSR signs an externally generated CSR using the wrapped CA.
func (a *caServiceAdapter) SignCSR(
	ctx context.Context,
	req *xkmstransport.SignCSRRequest,
) (*xkmstransport.SignCSRResponse, error) {

	if req == nil {
		return nil, ErrCAServiceAdapterNilRequest
	}
	if len(req.CSRPEM) == 0 {
		return nil, ErrCAServiceAdapterInvalidPEM
	}

	var opts *xkmssdk.TrustedCASignOptions
	if req.Profile != "" || req.ValidityDays > 0 {
		opts = &xkmssdk.TrustedCASignOptions{
			Profile:      req.Profile,
			ValidityDays: req.ValidityDays,
		}
	}

	cert, err := a.getCA().SignCSR(req.CSRPEM, opts)
	if err != nil {
		return nil, err
	}

	bundle, _ := a.getCA().CABundle()
	return &xkmstransport.SignCSRResponse{
		CertificatePEM: pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: cert.Raw,
		}),
		ChainPEM:     bundle,
		SerialNumber: cert.SerialNumber.Text(16),
	}, nil
}

// RevokeCertificate marks a certificate as revoked by serial number.
func (a *caServiceAdapter) RevokeCertificate(
	ctx context.Context,
	req *xkmstransport.RevokeCertificateRequest,
) (*xkmstransport.RevokeCertificateResponse, error) {

	if req == nil {
		return nil, ErrCAServiceAdapterNilRequest
	}
	if req.SerialNumber == "" {
		return nil, ErrCAServiceAdapterInvalidSerial
	}

	serial, ok := parseHexSerial(req.SerialNumber)
	if !ok {
		return nil, ErrCAServiceAdapterInvalidSerial
	}

	if err := a.getCA().Revoke(serial, req.Reason); err != nil {
		return &xkmstransport.RevokeCertificateResponse{
			Success: false,
			Message: err.Error(),
		}, err
	}
	return &xkmstransport.RevokeCertificateResponse{Success: true}, nil
}

// GetCABundle returns the CA bundle in PEM form.
func (a *caServiceAdapter) GetCABundle(
	ctx context.Context,
	req *xkmstransport.GetCABundleRequest,
) (*xkmstransport.GetCABundleResponse, error) {

	bundle, err := a.getCA().CABundle()
	if err != nil {
		return nil, err
	}

	return &xkmstransport.GetCABundleResponse{
		BundlePEM:    bundle,
		Certificates: splitPEMCertificates(bundle),
		ContentType:  "application/x-pem-file",
	}, nil
}

// GetCACertificate returns the CA's leaf certificate in PEM form.
func (a *caServiceAdapter) GetCACertificate(
	ctx context.Context,
	req *xkmstransport.GetCACertificateRequest,
) (*xkmstransport.GetCACertificateResponse, error) {

	cert, err := a.getCA().CACertificate()
	if err != nil {
		return nil, err
	}

	return &xkmstransport.GetCACertificateResponse{
		CertificatePEM: pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: cert.Raw,
		}),
		Subject:      cert.Subject.String(),
		Issuer:       cert.Issuer.String(),
		SerialNumber: cert.SerialNumber.Text(16),
		NotBefore:    cert.NotBefore.UTC().Format("2006-01-02T15:04:05Z"),
		NotAfter:     cert.NotAfter.UTC().Format("2006-01-02T15:04:05Z"),
		IsCA:         cert.IsCA,
	}, nil
}

// GenerateCRL produces a fresh CRL from the wrapped CA.
func (a *caServiceAdapter) GenerateCRL(
	ctx context.Context,
	req *xkmstransport.GenerateCRLRequest,
) (*xkmstransport.GenerateCRLResponse, error) {

	crlDER, err := a.getCA().GenerateCRL()
	if err != nil {
		return nil, err
	}
	return &xkmstransport.GenerateCRLResponse{
		CRLPEM: pem.EncodeToMemory(&pem.Block{
			Type:  "X509 CRL",
			Bytes: crlDER,
		}),
	}, nil
}

// IsRevoked checks whether a certificate is revoked by serial number.
func (a *caServiceAdapter) IsRevoked(
	ctx context.Context,
	req *xkmstransport.IsRevokedRequest,
) (*xkmstransport.IsRevokedResponse, error) {

	if req == nil {
		return nil, ErrCAServiceAdapterNilRequest
	}
	if req.SerialNumber == "" {
		return nil, ErrCAServiceAdapterInvalidSerial
	}

	serial, ok := parseHexSerial(req.SerialNumber)
	if !ok {
		return nil, ErrCAServiceAdapterInvalidSerial
	}

	revoked, err := a.getCA().IsRevoked(serial)
	if err != nil {
		return nil, err
	}
	return &xkmstransport.IsRevokedResponse{Revoked: revoked}, nil
}

// parseTransportSANs converts the prefixed-string SAN format used by
// xkmstransport ("DNS:host", "IP:1.2.3.4", "Email:a@b", "URI:...") into
// the structured SubjectAlternativeNames the TrustedCA expects. Entries
// without a recognized prefix are classified as IP if they parse as one
// and DNS otherwise, matching the behaviour of pkg/ca.parseSANStrings.
func parseTransportSANs(sans []string) *xkmssdk.TrustedCASubjectAlternativeNames {
	if len(sans) == 0 {
		return nil
	}
	out := &xkmssdk.TrustedCASubjectAlternativeNames{}
	for _, san := range sans {
		switch {
		case strings.HasPrefix(san, "DNS:"):
			out.DNS = append(out.DNS, strings.TrimPrefix(san, "DNS:"))
		case strings.HasPrefix(san, "IP:"):
			out.IPs = append(out.IPs, strings.TrimPrefix(san, "IP:"))
		case strings.HasPrefix(san, "Email:"):
			out.Email = append(out.Email, strings.TrimPrefix(san, "Email:"))
		case strings.HasPrefix(san, "URI:"):
			out.URIs = append(out.URIs, strings.TrimPrefix(san, "URI:"))
		default:
			if ip := net.ParseIP(san); ip != nil {
				out.IPs = append(out.IPs, san)
			} else {
				out.DNS = append(out.DNS, san)
			}
		}
	}
	return out
}

// parseHexSerial decodes a hexadecimal certificate serial number string
// into a big.Int. Falls back to base 10 to accept either format. Returns
// ok=false on failure rather than panicking so callers can convert the
// failure into a typed error.
func parseHexSerial(serial string) (*big.Int, bool) {
	n := new(big.Int)
	if _, ok := n.SetString(serial, 16); ok {
		return n, true
	}
	if _, ok := n.SetString(serial, 10); ok {
		return n, true
	}
	return nil, false
}

// splitPEMCertificates extracts each CERTIFICATE block from a PEM bundle
// and returns the DER bytes for each. Non-CERTIFICATE blocks are skipped.
// When PEM decoding produces no usable certificates the raw bundle is
// returned unchanged so callers always receive something they can hand
// off to subsequent validators.
func splitPEMCertificates(bundle []byte) [][]byte {
	if len(bundle) == 0 {
		return nil
	}
	var out [][]byte
	rest := bundle
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		if _, err := x509.ParseCertificate(block.Bytes); err == nil {
			out = append(out, block.Bytes)
		}
	}
	if len(out) == 0 {
		return [][]byte{bundle}
	}
	return out
}
