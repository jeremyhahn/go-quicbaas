//go:build mcp

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

// Package mcp implements Certificate tool definitions for the Model Context Protocol.
package mcp

// Certificate tool definitions for certificate management operations (6 tools).
// Each tool follows the MCP specification with JSON schema validation.

var (
	// CertificateIssueTool issues a new certificate.
	CertificateIssueTool = Tool{
		Name:        "quicbaas_certificate_issue",
		Description: "Issue a new certificate with the specified parameters",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"common_name": {
					Type:        "string",
					Description: "Common name (CN) for the certificate",
				},
				"organization": {
					Type:        "string",
					Description: "Organization name for the certificate",
				},
				"organizational_unit": {
					Type:        "string",
					Description: "Organizational unit for the certificate",
				},
				"country": {
					Type:        "string",
					Description: "Country code (e.g., US)",
				},
				"province": {
					Type:        "string",
					Description: "State or province",
				},
				"locality": {
					Type:        "string",
					Description: "City or locality",
				},
				"dns_names": {
					Type:        "string",
					Description: "JSON array of DNS names for SAN extension",
				},
				"ip_addresses": {
					Type:        "string",
					Description: "JSON array of IP addresses for SAN extension",
				},
				"email_addresses": {
					Type:        "string",
					Description: "JSON array of email addresses for SAN extension",
				},
				"uris": {
					Type:        "string",
					Description: "JSON array of URIs for SAN extension",
				},
				"validity_days": {
					Type:        "string",
					Description: "Number of days the certificate is valid",
				},
				"usage": {
					Type:        "string",
					Description: "Certificate usage: client, server, or both",
					Enum:        []string{"client", "server", "both"},
				},
				"is_ca": {
					Type:        "string",
					Description: "Set to 'true' to issue a CA certificate",
				},
				"max_path_len": {
					Type:        "string",
					Description: "Maximum path length for CA certificates",
				},
				"key_algorithm": {
					Type:        "string",
					Description: "Key algorithm: ecdsa or rsa",
					Enum:        []string{"ecdsa", "rsa"},
				},
				"key_size": {
					Type:        "string",
					Description: "Key size in bits (e.g., 2048, 4096 for RSA; 256, 384 for ECDSA)",
				},
			},
			Required: []string{"common_name"},
		},
	}

	// CertificateSignCSRTool signs a Certificate Signing Request.
	CertificateSignCSRTool = Tool{
		Name:        "quicbaas_certificate_sign_csr",
		Description: "Sign a Certificate Signing Request (CSR)",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"csr_pem": {
					Type:        "string",
					Description: "PEM-encoded Certificate Signing Request",
				},
				"validity_days": {
					Type:        "string",
					Description: "Number of days the signed certificate is valid",
				},
				"usage": {
					Type:        "string",
					Description: "Certificate usage: client, server, or both",
					Enum:        []string{"client", "server", "both"},
				},
			},
			Required: []string{"csr_pem"},
		},
	}

	// CertificateRevokeTool revokes a certificate.
	CertificateRevokeTool = Tool{
		Name:        "quicbaas_certificate_revoke",
		Description: "Revoke a certificate by serial number",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"serial_number": {
					Type:        "string",
					Description: "Serial number of the certificate to revoke",
				},
			},
			Required: []string{"serial_number"},
		},
	}

	// CertificateVerifyTool verifies a certificate.
	CertificateVerifyTool = Tool{
		Name:        "quicbaas_certificate_verify",
		Description: "Verify a certificate against the CA chain",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"certificate_pem": {
					Type:        "string",
					Description: "PEM-encoded certificate to verify",
				},
			},
			Required: []string{"certificate_pem"},
		},
	}

	// CertificateGetCATool retrieves the CA certificate.
	CertificateGetCATool = Tool{
		Name:        "quicbaas_certificate_get_ca",
		Description: "Get the CA certificate and bundle",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{},
			Required:   []string{},
		},
	}

	// CertificateGetTLSConfigTool retrieves TLS configuration.
	CertificateGetTLSConfigTool = Tool{
		Name:        "quicbaas_certificate_get_tls_config",
		Description: "Get TLS configuration parameters",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"server_name": {
					Type:        "string",
					Description: "Server name for SNI",
				},
				"require_client_cert": {
					Type:        "string",
					Description: "Set to 'true' to require client certificates",
				},
				"is_server": {
					Type:        "string",
					Description: "Set to 'true' for server configuration",
				},
				"min_version": {
					Type:        "string",
					Description: "Minimum TLS version: 1.2 or 1.3",
					Enum:        []string{"1.2", "1.3"},
				},
			},
			Required: []string{},
		},
	}
)

// getAllCertificateTools returns all Certificate tools: the six base verbs
// plus the three certificate-extension verbs declared in
// certificate_extensions_mcp.go.
func getAllCertificateTools() []Tool {
	tools := []Tool{
		CertificateIssueTool,
		CertificateSignCSRTool,
		CertificateRevokeTool,
		CertificateVerifyTool,
		CertificateGetCATool,
		CertificateGetTLSConfigTool,
	}
	return append(tools, getAllCertificateExtensionTools()...)
}
