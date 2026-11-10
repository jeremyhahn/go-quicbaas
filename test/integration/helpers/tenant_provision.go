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

//go:build integration

package helpers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// TenantProvisionError reports which step of provisioning a tenant, or of
// logging in as its admin, failed.
type TenantProvisionError struct {
	// Step names the failed step, e.g. "provision".
	Step string

	// TenantID is the tenant being provisioned.
	TenantID string

	// Cause is the underlying failure.
	Cause error
}

func (e *TenantProvisionError) Error() string {
	return "provision tenant " + e.TenantID + ": " + e.Step + ": " + e.Cause.Error()
}

func (e *TenantProvisionError) Unwrap() error { return e.Cause }

// MissingAdminCertError reports a cert-mode provision response that carried
// no admin certificate.
type MissingAdminCertError struct {
	// Body is the raw provision response.
	Body string
}

func (e *MissingAdminCertError) Error() string {
	return "provision response carries no admin_cert_pem: " + e.Body
}

// ProvisionedTenant is a tenant the system admin provisioned, and the
// credential of its admin.
type ProvisionedTenant struct {
	// TenantID is the provisioned tenant.
	TenantID string

	// AdminUserID is the id of the admin user the server created, as the
	// provision response reports it. Empty when the response has none.
	AdminUserID string

	// AdminCertPEM is the admin certificate the server signed; its subject
	// names the tenant.
	AdminCertPEM []byte

	// AdminKeyPEM is the private key of AdminCertPEM. It never left the
	// test process.
	AdminKeyPEM []byte

	// AdminBearer is the admin's bearer from a PIV login with AdminCertPEM.
	// The server binds it to TenantID.
	AdminBearer string
}

// ProvisionTenant provisions tenantID the way an operator does and logs in
// as its admin. The system admin (systemBearer) calls
// POST /api/v1/tenants/{tenant_id}/provision in cert mode with a CSR whose
// key stays here; the server creates the tenant and its admin and signs the
// admin certificate. A PIV login with that certificate yields a bearer bound
// to tenantID, which is what every tenant-scoped route requires: a
// system-bound bearer may not act for another tenant.
func ProvisionTenant(ctx context.Context, baseURL, tenantID, systemBearer string) (*ProvisionedTenant, error) {
	baseURL = strings.TrimRight(baseURL, "/")

	csrPEM, keyPEM, err := newTenantAdminCSR()
	if err != nil {
		return nil, &TenantProvisionError{Step: "create admin CSR", TenantID: tenantID, Cause: err}
	}

	reqBody := map[string]interface{}{
		"isolation_type":   "prefix",
		"admin_enrollment": "cert",
		"admin_csr_pem":    string(csrPEM),
	}
	endpoint := baseURL + "/api/v1/tenants/" + url.PathEscape(tenantID) + "/provision"
	body, err := postJSON(ctx, endpoint, systemBearer, reqBody)
	if err != nil {
		return nil, &TenantProvisionError{Step: "provision", TenantID: tenantID, Cause: err}
	}

	var resp struct {
		AdminCertPEM string `json:"admin_cert_pem"`
		AdminUserID  string `json:"admin_user_id"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, &TenantProvisionError{Step: "decode provision response", TenantID: tenantID, Cause: err}
	}
	if resp.AdminCertPEM == "" {
		return nil, &TenantProvisionError{Step: "decode provision response", TenantID: tenantID,
			Cause: &MissingAdminCertError{Body: string(body)}}
	}

	certPEM := []byte(resp.AdminCertPEM)
	bearer, err := LoginWithCert(ctx, baseURL, certPEM, keyPEM)
	if err != nil {
		return nil, &TenantProvisionError{Step: "admin PIV login", TenantID: tenantID, Cause: err}
	}

	return &ProvisionedTenant{
		TenantID:     tenantID,
		AdminUserID:  resp.AdminUserID,
		AdminCertPEM: certPEM,
		AdminKeyPEM:  keyPEM,
		AdminBearer:  bearer,
	}, nil
}

// TenantDeleteError reports that DeleteTenant could not delete a tenant.
type TenantDeleteError struct {
	// TenantID is the tenant being deleted.
	TenantID string

	// Cause is the underlying failure.
	Cause error
}

func (e *TenantDeleteError) Error() string {
	return "delete tenant " + e.TenantID + ": " + e.Cause.Error()
}

func (e *TenantDeleteError) Unwrap() error { return e.Cause }

// DeleteTenant deletes tenantID the way an operator does: a DELETE on the
// tenant, made with a system-admin bearer. A tenant that no longer exists is
// not an error.
func DeleteTenant(ctx context.Context, baseURL, tenantID, systemBearer string) error {
	endpoint := strings.TrimRight(baseURL, "/") + "/api/v1/tenants/" + url.PathEscape(tenantID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return &TenantDeleteError{TenantID: tenantID, Cause: err}
	}
	req.Header.Set("Authorization", "Bearer "+systemBearer)

	resp, err := NewHTTPClient(tenantSetupTimeout).Do(req)
	if err != nil {
		return &TenantDeleteError{TenantID: tenantID, Cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || (resp.StatusCode >= 200 && resp.StatusCode <= 299) {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &TenantDeleteError{TenantID: tenantID, Cause: err}
	}
	return &TenantDeleteError{TenantID: tenantID,
		Cause: &AuthBadStatusError{URL: endpoint, Status: resp.StatusCode, Body: string(body)}}
}

// newTenantAdminCSR generates a P-256 key and a CSR for it. The server sets
// the certificate's tenant itself, so the CSR names only the admin.
func newTenantAdminCSR() (csrPEM, keyPEM []byte, err error) {
	signer, keyAlgorithm, err := dbxkms.GenerateRequesterKey(dbxkms.KeyAlgorithmECDSAP256)
	if err != nil {
		return nil, nil, err
	}
	csrPEM, err = xkmssdk.CreateCSRWithKey(&xkmssdk.TrustedCACertificateRequest{
		Subject: xkmssdk.TrustedCASubject{CommonName: "admin"},
	}, signer)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err = xkmssdk.EncodePrivateKeyPEM(signer, keyAlgorithm, nil)
	if err != nil {
		return nil, nil, err
	}
	return csrPEM, keyPEM, nil
}
