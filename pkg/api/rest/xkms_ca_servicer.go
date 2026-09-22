//go:build rest

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

package rest

import (
	"context"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// xkmsCAServicer adapts an xkmssdk.TrustedCA (native Go types, used by the
// embedded CA) to the transport-level CAService interface shape expected by
// go-xkms's REST CA handlers. The adapter owns ZERO CA logic — every method
// delegates straight to the embedded TrustedCA; only request/response types
// are translated.
//
// This adapter exists because go-quicbaas embeds the CA in-process
// (s.xkmsCA in pkg/server) while the go-xkms REST handlers
// (pkg/api/rest/handlers_ca.go) operate on the transport.*Request/Response
// shapes over the CAService interface. Structurally,
// go-xkms/pkg/api/rest.CAHandlers accepts any type that matches the
// 7-method CAService surface; xkmsCAServicer is that type.
//
// NOTE: We purposefully do NOT reimplement the handler HTTP layer —
// CAHandlers from go-xkms handles validation, error mapping, logging, and
// status codes. Instantiating CAHandlers with this adapter reuses all of
// that production-tested code.
type xkmsCAServicer struct {
	ca xkmssdk.TrustedCA

	// spiffeTrustDomain is the operator-configured SPIFFE trust domain. When
	// non-empty alongside a request TenantID, the adapter instructs the
	// embedded CA to stamp a tenant-scoped SPIFFE URI SAN onto the issued
	// leaf so mTLS/PIV login can derive the tenant from the verified peer
	// certificate. Empty disables SPIFFE SAN synthesis.
	spiffeTrustDomain string
}

// Compile-time guarantee that the adapter satisfies the exported
// CAService interface. Because go-xkms's unexported caServicer interface
// (pkg/api/rest/handlers_ca.go) has the IDENTICAL method set, Go's
// structural interface satisfaction accepts this adapter for CAHandlers
// construction.
var _ xkmstransport.CAService = (*xkmsCAServicer)(nil)

// newXKMSCAServicer wires the adapter around an initialized TrustedCA.
// Returns nil when ca is nil so callers can skip handler registration.
func newXKMSCAServicer(ca xkmssdk.TrustedCA, spiffeTrustDomain string) *xkmsCAServicer {
	if ca == nil {
		return nil
	}
	return &xkmsCAServicer{ca: ca, spiffeTrustDomain: spiffeTrustDomain}
}

// GetCABundle returns the CA bundle as PEM bytes plus individual DER
// certificates, matching the go-xkms REST contract.
func (a *xkmsCAServicer) GetCABundle(_ context.Context, _ *xkmstransport.GetCABundleRequest) (*xkmstransport.GetCABundleResponse, error) {
	bundlePEM, err := a.ca.CABundle()
	if err != nil {
		return nil, &XKMSCAOperationError{Op: "get bundle", Err: err}
	}

	// Decompose the PEM bundle into individual DER certificates so
	// clients can inspect them without re-parsing PEM.
	certs := make([][]byte, 0, 2)
	rest := bundlePEM
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			der := make([]byte, len(block.Bytes))
			copy(der, block.Bytes)
			certs = append(certs, der)
		}
	}

	return &xkmstransport.GetCABundleResponse{
		BundlePEM:    bundlePEM,
		Certificates: certs,
		ContentType:  "application/x-pem-file",
	}, nil
}

// GetCACertificate returns the issuing CA certificate in PEM form along
// with its subject, issuer, serial, and validity metadata.
func (a *xkmsCAServicer) GetCACertificate(_ context.Context, _ *xkmstransport.GetCACertificateRequest) (*xkmstransport.GetCACertificateResponse, error) {
	cert, err := a.ca.CACertificate()
	if err != nil {
		return nil, &XKMSCAOperationError{Op: "get certificate", Err: err}
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})

	return &xkmstransport.GetCACertificateResponse{
		CertificatePEM: certPEM,
		Subject:        cert.Subject.String(),
		Issuer:         cert.Issuer.String(),
		SerialNumber:   cert.SerialNumber.Text(16),
		NotBefore:      cert.NotBefore.UTC().Format("2006-01-02T15:04:05Z"),
		NotAfter:       cert.NotAfter.UTC().Format("2006-01-02T15:04:05Z"),
		IsCA:           cert.IsCA,
	}, nil
}

