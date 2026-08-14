// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package iam

import (
	"context"
	"errors"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// InviteRecord is the projection of an invitation this package's callers use.
//
// It carries no token: the plaintext exists only in the value returned by
// Create, and only its digest is ever stored.
type InviteRecord struct {
	// UserID is the user this invite activates.
	UserID string

	// TenantID is the tenant the user belongs to.
	TenantID string

	// ExpiresAt is when the invite stops being redeemable.
	ExpiresAt time.Time

	// Used reports whether the invite has been consumed.
	Used bool
}

// InviteStore issues and redeems one-time activation invitations.
//
// Every method names its tenant because invitations live in that tenant's own
// store — see qrdb's invite.TenantStore. A token issued for one tenant is not
// present in another's store at all, so presenting it to the wrong tenant is
// ErrInviteNotFound, the same answer a fabricated token gets.
//
// The implementation is qrdb's; this interface exists so PIVActivator and the
// REST handlers can be tested without storage, and so quicbaas keeps its own
// HTTP-mapped error values. There is no second implementation of the
// invitation lifecycle in this repository — that duplication was removed.
type InviteStore interface {
	// Create issues an invitation and returns the plaintext token, which is
	// returned exactly once and is not recoverable afterwards.
	Create(ctx context.Context, tenantID, userID string, ttl time.Duration) (token string, record *InviteRecord, err error)

	// Lookup resolves a token within a tenant without consuming it.
	Lookup(ctx context.Context, tenantID, token string) (*InviteRecord, error)

	// Redeem consumes a token within a tenant exactly once. Concurrent
	// redemptions resolve to exactly one winner; the losers get ErrInviteUsed.
	Redeem(ctx context.Context, tenantID, token string) (*InviteRecord, error)
}

// qrdbInviteStore adapts qrdb's tenant-scoped invitation store to this
// package's interface and error values.
//
// The adaptation is deliberately thin: it translates errors and projects the
// entity. All token generation, digesting, single-use enforcement and tenant
// routing belong to qrdb, which is the point — this repository previously kept
// its own copy of each.
type qrdbInviteStore struct {
	store *qrdbsdk.TenantInviteStore
}

// NewQRDBInviteStore adapts a qrdb tenant invitation store.
func NewQRDBInviteStore(store *qrdbsdk.TenantInviteStore) InviteStore {
	return &qrdbInviteStore{store: store}
}

// Compile-time check.
var _ InviteStore = (*qrdbInviteStore)(nil)

// Create implements InviteStore.
func (s *qrdbInviteStore) Create(ctx context.Context, tenantID, userID string, ttl time.Duration) (string, *InviteRecord, error) {
	token, entity, err := s.store.Create(ctx, userID, tenantID, ttl)
	if err != nil {
		return "", nil, translateInviteError(err)
	}
	return token, projectInvite(entity), nil
}

// Lookup implements InviteStore.
func (s *qrdbInviteStore) Lookup(ctx context.Context, tenantID, token string) (*InviteRecord, error) {
	entity, err := s.store.Lookup(ctx, tenantID, token)
	if err != nil {
		return nil, translateInviteError(err)
	}
	return projectInvite(entity), nil
}

// Redeem implements InviteStore.
func (s *qrdbInviteStore) Redeem(ctx context.Context, tenantID, token string) (*InviteRecord, error) {
	entity, err := s.store.Redeem(ctx, tenantID, token)
	if err != nil {
		return nil, translateInviteError(err)
	}
	return projectInvite(entity), nil
}

// projectInvite narrows a qrdb invitation to what this package's callers read.
func projectInvite(entity *qrdbsdk.InviteEntity) *InviteRecord {
	if entity == nil {
		return nil
	}
	return &InviteRecord{
		UserID:    entity.UserID,
		TenantID:  entity.TenantID,
		ExpiresAt: entity.ExpiresAt,
		Used:      entity.Redeemed(),
	}
}

// translateInviteError maps qrdb's sentinel errors onto this package's
// HTTP-mapped QBError values.
//
// The mapping is not cosmetic: these values carry the status code the REST
// layer returns, so passing qrdb's plain sentinels through would turn a
// deliberate 404/403 into a 500. Anything unrecognised is returned unchanged
// rather than being flattened into "not found", which would report a storage
// outage as a missing invitation.
func translateInviteError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, qrdbsdk.ErrInviteNotFound), errors.Is(err, qrdbsdk.ErrInviteEmptyToken):
		return ErrInviteNotFound
	case errors.Is(err, qrdbsdk.ErrInviteExpired):
		return ErrInviteExpired
	case errors.Is(err, qrdbsdk.ErrInviteAlreadyRedeemed):
		return ErrInviteUsed
	default:
		return err
	}
}
