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

// Package mcp implements Certificate MCP integration handlers.
package mcp

import (
	"context"
	"encoding/json"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// registerCertificateTools registers all Certificate tool handlers on the ToolRegistry.
func (r *ToolRegistry) registerCertificateTools() {
	certificateTools := getAllCertificateTools()
	for _, tool := range certificateTools {
		r.addTool(ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: convertInputSchema(tool.InputSchema),
		})
	}
}

// executeCertificateTool executes a Certificate tool and returns the result.
func (r *ToolRegistry) executeCertificateTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	certService := r.service.Certificate()
	if certService == nil {
		return nil, api.NewServiceNotConfiguredError("certificate")
	}

	switch name {
	case "quicbaas_certificate_issue":
		return r.handleCertificateIssue(ctx, certService, args)
	case "quicbaas_certificate_sign_csr":
		return r.handleCertificateSignCSR(ctx, certService, args)
	case "quicbaas_certificate_revoke":
		return r.handleCertificateRevoke(ctx, certService, args)
	case "quicbaas_certificate_verify":
		return r.handleCertificateVerify(ctx, certService, args)
	case "quicbaas_certificate_get_ca":
		return r.handleCertificateGetCA(ctx, certService, args)
	case "quicbaas_certificate_get_tls_config":
		return r.handleCertificateGetTLSConfig(ctx, certService, args)

	// Certificate extensions — Security().CertificateExt(). See
	// certificate_extensions_mcp.go.
	case "quicbaas_certificate_cross_sign":
		return r.handleCertificateCrossSign(ctx, certService, args)
	case "quicbaas_certificate_chain":
		return r.handleCertificateChain(ctx, certService, args)
	case "quicbaas_certificate_issue_device_identity":
		return r.handleCertificateDeviceIdentity(ctx, certService, args)
	default:
		return nil, api.NewUnknownToolError(name)
	}
}

