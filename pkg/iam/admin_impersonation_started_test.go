package iam

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// grantImpersonation gives userID the impersonate permission on tenant1.
func grantImpersonation(t *testing.T, manager *AdminManager, userID uint64) {
	t.Helper()
	_, err := manager.CreateAssignment(context.Background(), CreateAdminAssignmentOptions{
		UserID:      userID,
		AdminRole:   AdminRoleSuperAdmin,
		TenantIDs:   []string{"tenant1"},
		Permissions: []AdminPermission{PermUserImpersonate},
		GrantedBy:   "system",
	})
	require.NoError(t, err)
}

// TestAdminManager_StartImpersonation_StampsStartedAt pins go-quicbaas#2131:
// the session StartImpersonation returns, the one it stores, and the JSON a
// transport writes all carry the start time, and ExpiresAt follows it.
func TestAdminManager_StartImpersonation_StampsStartedAt(t *testing.T) {
	manager, _ := createTestAdminManager(t)
	grantImpersonation(t, manager, 30001)

	before := time.Now().UTC()
	session, err := manager.StartImpersonation(ctxWithActor(30001), &StartImpersonationOptions{
		TargetUserID: 40001,
		TenantID:     "tenant1",
		Reason:       "Support ticket #2131",
		Duration:     time.Hour,
	})
	after := time.Now().UTC()
	require.NoError(t, err)
	require.NotNil(t, session)

	require.False(t, session.StartedAt.IsZero(), "StartedAt must be set")
	assert.False(t, session.StartedAt.Before(before), "StartedAt is before the call")
	assert.False(t, session.StartedAt.After(after), "StartedAt is after the call")
	assert.Equal(t, session.StartedAt.Add(time.Hour), session.ExpiresAt)

	stored, err := manager.GetImpersonationSession(context.Background(), session.ID)
	require.NoError(t, err)
	assert.True(t, session.StartedAt.Equal(stored.StartedAt), "the stored session keeps StartedAt")

	encoded, err := json.Marshal(session)
	require.NoError(t, err)
	var wire struct {
		StartedAt string `json:"started_at"`
		ExpiresAt string `json:"expires_at"`
	}
	require.NoError(t, json.Unmarshal(encoded, &wire))
	startedAt, err := time.Parse(time.RFC3339Nano, wire.StartedAt)
	require.NoError(t, err)
	assert.True(t, session.StartedAt.Equal(startedAt), "started_at on the wire matches the session")
	assert.NotEmpty(t, wire.ExpiresAt)
}

// TestAdminManager_StartImpersonation_MissingReasonHasNoSession pins the error
// path: no reason is a typed invalid-input error and no session is created.
func TestAdminManager_StartImpersonation_MissingReasonHasNoSession(t *testing.T) {
	manager, _ := createTestAdminManager(t)
	grantImpersonation(t, manager, 30001)

	session, err := manager.StartImpersonation(ctxWithActor(30001), &StartImpersonationOptions{
		TargetUserID: 40001,
		TenantID:     "tenant1",
	})
	require.Error(t, err)
	assert.True(t, dberrors.IsInvalidInput(err), "want invalid input, got %v", err)
	assert.Nil(t, session)
}
