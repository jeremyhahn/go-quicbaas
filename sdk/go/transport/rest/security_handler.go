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
// surface verbs (WebAuthn / FIDO2 / PIV / ACME / Trust). The bare default
// REST dispatch maps these to /api/v1/{service}/{method}, which the
// quicbaasd server does not expose, so every Security() verb failed on
// the REST transport.
//
// The WebAuthn ceremony has a real, production server surface: the
// IAM-mounted, tenant-scoped routes under
// /api/v1/iam/{tenant_id}/webauthn/{registration,login}/{begin,finish}.
// Those routes delegate to go-xkms's native WebAuthn HTTP handler, which
// exchanges opaque ceremony JSON plus an X-Session-Id correlation header.
// This handler adapts the SDK WebAuthn requests/responses to that wire
// contract so Security().Auth().WebAuthn().* reaches the server.
//
// The remaining Security() verbs (FIDO2 device operations, vendor-agnostic
// PIV reader/slot operations, ACME, and Trust) have no production server
// surface on quicbaasd today: the /api/v1/xkms/fido2/* handlers return
// HTTP 501, the /api/v1/xkms/piv/* routes are slot/backend-scoped (not the
// reader/slot model the SDK PIVClient uses), and there are no ACME/Trust
// REST routes at all. For those verbs this handler returns a precise typed
// "unimplemented" error naming the verb, rather than letting the generic
// dispatch issue a request to a nonexistent endpoint and surface an opaque
// 404. The (verb x transport) follow-up matrix is documented in the
// package routing notes.

package rest

import (
	"context"
	"encoding/json"
	"net/http"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// Security service identifiers used by the SDK to route requests here.
// These mirror the constants declared in sdk/go/services_security.go.
const (
	securityServiceACME     = "quicbaas.ACME"
	securityServiceTrust    = "quicbaas.Trust"
	securityServiceFIDO2    = "quicbaas.FIDO2"
	securityServiceWebAuthn = "quicbaas.WebAuthn"
	securityServicePIV      = "quicbaas.PIV"
)

// headerSessionID is the correlation header the go-xkms WebAuthn handler
// emits from begin and consumes on finish. It mirrors
// go-xkms/pkg/webauthn/http.HeaderSessionID.
const (
	headerSessionID = "X-Session-Id"
	headerUserID    = "X-User-Id"
)

// isSecurityService reports whether the supplied service name resolves to
// one of the Security() surface services routed through this handler.
func isSecurityService(service string) bool {
	switch trimRESTServicePrefix(service) {
	case "ACME", "Trust", "FIDO2", "WebAuthn", "PIV":
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
// REST endpoint. WebAuthn verbs reach the real IAM-mounted server routes;
// verbs without a production server surface return a typed unimplemented
// error naming the verb.
func (h *securityRESTHandler) handleRequest(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	switch trimRESTServicePrefix(service) {
	case "WebAuthn":
		return h.handleWebAuthn(ctx, method, req)
	case "FIDO2":
		return nil, errors.NewUnimplemented("rest.security.FIDO2."+method,
			"FIDO2 device operations have no REST server surface on quicbaasd; "+
				"use Security().Auth().WebAuthn() for the tenant-scoped ceremony")
	case "PIV":
		return nil, errors.NewUnimplemented("rest.security.PIV."+method,
			"vendor-agnostic PIV reader/slot operations have no REST server surface on quicbaasd")
	case "ACME":
		return nil, errors.NewUnimplemented("rest.security.ACME."+method,
			"ACME operations have no REST server surface on quicbaasd")
	case "Trust":
		return nil, errors.NewUnimplemented("rest.security.Trust."+method,
			"Trust (DANE/cross-sign/AIA) operations have no REST server surface on quicbaasd")
	default:
		return nil, errors.NewUnimplemented("rest.security.handleRequest",
			"unsupported security service: "+service)
	}
}

// webAuthnMethods maps WebAuthn SDK method names to their handler funcs
// for O(1) dispatch.
var webAuthnMethods = map[string]func(*securityRESTHandler, context.Context, interface{}) (interface{}, error){
	"BeginRegistration":  (*securityRESTHandler).webAuthnBeginRegistration,
	"FinishRegistration": (*securityRESTHandler).webAuthnFinishRegistration,
	"BeginLogin":         (*securityRESTHandler).webAuthnBeginLogin,
	"FinishLogin":        (*securityRESTHandler).webAuthnFinishLogin,
}

func (h *securityRESTHandler) handleWebAuthn(ctx context.Context, method string, req interface{}) (interface{}, error) {
	fn, ok := webAuthnMethods[method]
	if !ok {
		return nil, errors.NewUnimplemented("rest.security.WebAuthn."+method,
			"unsupported WebAuthn method")
	}
	return fn(h, ctx, req)
}

// tenant resolves the tenant identifier used to scope the IAM-mounted
// WebAuthn routes. It mirrors the IAM transport's mechanism: a tenant_id
// carried alongside the request takes precedence, otherwise the
// transport's configured default tenant (WithDefaultTenant) is used.
func (h *securityRESTHandler) tenant(reqMap map[string]interface{}) (string, error) {
	if id := getString(reqMap, "tenant_id"); id != "" {
		return id, nil
	}
	if h.transport.defaultTenant != "" {
		return h.transport.defaultTenant, nil
	}
	return "", errors.NewInvalidInput("rest.security.WebAuthn",
		"tenant is required; configure rest.WithDefaultTenant or supply tenant_id")
}

// webAuthnPath builds the IAM-mounted WebAuthn ceremony URL for a tenant.
func webAuthnPath(tenant, suffix string) string {
	return "/api/v1/iam/" + tenant + "/webauthn" + suffix
}

// beginRegistrationBody is the go-xkms WebAuthn begin-registration wire
// body (mirrors go-xkms/pkg/webauthn/http.BeginRegistrationRequest).
type beginRegistrationBody struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name,omitempty"`
}

// beginLoginBody mirrors go-xkms/pkg/webauthn/http.BeginLoginRequest.
type beginLoginBody struct {
	Email  string `json:"email,omitempty"`
	UserID string `json:"user_id,omitempty"`
}

// authResponseBody mirrors go-xkms/pkg/webauthn/http.AuthResponse.
type authResponseBody struct {
	Token  string `json:"token"`
	UserID string `json:"user_id"`
}

func (h *securityRESTHandler) webAuthnBeginRegistration(ctx context.Context, req interface{}) (interface{}, error) {
	reqMap, err := toMap(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput,
			"rest.security.WebAuthn.BeginRegistration", "invalid request")
	}
	tenant, err := h.tenant(reqMap)
	if err != nil {
		return nil, err
	}
	user := getMap(reqMap, "user")
	email := getString(user, "username")
	if email == "" {
		return nil, errors.NewInvalidInput("rest.security.WebAuthn.BeginRegistration",
			"username is required")
	}

	body, err := json.Marshal(beginRegistrationBody{
		Email:       email,
		DisplayName: getString(user, "display_name"),
	})
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput,
			"rest.security.WebAuthn.BeginRegistration", "failed to encode request")
	}

	resp, err := h.transport.doRawRequest(ctx, http.MethodPost,
		webAuthnPath(tenant, "/registration/begin"), body, nil)
	if err != nil {
		return nil, err
	}

	return &xkmstransport.BeginRegistrationResponse{
		SessionID:   resp.Header.Get(headerSessionID),
		OptionsJSON: string(resp.Body),
	}, nil
}

