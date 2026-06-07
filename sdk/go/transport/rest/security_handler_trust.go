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

// security_handler_trust.go wires the Security().Trust() cross-sign / AIA
// verbs to the quicbaasd /api/v1/trust REST routes. The handler returns
// the leaf client/trust wire types that the SDK trustServiceImpl decodes.
// PublishDANE is intentionally not wired (it has no REST server surface
// yet — pending a separate DNS-provider hook design).

package rest

import (
	"context"
	"net/http"

	trustclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/trust"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// trustMethods maps Trust SDK method names to their handler funcs for
// O(1) dispatch.
var trustMethods = map[string]func(*securityRESTHandler, context.Context, interface{}) (interface{}, error){
	"CrossSignedRootPool": (*securityRESTHandler).trustCrossSignedRootPool,
	"AutoImportAIA":       (*securityRESTHandler).trustAutoImportAIA,
	"PublishDANE":         (*securityRESTHandler).trustPublishDANE,
}

// handleTrust dispatches a Security().Trust() method to its REST handler.
// Unknown methods return a typed unimplemented error naming the verb.
func (h *securityRESTHandler) handleTrust(ctx context.Context, method string, req interface{}) (interface{}, error) {
	fn, ok := trustMethods[method]
	if !ok {
		return nil, errors.NewUnimplemented("rest.security.Trust."+method,
			"unsupported Trust method")
	}
	return fn(h, ctx, req)
}

// trustCrossSignedRootPool calls GET /api/v1/trust/cross-signed-roots and
// returns the cross-signed root set as the leaf trust response type.
func (h *securityRESTHandler) trustCrossSignedRootPool(ctx context.Context, _ interface{}) (interface{}, error) {
	var wire struct {
		RootPEMs [][]byte `json:"root_pems"`
	}
	if err := h.transport.doJSONRequest(ctx, http.MethodGet,
		"/api/v1/trust/cross-signed-roots", nil, &wire); err != nil {
		return nil, err
	}
	return &trustclient.CrossSignedRootPoolResponse{RootPEMs: wire.RootPEMs}, nil
}

// trustAutoImportAIA calls POST /api/v1/trust/auto-import-aia with the
// supplied DER chain and returns the import count.
func (h *securityRESTHandler) trustAutoImportAIA(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*trustclient.AutoImportAIARequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.security.Trust.AutoImportAIA",
			"request is required")
	}
	if len(r.ChainDER) == 0 {
		return nil, errors.NewInvalidInput("rest.security.Trust.AutoImportAIA",
			"chain is required")
	}

	wireReq := struct {
		ChainDER [][]byte `json:"chain_der"`
	}{ChainDER: r.ChainDER}

	var wire struct {
		Imported int `json:"imported"`
	}
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		"/api/v1/trust/auto-import-aia", &wireReq, &wire); err != nil {
		return nil, err
	}
	return &trustclient.AutoImportAIAResponse{Imported: wire.Imported}, nil
}

// trustPublishDANE calls POST /api/v1/trust/publish-dane with the supplied
// certificate, name and port and returns the published-record count. The
// server returns a typed not-configured error when no DANE publisher is
// registered, which propagates here via doJSONRequest.
func (h *securityRESTHandler) trustPublishDANE(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*trustclient.PublishDANERequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.security.Trust.PublishDANE",
			"request is required")
	}
	if r.Name == "" {
		return nil, errors.NewInvalidInput("rest.security.Trust.PublishDANE",
			"name is required")
	}
	if r.Port <= 0 {
		return nil, errors.NewInvalidInput("rest.security.Trust.PublishDANE",
			"port must be positive")
	}
	if len(r.CertDER) == 0 {
		return nil, errors.NewInvalidInput("rest.security.Trust.PublishDANE",
			"cert is required")
	}

	wireReq := struct {
		Name    string `json:"name"`
		Port    int    `json:"port"`
		CertDER []byte `json:"cert_der"`
	}{Name: r.Name, Port: r.Port, CertDER: r.CertDER}

	var wire struct {
		Published int `json:"published"`
	}
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		"/api/v1/trust/publish-dane", &wireReq, &wire); err != nil {
		return nil, err
	}
	return &trustclient.PublishDANEResponse{Published: wire.Published}, nil
}
