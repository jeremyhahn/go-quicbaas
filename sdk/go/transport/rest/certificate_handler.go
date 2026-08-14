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

// certificate_handler.go provides typed REST routing for the
// quicbaas.Certificate service. The bare default REST dispatch path
// returns a generic interface{} that the SDK certificate client cannot
// type-assert; this handler reads the routed HTTP body straight into
// the strongly-typed wire-format structs the client expects.
//
// Every Certificate service method exposed by the SDK client is
// dispatched here so callers always receive a strongly-typed response
// instead of an opaque map[string]interface{}. Methods without a
// matching server endpoint fall back to the generic dispatch path.

package rest

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// certificateServicePrefix is the gRPC-style service identifier the
// SDK uses to route Certificate requests to this handler.
const certificateServicePrefix = "quicbaas.Certificate"

// isCertificateService reports whether the supplied service name
// resolves to the Certificate service. Used by Transport.Request to
// branch into certificateRESTHandler.
func isCertificateService(service string) bool {
	return strings.HasPrefix(service, certificateServicePrefix) ||
		trimRESTServicePrefix(service) == "Certificate"
}

// trimRESTServicePrefix returns the trailing component of a
// dot-separated service name. "quicbaas.Certificate" → "Certificate".
func trimRESTServicePrefix(svc string) string {
	if idx := strings.LastIndex(svc, "."); idx >= 0 {
		return svc[idx+1:]
	}
	return svc
}

// certificateRESTHandler delegates Certificate service calls to the
// quicbaasd REST endpoints with proper response type conversion. It is
// constructed by Transport on init and lookup-routed in
// Transport.Request.
type certificateRESTHandler struct {
	transport *Transport
}

// newCertificateRESTHandler constructs a handler bound to t.
func newCertificateRESTHandler(t *Transport) *certificateRESTHandler {
	return &certificateRESTHandler{transport: t}
}

// handleRequest dispatches the Certificate service method to the
// matching REST endpoint. Methods not listed here fall through to the
// default request path which returns the raw interface{} body.
func (h *certificateRESTHandler) handleRequest(ctx context.Context, _ string, method string, req interface{}) (interface{}, error) {
	switch method {
	case "CAInfo":
		return h.handleCAInfo(ctx, req)
	case "GetCABundle":
		return h.handleGetCABundle(ctx, req)
	case "ListCertificates":
		return h.handleListCertificates(ctx, req)
	case "GetCACertificate":
		return h.handleGetCACertificate(ctx, req)
	case "IsRevoked":
		return h.handleIsRevoked(ctx, req)
	case "IsInitialized":
		return h.handleIsInitialized(ctx, req)
	case "IssueCertificate":
		return h.handleIssueCertificate(ctx, req)
	case "SignCSR":
		return h.handleSignCSR(ctx, req)
	case "RevokeCertificate":
		return h.handleRevokeCertificate(ctx, req)
	case "VerifyCertificate":
		return h.handleVerifyCertificate(ctx, req)
	case "GetTLSConfig":
		return h.handleGetTLSConfig(ctx, req)
	case "GenerateCRL":
		return h.handleGenerateCRL(ctx, req)

	// Certificate extensions — Security().CertificateExt(). See
	// certificate_extensions_handler.go. They must be routed explicitly: the
	// generic fallback below would derive "/api/v1/certificate/crosssign" and
	// return an untyped body.
	case "CrossSign":
		return h.handleCrossSign(ctx, req)
	case "Chain":
		return h.handleChain(ctx, req)
	case "IssueDeviceIdentity":
		return h.handleIssueDeviceIdentity(ctx, req)
	default:
		// Anything not explicitly typed falls back to the generic
		// dispatch path which returns interface{}.
		return h.transport.requestWithRetry(ctx, certificateServicePrefix, method, req)
	}
}

// caInfoRESTWire mirrors the quicbaasd CAInfoRESTResponse shape. We
// decode into this then convert to the typed certificate client struct
// because the timestamps on the wire are unix epoch integers, not
// time.Time values.
type caInfoRESTWire struct {
	CommonName         string `json:"common_name"`
	Subject            string `json:"subject"`
	Issuer             string `json:"issuer"`
	SerialNumber       string `json:"serial_number"`
	NotBefore          int64  `json:"not_before"`
	NotAfter           int64  `json:"not_after"`
	IsCA               bool   `json:"is_ca"`
	KeyType            string `json:"key_type"`
	KeyBits            int    `json:"key_bits,omitempty"`
	SignatureAlgorithm string `json:"signature_algorithm"`
	CertificatePEM     string `json:"certificate_pem"`
	CABundlePEM        string `json:"ca_bundle_pem"`
}

