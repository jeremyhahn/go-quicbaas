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

package service

// TODO(quicbaas/xkms-acme): For hardware-backed fleet node identities (TPM EK/AK
// attested enrollment), migrate from NewSDKFleetNodeCertIssuer to an
// ACME-enroll01 / endorse01 flow using go-xkms's ACME client
// (github.com/jeremyhahn/go-xkms/sdk/go/acme). The FleetNodeCertIssuer contract
// stays unchanged — only the concrete implementation swaps out.

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// =============================================================================
// Fleet node Certificate Issuer
// =============================================================================
//
// FleetNodeCertIssuer produces X.509 identity certificates for QuicBaaS fleet node
// daemons. The certificate is presented on the TLS handshake when the daemon
// calls POST /api/v1/fleet/enroll, replacing the legacy one-shot
// enrollment token as the authentication bootstrap.
//
// Production issuance flows through the server-embedded xkmssdk.TrustedCA —
// the same CA that signs the server's TLS certificate. The daemon and the
// control plane therefore share a single trust root without any operator key
// distribution ceremony.

// FleetNodeCertificateSubject captures the optional subject overrides a caller
// can provide. All fields are optional; sensible defaults are applied by
// the issuer when a field is left zero.
type FleetNodeCertificateSubject struct {
	// CommonName overrides the default "agent-<hostname>" CN. Empty falls
	// back to the hostname-derived default.
	CommonName string

	// Organization sets the X.509 O= value. Empty falls back to the tenant
	// identifier, which the server consumes as the daemon's tenant claim.
	Organization string

	// OrganizationalUnit sets the X.509 OU= value. Empty falls back to
	// "agents" — the server rejects certificates that are not stamped with
	// this OU (defence-in-depth against operator misuse of a client cert
	// that was minted for another purpose).
	OrganizationalUnit string
}

// FleetNodeCertificateRequest captures the inputs to IssueFleetNodeCertificate.
type FleetNodeCertificateRequest struct {
	// TenantID is the tenant the daemon serves. Required. Encoded into the
	// certificate's Organization field so the server can extract the tenant
	// claim from the mTLS peer certificate.
	TenantID string

	// Hostname identifies the target host. Used for the default CN and for
	// DNS SANs so the control plane can confirm it is issuing a cert for the
	// advertised host. Required.
	Hostname string

	// Subject optionally overrides the derived subject values. Empty fields
	// fall back to the defaults above.
	Subject FleetNodeCertificateSubject

	// ValidityDays bounds the certificate lifetime. Zero falls back to the
	// default (365 days).
	ValidityDays int

	// ExtraDNSNames are appended to the default DNS SAN list (hostname).
	// Useful when the daemon is reachable under multiple names.
	ExtraDNSNames []string

	// SPIFFETrustDomain is the operator-configured SPIFFE trust domain
	// (config iam.spiffe_trust_domain). When set, the issued certificate
	// carries the tenant-scoped SPIFFE URI SAN
	// spiffe://<trust-domain>/tenant/<TenantID>/svc/<CommonName>.
	//
	// That SAN is not decoration: go-qrdb derives an mTLS identity's TENANT
	// from it and falls back to the first Subject OU when it is absent
	// (go-qrdb pkg/auth MTLSAuthenticator.TenantFromCert). Every fleet node
	// certificate carries OU=fleet nodes, so a certificate issued without the SAN
	// authenticates into a phantom tenant literally named "agents" -- no
	// enrollment in the real tenant can ever authorize it. A caller that needs
	// the issued identity to hold authority MUST set this.
	//
	// Empty omits the SAN, which is correct only for a certificate used purely
	// as a TLS credential and never as an authorization principal.
	SPIFFETrustDomain string
}

// Validate checks that the request carries the minimum required fields.
func (r *FleetNodeCertificateRequest) Validate() error {
	if r == nil {
		return ErrFleetNodeCertRequestNil
	}
	if strings.TrimSpace(r.TenantID) == "" {
		return &FleetNodeCertRequestFieldError{Field: "tenant_id"}
	}
	if strings.TrimSpace(r.Hostname) == "" {
		return &FleetNodeCertRequestFieldError{Field: "hostname"}
	}
	return nil
}