func (h *securityRESTHandler) webAuthnFinishRegistration(ctx context.Context, req interface{}) (interface{}, error) {
	reqMap, err := toMap(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput,
			"rest.security.WebAuthn.FinishRegistration", "invalid request")
	}
	tenant, err := h.tenant(reqMap)
	if err != nil {
		return nil, err
	}
	response := getMap(reqMap, "response")
	sessionID := getString(response, "session_id")
	attestation := getString(response, "attestation_json")
	if sessionID == "" || attestation == "" {
		return nil, errors.NewInvalidInput("rest.security.WebAuthn.FinishRegistration",
			"session_id and attestation_json are required")
	}

	resp, err := h.transport.doRawRequest(ctx, http.MethodPost,
		webAuthnPath(tenant, "/registration/finish"), []byte(attestation),
		map[string]string{headerSessionID: sessionID})
	if err != nil {
		return nil, err
	}

	var auth authResponseBody
	if len(resp.Body) > 0 {
		if err := json.Unmarshal(resp.Body, &auth); err != nil {
			return nil, errors.Wrap(err, errors.CodeInternal,
				"rest.security.WebAuthn.FinishRegistration", "failed to decode response")
		}
	}

	return &xkmstransport.FinishRegistrationResponse{
		Success:      true,
		CredentialID: auth.UserID,
		Token:        auth.Token,
	}, nil
}

func (h *securityRESTHandler) webAuthnBeginLogin(ctx context.Context, req interface{}) (interface{}, error) {
	reqMap, err := toMap(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput,
			"rest.security.WebAuthn.BeginLogin", "invalid request")
	}
	tenant, err := h.tenant(reqMap)
	if err != nil {
		return nil, err
	}
	user := getMap(reqMap, "user")
	email := getString(user, "username")
	if email == "" {
		return nil, errors.NewInvalidInput("rest.security.WebAuthn.BeginLogin",
			"username is required")
	}

	body, err := json.Marshal(beginLoginBody{
		Email:  email,
		UserID: getString(user, "user_id"),
	})
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput,
			"rest.security.WebAuthn.BeginLogin", "failed to encode request")
	}

	resp, err := h.transport.doRawRequest(ctx, http.MethodPost,
		webAuthnPath(tenant, "/login/begin"), body, nil)
	if err != nil {
		return nil, err
	}

	return &xkmstransport.BeginAuthenticationResponse{
		SessionID:   resp.Header.Get(headerSessionID),
		OptionsJSON: string(resp.Body),
	}, nil
}

func (h *securityRESTHandler) webAuthnFinishLogin(ctx context.Context, req interface{}) (interface{}, error) {
	reqMap, err := toMap(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput,
			"rest.security.WebAuthn.FinishLogin", "invalid request")
	}
	tenant, err := h.tenant(reqMap)
	if err != nil {
		return nil, err
	}
	response := getMap(reqMap, "response")
	sessionID := getString(response, "session_id")
	assertion := getString(response, "assertion_json")
	if sessionID == "" || assertion == "" {
		return nil, errors.NewInvalidInput("rest.security.WebAuthn.FinishLogin",
			"session_id and assertion_json are required")
	}

	headers := map[string]string{headerSessionID: sessionID}
	if userID := getString(getMap(reqMap, "user"), "user_id"); userID != "" {
		headers[headerUserID] = userID
	}

	resp, err := h.transport.doRawRequest(ctx, http.MethodPost,
		webAuthnPath(tenant, "/login/finish"), []byte(assertion), headers)
	if err != nil {
		return nil, err
	}

	var auth authResponseBody
	if len(resp.Body) > 0 {
		if err := json.Unmarshal(resp.Body, &auth); err != nil {
			return nil, errors.Wrap(err, errors.CodeInternal,
				"rest.security.WebAuthn.FinishLogin", "failed to decode response")
		}
	}

	username := getString(getMap(reqMap, "user"), "username")
	return &xkmstransport.FinishAuthenticationResponse{
		Success:  true,
		Token:    auth.Token,
		Username: username,
	}, nil
}