func (h *certificateRESTHandler) handleCAInfo(ctx context.Context, _ interface{}) (interface{}, error) {
	var wire caInfoRESTWire
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, "/api/v1/certificate/ca/info", nil, &wire); err != nil {
		return nil, err
	}
	return &certificate.CAInfoResponse{
		CommonName:         wire.CommonName,
		Subject:            wire.Subject,
		Issuer:             wire.Issuer,
		SerialNumber:       wire.SerialNumber,
		NotBefore:          time.Unix(wire.NotBefore, 0).UTC(),
		NotAfter:           time.Unix(wire.NotAfter, 0).UTC(),
		IsCA:               wire.IsCA,
		KeyType:            wire.KeyType,
		KeyBits:            wire.KeyBits,
		SignatureAlgorithm: wire.SignatureAlgorithm,
		CertificatePEM:     wire.CertificatePEM,
		CABundlePEM:        wire.CABundlePEM,
	}, nil
}

type caBundleRESTWire struct {
	BundlePEM string `json:"bundle_pem"`
}

func (h *certificateRESTHandler) handleGetCABundle(ctx context.Context, _ interface{}) (interface{}, error) {
	var wire caBundleRESTWire
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, "/api/v1/certificate/ca/bundle", nil, &wire); err != nil {
		return nil, err
	}
	return &certificate.GetCABundleResponse{BundlePEM: wire.BundlePEM}, nil
}

type certificateSummaryWire struct {
	CommonName   string   `json:"common_name"`
	Subject      string   `json:"subject"`
	Issuer       string   `json:"issuer"`
	SerialNumber string   `json:"serial_number"`
	NotBefore    int64    `json:"not_before"`
	NotAfter     int64    `json:"not_after"`
	IsCA         bool     `json:"is_ca"`
	KeyUsage     string   `json:"key_usage,omitempty"`
	DNSNames     []string `json:"dns_names,omitempty"`
}

type listCertsRESTWire struct {
	Certificates []certificateSummaryWire `json:"certificates"`
	TotalCount   int                      `json:"total_count"`
}

func (h *certificateRESTHandler) handleListCertificates(ctx context.Context, req interface{}) (interface{}, error) {
	q := url.Values{}
	if r, ok := req.(*certificate.ListCertificatesRequest); ok && r != nil {
		if r.CommonNameFilter != "" {
			q.Set("common_name", r.CommonNameFilter)
		}
		if r.IncludeRevoked {
			q.Set("include_revoked", "true")
		}
		if r.IncludeExpired {
			q.Set("include_expired", "true")
		}
		if r.Limit > 0 {
			q.Set("limit", strconv.Itoa(r.Limit))
		}
	}

	endpoint := "/api/v1/certificate/ca/list"
	if encoded := q.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}

	var wire listCertsRESTWire
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &wire); err != nil {
		return nil, err
	}
	out := &certificate.ListCertificatesResponse{
		Certificates: make([]certificate.CertificateSummary, 0, len(wire.Certificates)),
		TotalCount:   wire.TotalCount,
	}
	for _, c := range wire.Certificates {
		out.Certificates = append(out.Certificates, certificate.CertificateSummary{
			CommonName:   c.CommonName,
			Subject:      c.Subject,
			Issuer:       c.Issuer,
			SerialNumber: c.SerialNumber,
			NotBefore:    time.Unix(c.NotBefore, 0).UTC(),
			NotAfter:     time.Unix(c.NotAfter, 0).UTC(),
			IsCA:         c.IsCA,
			KeyUsage:     c.KeyUsage,
			DNSNames:     append([]string(nil), c.DNSNames...),
		})
	}
	return out, nil
}

type getCACertWire struct {
	CACertificatePEM string `json:"ca_certificate_pem"`
	CABundlePEM      string `json:"ca_bundle_pem"`
	Identity         string `json:"identity"`
}

