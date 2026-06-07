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
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"strings"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"

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

	// ErrCAServiceAdapterCertStoreUnavailable is returned when the CA
	// has been initialized without a certificate store backend (callers
	// cannot enumerate certificates in that mode).
	ErrCAServiceAdapterCertStoreUnavailable = errors.New("server: ca service adapter has no certificate store")

	// ErrCAServiceAdapterUnsupportedAlgorithm is returned when an IssueCertificate
	// request names a key algorithm the adapter cannot generate.
	ErrCAServiceAdapterUnsupportedAlgorithm = errors.New("server: ca service adapter received an unsupported key algorithm")

	// ErrCAServiceAdapterNoRoot is returned when the cross-signed root pool
	// cannot be resolved because the CA has no materialised root certificate.
	ErrCAServiceAdapterNoRoot = errors.New("server: ca service adapter has no root certificate")
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

	// spiffeTrustDomain is the operator-configured SPIFFE trust domain. When
	// non-empty alongside a request TenantID, the adapter instructs the
	// embedded CA to stamp a tenant-scoped SPIFFE URI SAN
	// (spiffe://<trust-domain>/tenant/<tenantID>/<kind>/<name>) onto the
	// issued leaf so mTLS/PIV login can derive the tenant from the verified
	// peer certificate. Empty disables SPIFFE SAN synthesis (single-domain
	// / pre-broker deployments).
	spiffeTrustDomain string
}

// newCAServiceAdapter constructs an adapter that exposes a TrustedCA via
// the xkmstransport CAService method set. Returns ErrCAServiceAdapterNilCA
// when ca is nil.
// Compile-time assertion that the adapter satisfies the service-layer
// TrustCA capability, so the QuicBaaS builder can wire Security().Trust()
// off the same CA instance used for certificate operations.
var _ service.TrustCA = (*caServiceAdapter)(nil)

// Compile-time assertion that the adapter satisfies the service-layer
// ACMECA capability, so the QuicBaaS ACME servicer can perform server-side
// issuance off the same CA instance used for certificate operations.
var _ service.ACMECA = (*caServiceAdapter)(nil)

func newCAServiceAdapter(ca xkmssdk.TrustedCA) (*caServiceAdapter, error) {
	if ca == nil {
		return nil, ErrCAServiceAdapterNilCA
	}
	return &caServiceAdapter{ca: ca}, nil
}

// SetSPIFFETrustDomain records the operator-configured SPIFFE trust domain
// used to synthesize tenant-scoped SPIFFE URI SANs on issued certificates.
// Passing an empty string disables SPIFFE SAN synthesis. Safe to call before
// the adapter is handed to the service builder.
func (a *caServiceAdapter) SetSPIFFETrustDomain(trustDomain string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.spiffeTrustDomain = trustDomain
}

// getSPIFFETrustDomain returns the configured SPIFFE trust domain under a
// read lock.
func (a *caServiceAdapter) getSPIFFETrustDomain() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.spiffeTrustDomain
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

// IssueCertificate issues a new certificate from the wrapped CA, generating a
// fresh keypair in-process and returning the PEM-encoded private key alongside
// the signed certificate.
//
// The keypair is generated here rather than inside the TrustedCA keystore on
// purpose: keys created through the keystore are wrapped as non-exportable
// opaque signers (the composite backend never returns raw key material), so
// the "ca issue" contract — hand the requester both the certificate and its
// private key — can only be honoured by owning the key locally. The key is
// used to build a CSR, the CSR is signed by the CA, and only the signed leaf
// is persisted in the certificate store; the private key is returned to the
// caller and never retained by the server.
func (a *caServiceAdapter) IssueCertificate(
	ctx context.Context,
	req *xkmstransport.IssueCertificateRequest,
) (*xkmstransport.IssueCertificateResponse, error) {

	if req == nil {
		return nil, ErrCAServiceAdapterNilRequest
	}

	signer, keyAlgorithm, err := generateLeafKey(req.Algorithm)
	if err != nil {
		return nil, err
	}

	csrReq := &xkmssdk.TrustedCACertificateRequest{
		Subject: xkmssdk.TrustedCASubject{
			CommonName:   req.CommonName,
			Organization: req.Organization,
		},
		SANS:  parseTransportSANs(req.SANs),
		Valid: req.ValidityDays,
	}

	csrPEM, err := xkmssdk.CreateCSRWithKey(csrReq, signer)
	if err != nil {
		return nil, err
	}

	opts := a.signOptionsWithSPIFFE(req.Profile, req.ValidityDays, req.TenantID)

	cert, err := a.getCA().SignCSR(csrPEM, opts)
	if err != nil {
		return nil, err
	}

	keyPEM, err := xkmssdk.EncodePrivateKeyPEM(signer, keyAlgorithm, nil)
	if err != nil {
		return nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})

	chain, _ := a.getCA().CABundle()

	resp := &xkmstransport.IssueCertificateResponse{
		CertificatePEM: certPEM,
		ChainPEM:       chain,
		PrivateKeyPEM:  keyPEM,
	}
	if cert.SerialNumber != nil {
		resp.SerialNumber = cert.SerialNumber.Text(16)
	}
	return resp, nil
}

