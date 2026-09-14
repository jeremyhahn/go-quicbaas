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

// security_handler.go provides typed REST routing for the Security()
// surface verbs (FIDO2 / PIV / ACME / Trust / Attestation). The bare
// default REST dispatch maps these to /api/v1/{service}/{method}, which the
// quicbaasd server does not expose, so every Security() verb would fail on
// the REST transport without this handler.
//
// The FIDO2 ceremony rides the IAM-mounted, tenant-scoped routes under
// /api/v1/iam/{tenant_id}/webauthn/{registration,login}/{begin,finish}.
// Those routes delegate to go-xkms's native WebAuthn HTTP handler, which
// exchanges opaque ceremony JSON plus an X-Session-Id correlation header.
// This handler adapts the xkms FIDO2 wire requests/responses to that HTTP
// contract so Security().Auth().FIDO2().* reaches the server. The tenant is
// resolved from the request context (transport.ContextWithTenant) first,
// then the transport's configured default tenant (WithDefaultTenant).
//
// The PIV verbs bridge to the xkms-compatible REST surface at
// /api/v1/xkms/piv/*, which delegates server-side to the composed go-xkms
// PIV service. quicbaasd exposes the canonical six PIV routes (list slots,
// get / store / delete certificate, generate key, generate CSR);
// ImportPIVCertificate and ExportPIVCertificate map onto the store and get
// routes respectively because quicbaasd does not mount xkms's dedicated
// /import and /export aliases.
package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	webauthnhttp "github.com/jeremyhahn/go-xkms/pkg/webauthn/http"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// Security service identifiers used by the SDK to route requests here.
// These mirror the constants declared in sdk/go/services_security.go.
const (
	securityServiceACME   = "quicbaas.ACME"
	securityServiceTrust  = "quicbaas.Trust"
	securityServiceAttest = "quicbaas.Attestation"
	securityServiceFIDO2  = "quicbaas.FIDO2"
	securityServicePIV    = "quicbaas.PIV"
)

// isSecurityService reports whether the supplied service name resolves to
// one of the Security() surface services routed through this handler.
func isSecurityService(service string) bool {
	switch trimRESTServicePrefix(service) {
	case "ACME", "Trust", "FIDO2", "PIV", "Attestation":
		return true
	default:
		return false
	}
}

// securityRESTHandler routes Security() verbs to their REST endpoints.
// It is constructed by Transport on init and lookup-routed from
// Transport.Request.
type securityRESTHandler struct {
	transport *Transport
}

// newSecurityRESTHandler constructs a handler bound to t.
func newSecurityRESTHandler(t *Transport) *securityRESTHandler {
	return &securityRESTHandler{transport: t}
}

// handleRequest dispatches the Security() service+method to the matching
// REST endpoint.
func (h *securityRESTHandler) handleRequest(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	switch trimRESTServicePrefix(service) {
	case "FIDO2":
		return h.handleFIDO2(ctx, method, req)
	case "PIV":
		return h.handlePIV(ctx, method, req)
	case "ACME":
		return h.handleACME(ctx, method, req)
	case "Trust":
		return h.handleTrust(ctx, method, req)
	case "Attestation":
		return h.handleAttestation(ctx, method, req)
	default:
		return nil, errors.NewUnimplemented("rest.security.handleRequest",
			"unsupported security service: "+service)
	}
}

// =============================================================================
// FIDO2 ceremony (IAM-mounted WebAuthn routes)
// =============================================================================

// fido2Methods maps FIDO2 SDK method names to their handler funcs for O(1)
// dispatch.
var fido2Methods = map[string]func(*securityRESTHandler, context.Context, interface{}) (interface{}, error){
	"BeginRegistration":    (*securityRESTHandler).fido2BeginRegistration,
	"FinishRegistration":   (*securityRESTHandler).fido2FinishRegistration,
	"BeginAuthentication":  (*securityRESTHandler).fido2BeginAuthentication,
	"FinishAuthentication": (*securityRESTHandler).fido2FinishAuthentication,
}

func (h *securityRESTHandler) handleFIDO2(ctx context.Context, method string, req interface{}) (interface{}, error) {
	fn, ok := fido2Methods[method]
	if !ok {
		return nil, errors.NewUnimplemented("rest.security.FIDO2."+method,
			"unsupported FIDO2 method")
	}
	return fn(h, ctx, req)
}