// IssuedFleetNodeCertificate is the output of a successful IssueFleetNodeCertificate.
//
// CertPEM and CAPEM are plain []byte — they are not secrets. PrivateKey is
// held in a *secure.Buffer so the caller can upload it and then call
// Destroy() to zero the mlocked page immediately, leaving no plaintext key
// material on the operator's machine.
type IssuedFleetNodeCertificate struct {
	// CertPEM is the PEM-encoded leaf certificate.
	CertPEM []byte

	// PrivateKey is the PEM-encoded private key material, wrapped in a
	// mlocked Buffer. Callers MUST defer Destroy() the moment the key has
	// been written to its destination (file upload, in-memory keystore,
	// etc.) so the plaintext never lingers in resident memory.
	PrivateKey *secure.Buffer

	// CAPEM is the PEM-encoded CA bundle (root + any intermediates) the
	// daemon must trust when validating the server's TLS handshake. It is
	// the same bundle the server itself publishes via TrustedCA.CABundle.
	CAPEM []byte

	// ParsedCert exposes the parsed x509.Certificate for callers that need
	// to log the serial number, SANs, etc. without re-parsing CertPEM.
	ParsedCert *x509.Certificate
}

// FleetNodeCertIssuer produces mTLS client identities for fleet node daemons.
type FleetNodeCertIssuer interface {
	// IssueFleetNodeCertificate generates a fresh keypair and signs a client
	// certificate for the fleet node. The private key is returned in a
	// *secure.Buffer that the caller MUST destroy once the material has
	// been persisted at its destination.
	IssueFleetNodeCertificate(ctx context.Context, req *FleetNodeCertificateRequest) (*IssuedFleetNodeCertificate, error)
}

// =============================================================================
// XKMS-backed implementation
// =============================================================================

// DefaultFleetNodeCertValidityDays is the default cert lifetime when the caller
// does not set ValidityDays. 365 days balances rotation cadence against the
// deploy-service UX (operators should not have to re-deploy every week).
const DefaultFleetNodeCertValidityDays = 365

// DefaultFleetNodeCertOU is the Organizational Unit stamped on fleet node certs when
// the caller does not override it. The control plane's register handler requires
// this OU and rejects any other value — a belt-and-braces check that the
// certificate was minted for fleet node authentication and not repurposed from a
// cert the same CA issued for a different role.
const DefaultFleetNodeCertOU = "agents"

// XKMSFleetNodeCertIssuer implements FleetNodeCertIssuer on top of the server-
// embedded xkmssdk.TrustedCA. The CA is injected through the constructor so
// unit tests can substitute a stub without touching disk.
type XKMSFleetNodeCertIssuer struct {
	ca xkmssdk.TrustedCA
}

// Compile-time interface check.
var (
	_ FleetNodeCertIssuer = (*XKMSFleetNodeCertIssuer)(nil)
	_ FleetCABundleSource = (*XKMSFleetNodeCertIssuer)(nil)
)

// NewXKMSFleetNodeCertIssuer wires the issuer around the provided CA instance.
// The CA must already be initialized (Load() or Init() returned success) —
// the issuer never calls those lifecycle methods itself.
func NewXKMSFleetNodeCertIssuer(ca xkmssdk.TrustedCA) (*XKMSFleetNodeCertIssuer, error) {
	if ca == nil {
		return nil, ErrFleetNodeCertIssuerNilCA
	}
	return &XKMSFleetNodeCertIssuer{ca: ca}, nil
}

