//go:build rest

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// GetOAuthASMetadata handles GET /.well-known/oauth-authorization-server - OAuth 2.0 AS Metadata (RFC 8414).
// This endpoint is separate from OIDC discovery and provides OAuth 2.0 specific metadata for MCP clients.
func (h *IAMHandler) GetOAuthASMetadata(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get OIDC provider to access configuration
	oidcProvider, ok := h.server.service.(interface {
		GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "OIDC provider not available"))
		return
	}

	provider, err := oidcProvider.GetOIDCProvider(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if provider == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "OIDC provider not initialized"))
		return
	}

	// Get OAuth AS Metadata
	metadata := provider.GetOAuthASMetadata()

	RespondJSON(w, r, http.StatusOK, metadata)
}