// tenant resolves the tenant identifier used to scope the IAM-mounted
// WebAuthn ceremony routes. The request context (transport.ContextWithTenant)
// takes precedence, otherwise the transport's configured default tenant
// (WithDefaultTenant) is used.
func (h *securityRESTHandler) tenant(ctx context.Context) (string, error) {
	if id := transport.TenantFromContext(ctx); id != "" {
		return id, nil
	}
	if h.transport.defaultTenant != "" {
		return h.transport.defaultTenant, nil
	}
	return "", errors.NewInvalidInput("rest.security.FIDO2",
		"tenant is required; configure rest.WithDefaultTenant or use transport.ContextWithTenant")
}

// webAuthnPath builds the IAM-mounted WebAuthn ceremony URL for a tenant.
func webAuthnPath(tenant, suffix string) string {
	return "/api/v1/iam/" + url.PathEscape(tenant) + "/webauthn" + suffix
}

func (h *securityRESTHandler) fido2BeginRegistration(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*xkmstransport.BeginRegistrationRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.security.FIDO2.BeginRegistration",
			"invalid request type, expected *xkmstransport.BeginRegistrationRequest")
	}
	tenant, err := h.tenant(ctx)
	if err != nil {
		return nil, err
	}
	if r.Email == "" {
		return nil, errors.NewInvalidInput("rest.security.FIDO2.BeginRegistration",
			"email is required")
	}

	body, err := json.Marshal(webauthnhttp.BeginRegistrationRequest{
		Email:       r.Email,
		DisplayName: r.DisplayName,
	})
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput,
			"rest.security.FIDO2.BeginRegistration", "failed to encode request")
	}

	resp, err := h.transport.doRawRequest(ctx, http.MethodPost,
		webAuthnPath(tenant, "/registration/begin"), body, nil)
	if err != nil {
		return nil, err
	}

	return &xkmstransport.BeginRegistrationResponse{
		SessionID:   resp.Header.Get(webauthnhttp.HeaderSessionID),
		OptionsJSON: string(resp.Body),
	}, nil
}

func (h *securityRESTHandler) fido2FinishRegistration(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*xkmstransport.FinishRegistrationRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.security.FIDO2.FinishRegistration",
			"invalid request type, expected *xkmstransport.FinishRegistrationRequest")
	}
	tenant, err := h.tenant(ctx)
	if err != nil {
		return nil, err
	}
	if r.SessionID == "" || r.AttestationJSON == "" {
		return nil, errors.NewInvalidInput("rest.security.FIDO2.FinishRegistration",
			"session_id and attestation_json are required")
	}

	resp, err := h.transport.doRawRequest(ctx, http.MethodPost,
		webAuthnPath(tenant, "/registration/finish"), []byte(r.AttestationJSON),
		map[string]string{webauthnhttp.HeaderSessionID: r.SessionID})
	if err != nil {
		return nil, err
	}

	var auth webauthnhttp.AuthResponse
	if len(resp.Body) > 0 {
		if err := json.Unmarshal(resp.Body, &auth); err != nil {
			return nil, errors.Wrap(err, errors.CodeInternal,
				"rest.security.FIDO2.FinishRegistration", "failed to decode response")
		}
	}

	return &xkmstransport.FinishRegistrationResponse{
		Success:      true,
		CredentialID: auth.UserID,
		Token:        auth.Token,
	}, nil
}

func (h *securityRESTHandler) fido2BeginAuthentication(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*xkmstransport.BeginAuthenticationRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.security.FIDO2.BeginAuthentication",
			"invalid request type, expected *xkmstransport.BeginAuthenticationRequest")
	}
	tenant, err := h.tenant(ctx)
	if err != nil {
		return nil, err
	}
	if r.Email == "" {
		return nil, errors.NewInvalidInput("rest.security.FIDO2.BeginAuthentication",
			"email is required")
	}

	body, err := json.Marshal(webauthnhttp.BeginLoginRequest{Email: r.Email})
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput,
			"rest.security.FIDO2.BeginAuthentication", "failed to encode request")
	}

	resp, err := h.transport.doRawRequest(ctx, http.MethodPost,
		webAuthnPath(tenant, "/login/begin"), body, nil)
	if err != nil {
		return nil, err
	}

	return &xkmstransport.BeginAuthenticationResponse{
		SessionID:   resp.Header.Get(webauthnhttp.HeaderSessionID),
		OptionsJSON: string(resp.Body),
	}, nil
}