// generateLeafKey produces a fresh signer for a leaf certificate based on the
// requested algorithm string. The accepted spellings mirror the documented
// xkmstransport.IssueCertificateRequest.Algorithm values plus the CLI's
// shorthand ("ecdsa", "rsa"). An empty algorithm defaults to ECDSA P-256,
// matching the TrustedCA's own default. The returned x509.PublicKeyAlgorithm
// drives PEM encoding of the private key.
func generateLeafKey(algorithm string) (crypto.Signer, x509.PublicKeyAlgorithm, error) {
	switch strings.ToLower(strings.TrimSpace(algorithm)) {
	case "", "ecdsa", "ecdsa-p256", "ecdsap256", "p256":
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, x509.UnknownPublicKeyAlgorithm, err
		}
		return key, x509.ECDSA, nil
	case "ecdsa-p384", "ecdsap384", "p384":
		key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		if err != nil {
			return nil, x509.UnknownPublicKeyAlgorithm, err
		}
		return key, x509.ECDSA, nil
	case "rsa", "rsa2048", "rsa-2048":
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, x509.UnknownPublicKeyAlgorithm, err
		}
		return key, x509.RSA, nil
	case "rsa3072", "rsa-3072":
		key, err := rsa.GenerateKey(rand.Reader, 3072)
		if err != nil {
			return nil, x509.UnknownPublicKeyAlgorithm, err
		}
		return key, x509.RSA, nil
	case "rsa4096", "rsa-4096":
		key, err := rsa.GenerateKey(rand.Reader, 4096)
		if err != nil {
			return nil, x509.UnknownPublicKeyAlgorithm, err
		}
		return key, x509.RSA, nil
	case "ed25519":
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, x509.UnknownPublicKeyAlgorithm, err
		}
		return key, x509.Ed25519, nil
	default:
		return nil, x509.UnknownPublicKeyAlgorithm, ErrCAServiceAdapterUnsupportedAlgorithm
	}
}

// signOptionsWithSPIFFE builds the TrustedCASignOptions for a single signing
// operation, folding in the operator-configured SPIFFE trust domain, the
// request tenant, and the SPIFFE workload kind derived from the certificate
// profile. When the trust domain or tenant is empty the CA omits the SPIFFE
// SAN (single-domain / pre-broker mode). Returns nil when there is nothing to
// configure so the CA applies its own profile defaults unchanged.
func (a *caServiceAdapter) signOptionsWithSPIFFE(profile string, validityDays int, tenantID string) *xkmssdk.TrustedCASignOptions {
	trustDomain := a.getSPIFFETrustDomain()
	spiffeRequested := trustDomain != "" && tenantID != ""
	if profile == "" && validityDays <= 0 && !spiffeRequested {
		return nil
	}
	opts := &xkmssdk.TrustedCASignOptions{
		Profile:      profile,
		ValidityDays: validityDays,
	}
	if spiffeRequested {
		opts.SPIFFETrustDomain = trustDomain
		opts.TenantID = tenantID
		opts.SPIFFEKind = service.SPIFFEKindForProfile(profile)
	}
	return opts
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

	opts := a.signOptionsWithSPIFFE(req.Profile, req.ValidityDays, req.TenantID)

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

// SignCSRPEM signs a PEM-encoded CSR with the wrapped CA and returns the
// issued leaf certificate plus the CA chain, both PEM-encoded. It satisfies
// the service.ACMECA capability used by Security().ACME().Server().Issue:
// the ACME servicer hands the validated CSR here for server-side issuance.
//
// The default sign profile is applied (no SPIFFE synthesis): ACME issuance is
// driven by the requester's CSR identifiers, not a tenant-scoped login leaf.
func (a *caServiceAdapter) SignCSRPEM(_ context.Context, csrPEM []byte) (certPEM, chainPEM []byte, err error) {
	if len(csrPEM) == 0 {
		return nil, nil, ErrCAServiceAdapterInvalidPEM
	}
	ca := a.getCA()
	if ca == nil {
		return nil, nil, ErrCAServiceAdapterNilCA
	}
	cert, err := ca.SignCSR(csrPEM, nil)
	if err != nil {
		return nil, nil, err
	}
	chain, _ := ca.CABundle()
	certPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})
	return certPEM, chain, nil
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