// SignCSR signs a CSR and returns the leaf and chain PEMs. Profile and
// ValidityDays flow into SignOptions so the CA can apply the matching
// certificate profile.
func (a *xkmsCAServicer) SignCSR(ctx context.Context, req *xkmstransport.SignCSRRequest) (*xkmstransport.SignCSRResponse, error) {
	if req == nil {
		return nil, ErrXKMSCANilRequest
	}
	if len(req.CSRPEM) == 0 {
		return nil, ErrXKMSCAMissingCSR
	}

	opts := a.signOptionsWithSPIFFE(req.Profile, req.ValidityDays, req.TenantID)

	cert, err := a.ca.SignCSR(ctx, req.CSRPEM, opts)
	if err != nil {
		return nil, &XKMSCAOperationError{Op: "sign csr", Err: err}
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})

	// Include the issuing CA cert so the client can build a chain
	// without issuing a second GetCACertificate request.
	var chainPEM []byte
	if caCert, caErr := a.ca.CACertificate(); caErr == nil {
		chainPEM = pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: caCert.Raw,
		})
	}

	return &xkmstransport.SignCSRResponse{
		CertificatePEM: certPEM,
		ChainPEM:       chainPEM,
		SerialNumber:   cert.SerialNumber.Text(16),
	}, nil
}

// applyKeypairSPIFFE folds the operator-configured SPIFFE trust domain, the
// request tenant, and the profile-derived SPIFFE workload kind onto a keypair-
// issuance CertificateRequest. It is the generate-and-distribute counterpart of
// signOptionsWithSPIFFE: the CA forwards these request fields into the same
// spiffe.BuildURI synthesis site SignCSR uses. When the trust domain or tenant
// is empty no SPIFFE SAN is requested (single-domain / pre-broker mode).
func (a *xkmsCAServicer) applyKeypairSPIFFE(certReq *xkmssdk.TrustedCACertificateRequest, profile, tenantID string) {
	if a.spiffeTrustDomain == "" || tenantID == "" {
		return
	}
	certReq.SPIFFETrustDomain = a.spiffeTrustDomain
	certReq.TenantID = tenantID
	certReq.SPIFFEKind = service.SPIFFEKindForProfile(profile)
}

// signOptionsWithSPIFFE builds the TrustedCASignOptions for a CSR signing
// operation, folding in the operator-configured SPIFFE trust domain, the
// request tenant, and the SPIFFE workload kind derived from the certificate
// profile. When the trust domain or tenant is empty the CA omits the SPIFFE
// SAN (single-domain / pre-broker mode).
func (a *xkmsCAServicer) signOptionsWithSPIFFE(profile string, validityDays int, tenantID string) *xkmssdk.TrustedCASignOptions {
	opts := &xkmssdk.TrustedCASignOptions{
		Profile:      profile,
		ValidityDays: validityDays,
	}
	if a.spiffeTrustDomain != "" && tenantID != "" {
		opts.SPIFFETrustDomain = a.spiffeTrustDomain
		opts.TenantID = tenantID
		opts.SPIFFEKind = service.SPIFFEKindForProfile(profile)
	}
	return opts
}

