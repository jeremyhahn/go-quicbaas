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

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// securityServiceTrustedCA is the transport service name of the re-exported
// go-xkms CAService (quicbaas.TrustedCAService, Security().TrustedCA()).
const securityServiceTrustedCA = "quicbaas.TrustedCA"

// trustedCAPathPrefix is where the server mounts go-xkms's CA REST handlers
// (pkg/api/rest/routes.go). The request and response bodies are go-xkms's own:
// the same fields go-xkms's REST client sends to /api/v1/ca/*.
const trustedCAPathPrefix = "/api/v1/xkms/ca"

// handleTrustedCA serves one go-xkms CAService method over REST. Each request
// is the go-xkms transport request type and each result the go-xkms response
// type, so the SDK re-exports the CA under go-xkms's own names and types.
func (h *securityRESTHandler) handleTrustedCA(ctx context.Context, method string, req interface{}) (interface{}, error) {
	op := "rest.security.TrustedCA." + method
	switch method {
	case "GetCABundle":
		r, ok := req.(*xkmstransport.GetCABundleRequest)
		if !ok || r == nil {
			return nil, errors.NewInvalidInput(op, "invalid request type, expected *xkmstransport.GetCABundleRequest")
		}
		q := url.Values{}
		setIfNotEmpty(q, "store_type", r.StoreType)
		setIfNotEmpty(q, "algorithm", r.Algorithm)
		setIfNotEmpty(q, "tenant_id", r.TenantID)
		return trustedCADo[xkmstransport.GetCABundleResponse](ctx, h, op, http.MethodGet, withQuery(trustedCAPathPrefix+"/bundle", q), nil)
	case "GetCACertificate":
		r, ok := req.(*xkmstransport.GetCACertificateRequest)
		if !ok || r == nil {
			return nil, errors.NewInvalidInput(op, "invalid request type, expected *xkmstransport.GetCACertificateRequest")
		}
		q := url.Values{}
		setIfNotEmpty(q, "identity", r.Identity)
		setIfNotEmpty(q, "tenant_id", r.TenantID)
		return trustedCADo[xkmstransport.GetCACertificateResponse](ctx, h, op, http.MethodGet, withQuery(trustedCAPathPrefix+"/cert", q), nil)
	case "SignCSR":
		r, ok := req.(*xkmstransport.SignCSRRequest)
		if !ok || r == nil {
			return nil, errors.NewInvalidInput(op, "invalid request type, expected *xkmstransport.SignCSRRequest")
		}
		body := map[string]interface{}{"csr_pem": r.CSRPEM}
		if r.Profile != "" {
			body["profile"] = r.Profile
		}
		if r.ValidityDays > 0 {
			body["validity_days"] = r.ValidityDays
		}
		if r.TenantID != "" {
			body["tenant_id"] = r.TenantID
		}
		return trustedCADo[xkmstransport.SignCSRResponse](ctx, h, op, http.MethodPost, trustedCAPathPrefix+"/sign", body)
	case "IssueCertificate":
		r, ok := req.(*xkmstransport.IssueCertificateRequest)
		if !ok || r == nil {
			return nil, errors.NewInvalidInput(op, "invalid request type, expected *xkmstransport.IssueCertificateRequest")
		}
		body := map[string]interface{}{"common_name": r.CommonName, "profile": r.Profile}
		if r.Organization != "" {
			body["organization"] = r.Organization
		}
		if len(r.SANs) > 0 {
			body["sans"] = r.SANs
		}
		if r.ValidityDays > 0 {
			body["validity_days"] = r.ValidityDays
		}
		if r.Algorithm != "" {
			body["algorithm"] = r.Algorithm
		}
		if r.TenantID != "" {
			body["tenant_id"] = r.TenantID
		}
		return trustedCADo[xkmstransport.IssueCertificateResponse](ctx, h, op, http.MethodPost, trustedCAPathPrefix+"/issue", body)
	case "RevokeCertificate":
		r, ok := req.(*xkmstransport.RevokeCertificateRequest)
		if !ok || r == nil {
			return nil, errors.NewInvalidInput(op, "invalid request type, expected *xkmstransport.RevokeCertificateRequest")
		}
		body := map[string]interface{}{"serial_number": r.SerialNumber, "reason": r.Reason}
		if r.TenantID != "" {
			body["tenant_id"] = r.TenantID
		}
		return trustedCADo[xkmstransport.RevokeCertificateResponse](ctx, h, op, http.MethodPost, trustedCAPathPrefix+"/revoke", body)
	case "GenerateCRL":
		r, ok := req.(*xkmstransport.GenerateCRLRequest)
		if !ok || r == nil {
			return nil, errors.NewInvalidInput(op, "invalid request type, expected *xkmstransport.GenerateCRLRequest")
		}
		var body map[string]interface{}
		if r.TenantID != "" {
			body = map[string]interface{}{"tenant_id": r.TenantID}
		}
		return trustedCADo[xkmstransport.GenerateCRLResponse](ctx, h, op, http.MethodPost, trustedCAPathPrefix+"/crl", body)
	case "IsRevoked":
		r, ok := req.(*xkmstransport.IsRevokedRequest)
		if !ok || r == nil {
			return nil, errors.NewInvalidInput(op, "invalid request type, expected *xkmstransport.IsRevokedRequest")
		}
		if r.SerialNumber == "" {
			return nil, errors.NewInvalidInput(op, "serial number is required")
		}
		q := url.Values{}
		setIfNotEmpty(q, "tenant_id", r.TenantID)
		path := withQuery(trustedCAPathPrefix+"/revoked/"+url.PathEscape(r.SerialNumber), q)
		return trustedCADo[xkmstransport.IsRevokedResponse](ctx, h, op, http.MethodGet, path, nil)
	default:
		return nil, errors.NewUnimplemented(op, "unsupported TrustedCA method: "+method)
	}
}

// trustedCADo performs one CA round-trip and decodes the go-xkms response.
func trustedCADo[T any](ctx context.Context, h *securityRESTHandler, op, httpMethod, path string, body map[string]interface{}) (*T, error) {
	var raw []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, errors.Wrap(err, errors.CodeInternal, op, "failed to encode request")
		}
		raw = b
	}
	resp, err := h.transport.doRawRequest(ctx, httpMethod, path, raw, nil)
	if err != nil {
		return nil, err
	}
	var out T
	if len(resp.Body) > 0 {
		if err := json.Unmarshal(resp.Body, &out); err != nil {
			return nil, errors.Wrap(err, errors.CodeInternal, op, "failed to decode response")
		}
	}
	return &out, nil
}

func setIfNotEmpty(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}

func withQuery(path string, q url.Values) string {
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}
