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

// certificate_handler.go provides typed MCP routing for the
// quicbaas.Certificate service. The generic MCP dispatch path returns an
// opaque map[string]interface{} that the SDK certificate client cannot
// type-assert into its strongly-typed *Response structs; this handler maps
// each SDK method onto its MCP tool name, builds the tool arguments, and
// decodes the JSON-RPC tool result back into the concrete certificate
// response type the client expects.
//
// Only the Certificate methods backed by a registered quicbaasd MCP tool
// are dispatched here (Issue, SignCSR, Revoke, Verify, GetCACertificate,
// GetTLSConfig). Methods with no corresponding MCP tool surface a typed
// MethodNotSupportedError rather than silently returning the wrong shape.

package mcp

import (
	"crypto/tls"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// isMCPCertificateService reports whether service resolves to the
// Certificate service.
func isMCPCertificateService(service string) bool {
	serviceName := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		serviceName = service[idx+1:]
	}
	return serviceName == "Certificate"
}

// certificateMethodToTool maps Certificate SDK method names onto the
// quicbaasd MCP tool names. Methods absent from this table have no MCP tool
// and are rejected by the handler with a typed error.
var certificateMethodToTool = map[string]string{
	"IssueCertificate":  "quicbaas_certificate_issue",
	"SignCSR":           "quicbaas_certificate_sign_csr",
	"RevokeCertificate": "quicbaas_certificate_revoke",
	"VerifyCertificate": "quicbaas_certificate_verify",
	"GetCACertificate":  "quicbaas_certificate_get_ca",
	"GetTLSConfig":      "quicbaas_certificate_get_tls_config",

	// Certificate extensions — Security().CertificateExt(). See
	// certificate_extensions_handler.go.
	"CrossSign":           "quicbaas_certificate_cross_sign",
	"Chain":               "quicbaas_certificate_chain",
	"IssueDeviceIdentity": "quicbaas_certificate_issue_device_identity",
}

// mapCertificateMethodToTool returns the MCP tool name for a Certificate
// method and whether a mapping exists.
func mapCertificateMethodToTool(method string) (string, bool) {
	tool, ok := certificateMethodToTool[method]
	return tool, ok
}

// certificateHandler transforms Certificate SDK requests/responses for the
// MCP transport.
type certificateHandler struct {
	transport *Transport
}

// newCertificateHandler constructs a Certificate MCP handler bound to t.
func newCertificateHandler(t *Transport) *certificateHandler {
	return &certificateHandler{transport: t}
}

// transformRequest builds the MCP tool arguments for a Certificate method.
func (h *certificateHandler) transformRequest(method string, req interface{}) (map[string]interface{}, error) {
	switch method {
	case "IssueCertificate":
		return h.transformIssueRequest(req)
	case "SignCSR":
		return h.transformSignCSRRequest(req)
	case "RevokeCertificate":
		return h.transformRevokeRequest(req)
	case "VerifyCertificate":
		return h.transformVerifyRequest(req)
	case "GetCACertificate":
		return map[string]interface{}{}, nil
	case "GetTLSConfig":
		return h.transformGetTLSConfigRequest(req)
	case "CrossSign":
		return h.transformCrossSignRequest(req)
	case "Chain":
		return h.transformChainRequest(req)
	case "IssueDeviceIdentity":
		return h.transformIssueDeviceIdentityRequest(req)
	default:
		return nil, newCertificateUnsupportedError(method)
	}
}

// transformResponse decodes the MCP tool result for a Certificate method
// into the concrete SDK response type.
func (h *certificateHandler) transformResponse(method string, req, resp interface{}) (interface{}, error) {
	switch method {
	case "IssueCertificate":
		return h.transformIssueResponse(resp)
	case "SignCSR":
		return h.transformSignCSRResponse(resp)
	case "RevokeCertificate":
		return h.transformRevokeResponse(resp)
	case "VerifyCertificate":
		return h.transformVerifyResponse(resp)
	case "GetCACertificate":
		return h.transformGetCAResponse(resp)
	case "GetTLSConfig":
		return h.transformGetTLSConfigResponse(req, resp)
	case "CrossSign":
		return h.transformCrossSignResponse(resp)
	case "Chain":
		return h.transformChainResponse(resp)
	case "IssueDeviceIdentity":
		return h.transformIssueDeviceIdentityResponse(resp)
	default:
		return nil, newCertificateUnsupportedError(method)
	}
}

// newCertificateUnsupportedError builds a typed error for Certificate
// methods that have no MCP tool.
func newCertificateUnsupportedError(method string) error {
	return errors.New(errors.CodeUnimplemented, "mcp.certificate."+method,
		"certificate method is not supported by the MCP transport")
}

// =============================================================================
// IssueCertificate
// =============================================================================

func (h *certificateHandler) transformIssueRequest(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*certificate.IssueCertificateRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.certificate.IssueCertificate",
			"invalid request type, expected *certificate.IssueCertificateRequest")
	}

	args := map[string]interface{}{
		"common_name": r.CommonName,
	}
	putStringArg(args, "organization", r.Organization)
	putStringArg(args, "organizational_unit", r.OrganizationalUnit)
	putStringArg(args, "country", r.Country)
	putStringArg(args, "province", r.Province)
	putStringArg(args, "locality", r.Locality)
	putStringArg(args, "usage", r.Usage)
	putStringArg(args, "key_algorithm", r.KeyAlgorithm)

	if err := putJSONArrayArg(args, "dns_names", r.DNSNames); err != nil {
		return nil, err
	}
	if err := putJSONArrayArg(args, "ip_addresses", r.IPAddresses); err != nil {
		return nil, err
	}
	if err := putJSONArrayArg(args, "email_addresses", r.EmailAddresses); err != nil {
		return nil, err
	}
	if err := putJSONArrayArg(args, "uris", r.URIs); err != nil {
		return nil, err
	}

	if r.ValidityDays > 0 {
		args["validity_days"] = r.ValidityDays
	}
	if r.IsCA {
		args["is_ca"] = "true"
	}
	if r.MaxPathLen > 0 {
		args["max_path_len"] = r.MaxPathLen
	}
	if r.KeySize > 0 {
		args["key_size"] = r.KeySize
	}

	return args, nil
}

