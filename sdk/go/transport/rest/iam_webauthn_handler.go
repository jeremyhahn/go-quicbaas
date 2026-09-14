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

// handleWebAuthnRequest routes WebAuthn credential administration requests onto
// the tenant-nested, user-scoped REST surface
// (/api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials).
//
// The credential id is path-escaped: it is base64url, which needs no escaping
// in practice, but it arrives from an operator's shell and must not be able to
// steer the request at another route by carrying a slash.
func (h *iamRESTHandler) handleWebAuthnRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	reqMap, err := toMap(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, "iamRESTHandler.handleWebAuthnRequest", "invalid request")
	}

	tenantID := getString(reqMap, "tenant_id")
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iamRESTHandler.handleWebAuthnRequest", "tenant_id is required")
	}
	userID := getString(reqMap, "user_id")
	if userID == "" {
		return nil, errors.NewInvalidInput("iamRESTHandler.handleWebAuthnRequest", "user_id is required")
	}
	base := "/api/v1/tenants/" + tenantID + "/users/" + userID + "/webauthn/credentials"

	switch method {
	case "ListWebAuthnCredentials", "List":
		// GET .../webauthn/credentials
		return h.doRequest(ctx, http.MethodGet, base, nil)

	case "DeleteWebAuthnCredential", "Delete":
		// DELETE .../webauthn/credentials/{credential_id}
		credentialID := getString(reqMap, "credential_id")
		if credentialID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleWebAuthnRequest", "credential_id is required")
		}
		return h.doRequest(ctx, http.MethodDelete, base+"/"+url.PathEscape(credentialID), nil)

	case "DeleteWebAuthnCredentials", "DeleteAll":
		// DELETE .../webauthn/credentials
		return h.doRequest(ctx, http.MethodDelete, base, nil)

	default:
		return nil, errors.NewUnimplemented("iamRESTHandler.handleWebAuthnRequest", "unsupported method: "+method)
	}
}
