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

// ca_service_adapter_extensions.go composes the go-xkms TrustedCA / TCGCA
// verbs behind service.CertificateExtensionsCA so the SDK's
// Security().CertificateExt() surface — CrossSign, Chain and
// IssueDeviceIdentity — reaches a real Certificate Authority on every
// transport.
//
// Nothing cryptographic is implemented here. Cross-signing runs through
// xkms's canonical Issuer.Issue path (the single x509.CreateCertificate call
// site in go-xkms), chain resolution runs through the CA's AIA importer and
// certificate store, and device identity issuance runs through the CA's
// TCG verbs, which perform TCG-CSR-IDEVID verification before signing.

package server

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// =============================================================================
// Interface drift guards
// =============================================================================

// tcgIssuer is the slice of the xkms TCG certificate-authority surface the
// adapter invokes. It is declared locally so the runtime type assertion in
// deviceIdentityIssuer has an explicit, reviewable contract.
//
// A locally-declared interface asserted against a foreign implementation is
// exactly the construct that shipped broken in
// go-xkms/pkg/acme/challenges/enroll-01/ca_compliance.go: a signature drifted
// upstream, the assertion silently stopped matching, TPM IDevID enrollment
// became impossible on every deployment, and the package's own tests kept
// passing because they were written against a mock of the unsatisfiable
// declaration. The assertions immediately below make that failure mode a
// compile error here.
type tcgIssuer interface {
	// IssueEKCertificate issues an Endorsement Key certificate per the TCG
	// EK Credential Profile.
	IssueEKCertificate(ctx context.Context, request *xkmssdk.TrustedCACertificateRequest, ekPubKey crypto.PublicKey) (*x509.Certificate, error)

	// IssueAKCertificate issues an Attestation Key certificate per the TCG
	// specification.
	IssueAKCertificate(ctx context.Context, request *xkmssdk.TrustedCACertificateRequest, pubKey crypto.PublicKey) (*x509.Certificate, error)

	// SignTCGCSRIDevID verifies a TCG-CSR-IDEVID and issues the IAK and
	// IDevID certificates it requests.
	SignTCGCSRIDevID(ctx context.Context, tcgCSR *xkmssdk.TCG_CSR_IDEVID, request *xkmssdk.TrustedCACertificateRequest) (iakDER, idevidDER []byte, err error)
}

var (
	// Any xkms TCGCA must satisfy the narrow surface asserted at runtime. If
	// an upstream TCGCA signature drifts, this fails to build instead of
	// turning every device-identity issuance into a runtime "not supported".
	_ tcgIssuer = (xkmssdk.TCGCA)(nil)

	// The adapter is the CertificateExtensionsCA the service layer resolves.
	_ service.CertificateExtensionsCA = (*caServiceAdapter)(nil)
)

// =============================================================================
// Typed errors
// =============================================================================

var (
	// ErrCADeviceIdentityUnsupported is returned when the configured CA does
	// not implement the xkms TCG certificate-authority surface. Device
	// identity issuance fails closed rather than returning an empty
	// certificate.
	ErrCADeviceIdentityUnsupported = errors.New(
		"server: configured certificate authority does not implement the TCG device identity surface")

	// ErrCAChainIssuerNotFound is returned when a certificate's issuer cannot
	// be resolved from the CA certificate store after the AIA walk.
	ErrCAChainIssuerNotFound = errors.New(
		"server: issuer certificate not found while resolving chain")

	// ErrCAChainTooDeep is returned when a certificate chain exceeds the
	// maximum supported depth, which indicates a cycle or a malformed chain.
	ErrCAChainTooDeep = errors.New("server: certificate chain exceeds maximum depth")

	// ErrCACertStoreUnavailable is returned when chain resolution is
	// requested on a CA that was initialized without a certificate store.
	ErrCACertStoreUnavailable = errors.New(
		"server: certificate authority has no certificate store for chain resolution")

	// ErrCAInvalidPublicKey is returned when a supplied device public key is
	// neither PEM- nor DER-encoded PKIX.
	ErrCAInvalidPublicKey = errors.New("server: invalid device public key encoding")

	// ErrCAInvalidTCGCSR is returned when the supplied TCG-CSR-IDEVID cannot
	// be unmarshalled.
	ErrCAInvalidTCGCSR = errors.New("server: invalid TCG-CSR-IDEVID")
)