// IssueFleetNodeCertificate implements FleetNodeCertIssuer.IssueFleetNodeCertificate.
func (i *XKMSFleetNodeCertIssuer) IssueFleetNodeCertificate(ctx context.Context, req *FleetNodeCertificateRequest) (*IssuedFleetNodeCertificate, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	cn := strings.TrimSpace(req.Subject.CommonName)
	if cn == "" {
		cn = "agent-" + strings.TrimSpace(req.Hostname)
	}

	org := strings.TrimSpace(req.Subject.Organization)
	if org == "" {
		org = req.TenantID
	}

	ou := strings.TrimSpace(req.Subject.OrganizationalUnit)
	if ou == "" {
		ou = DefaultFleetNodeCertOU
	}

	validity := req.ValidityDays
	if validity <= 0 {
		validity = DefaultFleetNodeCertValidityDays
	}

	// Build the DNS SAN list: the hostname always, plus any extras the
	// caller supplied (e.g. a short alias or a LAN FQDN).
	dns := []string{req.Hostname}
	for _, extra := range req.ExtraDNSNames {
		trimmed := strings.TrimSpace(extra)
		if trimmed == "" || trimmed == req.Hostname {
			continue
		}
		dns = append(dns, trimmed)
	}

	// The fleet node's key pair is the AGENT's credential, not platform key material,
	// so it is created at the point of custody and the CA only ever sees the CSR
	// -- go-xkms's own rule for this shape of issuance (pkg/init
	// firstboot_adapter.go GenerateAdminCert), and the only shape that can work:
	// TrustedCA.IssueCertificate keeps the key it generates inside the keystore
	// as a non-exportable opaque handle and returns IssuedCertificate
	// .PrivateKeyPEM as nil on every backend. This issuer previously called it
	// and then required that field, so it failed on every invocation against a
	// real CA -- silently, because its only caller logs and continues
	// (go-quicbaas#910).
	//
	// Every crypto step is go-xkms's: the key comes from the single audited
	// generator in pkg/xkms (go-xkms FIPS RNG), the CSR from go-xkms
	// CreateCSRWithKey, the signature from the go-xkms CA, and the key encoding
	// from go-xkms EncodePrivateKeyPEM.
	signer, keyAlgorithm, err := dbxkms.GenerateRequesterKey(dbxkms.KeyAlgorithmECDSAP256)
	if err != nil {
		return nil, &FleetNodeCertIssueError{Op: "generate agent keypair", Cause: err}
	}

	csrPEM, err := xkmssdk.CreateCSRWithKey(&xkmssdk.TrustedCACertificateRequest{
		Subject: xkmssdk.TrustedCASubject{
			CommonName:         cn,
			Organization:       org,
			OrganizationalUnit: ou,
		},
		SANS: &xkmssdk.TrustedCASubjectAlternativeNames{
			DNS: dns,
		},
	}, signer)
	if err != nil {
		return nil, &FleetNodeCertIssueError{Op: "build agent csr", Cause: err}
	}

	// ExtKeyUsage is set explicitly rather than by naming a profile so the issued
	// certificate carries exactly clientAuth, which is what the control plane's
	// register handler requires of a fleet node identity.
	//
	// Bind the certificate to its tenant with a SPIFFE URI SAN when the operator
	// has declared a trust domain. The CA owns the single synthesis site
	// (go-xkms pkg/ca signCSR -> spiffe.BuildURI); we only forward the inputs,
	// so this path and every other CSR-sign path cannot drift.
	//
	// The workload kind is "svc": a fleet node is a machine identity, which is
	// exactly what go-qrdb's spiffe.KindService names. Nothing in the stack keys
	// off the kind -- TenantFromCert accepts either -- so the choice is about
	// what the SAN SAYS to whoever reads it.
	signOpts := &xkmssdk.TrustedCASignOptions{
		ValidityDays: validity,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	if trustDomain := strings.TrimSpace(req.SPIFFETrustDomain); trustDomain != "" {
		signOpts.SPIFFETrustDomain = trustDomain
		signOpts.TenantID = req.TenantID
		signOpts.SPIFFEKind = qrdbsdk.SpiffeKindService
		signOpts.SPIFFEName = cn
	}

	cert, err := i.ca.SignCSR(ctx, csrPEM, signOpts)
	if err != nil {
		return nil, &FleetNodeCertIssueError{Op: "issue agent certificate", Cause: err}
	}
	if cert == nil {
		return nil, &FleetNodeCertIssueError{Op: "issue agent certificate", Cause: errors.New("ca returned nil certificate")}
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})

	keyPEM, err := xkmssdk.EncodePrivateKeyPEM(signer, keyAlgorithm, nil)
	if err != nil {
		return nil, &FleetNodeCertIssueError{Op: "encode agent private key", Cause: err}
	}

	// Move the PEM into an mlocked buffer immediately; NewBufferFromBytes wipes
	// the source slice so the raw key never lingers on the heap.
	keyBuf, err := secure.NewBufferFromBytes(keyPEM)
	if err != nil {
		return nil, &FleetNodeCertIssueError{Op: "secure private key", Cause: err}
	}

	// Assemble the CA bundle the daemon must trust. The CA's CABundle() returns
	// the full chain (root + intermediates); fall back to the CA's own
	// certificate when it cannot produce a bundle (e.g. single-identity test
	// CAs).
	caPEM, bundleErr := i.ca.CABundle()
	if bundleErr != nil || len(caPEM) == 0 {
		caPEM = i.caCertificatePEM()
	}
	if len(caPEM) == 0 {
		keyBuf.Destroy()
		return nil, &FleetNodeCertIssueError{Op: "resolve ca bundle", Cause: errors.New("ca bundle is empty")}
	}

	return &IssuedFleetNodeCertificate{
		CertPEM:    certPEM,
		PrivateKey: keyBuf,
		CAPEM:      append([]byte(nil), caPEM...),
		ParsedCert: cert,
	}, nil
}

