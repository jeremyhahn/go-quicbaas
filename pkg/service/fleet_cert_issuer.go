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
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strconv"
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

	// OrganizationalUnit sets the requested X.509 OU= value. Empty falls
	// back to DefaultFleetNodeCertOU. The issuing CA replaces the first OU
	// with the tenant (CA m779 62(a)).
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
	// carries the tenant's SPIFFE URI SAN spiffe://<TenantID>/svc/<CommonName>.
	//
	// That SAN is not decoration: go-qrdb authenticates an mTLS identity into
	// a tenant only when the SAN's trust domain and the first Subject OU name
	// the same tenant (sdk TenantFromClientCert). go-xkms stamps the tenant
	// into that first OU (CA m779 62(a)); the SAN additionally names the
	// identity kind (service), which the fleet register handler checks.
	//
	// Required by XKMSFleetNodeCertIssuer, which refuses an empty value with
	// *FleetNodeCertNoTrustDomainError: the SAN's kind is the proof fleet
	// enrolment requires of an agent (CA m779, ruling 62(a)), so a certificate
	// without it is one enrolment refuses. A remote issuer leaves it unset:
	// the server signs under its own configured trust domain.
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

// DefaultFleetNodeCertOU is the Organizational Unit a fleet node CSR requests when
// the caller does not override it. The CA does not keep it: go-xkms v0.57
// replaces the issued certificate's first OU with the tenant (CA m779 62(a)),
// and the register handler identifies an agent by its SPIFFE kind instead.
const DefaultFleetNodeCertOU = "agents"

// XKMSFleetNodeCertIssuer implements FleetNodeCertIssuer on top of the server-
// embedded xkmssdk.TrustedCA. The CA is injected through the constructor so
// unit tests can substitute a stub without touching disk.
type XKMSFleetNodeCertIssuer struct {
	ca xkmssdk.TrustedCA
	// records remembers every certificate issued here; fleet enrollment
	// accepts no other (stopgap until go-quicbaas#1821).
	records FleetNodeCertRecords
}

// Compile-time interface check.
var (
	_ FleetNodeCertIssuer = (*XKMSFleetNodeCertIssuer)(nil)
	_ FleetCABundleSource = (*XKMSFleetNodeCertIssuer)(nil)
)

// NewXKMSFleetNodeCertIssuer wires the issuer around the provided CA instance.
// The CA must already be initialized (Load() or Init() returned success) —
// the issuer never calls those lifecycle methods itself.
func NewXKMSFleetNodeCertIssuer(ca xkmssdk.TrustedCA, records FleetNodeCertRecords) (*XKMSFleetNodeCertIssuer, error) {
	if ca == nil {
		return nil, ErrFleetNodeCertIssuerNilCA
	}
	if records == nil {
		return nil, ErrFleetNodeCertRecordsRequired
	}
	return &XKMSFleetNodeCertIssuer{ca: ca, records: records}, nil
}

// IssueFleetNodeCertificate implements FleetNodeCertIssuer.IssueFleetNodeCertificate.
// FleetNodeKey is a fresh fleet node key pair and the CSR that names it,
// built where the key will be held. The CA only ever sees CSRPEM.
type FleetNodeKey struct {
	// CSRPEM is the PEM-encoded PKCS#10 request.
	CSRPEM []byte

	// CommonName is the CSR's subject CN, the agent identity's username.
	CommonName string

	signer    crypto.Signer
	algorithm x509.PublicKeyAlgorithm
}