// maxChainDepth bounds the issuer walk performed by IssuerChainPEM. It
// matches the hop limit the xkms CA applies to its own root walk.
const maxChainDepth = 10

// =============================================================================
// CrossSignRoot
// =============================================================================

// CrossSignRoot re-certifies a foreign root's subject and public key under the
// local CA, producing a cross-certificate that lets relying parties anchored
// on the local CA validate chains rooted at the foreign root.
//
// The certificate is built by xkms's canonical Issuer.Issue path with the CA
// profile applied, so basic constraints, key usage and the issuer's CRL /
// OCSP / AIA endpoints come from xkms, not from this adapter. The validity
// window is the foreign root's own window clamped to the local CA's — a
// cross-certificate must never outlive its issuer.
//
// The cross-certificate is returned to the caller and is not persisted: the
// CA's cross-signed object store records the inverse direction (certificates
// other issuers minted for THIS CA's root, surfaced by
// Security().Trust().CrossSignedRootPool).
func (a *caServiceAdapter) CrossSignRoot(ctx context.Context, foreignRootPEM []byte) ([]byte, error) {
	if len(foreignRootPEM) == 0 {
		return nil, ErrCAServiceAdapterInvalidPEM
	}

	ca := a.getCA()
	if ca == nil {
		return nil, ErrCAServiceAdapterNilCA
	}

	foreignRoot, err := parseCertificatePEM(foreignRootPEM)
	if err != nil {
		return nil, err
	}

	signer, err := ca.Signer(ctx)
	if err != nil {
		return nil, err
	}

	issuerCert, err := ca.CACertificate()
	if err != nil {
		return nil, err
	}

	serial, err := a.serialGenerator().Generate(ctx)
	if err != nil {
		return nil, err
	}

	notBefore, notAfter := crossSignValidity(foreignRoot, issuerCert)

	issuer := &xkmssdk.Issuer{
		Signer:      signer,
		Certificate: issuerCert,
		Identity:    ca.Config(),
	}

	spec := &xkmssdk.CertificateSpec{
		Serial:    serial,
		Subject:   foreignRoot.Subject,
		NotBefore: notBefore,
		NotAfter:  notAfter,
		PublicKey: foreignRoot.PublicKey,
		Profile:   xkmssdk.NewCAProfile(),
		Request: &xkmssdk.TrustedCACertificateRequest{
			Subject: xkmssdk.TrustedCASubject{
				CommonName: foreignRoot.Subject.CommonName,
			},
			IsCA:       true,
			MaxPathLen: foreignRoot.MaxPathLen,
		},
	}

	crossSigned, err := issuer.Issue(rand.Reader, spec)
	if err != nil {
		return nil, err
	}

	return pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: crossSigned.Raw,
	}), nil
}

// crossSignValidity returns the validity window for a cross-certificate: the
// foreign root's own window, never starting before nor ending after the
// issuing CA's certificate is itself valid.
func crossSignValidity(foreignRoot, issuerCert *x509.Certificate) (notBefore, notAfter time.Time) {
	notBefore = foreignRoot.NotBefore
	if issuerCert.NotBefore.After(notBefore) {
		notBefore = issuerCert.NotBefore
	}
	notAfter = foreignRoot.NotAfter
	if issuerCert.NotAfter.Before(notAfter) {
		notAfter = issuerCert.NotAfter
	}
	return notBefore, notAfter
}

// =============================================================================
// IssuerChainPEM
// =============================================================================