// FleetCABundle implements FleetCABundleSource: the same bundle
// IssueFleetNodeCertificate hands the daemon, without issuing anything.
func (i *XKMSFleetNodeCertIssuer) FleetCABundle(_ context.Context, _ string) ([]byte, error) {
	caPEM, err := i.ca.CABundle()
	if err != nil || len(caPEM) == 0 {
		caPEM = i.caCertificatePEM()
	}
	if len(caPEM) == 0 {
		return nil, &FleetNodeCertIssueError{Op: "resolve ca bundle", Cause: errors.New("ca bundle is empty")}
	}
	return caPEM, nil
}

// caCertificatePEM returns the CA's own certificate PEM, or nil when the CA
// cannot produce one. Used only as the CABundle fallback.
func (i *XKMSFleetNodeCertIssuer) caCertificatePEM() []byte {
	caCert, err := i.ca.CACertificate()
	if err != nil || caCert == nil {
		return nil
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCert.Raw})
}

// =============================================================================
// SDK-backed implementation
// =============================================================================

// SDKFleetNodeCertIssuer implements FleetNodeCertIssuer by calling an xkmssdk.Client
// CA service (CAService interface from go-xkms/pkg/api/transport). Unlike
// XKMSFleetNodeCertIssuer (which holds a direct TrustedCA reference), this
// variant is transport-agnostic — it works the same whether the SDK client
// is speaking gRPC, REST, QUIC, Unix socket, MCP, or the embedded backend.
//
// Use SDKFleetNodeCertIssuer from CLI / remote fleet node-deploy workflows that only
// have an SDK client handle (no in-process CA). Use XKMSFleetNodeCertIssuer
// from code paths that already embed the CA (desktop/Wails GUI, platformd).
type SDKFleetNodeCertIssuer struct {
	ca xkmstransport.CAService
}

// Compile-time interface check.
var (
	_ FleetNodeCertIssuer = (*SDKFleetNodeCertIssuer)(nil)
	_ FleetCABundleSource = (*SDKFleetNodeCertIssuer)(nil)
)

// NewSDKFleetNodeCertIssuer wires the SDK-backed issuer around any
// CAService-capable client. xkmssdk.Client satisfies this via its embedded
// CAService methods, so callers can pass the whole SDK client directly.
func NewSDKFleetNodeCertIssuer(ca xkmstransport.CAService) (*SDKFleetNodeCertIssuer, error) {
	if ca == nil {
		return nil, ErrFleetNodeCertIssuerNilCA
	}
	return &SDKFleetNodeCertIssuer{ca: ca}, nil
}