// ListStoredCertificates enumerates every certificate currently
// stored in the wrapped CA's certificate store. Satisfies the
// service.CertStoreLister capability used by
// CertificateService.ListCertificates.
//
// The method is named ListStoredCertificates rather than
// ListCertificates so the adapter can ALSO satisfy
// xkmstransport.Client (whose ListCertificates method has a different
// signature and semantics — it enumerates key-store certs by backend).
//
// The context parameter is currently unused — the CertStore interface
// does not accept one — but it is part of the signature so the method
// can be augmented with cancellation in a future revision without
// breaking callers.
func (a *caServiceAdapter) ListStoredCertificates(_ context.Context) ([]*x509.Certificate, error) {
	ca := a.getCA()
	if ca == nil {
		return nil, ErrCAServiceAdapterNilCA
	}
	store := ca.CertStore()
	if store == nil {
		return nil, ErrCAServiceAdapterCertStoreUnavailable
	}
	return store.ListCertificates()
}

// CrossSignedRootPEMs returns the cross-sign-aware root set for the active
// CA as PEM-encoded certificates, satisfying service.TrustCA. The set is
// the native root anchor followed by every cross-signed root recorded for
// that root's subject CN. This mirrors the membership of the pool returned
// by TrustedCA.TrustedRootCertPoolWithXSigner without depending on
// x509.CertPool internals (which expose no enumeration of their members).
//
// A missing cross-signed set is NOT an error: a CA that has never been
// cross-signed legitimately returns just its native root.
func (a *caServiceAdapter) CrossSignedRootPEMs() ([][]byte, error) {
	ca := a.getCA()
	if ca == nil {
		return nil, ErrCAServiceAdapterNilCA
	}

	// Resolve the root anchor. Prefer the explicit TrustedRoot; fall back
	// to the issuing CA leaf when the hierarchy is single-tier (the CA is
	// its own root).
	root, err := ca.TrustedRoot()
	if err != nil || root == nil {
		root, err = ca.CACertificate()
		if err != nil {
			return nil, ErrCAServiceAdapterNoRoot
		}
	}

	out := make([][]byte, 0, 2)
	out = append(out, pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: root.Raw,
	}))

	// Append any cross-signed roots recorded for the root's subject CN.
	// CrossSignedPEM returns a not-found error when none exist; that is a
	// normal "no cross-signature" condition, not a failure, so it is
	// folded into the native-root-only result.
	xsignedPEM, xerr := ca.CrossSignedPEM(&xkmssdk.KeyAttributes{CN: root.Subject.CommonName})
	if xerr == nil && len(xsignedPEM) > 0 {
		for _, der := range splitPEMCertificates(xsignedPEM) {
			out = append(out, pem.EncodeToMemory(&pem.Block{
				Type:  "CERTIFICATE",
				Bytes: der,
			}))
		}
	}

	return out, nil
}

// ImportIssuingCAs recursively imports the issuing CA certificates
// referenced by cert's AIA extension into the certificate store,
// satisfying service.TrustCA. It delegates directly to the wrapped
// TrustedCA, which owns the canonical AIA-walk implementation.
func (a *caServiceAdapter) ImportIssuingCAs(cert *x509.Certificate) error {
	ca := a.getCA()
	if ca == nil {
		return ErrCAServiceAdapterNilCA
	}
	return ca.ImportIssuingCAs(cert)
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
