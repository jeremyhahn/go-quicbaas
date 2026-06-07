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

// security_handler_acme.go wires the Security().ACME().Server() control verbs
// (RegisterChallenge / Issue) to the quicbaasd /api/v1/acme REST routes. The
// handler returns the shared client/acme wire types the SDK acmeServerImpl
// type-asserts on.
//
// ACME is an HTTP-native protocol (RFC 8555). The protocol-CLIENT verbs
// (NewOrder / FinalizeOrder / Renew) are JWS-signed exchanges performed by an
// ACME protocol client against the directory the server mounts — they are NOT
// quicbaas transport RPCs and never reach this handler (the SDK
// acmeClientImpl returns a typed unimplemented error for them directly).

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
}

// handleACME dispatches a Security().ACME().Server() method to its REST
// handler. Unknown methods return a typed unimplemented error naming the verb.
func (h *securityRESTHandler) handleACME(ctx context.Context, method string, req interface{}) (interface{}, error) {
	fn, ok := acmeMethods[method]
	if !ok {
		return nil, errors.NewUnimplemented("rest.security.ACME."+method,
			"unsupported ACME method (protocol-client verbs NewOrder/FinalizeOrder/Renew are not transport RPCs)")
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
