//go:build rest && iam

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

// PublicTenantInfo is the subset of tenant information returned by the public
// tenant listing endpoint. It intentionally omits settings, quotas, and other
// operator-only fields.
type PublicTenantInfo struct {
	// ID is the unique tenant identifier.
	ID string `json:"id"`

	// Name is the internal tenant name.
	Name string `json:"name"`

	// DisplayName is the human-readable label shown in login UIs.
	// Falls back to Name when the tenant has no explicit display name.
	DisplayName string `json:"display_name"`
}

// PublicTenantsResponse is the envelope returned by GET /api/v1/iam/tenants/public.
type PublicTenantsResponse struct {
	Tenants []PublicTenantInfo `json:"tenants"`
}

// ListPublicTenants handles GET /api/v1/iam/tenants/public.
//
// This is a public, unauthenticated endpoint that returns tenants which allow
// self-service login. It is used by the login UI to populate the tenant selector
// before the user has obtained a token.
//
// Only tenants whose Settings.PublicLoginHidden is false (the default) are
// included. Operators can hide a tenant from the public selector by setting
// PublicLoginHidden: true in tenant settings.
func (h *IAMHandler) ListPublicTenants(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantMgr, ok := h.server.service.(interface {
		GetTenantManager(ctx context.Context) (TenantManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "tenant manager not available"))
		return
	}

	mgr, err := tenantMgr.GetTenantManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "tenant manager not initialized"))
		return
	}

	tenants, err := mgr.ListTenants(ctx, iam.ListTenantsOptions{
		Limit:  1000,
		Offset: 0,
	})
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	public := make([]PublicTenantInfo, 0, len(tenants))
	for _, t := range tenants {
		// Skip tenants that operators have explicitly hidden from self-service login.
		// The zero value (false) means visible — so all tenants appear by default.
		if t.Settings.PublicLoginHidden {
			continue
		}
		displayName := t.Settings.DisplayName
		if displayName == "" {
			displayName = t.Name
		}
		public = append(public, PublicTenantInfo{
			ID:          t.ID,
			Name:        t.Name,
			DisplayName: displayName,
		})
	}

	RespondJSON(w, r, http.StatusOK, PublicTenantsResponse{Tenants: public})
}
