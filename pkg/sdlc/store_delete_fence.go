package sdlc

import (
	"context"
	"time"
)

// A DELETE FENCE closes the window between a project delete's binding check
// and the delete itself (go-quicbaas#1303).
//
// The check and the delete are separate writes in separate stores, so a bind
// that commits between them would leave a binding on a deleted project. The
// fence turns the pair into two "write mine, then read theirs" sequences on
// one tenant KV:
//
//	delete: write fence   -> read binding -> delete -> fence becomes tombstone
//	bind:   write binding -> read fence   -> withdraw if fenced
//
// If both reads missed the other's write, each read would precede the other
// side's write, which precedes its own read: a cycle. So at least one side
// sees the other. A delete that sees a binding refuses and clears its fence;
// a bind that sees a fence withdraws its binding and refuses. They can both
// refuse, and the caller retries; they can never both succeed.
//
// THE PROTOCOL'S RULES, each closing a race the plain fence left open:
//
//   - ONE DELETE AT A TIME. FenceProjectDelete refuses with
//     DeleteInProgressError while a live (younger than DeleteFenceTTL) fence
//     is held; only an absent or stale fence is replaced. A second delete
//     replacing the first's fence would let the first's clear, or the
//     second's refusal, drop the fence the other still relies on.
//   - A DELETED PROJECT KEEPS A TOMBSTONE. On a successful delete the fence is
//     turned into a tombstone (MarkProjectDeleted) instead of being cleared,
//     so a bind whose fence read lands after the delete finished still sees
//     it and withdraws. ProjectDeleteFenced reports a tombstone whatever its
//     age, and ClearProjectDeleteFence never removes one. Project ids are
//     generated, never reused, so a tombstone is never in a live project's way.
//   - EVERY BIND READS THE FENCE, fresh bind or rebind, and withdraws only its
//     own write (Store.WithdrawBinding, keyed on the binding's WriteToken),
//     restoring the record it replaced.
//
// The fence lives beside the bindings, in the tenant's SDLC store, so both
// reads and writes go through the same linearizable KV on every node.

const (
	deleteFenceEntityType = "sdlc_delete_fence"
	deleteFenceEntityName = "project delete fence"
	deletedProjectEntity  = "project"

	// DeleteFenceTTL is how long a fence holds without its delete clearing
	// it. A delete that crashes after fencing leaves its fence behind; after
	// this long a bind no longer honours it, and another delete may replace
	// it, so a project that survived a crashed delete can be bound or deleted
	// again. A tombstone never goes stale.
	DeleteFenceTTL = 10 * time.Minute

	// fenceWriteAttempts bounds the rounds a fence write retries after losing
	// the slot to another writer.
	fenceWriteAttempts = 8
)