// handleCertificateIssue handles the quicbaas_certificate_issue tool.
func (r *ToolRegistry) handleCertificateIssue(ctx context.Context, svc service.CertificateService, args map[string]interface{}) (interface{}, error) {
	commonName := getString(args, "common_name")
	if commonName == "" {
		return nil, api.NewRequiredFieldError("common_name")
	}

	req := &service.IssueCertificateRequest{
		CommonName:         commonName,
		Organization:       getString(args, "organization"),
		OrganizationalUnit: getString(args, "organizational_unit"),
		Country:            getString(args, "country"),
		Province:           getString(args, "province"),
		Locality:           getString(args, "locality"),
		Usage:              getString(args, "usage"),
		KeyAlgorithm:       getString(args, "key_algorithm"),
	}

	// Parse DNS names
	if dnsNamesJSON := getString(args, "dns_names"); dnsNamesJSON != "" {
		var dnsNames []string
		if err := json.Unmarshal([]byte(dnsNamesJSON), &dnsNames); err == nil {
			req.DNSNames = dnsNames
		}
	}

	// Parse IP addresses
	if ipAddrsJSON := getString(args, "ip_addresses"); ipAddrsJSON != "" {
		var ipAddrs []string
		if err := json.Unmarshal([]byte(ipAddrsJSON), &ipAddrs); err == nil {
			req.IPAddresses = ipAddrs
		}
	}

	// Parse email addresses
	if emailsJSON := getString(args, "email_addresses"); emailsJSON != "" {
		var emails []string
		if err := json.Unmarshal([]byte(emailsJSON), &emails); err == nil {
			req.EmailAddresses = emails
		}
	}

	// Parse URIs
	if urisJSON := getString(args, "uris"); urisJSON != "" {
		var uris []string
		if err := json.Unmarshal([]byte(urisJSON), &uris); err == nil {
			req.URIs = uris
		}
	}

	// Parse validity days
	if validityDays := getInt(args, "validity_days"); validityDays > 0 {
		req.ValidityDays = validityDays
	}

	// Parse is_ca
	if isCA := getString(args, "is_ca"); isCA == "true" {
		req.IsCA = true
	}

	// Parse max_path_len
	if maxPathLen := getInt(args, "max_path_len"); maxPathLen > 0 {
		req.MaxPathLen = maxPathLen
	}

	// Parse key_size
	if keySize := getInt(args, "key_size"); keySize > 0 {
		req.KeySize = keySize
	}

	resp, err := svc.IssueCertificate(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("issue certificate", err)
	}

	return map[string]interface{}{
		"certificate_pem":    resp.CertificatePEM,
		"private_key_pem":    resp.PrivateKeyPEM,
		"ca_certificate_pem": resp.CACertificatePEM,
		"chain_pem":          resp.ChainPEM,
		"serial_number":      resp.SerialNumber,
		"not_before":         resp.NotBefore.Format("2006-01-02T15:04:05Z07:00"),
		"not_after":          resp.NotAfter.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

// handleCertificateSignCSR handles the quicbaas_certificate_sign_csr tool.
func (r *ToolRegistry) handleCertificateSignCSR(ctx context.Context, svc service.CertificateService, args map[string]interface{}) (interface{}, error) {
	csrPEM := getString(args, "csr_pem")
	if csrPEM == "" {
		return nil, api.NewRequiredFieldError("csr_pem")
	}

	req := &service.SignCSRRequest{
		CSRPEM: csrPEM,
		Usage:  getString(args, "usage"),
	}

	// Parse validity days
	if validityDays := getInt(args, "validity_days"); validityDays > 0 {
		req.ValidityDays = validityDays
	}

	resp, err := svc.SignCSR(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("sign csr", err)
	}

	return map[string]interface{}{
		"certificate_pem":    resp.CertificatePEM,
		"ca_certificate_pem": resp.CACertificatePEM,
		"chain_pem":          resp.ChainPEM,
		"serial_number":      resp.SerialNumber,
		"not_before":         resp.NotBefore.Format("2006-01-02T15:04:05Z07:00"),
		"not_after":          resp.NotAfter.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

// handleCertificateRevoke handles the quicbaas_certificate_revoke tool.
func (r *ToolRegistry) handleCertificateRevoke(ctx context.Context, svc service.CertificateService, args map[string]interface{}) (interface{}, error) {
	serialNumber := getString(args, "serial_number")
	if serialNumber == "" {
		return nil, api.NewRequiredFieldError("serial_number")
	}

	req := &service.RevokeCertificateRequest{
		SerialNumber: serialNumber,
	}

	resp, err := svc.RevokeCertificate(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("revoke certificate", err)
	}

	return map[string]interface{}{
		"success": resp.Success,
	}, nil
}

// handleCertificateVerify handles the quicbaas_certificate_verify tool.
func (r *ToolRegistry) handleCertificateVerify(ctx context.Context, svc service.CertificateService, args map[string]interface{}) (interface{}, error) {
	certificatePEM := getString(args, "certificate_pem")
	if certificatePEM == "" {
		return nil, api.NewRequiredFieldError("certificate_pem")
	}

	req := &service.VerifyCertificateRequest{
		CertificatePEM: certificatePEM,
	}

	resp, err := svc.VerifyCertificate(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("verify certificate", err)
	}

	result := map[string]interface{}{
		"valid": resp.Valid,
	}
	if resp.ErrorMessage != "" {
		result["error_message"] = resp.ErrorMessage
	}

	return result, nil
}

// handleCertificateGetCA handles the quicbaas_certificate_get_ca tool.
func (r *ToolRegistry) handleCertificateGetCA(ctx context.Context, svc service.CertificateService, args map[string]interface{}) (interface{}, error) {
	req := &service.GetCACertificateRequest{}

	resp, err := svc.GetCACertificate(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("get ca certificate", err)
	}

	return map[string]interface{}{
		"ca_certificate_pem": resp.CACertificatePEM,
		"ca_bundle_pem":      resp.CABundlePEM,
		"identity":           resp.Identity,
	}, nil
}

// handleCertificateGetTLSConfig handles the quicbaas_certificate_get_tls_config tool.
func (r *ToolRegistry) handleCertificateGetTLSConfig(ctx context.Context, svc service.CertificateService, args map[string]interface{}) (interface{}, error) {
	req := &service.GetTLSConfigRequest{
		ServerName: getString(args, "server_name"),
		MinVersion: getString(args, "min_version"),
	}

	// Parse boolean flags
	if requireClientCert := getString(args, "require_client_cert"); requireClientCert == "true" {
		req.RequireClientCert = true
	}
	if isServer := getString(args, "is_server"); isServer == "true" {
		req.IsServer = true
	}

	resp, err := svc.GetTLSConfig(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("get tls config", err)
	}

	// Return configuration parameters (not the actual tls.Config which is not serializable)
	result := map[string]interface{}{
		"configured": resp.Config != nil,
	}
	if resp.Config != nil {
		result["min_version"] = resp.Config.MinVersion
		result["server_name"] = resp.Config.ServerName
	}

	return result, nil
}

// isCertificateTool returns true if the tool name is a Certificate tool.
func isCertificateTool(name string) bool {
	switch name {
	case "quicbaas_certificate_issue",
		"quicbaas_certificate_sign_csr",
		"quicbaas_certificate_revoke",
		"quicbaas_certificate_verify",
		"quicbaas_certificate_get_ca",
		"quicbaas_certificate_get_tls_config":
		return true
	default:
		return isCertificateExtensionTool(name)
	}
}
