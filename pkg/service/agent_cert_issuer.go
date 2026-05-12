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

// TODO(quicbaas/xkms-acme): For hardware-backed agent identities (TPM EK/AK
// attested enrollment), migrate from NewSDKAgentCertIssuer to an
// ACME-enroll01 / endorse01 flow using go-xkms's ACME client
// (github.com/jeremyhahn/go-xkms/sdk/go/acme). The AgentCertIssuer contract
// stays unchanged — only the concrete implementation swaps out.

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// =============================================================================
// Agent Certificate Issuer
// =============================================================================
//
// AgentCertIssuer produces X.509 identity certificates for QuicBaaS agent
// daemons. The certificate is presented on the TLS handshake when the daemon
// calls POST /api/v1/agents/register, replacing the legacy one-shot
// enrollment token as the authentication bootstrap.
//
// Production issuance flows through the server-embedded xkmssdk.TrustedCA —
// the same CA that signs the server's TLS certificate. The daemon and the
// control plane therefore share a single trust root without any operator key
// distribution ceremony.

// AgentCertificateSubject captures the optional subject overrides a caller
// can provide. All fields are optional; sensible defaults are applied by
// the issuer when a field is left zero.
type AgentCertificateSubject struct {
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

// AgentCertificateRequest captures the inputs to IssueAgentCertificate.
type AgentCertificateRequest struct {
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
	Subject AgentCertificateSubject

	// ValidityDays bounds the certificate lifetime. Zero falls back to the
	// default (365 days).
	ValidityDays int

	// ExtraDNSNames are appended to the default DNS SAN list (hostname).
	// Useful when the daemon is reachable under multiple names.
	ExtraDNSNames []string
}

// Validate checks that the request carries the minimum required fields.
func (r *AgentCertificateRequest) Validate() error {
	if r == nil {
		return ErrAgentCertRequestNil
	}
	if strings.TrimSpace(r.TenantID) == "" {
		return &AgentCertRequestFieldError{Field: "tenant_id"}
	}
	if strings.TrimSpace(r.Hostname) == "" {
		return &AgentCertRequestFieldError{Field: "hostname"}
	}
	return nil
}

// IssuedAgentCertificate is the output of a successful IssueAgentCertificate.
//
// CertPEM and CAPEM are plain []byte — they are not secrets. PrivateKey is
// held in a *secure.Buffer so the caller can upload it and then call
// Destroy() to zero the mlocked page immediately, leaving no plaintext key
// material on the operator's machine.
type IssuedAgentCertificate struct {
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

// AgentCertIssuer produces mTLS client identities for agent daemons.
type AgentCertIssuer interface {
	// IssueAgentCertificate generates a fresh keypair and signs a client
	// certificate for the agent. The private key is returned in a
	// *secure.Buffer that the caller MUST destroy once the material has
	// been persisted at its destination.
	IssueAgentCertificate(ctx context.Context, req *AgentCertificateRequest) (*IssuedAgentCertificate, error)
}

// =============================================================================
// XKMS-backed implementation
// =============================================================================

// DefaultAgentCertValidityDays is the default cert lifetime when the caller
// does not set ValidityDays. 365 days balances rotation cadence against the
// deploy-service UX (operators should not have to re-deploy every week).
const DefaultAgentCertValidityDays = 365

// DefaultAgentCertOU is the Organizational Unit stamped on agent certs when
// the caller does not override it. The control plane's register handler requires
// this OU and rejects any other value — a belt-and-braces check that the
// certificate was minted for agent authentication and not repurposed from a
// cert the same CA issued for a different role.
const DefaultAgentCertOU = "agents"

// XKMSAgentCertIssuer implements AgentCertIssuer on top of the server-
// embedded xkmssdk.TrustedCA. The CA is injected through the constructor so
// unit tests can substitute a stub without touching disk.
type XKMSAgentCertIssuer struct {
	ca xkmssdk.TrustedCA
}

// Compile-time interface check.
var _ AgentCertIssuer = (*XKMSAgentCertIssuer)(nil)

// NewXKMSAgentCertIssuer wires the issuer around the provided CA instance.
// The CA must already be initialized (Load() or Init() returned success) —
// the issuer never calls those lifecycle methods itself.
func NewXKMSAgentCertIssuer(ca xkmssdk.TrustedCA) (*XKMSAgentCertIssuer, error) {
	if ca == nil {
		return nil, ErrAgentCertIssuerNilCA
	}
	return &XKMSAgentCertIssuer{ca: ca}, nil
}

// IssueAgentCertificate implements AgentCertIssuer.IssueAgentCertificate.
func (i *XKMSAgentCertIssuer) IssueAgentCertificate(ctx context.Context, req *AgentCertificateRequest) (*IssuedAgentCertificate, error) {
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
		ou = DefaultAgentCertOU
	}

	validity := req.ValidityDays
	if validity <= 0 {
		validity = DefaultAgentCertValidityDays
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

	certRequest := &xkmssdk.TrustedCACertificateRequest{
		Subject: xkmssdk.TrustedCASubject{
			CommonName:         cn,
			Organization:       org,
			OrganizationalUnit: ou,
		},
		SANS: &xkmssdk.TrustedCASubjectAlternativeNames{
			DNS: dns,
		},
		Valid:       validity,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	issued, err := i.ca.IssueCertificate(certRequest)
	if err != nil {
		return nil, &AgentCertIssueError{Op: "issue agent certificate", Cause: err}
	}
	if issued == nil {
		return nil, &AgentCertIssueError{Op: "issue agent certificate", Cause: errors.New("ca returned nil certificate")}
	}
	if len(issued.CertificatePEM) == 0 {
		return nil, &AgentCertIssueError{Op: "issue agent certificate", Cause: errors.New("ca returned empty certificate PEM")}
	}
	if len(issued.PrivateKeyPEM) == 0 {
		return nil, &AgentCertIssueError{Op: "issue agent certificate", Cause: errors.New("ca did not return exportable private key")}
	}

	// Copy the PEM into a mlocked buffer immediately; NewBufferFromBytes
	// wipes the source slice so the raw key never lingers on the heap.
	keyCopy := make([]byte, len(issued.PrivateKeyPEM))
	copy(keyCopy, issued.PrivateKeyPEM)
	keyBuf, err := secure.NewBufferFromBytes(keyCopy)
	if err != nil {
		return nil, &AgentCertIssueError{Op: "secure private key", Cause: err}
	}

	// Wipe the CA-returned slice as well. We no longer need it — the
	// caller reads the bytes via keyBuf.Bytes().
	for j := range issued.PrivateKeyPEM {
		issued.PrivateKeyPEM[j] = 0
	}

	// Assemble the CA bundle the daemon must trust. The CA's CABundle()
	// returns the full chain (root + intermediates); fall back to the
	// per-cert CACertificatePEM when the CA cannot produce a bundle (e.g.
	// single-identity test CAs).
	caPEM, bundleErr := i.ca.CABundle()
	if bundleErr != nil || len(caPEM) == 0 {
		caPEM = issued.CACertificatePEM
	}
	if len(caPEM) == 0 {
		keyBuf.Destroy()
		return nil, &AgentCertIssueError{Op: "resolve ca bundle", Cause: errors.New("ca bundle is empty")}
	}

	return &IssuedAgentCertificate{
		CertPEM:    append([]byte(nil), issued.CertificatePEM...),
		PrivateKey: keyBuf,
		CAPEM:      append([]byte(nil), caPEM...),
		ParsedCert: issued.Certificate,
	}, nil
}

// =============================================================================
// SDK-backed implementation
// =============================================================================

// SDKAgentCertIssuer implements AgentCertIssuer by calling an xkmssdk.Client
// CA service (CAService interface from go-xkms/pkg/api/transport). Unlike
// XKMSAgentCertIssuer (which holds a direct TrustedCA reference), this
// variant is transport-agnostic — it works the same whether the SDK client
// is speaking gRPC, REST, QUIC, Unix socket, MCP, or the embedded backend.
//
// Use SDKAgentCertIssuer from CLI / remote agent-deploy workflows that only
// have an SDK client handle (no in-process CA). Use XKMSAgentCertIssuer
// from code paths that already embed the CA (desktop/Wails GUI, platformd).
type SDKAgentCertIssuer struct {
	ca xkmstransport.CAService
}

// Compile-time interface check.
var _ AgentCertIssuer = (*SDKAgentCertIssuer)(nil)

// NewSDKAgentCertIssuer wires the SDK-backed issuer around any
// CAService-capable client. xkmssdk.Client satisfies this via its embedded
// CAService methods, so callers can pass the whole SDK client directly.
func NewSDKAgentCertIssuer(ca xkmstransport.CAService) (*SDKAgentCertIssuer, error) {
	if ca == nil {
		return nil, ErrAgentCertIssuerNilCA
	}
	return &SDKAgentCertIssuer{ca: ca}, nil
}

// IssueAgentCertificate implements AgentCertIssuer.IssueAgentCertificate
// by delegating to the SDK CA service. The CA SANs are encoded using the
// prefixed-string shape the go-xkms CAService expects ("DNS:host").
func (i *SDKAgentCertIssuer) IssueAgentCertificate(ctx context.Context, req *AgentCertificateRequest) (*IssuedAgentCertificate, error) {
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

	validity := req.ValidityDays
	if validity <= 0 {
		validity = DefaultAgentCertValidityDays
	}

	// Build prefixed SAN list. The embedded-profile CA maps unprefixed
	// entries to DNS, but being explicit keeps the behaviour portable
	// across transports.
	sans := make([]string, 0, 1+len(req.ExtraDNSNames))
	sans = append(sans, "DNS:"+req.Hostname)
	for _, extra := range req.ExtraDNSNames {
		trimmed := strings.TrimSpace(extra)
		if trimmed == "" || trimmed == req.Hostname {
			continue
		}
		sans = append(sans, "DNS:"+trimmed)
	}

	issueReq := &xkmstransport.IssueCertificateRequest{
		Profile:      "client",
		CommonName:   cn,
		Organization: org,
		SANs:         sans,
		ValidityDays: validity,
		TenantID:     req.TenantID,
	}

	resp, err := i.ca.IssueCertificate(ctx, issueReq)
	if err != nil {
		return nil, &AgentCertIssueError{Op: "issue agent certificate", Cause: err}
	}
	if resp == nil {
		return nil, &AgentCertIssueError{Op: "issue agent certificate", Cause: errors.New("ca returned nil response")}
	}
	if len(resp.CertificatePEM) == 0 {
		return nil, &AgentCertIssueError{Op: "issue agent certificate", Cause: errors.New("ca returned empty certificate PEM")}
	}
	if len(resp.PrivateKeyPEM) == 0 {
		return nil, &AgentCertIssueError{Op: "issue agent certificate", Cause: errors.New("ca did not return exportable private key")}
	}

	// Secure the private key in an mlocked buffer immediately.
	keyCopy := make([]byte, len(resp.PrivateKeyPEM))
	copy(keyCopy, resp.PrivateKeyPEM)
	keyBuf, err := secure.NewBufferFromBytes(keyCopy)
	if err != nil {
		return nil, &AgentCertIssueError{Op: "secure private key", Cause: err}
	}
	// Wipe the response key slice so plaintext key material is not
	// reachable from the heap.
	for j := range resp.PrivateKeyPEM {
		resp.PrivateKeyPEM[j] = 0
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
		return nil, &AgentCertIssueError{Op: "resolve ca bundle", Cause: errors.New("ca bundle is empty")}
	}

	// Parse the leaf certificate so callers can log serial/SAN details.
	parsed, parseErr := parseLeafCertPEM(resp.CertificatePEM)
	if parseErr != nil {
		keyBuf.Destroy()
		return nil, &AgentCertIssueError{Op: "parse issued certificate", Cause: parseErr}
	}

	return &IssuedAgentCertificate{
		CertPEM:    append([]byte(nil), resp.CertificatePEM...),
		PrivateKey: keyBuf,
		CAPEM:      append([]byte(nil), caPEM...),
		ParsedCert: parsed,
	}, nil
}

// fetchCABundle retrieves the CA bundle for the tenant. Uses the caller's
// context so retries/timeouts are propagated correctly.
func (i *SDKAgentCertIssuer) fetchCABundle(ctx context.Context, tenantID string) ([]byte, error) {
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
		return nil, fmt.Errorf("unexpected PEM type %q", block.Type)
	}
	return x509.ParseCertificate(block.Bytes)
}

// =============================================================================
// Typed errors
// =============================================================================

// ErrAgentCertIssuerNilCA is returned by NewXKMSAgentCertIssuer when called
// with a nil CA. Treating this as a programming error (fail-fast at wiring)
// is safer than constructing an issuer that will panic on the first call.
var ErrAgentCertIssuerNilCA = errors.New("agent cert issuer: CA is required")

// ErrAgentCertRequestNil is returned by AgentCertificateRequest.Validate
// when the receiver is nil.
var ErrAgentCertRequestNil = errors.New("agent cert issuer: request is nil")

// AgentCertRequestFieldError identifies a missing required field in
// AgentCertificateRequest.
type AgentCertRequestFieldError struct {
	// Field is the snake_case name of the missing or invalid field.
	Field string
}

// Error implements the error interface.
func (e *AgentCertRequestFieldError) Error() string {
	return fmt.Sprintf("agent cert issuer: field %s is required", e.Field)
}

// AgentCertIssueError wraps a failure from the underlying CA. The Op field
// labels the stage for operator diagnostics; Cause carries the CA's error.
type AgentCertIssueError struct {
	// Op is a short human-readable stage label ("issue agent certificate",
	// "secure private key", "resolve ca bundle").
	Op string

	// Cause is the wrapped CA/underlying error. Always non-nil.
	Cause error
}

// Error implements the error interface.
func (e *AgentCertIssueError) Error() string {
	return fmt.Sprintf("agent cert issuer: %s: %v", e.Op, e.Cause)
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *AgentCertIssueError) Unwrap() error {
	return e.Cause
}