// deleteFence is the record a delete in flight holds on its project, and,
// with Deleted set, the tombstone a completed delete leaves behind.
type deleteFence struct {
	ProjectID string    `json:"project_id"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
	Deleted   bool      `json:"deleted,omitempty"`
}

func deleteFenceKey(projectID string) string {
	return recordKey(deleteFenceEntityType, entityIDFor(projectID))
}

// FenceProjectDelete marks projectID as being deleted and returns the token
// that settles the mark. The caller checks for a binding only AFTER this
// returns; it then either turns the fence into a tombstone with
// MarkProjectDeleted (the project is gone) or removes it with
// ClearProjectDeleteFence (the delete was refused or failed).
//
// A live fence held by another delete refuses this one with
// *DeleteInProgressError; a stale fence (a crashed delete's) is replaced. A
// tombstone means the project is already deleted, and is reported as
// *NotFoundError.
func (s *Store) FenceProjectDelete(ctx context.Context, tenantID, projectID string) (string, error) {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return "", err
	}
	store, err := s.guardedKV(ctx, "FenceProjectDelete", tenantID)
	if err != nil {
		return "", err
	}
	token, err := newNonce()
	if err != nil {
		return "", backendError("FenceProjectDelete.token", err)
	}
	key := deleteFenceKey(projectID)
	for attempt := 0; attempt < fenceWriteAttempts; attempt++ {
		held, raw, err := loadRecord[deleteFence](ctx, "FenceProjectDelete", store, key)
		if err != nil {
			return "", err
		}
		now := s.now()
		if held != nil && held.ProjectID == projectID {
			if held.Deleted {
				return "", &NotFoundError{Entity: deletedProjectEntity, TenantID: tenantID, ID: projectID}
			}
			if now.Sub(held.CreatedAt) < DeleteFenceTTL {
				return "", &DeleteInProgressError{TenantID: tenantID, ProjectID: projectID}
			}
		}
		fence := &deleteFence{ProjectID: projectID, Token: token, CreatedAt: now}
		applied, err := storeRecord(ctx, "FenceProjectDelete", store, key, raw, fence)
		if err != nil {
			return "", err
		}
		if applied {
			return token, nil
		}
		if err := ctx.Err(); err != nil {
			return "", backendError("FenceProjectDelete", err)
		}
	}
	return "", &VersionConflictError{Entity: deleteFenceEntityName, TenantID: tenantID, ID: projectID}
}

// MarkProjectDeleted turns the fence token placed into a tombstone: the
// project is deleted, and every later fence read reports it. Only the
// caller's own fence is turned (a compare-and-swap on the exact record read);
// a fence that is absent or carries another token is refused with
// *VersionConflictError and nothing is written. Marking an already-marked
// tombstone of the same token is a no-op.
func (s *Store) MarkProjectDeleted(ctx context.Context, tenantID, projectID, token string) error {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return err
	}
	store, err := s.guardedKV(ctx, "MarkProjectDeleted", tenantID)
	if err != nil {
		return err
	}
	key := deleteFenceKey(projectID)
	conflict := &VersionConflictError{Entity: deleteFenceEntityName, TenantID: tenantID, ID: projectID}
	for attempt := 0; attempt < fenceWriteAttempts; attempt++ {
		held, raw, err := loadRecord[deleteFence](ctx, "MarkProjectDeleted", store, key)
		if err != nil {
			return err
		}
		if held == nil || held.ProjectID != projectID || held.Token != token {
			return conflict
		}
		if held.Deleted {
			return nil
		}
		tombstone := *held
		tombstone.Deleted = true
		applied, err := storeRecord(ctx, "MarkProjectDeleted", store, key, raw, &tombstone)
		if err != nil {
			return err
		}
		if applied {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return backendError("MarkProjectDeleted", err)
		}
	}
	return conflict
}

// ClearProjectDeleteFence removes the fence token placed. A fence another
// delete has since placed is left alone, a tombstone is never removed, and an
// absent fence is not an error.
func (s *Store) ClearProjectDeleteFence(ctx context.Context, tenantID, projectID, token string) error {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return err
	}
	store, err := s.guardedKV(ctx, "ClearProjectDeleteFence", tenantID)
	if err != nil {
		return err
	}
	_, _, err = removeRecord(ctx, "ClearProjectDeleteFence", store, deleteFenceKey(projectID), func(f *deleteFence) bool {
		return f.ProjectID == projectID && f.Token == token && !f.Deleted
	})
	return err
}

// ProjectDeleteFenced reports whether projectID is being or has been deleted:
// a tombstone is held, whatever its age, or a fence younger than
// DeleteFenceTTL is.
func (s *Store) ProjectDeleteFenced(ctx context.Context, tenantID, projectID string) (bool, error) {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return false, err
	}
	store, err := s.tenantKV(ctx, "ProjectDeleteFenced", tenantID)
	if err != nil {
		return false, err
	}
	fence, _, err := loadRecord[deleteFence](ctx, "ProjectDeleteFenced", store, deleteFenceKey(projectID))
	if err != nil {
		return false, err
	}
	if fence == nil || fence.ProjectID != projectID {
		return false, nil
	}
	if fence.Deleted {
		return true, nil
	}
	return s.now().Sub(fence.CreatedAt) < DeleteFenceTTL, nil
}