// NewFleetNodeKey generates the agent key pair and its CSR, applying the
// request's subject defaults: CN "agent-<hostname>", O the tenant, OU
// DefaultFleetNodeCertOU, and DNS SANs of the hostname plus any extras.
//
// The fleet node's key pair is the AGENT's credential, not platform key
// material, so it is created at the point of custody and the CA only ever sees
// the CSR -- go-xkms's own rule for this shape of issuance (pkg/init
// firstboot_adapter.go GenerateAdminCert). TrustedCA.IssueCertificate keeps
// the key it generates inside the keystore as a non-exportable handle, so it
// cannot serve here (go-quicbaas#910).
//
// Every crypto step is go-xkms's: the key comes from the single audited
// generator in pkg/xkms (go-xkms FIPS RNG) and the CSR from go-xkms
// CreateCSRWithKey.
func NewFleetNodeKey(req *FleetNodeCertificateRequest) (*FleetNodeKey, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	hostname := strings.TrimSpace(req.Hostname)

	cn := strings.TrimSpace(req.Subject.CommonName)
	if cn == "" {
		cn = "agent-" + hostname
	}
	org := strings.TrimSpace(req.Subject.Organization)
	if org == "" {
		org = req.TenantID
	}
	ou := strings.TrimSpace(req.Subject.OrganizationalUnit)
	if ou == "" {
		ou = DefaultFleetNodeCertOU
	}

	// The hostname always, plus any extras the caller supplied (e.g. a short
	// alias or a LAN FQDN).
	dns := []string{hostname}
	for _, extra := range req.ExtraDNSNames {
		trimmed := strings.TrimSpace(extra)
		if trimmed == "" || trimmed == hostname {
			continue
		}
		dns = append(dns, trimmed)
	}

	signer, algorithm, err := dbxkms.GenerateRequesterKey(dbxkms.KeyAlgorithmECDSAP256)
	if err != nil {
		return nil, &FleetNodeCertIssueError{Op: "generate agent keypair", Cause: err}
	}
	csrPEM, err := xkmssdk.CreateCSRWithKey(&xkmssdk.TrustedCACertificateRequest{
		Subject: xkmssdk.TrustedCASubject{
			CommonName:         cn,
			Organization:       org,
			OrganizationalUnit: ou,
		},
		SANS: &xkmssdk.TrustedCASubjectAlternativeNames{DNS: dns},
	}, signer)
	if err != nil {
		return nil, &FleetNodeCertIssueError{Op: "build agent csr", Cause: err}
	}
	return &FleetNodeKey{CSRPEM: csrPEM, CommonName: cn, signer: signer, algorithm: algorithm}, nil
}

// PrivateKeyPEM encodes the private key with go-xkms EncodePrivateKeyPEM and
// moves it straight into an mlocked buffer; NewBufferFromBytes wipes the
// source slice so the raw key never lingers on the heap. The caller MUST
// Destroy the buffer once the key is at its destination.
func (k *FleetNodeKey) PrivateKeyPEM() (*secure.Buffer, error) {
	keyPEM, err := xkmssdk.EncodePrivateKeyPEM(k.signer, k.algorithm, nil)
	if err != nil {
		return nil, &FleetNodeCertIssueError{Op: "encode agent private key", Cause: err}
	}
	keyBuf, err := secure.NewBufferFromBytes(keyPEM)
	if err != nil {
		return nil, &FleetNodeCertIssueError{Op: "secure private key", Cause: err}
	}
	return keyBuf, nil
}

// fleetNodeValidityDays applies the default lifetime to a zero or negative
// request.
func fleetNodeValidityDays(days int) int {
	if days <= 0 {
		return DefaultFleetNodeCertValidityDays
	}
	return days
}

func (i *XKMSFleetNodeCertIssuer) IssueFleetNodeCertificate(ctx context.Context, req *FleetNodeCertificateRequest) (*IssuedFleetNodeCertificate, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	// go-xkms mints the SPIFFE SAN only when the signing names a trust domain
	// (pkg/ca signCSR). Without the SAN the certificate carries no agent kind,
	// and fleet enrolment refuses it, so none is issued.
	trustDomain := strings.TrimSpace(req.SPIFFETrustDomain)
	if trustDomain == "" {
		return nil, &FleetNodeCertNoTrustDomainError{TenantID: req.TenantID}
	}

	key, err := NewFleetNodeKey(req)
	if err != nil {
		return nil, err
	}
	cert, err := i.signAndRecord(ctx, req.TenantID, trustDomain, key.CommonName, fleetNodeValidityDays(req.ValidityDays), key.CSRPEM)
	if err != nil {
		return nil, err
	}
	keyBuf, err := key.PrivateKeyPEM()
	if err != nil {
		return nil, err
	}

	// The CA bundle the daemon must trust.
	caPEM, err := i.FleetCABundle(ctx, req.TenantID)
	if err != nil {
		keyBuf.Destroy()
		return nil, err
	}

	return &IssuedFleetNodeCertificate{
		CertPEM:    pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}),
		PrivateKey: keyBuf,
		CAPEM:      append([]byte(nil), caPEM...),
		ParsedCert: cert,
	}, nil
}

// FleetNodeCSRRequest asks the fleet CA to sign a CSR for a fleet node whose
// key was generated off the server host — by `quicbaas fleet deploy` on an
// admin workstation — so the private key never reaches the server.
type FleetNodeCSRRequest struct {
	// TenantID is the tenant the daemon serves. Required.
	TenantID string

	// CSRPEM is the PEM-encoded PKCS#10 request. Required. Its subject
	// CommonName becomes the agent identity's username.
	CSRPEM []byte

	// ValidityDays bounds the certificate lifetime. Zero falls back to the
	// default (365 days).
	ValidityDays int

	// SPIFFETrustDomain is the operator-configured trust domain the agent
	// SPIFFE SAN is minted under. Required, as for IssueFleetNodeCertificate.
	SPIFFETrustDomain string
}

