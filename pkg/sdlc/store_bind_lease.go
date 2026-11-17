package sdlc

import (
	"context"
	"time"
)

// A BIND LEASE serialises the binds and unbinds of one project
// (go-quicbaas#1679).
//
// A bind is several writes: the prior delivery is retired, the route is
// recorded, the binding is put, the delete fence is read and, if it is held,
// the binding is withdrawn and the route forgotten. Two binds of one project
// that interleave those writes can leave a route without a binding, or a
// withdrawal that restores a record the other bind already replaced. The
// lease makes the sequence exclusive: it is taken right before the first
// write and released after the last, and a second bind or unbind of the
// project is refused with *BindInProgressError while it is held.
//
// The lease is built like the delete fence (store_delete_fence.go): a record
// in the tenant's SDLC KV, placed by compare-and-swap, carrying the token
// that releases it. A holder that crashes leaves its lease behind; after
// BindLeaseTTL it is stale and the next bind replaces it.

const (
	bindLeaseEntityType = "sdlc_bind_lease"
	bindLeaseEntityName = "project bind lease"

	// BindLeaseTTL is how long a lease holds without its holder releasing
	// it. A bind runs well inside it; a lease older than this belongs to a
	// crashed bind and is replaced.
	BindLeaseTTL = 2 * time.Minute
)

// bindLease is the record a bind or unbind in flight holds on its project.
type bindLease struct {
	ProjectID string    `json:"project_id"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
}

func bindLeaseKey(projectID string) string {
	return recordKey(bindLeaseEntityType, entityIDFor(projectID))
}

// AcquireBindLease takes projectID's bind lease and returns the token that
// releases it. A live lease held by another caller refuses with
// *BindInProgressError; a stale lease is replaced.
func (s *Store) AcquireBindLease(ctx context.Context, tenantID, projectID string) (string, error) {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return "", err
	}
	store, err := s.guardedKV(ctx, "AcquireBindLease", tenantID)
	if err != nil {
		return "", err
	}
	token, err := newNonce()
	if err != nil {
		return "", backendError("AcquireBindLease.token", err)
	}
	key := bindLeaseKey(projectID)
	for attempt := 0; attempt < fenceWriteAttempts; attempt++ {
		held, raw, err := loadRecord[bindLease](ctx, "AcquireBindLease", store, key)
		if err != nil {
			return "", err
		}
		now := s.now()
		if held != nil && held.ProjectID == projectID && now.Sub(held.CreatedAt) < BindLeaseTTL {
			return "", &BindInProgressError{TenantID: tenantID, ProjectID: projectID}
		}
		lease := &bindLease{ProjectID: projectID, Token: token, CreatedAt: now}
		applied, err := storeRecord(ctx, "AcquireBindLease", store, key, raw, lease)
		if err != nil {
			return "", err
		}
		if applied {
			return token, nil
		}
		if err := ctx.Err(); err != nil {
			return "", backendError("AcquireBindLease", err)
		}
	}
	return "", &VersionConflictError{Entity: bindLeaseEntityName, TenantID: tenantID, ID: projectID}
}

// ReleaseBindLease removes the lease token took. A lease another caller has
// since taken (after this one went stale) is left alone, and an absent lease
// is not an error.
func (s *Store) ReleaseBindLease(ctx context.Context, tenantID, projectID, token string) error {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return err
	}
	store, err := s.guardedKV(ctx, "ReleaseBindLease", tenantID)
	if err != nil {
		return err
	}
	_, _, err = removeRecord(ctx, "ReleaseBindLease", store, bindLeaseKey(projectID), func(l *bindLease) bool {
		return l.ProjectID == projectID && l.Token == token
	})
	return err
}
