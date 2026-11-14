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
//	delete: write fence   -> read binding
//	bind:   write binding -> read fence
//
// If both reads missed the other's write, each read would precede the other
// side's write, which precedes its own read: a cycle. So at least one side
// sees the other. A delete that sees a binding refuses; a fresh bind that sees
// a fence withdraws its binding and refuses. They can both refuse, and the
// caller retries; they can never both succeed.
//
// The fence lives beside the bindings, in the tenant's SDLC store, so both
// reads and writes go through the same linearizable KV on every node.

const (
	deleteFenceEntityType = "sdlc_delete_fence"
	deleteFenceEntityName = "project delete fence"

	// DeleteFenceTTL is how long a fence holds without its delete clearing
	// it. A delete that crashes after fencing leaves its fence behind; after
	// this long a bind no longer honours it, so a project that survived a
	// crashed delete can be bound again.
	DeleteFenceTTL = 10 * time.Minute
)

// deleteFence is the record a delete in flight holds on its project.
type deleteFence struct {
	ProjectID string    `json:"project_id"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
}

func deleteFenceKey(projectID string) string {
	return recordKey(deleteFenceEntityType, entityIDFor(projectID))
}

// FenceProjectDelete marks projectID as being deleted and returns the token
// that clears the mark. The caller checks for a binding only AFTER this
// returns, and clears the fence with ClearProjectDeleteFence whatever the
// delete's outcome. A fence already held, by a concurrent delete or a crashed
// one, is replaced: the newer delete is the one in flight.
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
	fence := &deleteFence{ProjectID: projectID, Token: token, CreatedAt: s.now()}
	key := deleteFenceKey(projectID)
	for attempt := 0; attempt < 8; attempt++ {
		_, raw, err := loadRecord[deleteFence](ctx, "FenceProjectDelete", store, key)
		if err != nil {
			return "", err
		}
		applied, err := storeRecord(ctx, "FenceProjectDelete", store, key, raw, fence)
		if err != nil {
			return "", err
		}
		if applied {
			return token, nil
		}
	}
	return "", &VersionConflictError{Entity: deleteFenceEntityName, TenantID: tenantID, ID: projectID}
}

// ClearProjectDeleteFence removes the fence token placed. A fence another
// delete has since placed is left alone, and an absent fence is not an error.
func (s *Store) ClearProjectDeleteFence(ctx context.Context, tenantID, projectID, token string) error {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return err
	}
	store, err := s.guardedKV(ctx, "ClearProjectDeleteFence", tenantID)
	if err != nil {
		return err
	}
	_, _, err = removeRecord(ctx, "ClearProjectDeleteFence", store, deleteFenceKey(projectID), func(f *deleteFence) bool {
		return f.ProjectID == projectID && f.Token == token
	})
	return err
}

// ProjectDeleteFenced reports whether a delete of projectID is in flight: a
// fence is held and is younger than DeleteFenceTTL.
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
	return s.now().Sub(fence.CreatedAt) < DeleteFenceTTL, nil
}
