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

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// securityServiceTCGCA is the transport service name of the re-exported
// go-xkms TCGCAService (Security().TCGCA()).
const securityServiceTCGCA = "quicbaas.TCGCA"

// tcgCAPathPrefix is where the server mounts go-xkms's TCG CA handlers.
const tcgCAPathPrefix = trustedCAPathPrefix + "/tcg"

// handleTCGCA routes the go-xkms TCGCAService verbs to the server's
// /api/v1/xkms/ca/tcg/* routes. go-xkms's request types carry their own JSON
// tags, which go-xkms's CAHandlers decode, so each request is posted as-is
// (#1330).
func (h *securityRESTHandler) handleTCGCA(ctx context.Context, method string, req interface{}) (interface{}, error) {
	op := "rest.security.TCGCA." + method
	switch method {
	case "IssueEKCertificate":
		r, ok := req.(*xkmstransport.IssueEKCertificateRequest)
		if !ok || r == nil {
			return nil, errors.NewInvalidInput(op, "invalid request type, expected *xkmstransport.IssueEKCertificateRequest")
		}
		return tcgCAPost[xkmstransport.IssueEKCertificateResponse](ctx, h, op, "/ek", r)
	case "IssueAKCertificate":
		r, ok := req.(*xkmstransport.IssueAKCertificateRequest)
		if !ok || r == nil {
			return nil, errors.NewInvalidInput(op, "invalid request type, expected *xkmstransport.IssueAKCertificateRequest")
		}
		return tcgCAPost[xkmstransport.IssueAKCertificateResponse](ctx, h, op, "/ak", r)
	case "SignTCGCSR":
		r, ok := req.(*xkmstransport.SignTCGCSRRequest)
		if !ok || r == nil {
			return nil, errors.NewInvalidInput(op, "invalid request type, expected *xkmstransport.SignTCGCSRRequest")
		}
		return tcgCAPost[xkmstransport.SignTCGCSRResponse](ctx, h, op, "/sign-csr", r)
	case "EnrollDevice":
		r, ok := req.(*xkmstransport.EnrollDeviceRequest)
		if !ok || r == nil {
			return nil, errors.NewInvalidInput(op, "invalid request type, expected *xkmstransport.EnrollDeviceRequest")
		}
		return tcgCAPost[xkmstransport.EnrollDeviceResponse](ctx, h, op, "/enroll", r)
	default:
		return nil, errors.NewUnimplemented(op, "unsupported TCGCA method: "+method)
	}
}

// tcgCAPost posts one go-xkms TCG request as JSON and decodes the go-xkms
// response.
func tcgCAPost[T any](ctx context.Context, h *securityRESTHandler, op, suffix string, body interface{}) (*T, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, op, "failed to encode request")
	}
	resp, err := h.transport.doRawRequest(ctx, http.MethodPost, tcgCAPathPrefix+suffix, raw, nil)
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