// IssueCertificate asks the CA to generate a keypair and sign a certificate for
// it. Mirrors XKMSService.IssueCertificate in go-xkms so REST clients see a
// consistent surface — including the part that surprises people: the key is
// generated INSIDE the CA's keystore as a non-exportable opaque handle, so
// PrivateKeyPEM comes back EMPTY on every go-xkms backend. This route hands the
// caller a certificate, not a credential.
//
// A caller that needs to hold the private key uses the CSR-sign route
// (/api/v1/xkms/ca/sign-csr) with a key it generated itself, which is what
// `quicbaas agent deploy` does, or the quicbaas certificate service
// (Security().Certificate().IssueCertificate), which owns the
// generate-and-distribute contract. See pkg/xkms/keygen.go for why the split
// exists (go-quicbaas#910).
func (a *xkmsCAServicer) IssueCertificate(ctx context.Context, req *xkmstransport.IssueCertificateRequest) (*xkmstransport.IssueCertificateResponse, error) {
	if req == nil {
		return nil, ErrXKMSCANilRequest
	}
	if req.CommonName == "" {
		return nil, ErrXKMSCAMissingCommonName
	}

	sans := parseSANs(req.SANs)
	certReq := &xkmssdk.TrustedCACertificateRequest{
		Subject: xkmssdk.TrustedCASubject{
			CommonName:   req.CommonName,
			Organization: req.Organization,
		},
		SANS:  sans,
		Valid: req.ValidityDays,
	}

	// Stamp the tenant-scoped SPIFFE URI SAN on the generate-and-distribute
	// (keypair) path, symmetric with signOptionsWithSPIFFE on the CSR-sign
	// path. The CertificateRequest SPIFFE fields are forwarded by the CA into
	// the same spiffe.BuildURI synthesis site SignCSR uses, so both paths emit
	// the identical canonical SAN.
	a.applyKeypairSPIFFE(certReq, req.Profile, req.TenantID)

	// Route through IssueCertificateWithProfile when a profile is named so
	// the CA picks up the profile's KeyUsage/ExtKeyUsage/validity defaults.
	// Otherwise fall back to IssueCertificate, which uses the CA's
	// built-in heuristics (server cert when DNS SANs exist, client when
	// only email SANs, etc.).
	var issued *xkmssdk.TrustedCAIssuedCertificate
	var err error
	if strings.TrimSpace(req.Profile) != "" {
		issued, err = a.ca.IssueCertificateWithProfile(ctx, certReq, req.Profile)
	} else {
		issued, err = a.ca.IssueCertificate(ctx, certReq)
	}
	if err != nil {
		return nil, &XKMSCAOperationError{Op: "issue certificate", Err: err}
	}
	if issued == nil {
		return nil, &XKMSCAOperationError{Op: "issue certificate", Err: errors.New("ca returned nil certificate")}
	}

	return &xkmstransport.IssueCertificateResponse{
		CertificatePEM: append([]byte(nil), issued.CertificatePEM...),
		ChainPEM:       append([]byte(nil), issued.ChainPEM...),
		PrivateKeyPEM:  append([]byte(nil), issued.PrivateKeyPEM...),
		SerialNumber:   issued.SerialNumber.Text(16),
	}, nil
}

// RevokeCertificate marks a serial as revoked using the RFC 5280 reason
// code supplied by the caller.
func (a *xkmsCAServicer) RevokeCertificate(ctx context.Context, req *xkmstransport.RevokeCertificateRequest) (*xkmstransport.RevokeCertificateResponse, error) {
	if req == nil {
		return nil, ErrXKMSCANilRequest
	}
	if req.SerialNumber == "" {
		return nil, ErrXKMSCAMissingSerial
	}

	serial, err := parseHexSerial(req.SerialNumber)
	if err != nil {
		return nil, err
	}

	if err := a.ca.Revoke(ctx, serial, req.Reason); err != nil {
		return nil, &XKMSCAOperationError{Op: "revoke", Err: err}
	}

	return &xkmstransport.RevokeCertificateResponse{
		Success: true,
		Message: "certificate " + req.SerialNumber + " revoked",
	}, nil
}

// GenerateCRL produces a signed PEM-encoded CRL covering all currently
// revoked certificates.
func (a *xkmsCAServicer) GenerateCRL(ctx context.Context, _ *xkmstransport.GenerateCRLRequest) (*xkmstransport.GenerateCRLResponse, error) {
	crlDER, err := a.ca.GenerateCRL(ctx)
	if err != nil {
		return nil, &XKMSCAOperationError{Op: "generate crl", Err: err}
	}

	crlPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "X509 CRL",
		Bytes: crlDER,
	})

	return &xkmstransport.GenerateCRLResponse{
		CRLPEM: crlPEM,
	}, nil
}

// CRL returns the CA's current DER-encoded CRL. The CA regenerates and
// persists it first only when none has been published yet or the published
// one's NextUpdate has passed; otherwise it serves the persisted artifact
// without signing.
func (a *xkmsCAServicer) CRL(ctx context.Context) ([]byte, error) {
	crlDER, err := a.ca.CRL(ctx)
	if err != nil {
		return nil, &XKMSCAOperationError{Op: "get crl", Err: err}
	}
	return crlDER, nil
}

