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

package iam

import "strings"

// tenantIDSeparator delimits the tenant prefix from the random component of a
// tenant-encoded identifier. Tenant IDs are constrained to [a-z0-9-] and the
// random components used by quicbaas (UUIDs, hex token hashes, nonces) never
// contain a '.', so the separator is unambiguous and a single SplitN cleanly
// recovers the tenant.
//
// The scheme mirrors the OAuth client-ID convention (app_<tenant>_…): an
// identifier carries its own tenant so by-ID / by-token lookups — which arrive
// with no tenant in context — can route to the owning tenant's barrier-isolated
// store without a global directory table.
const tenantIDSeparator = "."

// systemTenantID is the shared / system tenant used as the read-tolerant
// fallback for legacy identifiers that predate tenant-encoding (i.e. ids minted
// before this scheme that carry no separator). It aliases the package's
// canonical SystemTenantID (operator_sso.go), which in turn aliases qrdb's
// reserved "system" tenant, so the routing KVStore resolves it to the shared
// store and the two constants can never drift apart.
const systemTenantID = SystemTenantID

// encodeTenantID mints a tenant-encoded identifier of the form
// "<tenantID><sep><raw>". When tenantID is empty the raw id is returned
// unchanged (single-tenant / system callers), which decodes back to the system
// tenant via decodeTenantID.
func encodeTenantID(tenantID, raw string) string {
	if tenantID == "" {
		return raw
	}
	return tenantID + tenantIDSeparator + raw
}

// decodeTenantID recovers the owning tenant from a tenant-encoded identifier.
// An id lacking the separator is treated as belonging to the system / shared
// tenant (read-tolerant for legacy data); the returned id is the input
// unchanged so callers can still look it up.
//
// It returns the tenant ID and the original (full) id — the full id is what was
// persisted as the storage-key suffix, so callers route on tenantID but key on
// the full id.
func decodeTenantID(id string) (tenantID string, fullID string) {
	parts := strings.SplitN(id, tenantIDSeparator, 2)
	if len(parts) != 2 || parts[0] == "" {
		return systemTenantID, id
	}
	return parts[0], id
}