func (h *certificateHandler) transformIssueResponse(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.certificate.IssueCertificate")
	if err != nil {
		return nil, err
	}
	return &certificate.IssueCertificateResponse{
		CertificatePEM:   getResultString(m, "certificate_pem"),
		PrivateKeyPEM:    getResultString(m, "private_key_pem"),
		CACertificatePEM: getResultString(m, "ca_certificate_pem"),
		ChainPEM:         getResultString(m, "chain_pem"),
		SerialNumber:     getResultString(m, "serial_number"),
		NotBefore:        getResultTime(m, "not_before"),
		NotAfter:         getResultTime(m, "not_after"),
	}, nil
}

// =============================================================================
// SignCSR
// =============================================================================

func (h *certificateHandler) transformSignCSRRequest(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*certificate.SignCSRRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.certificate.SignCSR",
			"invalid request type, expected *certificate.SignCSRRequest")
	}
	args := map[string]interface{}{
		"csr_pem": r.CSRPEM,
	}
	putStringArg(args, "usage", r.Usage)
	if r.ValidityDays > 0 {
		args["validity_days"] = r.ValidityDays
	}
	return args, nil
}

func (h *certificateHandler) transformSignCSRResponse(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.certificate.SignCSR")
	if err != nil {
		return nil, err
	}
	return &certificate.SignCSRResponse{
		CertificatePEM:   getResultString(m, "certificate_pem"),
		CACertificatePEM: getResultString(m, "ca_certificate_pem"),
		ChainPEM:         getResultString(m, "chain_pem"),
		SerialNumber:     getResultString(m, "serial_number"),
		NotBefore:        getResultTime(m, "not_before"),
		NotAfter:         getResultTime(m, "not_after"),
	}, nil
}

// =============================================================================
// RevokeCertificate
// =============================================================================

func (h *certificateHandler) transformRevokeRequest(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*certificate.RevokeCertificateRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.certificate.RevokeCertificate",
			"invalid request type, expected *certificate.RevokeCertificateRequest")
	}
	return map[string]interface{}{
		"serial_number": r.SerialNumber,
	}, nil
}

func (h *certificateHandler) transformRevokeResponse(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.certificate.RevokeCertificate")
	if err != nil {
		return nil, err
	}
	return &certificate.RevokeCertificateResponse{
		Success: getResultBool(m, "success"),
	}, nil
}

// =============================================================================
// VerifyCertificate
// =============================================================================

func (h *certificateHandler) transformVerifyRequest(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*certificate.VerifyCertificateRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.certificate.VerifyCertificate",
			"invalid request type, expected *certificate.VerifyCertificateRequest")
	}
	return map[string]interface{}{
		"certificate_pem": r.CertificatePEM,
	}, nil
}

func (h *certificateHandler) transformVerifyResponse(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.certificate.VerifyCertificate")
	if err != nil {
		return nil, err
	}
	return &certificate.VerifyCertificateResponse{
		Valid:        getResultBool(m, "valid"),
		ErrorMessage: getResultString(m, "error_message"),
	}, nil
}

// =============================================================================
// GetCACertificate
// =============================================================================

func (h *certificateHandler) transformGetCAResponse(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.certificate.GetCACertificate")
	if err != nil {
		return nil, err
	}
	return &certificate.GetCACertificateResponse{
		CACertificatePEM: getResultString(m, "ca_certificate_pem"),
		CABundlePEM:      getResultString(m, "ca_bundle_pem"),
		Identity:         getResultString(m, "identity"),
	}, nil
}

// =============================================================================
// GetTLSConfig
// =============================================================================

func (h *certificateHandler) transformGetTLSConfigRequest(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*certificate.GetTLSConfigRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.certificate.GetTLSConfig",
			"invalid request type, expected *certificate.GetTLSConfigRequest")
	}
	args := map[string]interface{}{}
	putStringArg(args, "server_name", r.ServerName)
	putStringArg(args, "min_version", r.MinVersion)
	if r.RequireClientCert {
		args["require_client_cert"] = "true"
	}
	if r.IsServer {
		args["is_server"] = "true"
	}
	return args, nil
}

// transformGetTLSConfigResponse reconstructs a minimal *tls.Config from the
// originating request. The server reports a boolean "configured" flag plus
// the negotiated parameters but cannot transmit a *tls.Config (not
// JSON-serializable); the SDK therefore rebuilds the config from the caller
// request, matching the REST transport behavior.
func (h *certificateHandler) transformGetTLSConfigResponse(req, resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.certificate.GetTLSConfig")
	if err != nil {
		return nil, err
	}
	if !getResultBool(m, "configured") {
		return nil, errors.New(errors.CodeInternal, "mcp.certificate.GetTLSConfig",
			"server did not return a configured TLS configuration")
	}

	r, ok := req.(*certificate.GetTLSConfigRequest)
	if !ok || r == nil {
		r = &certificate.GetTLSConfigRequest{}
	}

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

// transformTimeFromString parses an RFC 3339 timestamp string into a
// time.Time, returning the zero value if parsing fails.
func transformTimeFromString(s string) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	return time.Time{}
}
