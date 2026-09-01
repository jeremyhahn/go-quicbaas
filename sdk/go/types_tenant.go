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

package quicbaas

import (
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// System-tenant re-exports. QuicBaaS is the single SDK frontage for the
// stack, so an embedding host (e.g. go-trusted-platform, whose layer guard
// forbids a direct go-qrdb import) must be able to ask "is this the system
// tenant" without reaching past this SDK. These are pure aliases — no
// transport state, no behavior — so the answer here is by construction the
// same answer the engine gives itself.

// SystemTenantID is the identifier of the reserved system tenant: the
// structurally privileged scope that owns cross-tenant state and the shared
// system barrier. Prefer the IsSystemTenant predicate over comparing against
// this constant, because an unbound tenant also resolves to the system scope.
const SystemTenantID = qrdbsdk.SystemTenantID

// IsSystemTenant reports whether id denotes the structurally privileged
// system tenant. It returns true for both SystemTenantID and the empty
// string, since an unbound tenant resolves to the system tenant. It is the
// canonical predicate for "is this the system scope" and must be used instead
// of comparing against SystemTenantID directly — a bare string comparison
// misses the empty-tenant case and silently under-privileges internal callers.
var IsSystemTenant = qrdbsdk.IsSystemTenant

// AuthIdentity is the authenticated principal go-qrdb carries on a request
// context (subject, tenant, roles). It is named AuthIdentity rather than
// Identity because this package already exports Identity for the resolved
// entity on a login-ceremony FinishLoginResponse (see services_security.go);
// the two are distinct types and collapsing them would be a silent widening.
type AuthIdentity = qrdbsdk.Identity

// IdentityIsSystemTenant reports whether identity is scoped to the
// structurally privileged system tenant. A nil identity is treated as
// system-scoped: no auth context implies an internal, unauthenticated
// pipeline such as platform boot. A non-nil identity delegates to
// IsSystemTenant on its tenant ID, so the string-level rule has exactly one
// source of truth.
var IdentityIsSystemTenant = qrdbsdk.IdentityIsSystemTenant

// TenantIsolationType identifies a tenant's engine-side isolation mode. It is
// re-exported here so a host can name the type when it fills in the tenant
// provisioning knobs on iam.CreateTenantRequest.
type TenantIsolationType = qrdbsdk.TenantIsolationType

// Engine isolation modes, ordered from weakest to strongest separation. An
// empty TenantIsolationType defers to the engine default (prefix).
const (
	// TenantIsolationPrefix shares the system barrier and separates tenant
	// data by logical key prefixing.
	TenantIsolationPrefix = qrdbsdk.TenantIsolationPrefix

	// TenantIsolationBarrier gives the tenant its own DEK and a dedicated
	// namespace under the system data root.
	TenantIsolationBarrier = qrdbsdk.TenantIsolationBarrier

	// TenantIsolationVolume gives the tenant a dedicated filesystem DataRoot.
	TenantIsolationVolume = qrdbsdk.TenantIsolationVolume

	// TenantIsolationRaft gives the tenant a dedicated Raft group per database.
	TenantIsolationRaft = qrdbsdk.TenantIsolationRaft
)
