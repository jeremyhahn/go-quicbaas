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

// security_handler_acme.go wires the Security().ACME() verbs to the quicbaasd
// /api/v1/acme REST routes. The handler returns the shared client/acme wire
// types the SDK acmeServerImpl / acmeClientImpl type-assert on.
//
// ACME is an HTTP-native protocol (RFC 8555). QuicBaaS is the broker-as-CA, so
// both the server-control verbs (RegisterChallenge / Issue) and the protocol-
// client verbs (NewOrder / FinalizeOrder / Renew) are in-process service calls
// reached over this REST surface — NOT JWS-signed exchanges against an external
// ACME directory.

package rest

import (
	"context"
	"net/http"

	acmeclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/acme"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// acmeMethods maps ACME SDK method names to their handler funcs for O(1)
// dispatch.
var acmeMethods = map[string]func(*securityRESTHandler, context.Context, interface{}) (interface{}, error){
	"RegisterChallenge": (*securityRESTHandler).acmeRegisterChallenge,
	"Issue":             (*securityRESTHandler).acmeIssue,
	"NewOrder":          (*securityRESTHandler).acmeNewOrder,
	"FinalizeOrder":     (*securityRESTHandler).acmeFinalizeOrder,
	"Renew":             (*securityRESTHandler).acmeRenew,
}

// handleACME dispatches a Security().ACME() method to its REST handler.
// Unknown methods return a typed unimplemented error naming the verb.
func (h *securityRESTHandler) handleACME(ctx context.Context, method string, req interface{}) (interface{}, error) {
	fn, ok := acmeMethods[method]
	if !ok {
		return nil, errors.NewUnimplemented("rest.security.ACME."+method,
			"unsupported ACME method")
	}
	return fn(h, ctx, req)
}

// acmeRegisterChallenge calls POST /api/v1/acme/register-challenge.
func (h *securityRESTHandler) acmeRegisterChallenge(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*acmeclient.RegisterChallengeRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.security.ACME.RegisterChallenge", "request is required")
	}
	if r.Name == "" {
		return nil, errors.NewInvalidInput("rest.security.ACME.RegisterChallenge", "challenge name is required")
	}

	var wire acmeclient.RegisterChallengeResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		"/api/v1/acme/register-challenge", r, &wire); err != nil {
		return nil, err
	}
	return &wire, nil
}

// acmeIssue calls POST /api/v1/acme/issue and returns the issued chain.
func (h *securityRESTHandler) acmeIssue(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*acmeclient.IssueRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.security.ACME.Issue", "request is required")
	}
	if len(r.CSRPEM) == 0 {
		return nil, errors.NewInvalidInput("rest.security.ACME.Issue", "csr is required")
	}
	if r.ChallengeType == "" {
		return nil, errors.NewInvalidInput("rest.security.ACME.Issue", "challenge type is required")
	}

	var wire acmeclient.IssueResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		"/api/v1/acme/issue", r, &wire); err != nil {
		return nil, err
	}
	return &wire, nil
}

// acmeNewOrder calls POST /api/v1/acme/new-order and returns the order
// descriptor.
func (h *securityRESTHandler) acmeNewOrder(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*acmeclient.NewOrderRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.security.ACME.NewOrder", "request is required")
	}
	if len(r.Identifiers) == 0 {
		return nil, errors.NewInvalidInput("rest.security.ACME.NewOrder", "at least one identifier is required")
	}

	var wire acmeclient.NewOrderResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		"/api/v1/acme/new-order", r, &wire); err != nil {
		return nil, err
	}
	return &wire, nil
}

// acmeFinalizeOrder calls POST /api/v1/acme/finalize and returns the issued
// chain.
func (h *securityRESTHandler) acmeFinalizeOrder(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*acmeclient.FinalizeOrderRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.security.ACME.FinalizeOrder", "request is required")
	}
	if len(r.CSRPEM) == 0 {
		return nil, errors.NewInvalidInput("rest.security.ACME.FinalizeOrder", "csr is required")
	}

	var wire acmeclient.FinalizeOrderResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		"/api/v1/acme/finalize", r, &wire); err != nil {
		return nil, err
	}
	return &wire, nil
}

// acmeRenew calls POST /api/v1/acme/renew. In the broker-as-CA model renewal
// requires a fresh CSR via NewOrder/FinalizeOrder; the server returns a typed
// error which the transport surfaces here.
func (h *securityRESTHandler) acmeRenew(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*acmeclient.RenewRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.security.ACME.Renew", "request is required")
	}
	if r.CertURL == "" {
		return nil, errors.NewInvalidInput("rest.security.ACME.Renew", "certificate url is required")
	}

	var wire acmeclient.RenewResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		"/api/v1/acme/renew", r, &wire); err != nil {
		return nil, err
	}
	return &wire, nil
}
