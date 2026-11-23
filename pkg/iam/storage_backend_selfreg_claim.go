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

import (
	"context"
	"encoding/json"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// selfRegistrationClaimAttempts bounds how many lost swaps a claim retries
// before it reports the registration as held.
const selfRegistrationClaimAttempts = 3

// compareAndSwapKV is the guarded write a self-registration claim needs: the
// value is replaced only while it still holds what the claim read.
type compareAndSwapKV interface {
	CompareAndSwap(ctx context.Context, key string, expected, next []byte) (bool, error)
}

var _ selfRegistrationClaimStorage = (*BackendStorage)(nil)

// ClaimSelfRegistration takes a claim on the tenant's unfinished
// self-registration by a compare-and-swap of the tenant record, so a claim
// made on any node sees and refuses another's live claim. A store that cannot
// swap refuses the claim; it never falls back to an unconditional write.
func (s *BackendStorage) ClaimSelfRegistration(ctx context.Context, tenantID string, claim SelfRegistrationClaim) (*Tenant, error) {
	cas, ok := s.kv.(compareAndSwapKV)
	if !ok {
		return nil, &SelfRegistrationClaimError{TenantID: tenantID, Reason: SelfRegistrationClaimUnsupported}
	}
	key := tenantKey(tenantID)
	for range selfRegistrationClaimAttempts {
		raw, err := s.get(ctx, key)
		if err != nil {
			if dberrors.IsNotFound(err) {
				return nil, &SelfRegistrationClaimError{TenantID: tenantID, Reason: SelfRegistrationClaimFinished}
			}
			return nil, err
		}
		var cur Tenant
		if err := json.Unmarshal(raw, &cur); err != nil {
			return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.tenant.claim_self_registration", Err: err}
		}
		next, err := decideSelfRegistrationClaim(&cur, claim)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(next)
		if err != nil {
			return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.tenant.claim_self_registration", Err: err}
		}
		swapped, err := cas.CompareAndSwap(ctx, key, raw, data)
		if err != nil {
			return nil, err
		}
		if swapped {
			return next, nil
		}
	}
	return nil, &SelfRegistrationClaimError{TenantID: tenantID, Reason: SelfRegistrationClaimHeld}
}