// IssuerChainPEM resolves the issuer chain for certPEM and returns it ordered
// issuer-first, root-last. The leaf is not included.
//
// Resolution first runs the CA's Authority Information Access walk
// (ImportIssuingCAs), which downloads and imports every issuer the
// certificate's AIA extension points at, then walks the certificate store
// upwards by issuer Common Name until a self-signed certificate is reached.
// A self-signed input has no issuers and yields an empty chain.
func (a *caServiceAdapter) IssuerChainPEM(ctx context.Context, certPEM []byte) ([][]byte, error) {
	if len(certPEM) == 0 {
		return nil, ErrCAServiceAdapterInvalidPEM
	}

	ca := a.getCA()
	if ca == nil {
		return nil, ErrCAServiceAdapterNilCA
	}

	leaf, err := parseCertificatePEM(certPEM)
	if err != nil {
		return nil, err
	}

	// Canonical AIA walk: populates the certificate store with any issuer the
	// certificate names but the store does not yet hold.
	if err := ca.ImportIssuingCAs(ctx, leaf); err != nil {
		return nil, err
	}

	store := ca.CertStore()
	if store == nil {
		return nil, ErrCACertStoreUnavailable
	}

	chain := make([][]byte, 0, 2)
	current := leaf
	for hop := 0; hop < maxChainDepth; hop++ {
		if current.Issuer.CommonName == current.Subject.CommonName {
			return chain, nil
		}
		issuer, err := store.GetCertificate(current.Issuer.CommonName)
		if err != nil || issuer == nil {
			return nil, ErrCAChainIssuerNotFound
		}
		chain = append(chain, pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: issuer.Raw,
		}))
		current = issuer
	}
	return nil, ErrCAChainTooDeep
}

// =============================================================================
// IssueDeviceIdentityCert
// =============================================================================

// deviceIdentityHandler issues one flavor of TCG device identity certificate.
type deviceIdentityHandler func(
	ctx context.Context,
	tcg tcgIssuer,
	req *service.DeviceIdentityIssuance,
	certReq *xkmssdk.TrustedCACertificateRequest,
) (*x509.Certificate, error)

// deviceIdentityHandlers dispatches on the requested identity flavor in
// constant time. The service layer has already validated that the flavor is
// known and that its required inputs are present.
var deviceIdentityHandlers = map[string]deviceIdentityHandler{
	service.DeviceIdentityTypeEK:     issueEKIdentity,
	service.DeviceIdentityTypeAK:     issueAKIdentity,
	service.DeviceIdentityTypeIDevID: issueIDevIDIdentity,
}

// IssueDeviceIdentityCert issues a TCG device identity certificate (EK, AK or
// IDevID) from the composed CA.
//
// The CA must implement the xkms TCG surface; when it does not — the
// interface-drift case — issuance fails closed with
// ErrCADeviceIdentityUnsupported rather than returning an unsigned or empty
// result.
func (a *caServiceAdapter) IssueDeviceIdentityCert(ctx context.Context, req *service.DeviceIdentityIssuance) (*service.IssuedDeviceIdentity, error) {
	if req == nil {
		return nil, ErrCAServiceAdapterNilRequest
	}

	ca := a.getCA()
	if ca == nil {
		return nil, ErrCAServiceAdapterNilCA
	}

	tcg, ok := ca.(tcgIssuer)
	if !ok {
		return nil, ErrCADeviceIdentityUnsupported
	}

	handler, known := deviceIdentityHandlers[req.Type]
	if !known {
		return nil, service.ErrCertExtUnknownIdentityType
	}

	certReq := &xkmssdk.TrustedCACertificateRequest{
		Subject: xkmssdk.TrustedCASubject{
			CommonName: req.DeviceID,
		},
		Valid:       req.ValidityDays,
		PermanentID: req.DeviceID,
	}

	cert, err := handler(ctx, tcg, req, certReq)
	if err != nil {
		return nil, err
	}

	chain, _ := ca.CABundle()

	issued := &service.IssuedDeviceIdentity{
		CertificatePEM: pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: cert.Raw,
		}),
		ChainPEM:  chain,
		NotBefore: cert.NotBefore.UTC(),
		NotAfter:  cert.NotAfter.UTC(),
	}
	if cert.SerialNumber != nil {
		issued.SerialNumber = cert.SerialNumber.Text(16)
	}
	return issued, nil
}