func (h *certificateRESTHandler) handleGetCACertificate(ctx context.Context, _ interface{}) (interface{}, error) {
	var wire getCACertWire
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, "/api/v1/certificate/ca", nil, &wire); err != nil {
		return nil, err
	}
	return &certificate.GetCACertificateResponse{
		CACertificatePEM: wire.CACertificatePEM,
		CABundlePEM:      wire.CABundlePEM,
		Identity:         wire.Identity,
	}, nil
}

type isRevokedWire struct {
	Revoked bool   `json:"revoked"`
	Reason  int    `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

func (h *certificateRESTHandler) handleIsRevoked(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*certificate.IsRevokedRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.certificate.IsRevoked", "request is required")
	}
	if r.SerialNumber == "" {
		return nil, errors.NewInvalidInput("rest.certificate.IsRevoked", "serial number is required")
	}
	var wire isRevokedWire
	endpoint := "/api/v1/certificate/revoked/" + url.PathEscape(r.SerialNumber)
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &wire); err != nil {
		return nil, err
	}
	return &certificate.IsRevokedResponse{
		Revoked: wire.Revoked,
		Reason:  wire.Reason,
		Message: wire.Message,
	}, nil
}

type isInitializedWire struct {
	Initialized bool `json:"initialized"`
}

func (h *certificateRESTHandler) handleIsInitialized(ctx context.Context, _ interface{}) (interface{}, error) {
	// IsInitialized is functionally GetCACertificate→ok; map to the
	// existing /ca endpoint and report true when the server returns
	// the CA cert without error.
	var wire getCACertWire
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, "/api/v1/certificate/ca", nil, &wire); err != nil {
		// Treat any failure as "not initialized"; the SDK contract
		// returns (false, err).
		return &certificate.IsInitializedResponse{Initialized: false}, nil
	}
	return &certificate.IsInitializedResponse{Initialized: wire.CACertificatePEM != ""}, nil
}

// =============================================================================
// IssueCertificate
// =============================================================================

// issueCertificateRESTRequest is the wire request shape posted to
// POST /api/v1/certificate/issue. The field set mirrors the SDK
// IssueCertificateRequest so a direct value-copy is sufficient; we
// declare an explicit type rather than passing the SDK request through
// to keep the wire contract decoupled from the public SDK surface.
type issueCertificateRESTRequest struct {
	CommonName         string            `json:"common_name"`
	Organization       string            `json:"organization,omitempty"`
	OrganizationalUnit string            `json:"organizational_unit,omitempty"`
	Country            string            `json:"country,omitempty"`
	Province           string            `json:"province,omitempty"`
	Locality           string            `json:"locality,omitempty"`
	DNSNames           []string          `json:"dns_names,omitempty"`
	IPAddresses        []string          `json:"ip_addresses,omitempty"`
	EmailAddresses     []string          `json:"email_addresses,omitempty"`
	URIs               []string          `json:"uris,omitempty"`
	ValidityDays       int               `json:"validity_days,omitempty"`
	Usage              string            `json:"usage,omitempty"`
	IsCA               bool              `json:"is_ca,omitempty"`
	MaxPathLen         int               `json:"max_path_len,omitempty"`
	KeyAlgorithm       string            `json:"key_algorithm,omitempty"`
	KeySize            int               `json:"key_size,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
}

// issueCertificateRESTResponse mirrors the quicbaasd
// IssueCertificateRESTResponse — NotBefore/NotAfter are unix epoch
// integers on the wire, converted to time.Time on the SDK boundary.
type issueCertificateRESTResponse struct {
	CertificatePEM   string `json:"certificate_pem"`
	PrivateKeyPEM    string `json:"private_key_pem"`
	CACertificatePEM string `json:"ca_certificate_pem"`
	ChainPEM         string `json:"chain_pem"`
	SerialNumber     string `json:"serial_number"`
	NotBefore        int64  `json:"not_before"`
	NotAfter         int64  `json:"not_after"`
}

