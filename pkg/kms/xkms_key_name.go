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

package kms

import (
	"context"

	"github.com/jeremyhahn/go-xkms/pkg/validation"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// keyTenant returns the tenant a pkg/kms call acts for: the tenant bound to
// ctx, or "" when none is bound or the system tenant is. "" means a platform
// key. A tenant key is go-xkms CN=<name> with TenantID=<tenant>, so go-xkms
// keeps tenants apart itself; the name never carries the tenant.
func keyTenant(ctx context.Context) string {
	tenantID, ok := tenantctx.Lookup(ctx)
	if !ok || tenantctx.IsSystem(tenantID) {
		return ""
	}
	return tenantID
}

// validateKeyName checks a caller's bare key name. go-xkms's rules refuse
// "/", so no name can spell a tenant or a path.
func validateKeyName(name string) error {
	if err := validation.ValidateKeyID(name); err != nil {
		return api.NewInvalidInputCauseError("invalid key name "+name, err)
	}
	return nil
}