// issueEKIdentity issues an Endorsement Key certificate over the supplied EK
// public key.
func issueEKIdentity(
	ctx context.Context,
	tcg tcgIssuer,
	req *service.DeviceIdentityIssuance,
	certReq *xkmssdk.TrustedCACertificateRequest,
) (*x509.Certificate, error) {
	pub, err := parsePublicKey(req.EKPub)
	if err != nil {
		return nil, err
	}
	return tcg.IssueEKCertificate(ctx, certReq, pub)
}

// issueAKIdentity issues an Attestation Key certificate over the supplied AK
// public key.
func issueAKIdentity(
	ctx context.Context,
	tcg tcgIssuer,
	req *service.DeviceIdentityIssuance,
	certReq *xkmssdk.TrustedCACertificateRequest,
) (*x509.Certificate, error) {
	pub, err := parsePublicKey(req.AKPub)
	if err != nil {
		return nil, err
	}
	return tcg.IssueAKCertificate(ctx, certReq, pub)
}

// issueIDevIDIdentity verifies the supplied TCG-CSR-IDEVID and issues the
// IEEE 802.1AR IDevID certificate it requests. Verification — the CSR
// signature and the TPM attestation invariants binding the IAK to the IDevID
// key — is performed by the CA inside SignTCGCSRIDevID; this adapter never
// signs an unverified CSR.
func issueIDevIDIdentity(
	ctx context.Context,
	tcg tcgIssuer,
	req *service.DeviceIdentityIssuance,
	certReq *xkmssdk.TrustedCACertificateRequest,
) (*x509.Certificate, error) {
	tcgCSR, err := xkmssdk.UnmarshalIDevIDCSR(unwrapPEM(req.CSR))
	if err != nil {
		return nil, ErrCAInvalidTCGCSR
	}

	_, idevidDER, err := tcg.SignTCGCSRIDevID(ctx, tcgCSR, certReq)
	if err != nil {
		return nil, err
	}

	cert, err := x509.ParseCertificate(idevidDER)
	if err != nil {
		return nil, ErrCAServiceAdapterInvalidPEM
	}
	return cert, nil
}

// =============================================================================
// Helpers
// =============================================================================

// serialGenerator returns the adapter's serial generator, which produces the
// 128-bit random serials xkms uses for every certificate it issues.
func (a *caServiceAdapter) serialGenerator() xkmssdk.SerialGenerator {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.serialGen == nil {
		a.serialGen = xkmssdk.NewSerialGenerator(xkmssdk.NewMemoryStorage())
	}
	return a.serialGen
}

// parseCertificatePEM decodes the first CERTIFICATE block in pemBytes.
// DER input is accepted as well so callers that hand over raw bytes are not
// rejected on an encoding technicality.
func parseCertificatePEM(pemBytes []byte) (*x509.Certificate, error) {
	if block, _ := pem.Decode(pemBytes); block != nil {
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, ErrCAServiceAdapterInvalidPEM
		}
		return cert, nil
	}
	cert, err := x509.ParseCertificate(pemBytes)
	if err != nil {
		return nil, ErrCAServiceAdapterInvalidPEM
	}
	return cert, nil
}

// parsePublicKey decodes a PKIX public key from PEM or raw DER.
func parsePublicKey(keyBytes []byte) (crypto.PublicKey, error) {
	if len(keyBytes) == 0 {
		return nil, ErrCAInvalidPublicKey
	}
	der := unwrapPEM(keyBytes)
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, ErrCAInvalidPublicKey
	}
	return pub, nil
}

// unwrapPEM returns the DER payload of the first PEM block in data, or data
// unchanged when it is not PEM-encoded. A TCG-CSR-IDEVID is a packed binary
// blob, so callers may legitimately supply either form.
func unwrapPEM(data []byte) []byte {
	if block, _ := pem.Decode(data); block != nil {
		return block.Bytes
	}
	return data
}
