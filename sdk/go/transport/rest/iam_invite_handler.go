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
	"net/http"
	"net/url"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// handleInviteRequest routes invite activation requests onto the public,
// tenant-scoped REST surface (/api/v1/iam/{tenant_id}/invites/{token}).
//
// The token is path-escaped: it arrives from an invitation link the caller
// does not control and must not be able to steer the request at another route
// by carrying a slash.
func (h *iamRESTHandler) handleInviteRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	reqMap, err := toMap(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, "iamRESTHandler.handleInviteRequest", "invalid request")
	}

	tenantID := getString(reqMap, "tenant_id")
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iamRESTHandler.handleInviteRequest", "tenant_id is required")
	}
	token := getString(reqMap, "token")
	if token == "" {
		return nil, errors.NewInvalidInput("iamRESTHandler.handleInviteRequest", "token is required")
	}
	base := "/api/v1/iam/" + url.PathEscape(tenantID) + "/invites/" + url.PathEscape(token)

	switch method {
	case "GetInvite":
		// GET .../invites/{token}
		return h.doRequest(ctx, http.MethodGet, base, nil)

	case "ActivateFIDO2Begin":
		// POST .../invites/{token}/activate/fido2/begin
		return h.doRequest(ctx, http.MethodPost, base+"/activate/fido2/begin", nil)

	case "ActivateFIDO2Finish":
		// POST .../invites/{token}/activate/fido2/finish
		return h.doRequest(ctx, http.MethodPost, base+"/activate/fido2/finish",
			map[string]interface{}{"credential": reqMap["credential"]})

	case "ActivatePIV":
		// POST .../invites/{token}/activate/piv
		return h.doRequest(ctx, http.MethodPost, base+"/activate/piv",
			map[string]interface{}{"cert_pem": getString(reqMap, "cert_pem")})

	default:
		return nil, errors.NewUnimplemented("iamRESTHandler.handleInviteRequest", "unsupported method: "+method)
	}
}