// IssueFleetNodeCertificate implements FleetNodeCertIssuer.IssueFleetNodeCertificate by
// creating the fleet node's key pair at the point of custody and asking the CA
// service to sign a CSR for it.
//
// It does NOT call CAService.IssueCertificate, and it must not: that route
// generates inside the go-xkms keystore, where the key is a non-exportable
// opaque handle, so its response carries no private key on any backend or
// transport and a fleet node cannot be enrolled from it. This issuer used to call it
// and then require IssueCertificateResponse.PrivateKeyPEM, which no served
// implementation populates -- the same defect go-quicbaas#910 fixed in
// XKMSFleetNodeCertIssuer, on the same interface.
//
// Every cryptographic step is go-xkms's: the key comes from the single audited
// generator in pkg/xkms, the CSR from go-xkms CreateCSRWithKey, the signature
// from the remote go-xkms CA, and the key encoding from go-xkms
// EncodePrivateKeyPEM. The private key never crosses the network.
//
// req.SPIFFETrustDomain is NOT forwarded here and must not be: the transport's
// SignCSRRequest carries no trust-domain field, because on this path the SERVER
// owns it. The CA service adapter folds its own operator-configured trust domain
// into the signing options for the request's TenantID (pkg/server
// caServiceAdapter.signOptionsWithSPIFFE), so a client cannot name the trust
// domain its certificate is bound under.
func (i *SDKFleetNodeCertIssuer) IssueFleetNodeCertificate(ctx context.Context, req *FleetNodeCertificateRequest) (*IssuedFleetNodeCertificate, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	cn := strings.TrimSpace(req.Subject.CommonName)
	if cn == "" {
		cn = "agent-" + strings.TrimSpace(req.Hostname)
	}

	org := strings.TrimSpace(req.Subject.Organization)
	if org == "" {
		org = req.TenantID
	}

	// The OU is load-bearing, not decorative: the control plane's fleet node
	// register handler accepts DefaultFleetNodeCertOU and nothing else. The
	// IssueCertificate path this issuer used to take could not carry it at all
	// -- xkmstransport.IssueCertificateRequest has no OU field -- so a
	// certificate obtained that way was unusable even when it was issued.
	ou := strings.TrimSpace(req.Subject.OrganizationalUnit)
	if ou == "" {
		ou = DefaultFleetNodeCertOU
	}

	validity := req.ValidityDays
	if validity <= 0 {
		validity = DefaultFleetNodeCertValidityDays
	}

	// The SANs go in the CSR, which is where the CA reads them from on this
	// path; there is no prefixed-string list to build.
	dns := make([]string, 0, 1+len(req.ExtraDNSNames))
	dns = append(dns, req.Hostname)
	for _, extra := range req.ExtraDNSNames {
		trimmed := strings.TrimSpace(extra)
		if trimmed == "" || trimmed == req.Hostname {
			continue
		}
		dns = append(dns, trimmed)
	}

	signer, keyAlgorithm, err := dbxkms.GenerateRequesterKey(dbxkms.KeyAlgorithmECDSAP256)
	if err != nil {
		return nil, &FleetNodeCertIssueError{Op: "generate agent keypair", Cause: err}
	}

	csrPEM, err := xkmssdk.CreateCSRWithKey(&xkmssdk.TrustedCACertificateRequest{
		Subject: xkmssdk.TrustedCASubject{
			CommonName:         cn,
			Organization:       org,
			OrganizationalUnit: ou,
		},
		SANS: &xkmssdk.TrustedCASubjectAlternativeNames{
			DNS: dns,
		},
	}, signer)
	if err != nil {
		return nil, &FleetNodeCertIssueError{Op: "build agent csr", Cause: err}
	}

	resp, err := i.ca.SignCSR(ctx, &xkmstransport.SignCSRRequest{
		CSRPEM:       csrPEM,
		Profile:      "client",
		ValidityDays: validity,
		TenantID:     req.TenantID,
	})
	if err != nil {
		return nil, &FleetNodeCertIssueError{Op: "issue agent certificate", Cause: err}
	}
	if resp == nil {
		return nil, &FleetNodeCertIssueError{Op: "issue agent certificate", Cause: errors.New("ca returned nil response")}
	}
	if len(resp.CertificatePEM) == 0 {
		return nil, &FleetNodeCertIssueError{Op: "issue agent certificate", Cause: errors.New("ca returned empty certificate PEM")}
	}

	keyPEM, err := xkmssdk.EncodePrivateKeyPEM(signer, keyAlgorithm, nil)
	if err != nil {
		return nil, &FleetNodeCertIssueError{Op: "encode agent private key", Cause: err}
	}

	// Move the PEM into an mlocked buffer immediately; NewBufferFromBytes wipes
	// the source slice so the raw key never lingers on the heap.
	keyBuf, err := secure.NewBufferFromBytes(keyPEM)
	if err != nil {
		return nil, &FleetNodeCertIssueError{Op: "secure private key", Cause: err}
	}

	// Resolve the CA trust bundle. Prefer the dedicated GetCABundle call
	// (always complete chain). Fall back to the issued cert's ChainPEM
	// when the service implementation does not expose GetCABundle.
	caPEM, bundleErr := i.fetchCABundle(ctx, req.TenantID)
	if bundleErr != nil || len(caPEM) == 0 {
		caPEM = resp.ChainPEM
	}
	if len(caPEM) == 0 {
		keyBuf.Destroy()
		return nil, &FleetNodeCertIssueError{Op: "resolve ca bundle", Cause: errors.New("ca bundle is empty")}
	}

	// Parse the leaf certificate so callers can log serial/SAN details.
	parsed, parseErr := parseLeafCertPEM(resp.CertificatePEM)
	if parseErr != nil {
		keyBuf.Destroy()
		return nil, &FleetNodeCertIssueError{Op: "parse issued certificate", Cause: parseErr}
	}

	return &IssuedFleetNodeCertificate{
		CertPEM:    append([]byte(nil), resp.CertificatePEM...),
		PrivateKey: keyBuf,
		CAPEM:      append([]byte(nil), caPEM...),
		ParsedCert: parsed,
	}, nil
}