func (h *certificateRESTHandler) handleIssueCertificate(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*certificate.IssueCertificateRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.certificate.IssueCertificate", "request is required")
	}

	wireReq := issueCertificateRESTRequest{
		CommonName:         r.CommonName,
		Organization:       r.Organization,
		OrganizationalUnit: r.OrganizationalUnit,
		Country:            r.Country,
		Province:           r.Province,
		Locality:           r.Locality,
		DNSNames:           r.DNSNames,
		IPAddresses:        r.IPAddresses,
		EmailAddresses:     r.EmailAddresses,
		URIs:               r.URIs,
		ValidityDays:       r.ValidityDays,
		Usage:              r.Usage,
		IsCA:               r.IsCA,
		MaxPathLen:         r.MaxPathLen,
		KeyAlgorithm:       r.KeyAlgorithm,
		KeySize:            r.KeySize,
		Metadata:           r.Metadata,
	}

	var wire issueCertificateRESTResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/certificate/issue", &wireReq, &wire); err != nil {
		return nil, err
	}
	return &certificate.IssueCertificateResponse{
		CertificatePEM:   wire.CertificatePEM,
		PrivateKeyPEM:    wire.PrivateKeyPEM,
		CACertificatePEM: wire.CACertificatePEM,
		ChainPEM:         wire.ChainPEM,
		SerialNumber:     wire.SerialNumber,
		NotBefore:        time.Unix(wire.NotBefore, 0).UTC(),
		NotAfter:         time.Unix(wire.NotAfter, 0).UTC(),
	}, nil
}

// =============================================================================
// SignCSR
// =============================================================================

type signCSRRESTRequest struct {
	CSRPEM       string `json:"csr_pem"`
	ValidityDays int    `json:"validity_days,omitempty"`
	Usage        string `json:"usage,omitempty"`
}

type signCSRRESTResponse struct {
	CertificatePEM   string `json:"certificate_pem"`
	CACertificatePEM string `json:"ca_certificate_pem"`
	ChainPEM         string `json:"chain_pem"`
	SerialNumber     string `json:"serial_number"`
	NotBefore        int64  `json:"not_before"`
	NotAfter         int64  `json:"not_after"`
}

func (h *certificateRESTHandler) handleSignCSR(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*certificate.SignCSRRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.certificate.SignCSR", "request is required")
	}
	if r.CSRPEM == "" {
		return nil, errors.NewInvalidInput("rest.certificate.SignCSR", "CSR PEM is required")
	}

	wireReq := signCSRRESTRequest{
		CSRPEM:       r.CSRPEM,
		ValidityDays: r.ValidityDays,
		Usage:        r.Usage,
	}

	var wire signCSRRESTResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/certificate/sign-csr", &wireReq, &wire); err != nil {
		return nil, err
	}
	return &certificate.SignCSRResponse{
		CertificatePEM:   wire.CertificatePEM,
		CACertificatePEM: wire.CACertificatePEM,
		ChainPEM:         wire.ChainPEM,
		SerialNumber:     wire.SerialNumber,
		NotBefore:        time.Unix(wire.NotBefore, 0).UTC(),
		NotAfter:         time.Unix(wire.NotAfter, 0).UTC(),
	}, nil
}

// =============================================================================
// RevokeCertificate
// =============================================================================

type revokeCertificateRESTRequest struct {
	SerialNumber string `json:"serial_number"`
}

type revokeCertificateRESTResponse struct {
	Success bool `json:"success"`
}

func (h *certificateRESTHandler) handleRevokeCertificate(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*certificate.RevokeCertificateRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.certificate.RevokeCertificate", "request is required")
	}
	if r.SerialNumber == "" {
		return nil, errors.NewInvalidInput("rest.certificate.RevokeCertificate", "serial number is required")
	}

	wireReq := revokeCertificateRESTRequest{SerialNumber: r.SerialNumber}

	var wire revokeCertificateRESTResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/certificate/revoke", &wireReq, &wire); err != nil {
		return nil, err
	}
	return &certificate.RevokeCertificateResponse{Success: wire.Success}, nil
}

// =============================================================================
// VerifyCertificate
// =============================================================================

type verifyCertificateRESTRequest struct {
	CertificatePEM string `json:"certificate_pem"`
}

type verifyCertificateRESTResponse struct {
	Valid        bool   `json:"valid"`
	ErrorMessage string `json:"error_message,omitempty"`
}

func (h *certificateRESTHandler) handleVerifyCertificate(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*certificate.VerifyCertificateRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.certificate.VerifyCertificate", "request is required")
	}
	if r.CertificatePEM == "" {
		return nil, errors.NewInvalidInput("rest.certificate.VerifyCertificate", "certificate PEM is required")
	}

	wireReq := verifyCertificateRESTRequest{CertificatePEM: r.CertificatePEM}

	var wire verifyCertificateRESTResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/certificate/verify", &wireReq, &wire); err != nil {
		return nil, err
	}
	return &certificate.VerifyCertificateResponse{
		Valid:        wire.Valid,
		ErrorMessage: wire.ErrorMessage,
	}, nil
}