func (h *securityRESTHandler) fido2FinishAuthentication(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*xkmstransport.FinishAuthenticationRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.security.FIDO2.FinishAuthentication",
			"invalid request type, expected *xkmstransport.FinishAuthenticationRequest")
	}
	tenant, err := h.tenant(ctx)
	if err != nil {
		return nil, err
	}
	if r.SessionID == "" || r.AssertionJSON == "" {
		return nil, errors.NewInvalidInput("rest.security.FIDO2.FinishAuthentication",
			"session_id and assertion_json are required")
	}

	resp, err := h.transport.doRawRequest(ctx, http.MethodPost,
		webAuthnPath(tenant, "/login/finish"), []byte(r.AssertionJSON),
		map[string]string{webauthnhttp.HeaderSessionID: r.SessionID})
	if err != nil {
		return nil, err
	}

	var auth webauthnhttp.AuthResponse
	if len(resp.Body) > 0 {
		if err := json.Unmarshal(resp.Body, &auth); err != nil {
			return nil, errors.Wrap(err, errors.CodeInternal,
				"rest.security.FIDO2.FinishAuthentication", "failed to decode response")
		}
	}

	return &xkmstransport.FinishAuthenticationResponse{
		Success: true,
		Token:   auth.Token,
		// The RESPONSE still carries Username; only the request's field was
		// renamed in xkms v0.12. Echoing the login key the caller supplied is
		// what it has always meant.
		Username: r.Email,
	}, nil
}

// =============================================================================
// PIV (xkms-compatible /api/v1/xkms/piv/* routes)
// =============================================================================

// pivMethods maps PIV SDK method names to their handler funcs for O(1)
// dispatch.
var pivMethods = map[string]func(*securityRESTHandler, context.Context, interface{}) (interface{}, error){
	"ListPIVSlots":         (*securityRESTHandler).pivListSlots,
	"GetPIVCertificate":    (*securityRESTHandler).pivGetCertificate,
	"StorePIVCertificate":  (*securityRESTHandler).pivStoreCertificate,
	"DeletePIVCertificate": (*securityRESTHandler).pivDeleteCertificate,
	"GeneratePIVKey":       (*securityRESTHandler).pivGenerateKey,
	"ImportPIVCertificate": (*securityRESTHandler).pivImportCertificate,
	"ExportPIVCertificate": (*securityRESTHandler).pivExportCertificate,
	"GeneratePIVCSR":       (*securityRESTHandler).pivGenerateCSR,
}

func (h *securityRESTHandler) handlePIV(ctx context.Context, method string, req interface{}) (interface{}, error) {
	fn, ok := pivMethods[method]
	if !ok {
		return nil, errors.NewUnimplemented("rest.security.PIV."+method,
			"unsupported PIV method")
	}
	return fn(h, ctx, req)
}

// pivSlotPath builds an /api/v1/xkms/piv/slots/{slot}{suffix} URL with the
// given query values.
func pivSlotPath(slot, suffix string, query url.Values) string {
	p := "/api/v1/xkms/piv/slots/" + url.PathEscape(slot) + suffix
	if len(query) > 0 {
		p += "?" + query.Encode()
	}
	return p
}

func (h *securityRESTHandler) pivListSlots(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*xkmstransport.ListPIVSlotsRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.security.PIV.ListPIVSlots",
			"invalid request type, expected *xkmstransport.ListPIVSlotsRequest")
	}
	path := "/api/v1/xkms/piv/slots"
	if r.Backend != "" {
		path += "?backend=" + url.QueryEscape(r.Backend)
	}
	resp, err := h.transport.doRawRequest(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	var out xkmstransport.ListPIVSlotsResponse
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"rest.security.PIV.ListPIVSlots", "failed to decode response")
	}
	return &out, nil
}

// pivGetLike executes the GET-certificate wire exchange shared by
// GetPIVCertificate and ExportPIVCertificate (quicbaasd serves both from
// the same route).
func (h *securityRESTHandler) pivGetLike(ctx context.Context, op string, req interface{}) (interface{}, error) {
	r, ok := req.(*xkmstransport.GetPIVCertificateRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op,
			"invalid request type, expected *xkmstransport.GetPIVCertificateRequest")
	}
	if r.Slot == "" {
		return nil, errors.NewInvalidInput(op, "slot is required")
	}
	q := url.Values{}
	if r.Backend != "" {
		q.Set("backend", r.Backend)
	}
	if r.Format != "" {
		q.Set("format", r.Format)
	}
	resp, err := h.transport.doRawRequest(ctx, http.MethodGet,
		pivSlotPath(r.Slot, "/cert", q), nil, nil)
	if err != nil {
		return nil, err
	}
	var out xkmstransport.GetPIVCertificateResponse
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, op,
			"failed to decode response")
	}
	return &out, nil
}

