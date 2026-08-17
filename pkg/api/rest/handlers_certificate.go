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
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Certificate Service Route Registration
// =============================================================================

// =============================================================================
// Request/Response Types for REST API
// =============================================================================

// IssueCertificateRESTRequest represents the JSON request body for certificate issuance.
type IssueCertificateRESTRequest struct {
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
	Usage              string            `json:"usage,omitempty"` // "client", "server", "both"
	IsCA               bool              `json:"is_ca,omitempty"`
	MaxPathLen         int               `json:"max_path_len,omitempty"`
	KeyAlgorithm       string            `json:"key_algorithm,omitempty"` // "ecdsa", "rsa"
	KeySize            int               `json:"key_size,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
}

// IssueCertificateRESTResponse represents the JSON response for certificate issuance.
type IssueCertificateRESTResponse struct {
	CertificatePEM   string `json:"certificate_pem"`
	PrivateKeyPEM    string `json:"private_key_pem"`
	CACertificatePEM string `json:"ca_certificate_pem"`
	ChainPEM         string `json:"chain_pem"`
	SerialNumber     string `json:"serial_number"`
	NotBefore        int64  `json:"not_before"`
	NotAfter         int64  `json:"not_after"`
	Timestamp        int64  `json:"timestamp"`
}

// SignCSRRESTRequest represents the JSON request body for signing a CSR.
type SignCSRRESTRequest struct {
	CSRPEM       string `json:"csr_pem"`
	ValidityDays int    `json:"validity_days,omitempty"`
	Usage        string `json:"usage,omitempty"` // "client", "server", "both"
}

// SignCSRRESTResponse represents the JSON response for signing a CSR.
type SignCSRRESTResponse struct {
	CertificatePEM   string `json:"certificate_pem"`
	CACertificatePEM string `json:"ca_certificate_pem"`
	ChainPEM         string `json:"chain_pem"`
	SerialNumber     string `json:"serial_number"`
	NotBefore        int64  `json:"not_before"`
	NotAfter         int64  `json:"not_after"`
	Timestamp        int64  `json:"timestamp"`
}

// RevokeCertificateRESTRequest represents the JSON request body for certificate revocation.
type RevokeCertificateRESTRequest struct {
	SerialNumber string `json:"serial_number"`
}

// RevokeCertificateRESTResponse represents the JSON response for certificate revocation.
type RevokeCertificateRESTResponse struct {
	Success   bool  `json:"success"`
	Timestamp int64 `json:"timestamp"`
}

// VerifyCertificateRESTRequest represents the JSON request body for certificate verification.
type VerifyCertificateRESTRequest struct {
	CertificatePEM string `json:"certificate_pem"`
}

// VerifyCertificateRESTResponse represents the JSON response for certificate verification.
type VerifyCertificateRESTResponse struct {
	Valid        bool   `json:"valid"`
	ErrorMessage string `json:"error_message,omitempty"`
	Timestamp    int64  `json:"timestamp"`
}

// GetCACertificateRESTResponse represents the JSON response for getting the CA certificate.
type GetCACertificateRESTResponse struct {
	CACertificatePEM string `json:"ca_certificate_pem"`
	CABundlePEM      string `json:"ca_bundle_pem"`
	Identity         string `json:"identity"`
	Timestamp        int64  `json:"timestamp"`
}

// GetTLSConfigRESTRequest represents the JSON request body for getting TLS configuration.
type GetTLSConfigRESTRequest struct {
	ServerName        string `json:"server_name,omitempty"`
	RequireClientCert bool   `json:"require_client_cert,omitempty"`
	IsServer          bool   `json:"is_server,omitempty"`
	MinVersion        string `json:"min_version,omitempty"` // "1.2", "1.3"
}

// GetTLSConfigRESTResponse represents the JSON response for getting TLS configuration.
type GetTLSConfigRESTResponse struct {
	Configured bool  `json:"configured"`
	Timestamp  int64 `json:"timestamp"`
}

// =============================================================================
// Handler Functions
// =============================================================================

// HandleIssueCertificate handles POST /v1/certificate/issue - Issue a new certificate.
func (s *Server) HandleIssueCertificate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req IssueCertificateRESTRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.CommonName == "" {
		RespondBadRequest(w, r, "common_name is required")
		return
	}

	certService := s.service.Certificate()
	if certService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrCertificateServiceUnavailable)
		return
	}

	svcReq := &service.IssueCertificateRequest{
		CommonName:         req.CommonName,
		Organization:       req.Organization,
		OrganizationalUnit: req.OrganizationalUnit,
		Country:            req.Country,
		Province:           req.Province,
		Locality:           req.Locality,
		DNSNames:           req.DNSNames,
		IPAddresses:        req.IPAddresses,
		EmailAddresses:     req.EmailAddresses,
		URIs:               req.URIs,
		ValidityDays:       req.ValidityDays,
		Usage:              req.Usage,
		IsCA:               req.IsCA,
		MaxPathLen:         req.MaxPathLen,
		KeyAlgorithm:       req.KeyAlgorithm,
		KeySize:            req.KeySize,
		Metadata:           req.Metadata,
	}

	svcResp, err := certService.IssueCertificate(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	resp := &IssueCertificateRESTResponse{
		CertificatePEM:   svcResp.CertificatePEM,
		PrivateKeyPEM:    svcResp.PrivateKeyPEM,
		CACertificatePEM: svcResp.CACertificatePEM,
		ChainPEM:         svcResp.ChainPEM,
		SerialNumber:     svcResp.SerialNumber,
		NotBefore:        svcResp.NotBefore.Unix(),
		NotAfter:         svcResp.NotAfter.Unix(),
		Timestamp:        now.Unix(),
	}

	RespondJSON(w, r, http.StatusCreated, resp)
}

// HandleSignCSR handles POST /v1/certificate/sign-csr - Sign a CSR.
func (s *Server) HandleSignCSR(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req SignCSRRESTRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.CSRPEM == "" {
		RespondBadRequest(w, r, "csr_pem is required")
		return
	}

	certService := s.service.Certificate()
	if certService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrCertificateServiceUnavailable)
		return
	}

	svcReq := &service.SignCSRRequest{
		CSRPEM:       req.CSRPEM,
		ValidityDays: req.ValidityDays,
		Usage:        req.Usage,
	}

	svcResp, err := certService.SignCSR(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	resp := &SignCSRRESTResponse{
		CertificatePEM:   svcResp.CertificatePEM,
		CACertificatePEM: svcResp.CACertificatePEM,
		ChainPEM:         svcResp.ChainPEM,
		SerialNumber:     svcResp.SerialNumber,
		NotBefore:        svcResp.NotBefore.Unix(),
		NotAfter:         svcResp.NotAfter.Unix(),
		Timestamp:        now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleRevokeCertificate handles POST /v1/certificate/revoke - Revoke a certificate.
func (s *Server) HandleRevokeCertificate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req RevokeCertificateRESTRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.SerialNumber == "" {
		RespondBadRequest(w, r, "serial_number is required")
		return
	}

	certService := s.service.Certificate()
	if certService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrCertificateServiceUnavailable)
		return
	}

	svcReq := &service.RevokeCertificateRequest{
		SerialNumber: req.SerialNumber,
	}

	svcResp, err := certService.RevokeCertificate(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	resp := &RevokeCertificateRESTResponse{
		Success:   svcResp.Success,
		Timestamp: now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleVerifyCertificate handles POST /v1/certificate/verify - Verify a certificate.
func (s *Server) HandleVerifyCertificate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req VerifyCertificateRESTRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.CertificatePEM == "" {
		RespondBadRequest(w, r, "certificate_pem is required")
		return
	}

	certService := s.service.Certificate()
	if certService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrCertificateServiceUnavailable)
		return
	}

	svcReq := &service.VerifyCertificateRequest{
		CertificatePEM: req.CertificatePEM,
	}

	svcResp, err := certService.VerifyCertificate(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	resp := &VerifyCertificateRESTResponse{
		Valid:        svcResp.Valid,
		ErrorMessage: svcResp.ErrorMessage,
		Timestamp:    now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGetCACertificate handles GET /v1/certificate/ca - Get the CA certificate.
func (s *Server) HandleGetCACertificate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	certService := s.service.Certificate()
	if certService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrCertificateServiceUnavailable)
		return
	}

	svcReq := &service.GetCACertificateRequest{}

	svcResp, err := certService.GetCACertificate(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	resp := &GetCACertificateRESTResponse{
		CACertificatePEM: svcResp.CACertificatePEM,
		CABundlePEM:      svcResp.CABundlePEM,
		Identity:         svcResp.Identity,
		Timestamp:        now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGetTLSConfig handles POST /v1/certificate/tls-config - Get TLS configuration.
func (s *Server) HandleGetTLSConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req GetTLSConfigRESTRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			RespondBadRequest(w, r, "invalid JSON body")
			return
		}
	}

	certService := s.service.Certificate()
	if certService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrCertificateServiceUnavailable)
		return
	}

	svcReq := &service.GetTLSConfigRequest{
		ServerName:        req.ServerName,
		RequireClientCert: req.RequireClientCert,
		IsServer:          req.IsServer,
		MinVersion:        req.MinVersion,
	}

	_, err = certService.GetTLSConfig(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	resp := &GetTLSConfigRESTResponse{
		Configured: true,
		Timestamp:  now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// CA Administration Request/Response Types
// =============================================================================

// CAInfoRESTResponse is the JSON response body for GET /api/v1/certificate/ca/info.
type CAInfoRESTResponse struct {
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
	Timestamp          int64  `json:"timestamp"`
}

// CABundleRESTResponse is the JSON response body for GET /api/v1/certificate/ca/bundle.
type CABundleRESTResponse struct {
	BundlePEM string `json:"bundle_pem"`
	Timestamp int64  `json:"timestamp"`
}

// CertificateSummaryREST is the REST projection of a single certificate
// in a ListCertificates response.
type CertificateSummaryREST struct {
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

// ListCertificatesRESTResponse is the JSON response body for
// GET /api/v1/certificate/ca/list.
type ListCertificatesRESTResponse struct {
	Certificates []CertificateSummaryREST `json:"certificates"`
	TotalCount   int                      `json:"total_count"`
	Timestamp    int64                    `json:"timestamp"`
}

// =============================================================================
// CA Administration Handlers
// =============================================================================

// HandleCAInfo handles GET /api/v1/certificate/ca/info — returns parsed
// metadata for the active issuing CA.
func (s *Server) HandleCAInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	certService := s.service.Certificate()
	if certService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrCertificateServiceUnavailable)
		return
	}

	svcResp, err := certService.CAInfo(ctx, &service.CAInfoRequest{})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	RespondJSON(w, r, http.StatusOK, &CAInfoRESTResponse{
		CommonName:         svcResp.CommonName,
		Subject:            svcResp.Subject,
		Issuer:             svcResp.Issuer,
		SerialNumber:       svcResp.SerialNumber,
		NotBefore:          svcResp.NotBefore.Unix(),
		NotAfter:           svcResp.NotAfter.Unix(),
		IsCA:               svcResp.IsCA,
		KeyType:            svcResp.KeyType,
		KeyBits:            svcResp.KeyBits,
		SignatureAlgorithm: svcResp.SignatureAlgorithm,
		CertificatePEM:     svcResp.CertificatePEM,
		CABundlePEM:        svcResp.CABundlePEM,
		Timestamp:          now.Unix(),
	})
}

// HandleCABundle handles GET /api/v1/certificate/ca/bundle — returns the
// CA trust-chain PEM. Equivalent to the bundle field on the legacy
// GetCACertificate handler, surfaced as its own resource for the
// canonical `quicbaas ca bundle` CLI verb.
func (s *Server) HandleCABundle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	certService := s.service.Certificate()
	if certService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrCertificateServiceUnavailable)
		return
	}

	svcResp, err := certService.GetCABundle(ctx, &service.GetCABundleRequest{})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &CABundleRESTResponse{
		BundlePEM: svcResp.BundlePEM,
		Timestamp: time.Now().Unix(),
	})
}

// HandleListCertificates handles GET /api/v1/certificate/ca/list —
// returns summaries of certificates stored in the CA's certificate
// store. Query params: common_name, include_revoked, include_expired,
// limit.
func (s *Server) HandleListCertificates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	certService := s.service.Certificate()
	if certService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrCertificateServiceUnavailable)
		return
	}

	q := r.URL.Query()
	req := &service.ListCertificatesRequest{
		CommonNameFilter: q.Get("common_name"),
		IncludeRevoked:   parseBoolQuery(q.Get("include_revoked")),
		IncludeExpired:   parseBoolQuery(q.Get("include_expired")),
	}
	if v := q.Get("limit"); v != "" {
		if n, perr := strconv.Atoi(v); perr == nil {
			if n < 0 {
				RespondBadRequest(w, r, "limit must not be negative")
				return
			}
			req.Limit = n
		} else {
			RespondBadRequest(w, r, "limit is not a valid integer")
			return
		}
	}

	svcResp, err := certService.ListCertificates(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	out := &ListCertificatesRESTResponse{
		Certificates: make([]CertificateSummaryREST, 0, len(svcResp.Certificates)),
		TotalCount:   svcResp.TotalCount,
		Timestamp:    time.Now().Unix(),
	}
	for _, c := range svcResp.Certificates {
		out.Certificates = append(out.Certificates, CertificateSummaryREST{
			CommonName:   c.CommonName,
			Subject:      c.Subject,
			Issuer:       c.Issuer,
			SerialNumber: c.SerialNumber,
			NotBefore:    c.NotBefore.Unix(),
			NotAfter:     c.NotAfter.Unix(),
			IsCA:         c.IsCA,
			KeyUsage:     c.KeyUsage,
			DNSNames:     append([]string(nil), c.DNSNames...),
		})
	}

	RespondJSON(w, r, http.StatusOK, out)
}

// parseBoolQuery treats "1", "true", "yes" (case-insensitive) as true,
// everything else as false.
func parseBoolQuery(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}