// IsRevoked reports whether the given serial number has been revoked.
func (a *xkmsCAServicer) IsRevoked(_ context.Context, req *xkmstransport.IsRevokedRequest) (*xkmstransport.IsRevokedResponse, error) {
	if req == nil {
		return nil, ErrXKMSCANilRequest
	}
	if req.SerialNumber == "" {
		return nil, ErrXKMSCAMissingSerial
	}

	serial, err := parseHexSerial(req.SerialNumber)
	if err != nil {
		return nil, err
	}

	revoked, err := a.ca.IsRevoked(serial)
	if err != nil {
		return nil, &XKMSCAOperationError{Op: "is revoked", Err: err}
	}

	resp := &xkmstransport.IsRevokedResponse{
		Revoked: revoked,
	}
	if revoked {
		resp.Message = "certificate is revoked"
	} else {
		resp.Message = "certificate is not revoked"
	}
	return resp, nil
}

// parseSANs splits prefixed SAN strings ("DNS:x", "IP:y", "Email:z",
// "URI:u") into a TrustedCASubjectAlternativeNames struct. Entries with
// no prefix default to DNS, matching the go-xkms behaviour.
func parseSANs(raw []string) *xkmssdk.TrustedCASubjectAlternativeNames {
	if len(raw) == 0 {
		return nil
	}
	sans := &xkmssdk.TrustedCASubjectAlternativeNames{}
	for _, s := range raw {
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			continue
		}
		if idx := strings.IndexByte(trimmed, ':'); idx > 0 {
			kind := strings.ToLower(trimmed[:idx])
			value := trimmed[idx+1:]
			switch kind {
			case "dns":
				sans.DNS = append(sans.DNS, value)
			case "ip":
				sans.IPs = append(sans.IPs, value)
			case "email":
				sans.Email = append(sans.Email, value)
			case "uri":
				sans.URIs = append(sans.URIs, value)
			default:
				sans.DNS = append(sans.DNS, trimmed)
			}
			continue
		}
		sans.DNS = append(sans.DNS, trimmed)
	}
	return sans
}

// parseHexSerial parses a hex serial number string, stripping optional
// 0x/0X prefix and returning a typed error on malformed input.
func parseHexSerial(s string) (*big.Int, error) {
	serialStr := strings.TrimPrefix(s, "0x")
	serialStr = strings.TrimPrefix(serialStr, "0X")
	serial := new(big.Int)
	if _, ok := serial.SetString(serialStr, 16); !ok {
		return nil, &XKMSCAInvalidSerialError{Value: s}
	}
	return serial, nil
}

// =============================================================================
// Typed errors
// =============================================================================

// ErrXKMSCANilRequest is returned when a CA operation receives a nil request.
var ErrXKMSCANilRequest = errors.New("xkms ca: request is required")

// ErrXKMSCAMissingCSR is returned when SignCSR is called with empty CSR PEM.
var ErrXKMSCAMissingCSR = errors.New("xkms ca: csr pem is required")

// ErrXKMSCAMissingCommonName is returned when IssueCertificate is called
// without a common name.
var ErrXKMSCAMissingCommonName = errors.New("xkms ca: common name is required")

// ErrXKMSCAMissingSerial is returned when a revocation operation is called
// without a serial number.
var ErrXKMSCAMissingSerial = errors.New("xkms ca: serial number is required")

// XKMSCAOperationError wraps an underlying CA failure with the operation
// name for operator diagnostics.
type XKMSCAOperationError struct {
	// Op is the short operation label ("sign csr", "issue certificate", ...).
	Op string

	// Err is the underlying CA error. Always non-nil.
	Err error
}

// Error implements the error interface.
func (e *XKMSCAOperationError) Error() string {
	return "xkms ca: " + e.Op + ": " + e.Err.Error()
}

// Unwrap exposes the underlying error for errors.Is / errors.As.
func (e *XKMSCAOperationError) Unwrap() error {
	return e.Err
}

// XKMSCAInvalidSerialError is returned when a serial-number hex string
// cannot be decoded.
type XKMSCAInvalidSerialError struct {
	// Value is the malformed serial string.
	Value string
}

// Error implements the error interface.
func (e *XKMSCAInvalidSerialError) Error() string {
	return "xkms ca: invalid serial number format: " + e.Value
}
