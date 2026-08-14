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

// security_handler_attestation.go wires the Security().Attestation() verbs to
// the quicbaasd /api/v1/attestation REST routes. The handler returns the leaf
// client/attestation wire types that the SDK attestationServiceImpl decodes.
//
// The server's JSON bodies mirror the leaf types field-for-field, so each
// verb decodes straight into the response type rather than through an
// intermediate wire struct.

package rest

import (
	"context"
	"net/http"

	attestationclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/attestation"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// attestationMethods maps Attestation SDK method names to their handler funcs
// for O(1) dispatch.
var attestationMethods = map[string]func(*securityRESTHandler, context.Context, interface{}) (interface{}, error){
	"Verify":          (*securityRESTHandler).attestationVerify,
	"VerifyChain":     (*securityRESTHandler).attestationVerifyChain,
	"AttestContainer": (*securityRESTHandler).attestationAttestContainer,
}

// handleAttestation dispatches a Security().Attestation() method to its REST
// handler. Unknown methods return a typed unimplemented error naming the verb.
func (h *securityRESTHandler) handleAttestation(ctx context.Context, method string, req interface{}) (interface{}, error) {
	fn, ok := attestationMethods[method]
	if !ok {
		return nil, errors.NewUnimplemented("rest.security.Attestation."+method,
			"unsupported Attestation method")
	}
	return fn(h, ctx, req)
}

// attestationVerify calls POST /api/v1/attestation/verify.
func (h *securityRESTHandler) attestationVerify(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*attestationclient.VerifyRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.security.Attestation.Verify",
			"request is required")
	}
	if r.Format == "" {
		return nil, errors.NewInvalidInput("rest.security.Attestation.Verify",
			"format is required")
	}
	if len(r.ChainPEM) == 0 {
		return nil, errors.NewInvalidInput("rest.security.Attestation.Verify",
			"chain is required")
	}

	var resp attestationclient.VerifyResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		"/api/v1/attestation/verify", r, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// attestationVerifyChain calls POST /api/v1/attestation/verify-chain.
func (h *securityRESTHandler) attestationVerifyChain(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*attestationclient.VerifyChainRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.security.Attestation.VerifyChain",
			"request is required")
	}
	if len(r.ChainPEM) == 0 {
		return nil, errors.NewInvalidInput("rest.security.Attestation.VerifyChain",
			"chain is required")
	}

	var resp attestationclient.VerifyChainResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		"/api/v1/attestation/verify-chain", r, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// attestationAttestContainer calls POST /api/v1/attestation/attest-container.
// The server returns a typed not-configured error when no container attestor
// is registered, which propagates here via doJSONRequest.
func (h *securityRESTHandler) attestationAttestContainer(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*attestationclient.AttestContainerRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.security.Attestation.AttestContainer",
			"request is required")
	}
	if r.Image == "" {
		return nil, errors.NewInvalidInput("rest.security.Attestation.AttestContainer",
			"image is required")
	}

	var resp attestationclient.AttestContainerResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		"/api/v1/attestation/attest-container", r, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
