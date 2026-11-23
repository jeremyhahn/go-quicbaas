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

package iam

import "context"

var _ selfRegistrationClaimStorage = (*MemoryStorage)(nil)

// ClaimSelfRegistration takes a claim on the tenant's unfinished
// self-registration. The read, the decision and the write happen under the
// storage lock, so two claims never both succeed. A missing tenant holds no
// registration and reads as finished.
func (m *MemoryStorage) ClaimSelfRegistration(_ context.Context, tenantID string, claim SelfRegistrationClaim) (*Tenant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cur, exists := m.tenants[tenantID]
	if !exists {
		return nil, &SelfRegistrationClaimError{TenantID: tenantID, Reason: SelfRegistrationClaimFinished}
	}
	next, err := decideSelfRegistrationClaim(cur, claim)
	if err != nil {
		return nil, err
	}
	m.tenants[tenantID] = next
	out := *next
	if next.SelfRegistration != nil {
		reg := *next.SelfRegistration
		out.SelfRegistration = &reg
	}
	return &out, nil
}