// FleetCABundle implements FleetCABundleSource: the tenant's CA bundle, the
// one IssueFleetNodeCertificate hands the daemon, without issuing anything.
func (i *SDKFleetNodeCertIssuer) FleetCABundle(ctx context.Context, tenantID string) ([]byte, error) {
	caPEM, err := i.fetchCABundle(ctx, tenantID)
	if err != nil {
		return nil, &FleetNodeCertIssueError{Op: "resolve ca bundle", Cause: err}
	}
	if len(caPEM) == 0 {
		return nil, &FleetNodeCertIssueError{Op: "resolve ca bundle", Cause: errors.New("ca bundle is empty")}
	}
	return caPEM, nil
}

// fetchCABundle retrieves the CA bundle for the tenant. Uses the caller's
// context so retries/timeouts are propagated correctly.
func (i *SDKFleetNodeCertIssuer) fetchCABundle(ctx context.Context, tenantID string) ([]byte, error) {
	resp, err := i.ca.GetCABundle(ctx, &xkmstransport.GetCABundleRequest{
		TenantID: tenantID,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("ca returned nil bundle response")
	}
	return resp.BundlePEM, nil
}

// parseLeafCertPEM decodes the first CERTIFICATE block in the supplied
// PEM bytes. Returns a typed error when the PEM is empty or malformed.
func parseLeafCertPEM(certPEM []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, errors.New("no PEM block found in certificate")
	}
	if block.Type != "CERTIFICATE" {
		return nil, &UnexpectedPEMTypeError{PEMType: block.Type}
	}
	return x509.ParseCertificate(block.Bytes)
}

// =============================================================================
// Typed errors
// =============================================================================

// ErrFleetNodeCertIssuerNilCA is returned by NewXKMSFleetNodeCertIssuer when called
// with a nil CA. Treating this as a programming error (fail-fast at wiring)
// is safer than constructing an issuer that will panic on the first call.
var ErrFleetNodeCertIssuerNilCA = errors.New("agent cert issuer: CA is required")

// ErrFleetNodeCertRequestNil is returned by FleetNodeCertificateRequest.Validate
// when the receiver is nil.
var ErrFleetNodeCertRequestNil = errors.New("agent cert issuer: request is nil")

// FleetNodeCertRequestFieldError identifies a missing required field in
// FleetNodeCertificateRequest.
type FleetNodeCertRequestFieldError struct {
	// Field is the snake_case name of the missing or invalid field.
	Field string
}

// Error implements the error interface.
func (e *FleetNodeCertRequestFieldError) Error() string {
	return fmt.Sprintf("agent cert issuer: field %s is required", e.Field)
}

// FleetNodeCertIssueError wraps a failure from the underlying CA. The Op field
// labels the stage for operator diagnostics; Cause carries the CA's error.
type FleetNodeCertIssueError struct {
	// Op is a short human-readable stage label ("issue fleet node certificate",
	// "secure private key", "resolve ca bundle").
	Op string

	// Cause is the wrapped CA/underlying error. Always non-nil.
	Cause error
}

// Error implements the error interface.
func (e *FleetNodeCertIssueError) Error() string {
	return fmt.Sprintf("agent cert issuer: %s: %v", e.Op, e.Cause)
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *FleetNodeCertIssueError) Unwrap() error {
	return e.Cause
}

// UnexpectedPEMTypeError indicates a PEM block had an unexpected type when a
// CERTIFICATE block was required.
type UnexpectedPEMTypeError struct {
	PEMType string
}

// Error implements the error interface.
func (e *UnexpectedPEMTypeError) Error() string {
	return fmt.Sprintf("unexpected PEM type %q", e.PEMType)
}