// SignedFleetNodeCertificate is the output of SignFleetNodeCSR. It carries
// only public material; the key stays with whoever built the CSR.
type SignedFleetNodeCertificate struct {
	// CertPEM is the PEM-encoded leaf certificate.
	CertPEM []byte

	// CAPEM is the CA bundle the daemon must trust.
	CAPEM []byte

	// ParsedCert is the parsed leaf.
	ParsedCert *x509.Certificate
}

// SignFleetNodeCSR signs a fleet node's own CSR exactly as
// IssueFleetNodeCertificate signs the one it builds: the agent SPIFFE kind
// under the tenant, clientAuth only, and an issued-cert record so fleet
// enrolment accepts it. The CSR's signature must prove possession of the key
// it names.
func (i *XKMSFleetNodeCertIssuer) SignFleetNodeCSR(ctx context.Context, req *FleetNodeCSRRequest) (*SignedFleetNodeCertificate, error) {
	if req == nil {
		return nil, ErrFleetNodeCertRequestNil
	}
	if strings.TrimSpace(req.TenantID) == "" {
		return nil, &FleetNodeCertRequestFieldError{Field: "tenant_id"}
	}
	if len(req.CSRPEM) == 0 {
		return nil, &FleetNodeCertRequestFieldError{Field: "csr_pem"}
	}
	trustDomain := strings.TrimSpace(req.SPIFFETrustDomain)
	if trustDomain == "" {
		return nil, &FleetNodeCertNoTrustDomainError{TenantID: req.TenantID}
	}

	csr, err := parseFleetNodeCSR(req.CSRPEM)
	if err != nil {
		return nil, err
	}
	cn := strings.TrimSpace(csr.Subject.CommonName)
	if cn == "" {
		return nil, &FleetNodeCertRequestFieldError{Field: "csr_pem common_name"}
	}

	cert, err := i.signAndRecord(ctx, req.TenantID, trustDomain, cn, fleetNodeValidityDays(req.ValidityDays), req.CSRPEM)
	if err != nil {
		return nil, err
	}
	caPEM, err := i.FleetCABundle(ctx, req.TenantID)
	if err != nil {
		return nil, err
	}
	return &SignedFleetNodeCertificate{
		CertPEM:    pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}),
		CAPEM:      append([]byte(nil), caPEM...),
		ParsedCert: cert,
	}, nil
}

// parseFleetNodeCSR decodes a PEM CSR and checks its self-signature.
func parseFleetNodeCSR(csrPEM []byte) (*x509.CertificateRequest, error) {
	block, _ := pem.Decode(csrPEM)
	if block == nil {
		return nil, &FleetNodeCertRequestFieldError{Field: "csr_pem"}
	}
	if block.Type != "CERTIFICATE REQUEST" {
		return nil, &FleetNodeCSRInvalidError{Reason: "parse", Cause: &UnexpectedPEMTypeError{PEMType: block.Type}}
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, &FleetNodeCSRInvalidError{Reason: "parse", Cause: err}
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, &FleetNodeCSRInvalidError{Reason: "verify signature", Cause: err}
	}
	return csr, nil
}

