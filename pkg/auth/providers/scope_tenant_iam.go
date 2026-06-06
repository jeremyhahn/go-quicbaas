//go:build iam

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

package providers

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// init installs the canonical IAM target-tenant extractor for the RBAC scope
// gate. In iam builds the request's target tenant is carried on the context by
// iam.ContextWithTenant (set by the REST / MCP / service layers), so the scope
// gate resolves it via iam.TenantFromContext. In non-iam builds the default
// extractor (returns "") remains, and the scope gate falls back to the
// subject's own tenant — there are no multi-tenant scopes without IAM.
func init() {
	targetTenantExtractor = iam.TenantFromContext
}