func (h *securityRESTHandler) pivGetCertificate(ctx context.Context, req interface{}) (interface{}, error) {
	return h.pivGetLike(ctx, "rest.security.PIV.GetPIVCertificate", req)
}

// pivStoreLike executes the store-certificate wire exchange shared by
// StorePIVCertificate and ImportPIVCertificate (quicbaasd serves both from
// the same PUT route; xkms's dedicated /import alias is not mounted).
func (h *securityRESTHandler) pivStoreLike(ctx context.Context, op string, req interface{}) (interface{}, error) {
	r, ok := req.(*xkmstransport.StorePIVCertificateRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op,
			"invalid request type, expected *xkmstransport.StorePIVCertificateRequest")
	}
	if r.Slot == "" {
		return nil, errors.NewInvalidInput(op, "slot is required")
	}
	body, err := json.Marshal(r)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, op,
			"failed to encode request")
	}
	if _, err := h.transport.doRawRequest(ctx, http.MethodPut,
		pivSlotPath(r.Slot, "/cert", nil), body, nil); err != nil {
		return nil, err
	}
	return nil, nil
}

func (h *securityRESTHandler) pivStoreCertificate(ctx context.Context, req interface{}) (interface{}, error) {
	return h.pivStoreLike(ctx, "rest.security.PIV.StorePIVCertificate", req)
}

func (h *securityRESTHandler) pivDeleteCertificate(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*xkmstransport.DeletePIVCertificateRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.security.PIV.DeletePIVCertificate",
			"invalid request type, expected *xkmstransport.DeletePIVCertificateRequest")
	}
	if r.Slot == "" {
		return nil, errors.NewInvalidInput("rest.security.PIV.DeletePIVCertificate",
			"slot is required")
	}
	q := url.Values{}
	if r.Backend != "" {
		q.Set("backend", r.Backend)
	}
	if _, err := h.transport.doRawRequest(ctx, http.MethodDelete,
		pivSlotPath(r.Slot, "/cert", q), nil, nil); err != nil {
		return nil, err
	}
	return nil, nil
}

func (h *securityRESTHandler) pivGenerateKey(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*xkmstransport.GeneratePIVKeyRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.security.PIV.GeneratePIVKey",
			"invalid request type, expected *xkmstransport.GeneratePIVKeyRequest")
	}
	if r.Slot == "" {
		return nil, errors.NewInvalidInput("rest.security.PIV.GeneratePIVKey",
			"slot is required")
	}
	body, err := json.Marshal(r)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput,
			"rest.security.PIV.GeneratePIVKey", "failed to encode request")
	}
	resp, err := h.transport.doRawRequest(ctx, http.MethodPost,
		pivSlotPath(r.Slot, "/key", nil), body, nil)
	if err != nil {
		return nil, err
	}
	var out xkmstransport.GeneratePIVKeyResponse
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"rest.security.PIV.GeneratePIVKey", "failed to decode response")
	}
	return &out, nil
}

func (h *securityRESTHandler) pivImportCertificate(ctx context.Context, req interface{}) (interface{}, error) {
	return h.pivStoreLike(ctx, "rest.security.PIV.ImportPIVCertificate", req)
}

func (h *securityRESTHandler) pivExportCertificate(ctx context.Context, req interface{}) (interface{}, error) {
	return h.pivGetLike(ctx, "rest.security.PIV.ExportPIVCertificate", req)
}

func (h *securityRESTHandler) pivGenerateCSR(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*xkmstransport.GeneratePIVCSRRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.security.PIV.GeneratePIVCSR",
			"invalid request type, expected *xkmstransport.GeneratePIVCSRRequest")
	}
	if r.Slot == "" {
		return nil, errors.NewInvalidInput("rest.security.PIV.GeneratePIVCSR",
			"slot is required")
	}
	body, err := json.Marshal(r)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput,
			"rest.security.PIV.GeneratePIVCSR", "failed to encode request")
	}
	resp, err := h.transport.doRawRequest(ctx, http.MethodPost,
		pivSlotPath(r.Slot, "/csr", nil), body, nil)
	if err != nil {
		return nil, err
	}
	var out xkmstransport.GeneratePIVCSRResponse
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"rest.security.PIV.GeneratePIVCSR", "failed to decode response")
	}
	return &out, nil
}