// signAndRecord signs an agent CSR and records the certificate for tenantID.
//
// ExtKeyUsage is set explicitly rather than by naming a profile so the issued
// certificate carries exactly clientAuth, which is what the control plane's
// register handler requires of a fleet node identity.
//
// The certificate is bound to its tenant with a SPIFFE URI SAN under the
// operator's trust domain. The CA owns the single synthesis site (go-xkms
// pkg/ca signCSR -> spiffe.BuildURI); this only forwards the inputs, so this
// path and every other CSR-sign path cannot drift.
//
// The workload kind is "svc": a fleet node is a machine identity, which is
// exactly what go-qrdb's spiffe.KindService names, and the kind fleet
// enrolment requires as the proof that the certificate is an agent's.
func (i *XKMSFleetNodeCertIssuer) signAndRecord(ctx context.Context, tenantID, trustDomain, cn string, validity int, csrPEM []byte) (*x509.Certificate, error) {
	signOpts := &xkmssdk.TrustedCASignOptions{
		ValidityDays:      validity,
		ExtKeyUsage:       []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		TenantID:          tenantID,
		SPIFFETrustDomain: trustDomain,
		SPIFFEKind:        qrdbsdk.SpiffeKindService,
		SPIFFEName:        cn,
	}

	cert, err := i.ca.SignCSR(ctx, csrPEM, signOpts)
	if err != nil {
		return nil, &FleetNodeCertIssueError{Op: "issue agent certificate", Cause: err}
	}
	if cert == nil {
		return nil, &FleetNodeCertIssueError{Op: "issue agent certificate", Cause: errors.New("ca returned nil certificate")}
	}
	// Stopgap until go-quicbaas#1821: enrollment accepts only a certificate
	// recorded here, so the record is written before the certificate leaves.
	if err := i.records.RecordFleetNodeCertificate(ctx, tenantID, cert); err != nil {
		return nil, &FleetNodeCertIssueError{Op: "record agent certificate", Cause: err}
	}
	return cert, nil
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
// It serves FleetCABundle, but refuses every IssueFleetNodeCertificate with
// *FleetNodeCertKindUnavailableError: the transport cannot request the agent
// SPIFFE kind fleet enrolment requires. Use XKMSFleetNodeCertIssuer, from code
// paths that embed the CA (desktop/Wails GUI, platformd), to issue agent
// certificates.
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

// IssueFleetNodeCertificate implements FleetNodeCertIssuer.IssueFleetNodeCertificate
// by refusing every request with *FleetNodeCertKindUnavailableError, before it
// generates a key or calls the CA.
//
// The only signing call this transport has, CAService.SignCSR, takes a
// SignCSRRequest with no SPIFFE kind or trust-domain field. The server folds
// its own trust domain in, but not the agent ("svc") kind, so the certificate
// that comes back is user-kind or carries no SPIFFE SAN, and fleet enrolment
// refuses either. Issuing it would hand the agent a credential that cannot
// register. The refusal stands until the CA rules on how this transport asks
// for the agent kind; the in-process XKMSFleetNodeCertIssuer is unaffected.
//
// CAService.IssueCertificate is no alternative: it generates the key inside the
// go-xkms keystore as a non-exportable handle, so its response carries no
// private key (go-quicbaas#910).
func (i *SDKFleetNodeCertIssuer) IssueFleetNodeCertificate(_ context.Context, req *FleetNodeCertificateRequest) (*IssuedFleetNodeCertificate, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	return nil, &FleetNodeCertKindUnavailableError{TenantID: req.TenantID}
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
// FleetNodeCertNoTrustDomainError reports an agent certificate request that
// names no SPIFFE trust domain. go-xkms omits the SPIFFE SAN without one, and a
// certificate with no SAN carries no agent kind, which fleet enrolment
// requires (CA m779, ruling 62(a)). Configure iam.spiffe_trust_domain.
type FleetNodeCertNoTrustDomainError struct {
	// TenantID is the tenant the certificate was requested for.
	TenantID string
}

// Error implements the error interface.
func (e *FleetNodeCertNoTrustDomainError) Error() string {
	return "agent cert: no SPIFFE trust domain configured for tenant " + strconv.Quote(e.TenantID) +
		": an agent certificate without a SPIFFE SAN cannot enroll (set iam.spiffe_trust_domain)"
}

// FleetNodeCertKindUnavailableError reports an agent certificate request on a
// path whose CA transport cannot ask for the agent SPIFFE kind. go-xkms's
// transport SignCSRRequest has no SPIFFE kind or trust-domain field, so the
// certificate it returns is user-kind or carries no SPIFFE SAN, and fleet
// enrolment refuses both (CA m779, ruling 62(a)). The issuer refuses before it
// generates a key, so no certificate is issued that cannot enroll.
type FleetNodeCertKindUnavailableError struct {
	// TenantID is the tenant the certificate was requested for.
	TenantID string
}

// Error implements the error interface.
func (e *FleetNodeCertKindUnavailableError) Error() string {
	return "agent cert: the CA transport cannot request the agent SPIFFE kind for tenant " + strconv.Quote(e.TenantID) +
		": a certificate signed over it cannot enroll (deploy from the server host, whose in-process CA can)"
}

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

// FleetNodeCSRInvalidError reports a fleet node CSR that cannot be signed: it
// is not a PKCS#10 request, or its signature does not prove possession of the
// key it names. Reason is the failed stage; Cause the parser's error.
type FleetNodeCSRInvalidError struct {
	Reason string
	Cause  error
}

// Error implements the error interface.
func (e *FleetNodeCSRInvalidError) Error() string {
	return fmt.Sprintf("agent cert issuer: invalid csr: %s: %v", e.Reason, e.Cause)
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *FleetNodeCSRInvalidError) Unwrap() error {
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