// =============================================================================
// GetTLSConfig
// =============================================================================

// getTLSConfigRESTRequest mirrors GetTLSConfigRESTRequest on the server.
type getTLSConfigRESTRequest struct {
	ServerName        string `json:"server_name,omitempty"`
	RequireClientCert bool   `json:"require_client_cert,omitempty"`
	IsServer          bool   `json:"is_server,omitempty"`
	MinVersion        string `json:"min_version,omitempty"`
}

// getTLSConfigRESTResponse mirrors GetTLSConfigRESTResponse on the
// server. The server only reports a boolean "configured" flag because
// a real *tls.Config is not JSON-serializable; the SDK constructs a
// minimum-viable *tls.Config from the request parameters when the
// server confirms the configuration is valid.
type getTLSConfigRESTResponse struct {
	Configured bool `json:"configured"`
}

func (h *certificateRESTHandler) handleGetTLSConfig(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*certificate.GetTLSConfigRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.certificate.GetTLSConfig", "request is required")
	}

	wireReq := getTLSConfigRESTRequest{
		ServerName:        r.ServerName,
		RequireClientCert: r.RequireClientCert,
		IsServer:          r.IsServer,
		MinVersion:        r.MinVersion,
	}

	var wire getTLSConfigRESTResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/certificate/tls-config", &wireReq, &wire); err != nil {
		return nil, err
	}
	if !wire.Configured {
		return nil, errors.NewInternal("rest.certificate.GetTLSConfig",
			"server did not return a configured TLS configuration")
	}

	// The server validated that a TLS config can be built but does not
	// transmit a *tls.Config (not JSON-serializable). Reconstruct a
	// minimal config from the caller's request so the response matches
	// the SDK contract.
	cfg := &tls.Config{
		ServerName: r.ServerName,
	}
	switch r.MinVersion {
	case "1.3":
		cfg.MinVersion = tls.VersionTLS13
	case "", "1.2":
		cfg.MinVersion = tls.VersionTLS12
	}
	if r.IsServer && r.RequireClientCert {
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return &certificate.GetTLSConfigResponse{Config: cfg}, nil
}

// =============================================================================
// GenerateCRL
// =============================================================================

// generateCRLRESTResponse mirrors the xkms.GenerateCRLResponse wire
// shape exposed by /api/v1/xkms/ca/crl. The server returns the CRL as
// base64-encoded bytes (PEM payload); JSON unmarshals that into the
// []byte field automatically.
type generateCRLRESTResponse struct {
	CRLPEM []byte `json:"crl_pem"`
}

func (h *certificateRESTHandler) handleGenerateCRL(ctx context.Context, req interface{}) (interface{}, error) {
	// The SDK exposes GenerateCRLRequest as a zero-fielded struct today;
	// accept both *certificate.GenerateCRLRequest and a bare nil for
	// callers that don't construct one.
	if req != nil {
		if _, ok := req.(*certificate.GenerateCRLRequest); !ok {
			return nil, errors.NewInvalidInput("rest.certificate.GenerateCRL",
				"invalid request type")
		}
	}

	// The CRL endpoint is mounted under the xkms route group because
	// the underlying implementation lives in the xkms CA package; the
	// service-layer plumbing makes it available to Certificate clients
	// transparently.
	var wire generateCRLRESTResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/xkms/ca/crl", struct{}{}, &wire); err != nil {
		return nil, err
	}

	// The wire payload is PEM bytes when the server already encoded
	// the CRL, or DER bytes if not. Detect by looking for the standard
	// PEM header and PEM-encode the DER form on demand so the SDK
	// always returns a textual CRLPEM string.
	pemString := string(wire.CRLPEM)
	if !strings.Contains(pemString, "-----BEGIN") {
		encoded := pem.EncodeToMemory(&pem.Block{
			Type:  "X509 CRL",
			Bytes: wire.CRLPEM,
		})
		pemString = string(encoded)
	}

	return &certificate.GenerateCRLResponse{CRLPEM: pemString}, nil
}
