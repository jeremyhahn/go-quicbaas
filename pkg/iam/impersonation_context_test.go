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

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithImpersonationContext(t *testing.T) {
	t.Run("AttachesContextSuccessfully", func(t *testing.T) {
		ctx := context.Background()
		impCtx := &ImpersonationContext{
			SessionID:      "imp_test123",
			AdminUserID:    30021,
			AdminUsername:  "admin@example.com",
			TargetUserID:   1,
			TargetUsername: "user@example.com",
			TenantID:       "tenant1",
			Reason:         "support ticket #123",
		}

		newCtx := WithImpersonationContext(ctx, impCtx)

		retrieved := GetImpersonationContext(newCtx)
		require.NotNil(t, retrieved)
		assert.Equal(t, "imp_test123", retrieved.SessionID)
		assert.Equal(t, uint64(30021), retrieved.AdminUserID)
		assert.Equal(t, "admin@example.com", retrieved.AdminUsername)
		assert.Equal(t, uint64(1), retrieved.TargetUserID)
		assert.Equal(t, "user@example.com", retrieved.TargetUsername)
		assert.Equal(t, "tenant1", retrieved.TenantID)
		assert.Equal(t, "support ticket #123", retrieved.Reason)
	})

	t.Run("NilImpersonationContext", func(t *testing.T) {
		ctx := context.Background()
		newCtx := WithImpersonationContext(ctx, nil)

		retrieved := GetImpersonationContext(newCtx)
		assert.Nil(t, retrieved)
	})
}

func TestGetImpersonationContext(t *testing.T) {
	t.Run("ReturnsNilForEmptyContext", func(t *testing.T) {
		ctx := context.Background()
		retrieved := GetImpersonationContext(ctx)
		assert.Nil(t, retrieved)
	})

	t.Run("ReturnsNilForWrongValueType", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), impersonationContextKey{}, "wrong type")
		retrieved := GetImpersonationContext(ctx)
		assert.Nil(t, retrieved)
	})
}

func TestIsImpersonating(t *testing.T) {
	t.Run("ReturnsTrueWhenImpersonating", func(t *testing.T) {
		ctx := context.Background()
		impCtx := &ImpersonationContext{
			SessionID: "imp_test123",
		}
		newCtx := WithImpersonationContext(ctx, impCtx)

		assert.True(t, IsImpersonating(newCtx))
	})

	t.Run("ReturnsFalseWhenNotImpersonating", func(t *testing.T) {
		ctx := context.Background()
		assert.False(t, IsImpersonating(ctx))
	})
}

func TestImpersonationContextFromSession(t *testing.T) {
	t.Run("CreatesContextFromSession", func(t *testing.T) {
		session := &ImpersonationSession{
			ID:             "imp_session1",
			AdminUserID:    30021,
			AdminUsername:  "admin@example.com",
			TargetUserID:   1,
			TargetUsername: "user@example.com",
			TenantID:       "tenant1",
			Reason:         "testing",
			StartedAt:      time.Now().UTC(),
			ExpiresAt:      time.Now().UTC().Add(time.Hour),
		}

		impCtx := ImpersonationContextFromSession(session)

		require.NotNil(t, impCtx)
		assert.Equal(t, session.ID, impCtx.SessionID)
		assert.Equal(t, session.AdminUserID, impCtx.AdminUserID)
		assert.Equal(t, session.AdminUsername, impCtx.AdminUsername)
		assert.Equal(t, session.TargetUserID, impCtx.TargetUserID)
		assert.Equal(t, session.TargetUsername, impCtx.TargetUsername)
		assert.Equal(t, session.TenantID, impCtx.TenantID)
		assert.Equal(t, session.Reason, impCtx.Reason)
	})

	t.Run("ReturnsNilForNilSession", func(t *testing.T) {
		impCtx := ImpersonationContextFromSession(nil)
		assert.Nil(t, impCtx)
	})
}

func TestBuildImpersonatedActor(t *testing.T) {
	t.Run("BuildsActorCorrectly", func(t *testing.T) {
		impCtx := &ImpersonationContext{
			SessionID:      "imp_test123",
			AdminUserID:    30021,
			AdminUsername:  "admin@example.com",
			TargetUserID:   1,
			TargetUsername: "user@example.com",
		}

		actor := BuildImpersonatedActor(impCtx)

		require.NotNil(t, actor)
		assert.Equal(t, "1", actor.UserID)
		assert.Equal(t, "user@example.com", actor.Username)
		assert.Equal(t, "30021", actor.ImpersonatingUserID)
		assert.Equal(t, "admin@example.com", actor.ImpersonatingUsername)
	})

	t.Run("ReturnsNilForNilContext", func(t *testing.T) {
		actor := BuildImpersonatedActor(nil)
		assert.Nil(t, actor)
	})
}

func TestBuildAuditEventForImpersonatedOperation(t *testing.T) {
	t.Run("BuildsEventCorrectly", func(t *testing.T) {
		impCtx := &ImpersonationContext{
			SessionID:      "imp_test123",
			AdminUserID:    30021,
			AdminUsername:  "admin@example.com",
			TargetUserID:   1,
			TargetUsername: "user@example.com",
			TenantID:       "tenant1",
			Reason:         "support ticket #123",
		}

		resource := &AuditResource{
			Type: "document",
			ID:   "doc123",
			Name: "Test Document",
		}

		details := map[string]interface{}{
			"action_detail": "read",
		}

		event, err := BuildAuditEventForImpersonatedOperation(
			impCtx,
			"document.read",
			AuditEventAuthorization,
			AuditCategorySecurity,
			AuditOutcomeSuccess,
			resource,
			details,
		)

		require.NoError(t, err)
		require.NotNil(t, event)
		assert.NotEmpty(t, event.ID)
		assert.Equal(t, "tenant1", event.TenantID)
		assert.Equal(t, "document.read", event.Action)
		assert.Equal(t, AuditEventAuthorization, event.EventType)
		assert.Equal(t, AuditCategorySecurity, event.Category)
		assert.Equal(t, AuditOutcomeSuccess, event.Outcome)
		assert.Equal(t, "imp_test123", event.ImpersonationSessionID)
		assert.Equal(t, "imp_test123", event.CorrelationID)

		// Check actor
		require.NotNil(t, event.Actor)
		assert.Equal(t, "1", event.Actor.UserID)
		assert.Equal(t, "user@example.com", event.Actor.Username)
		assert.Equal(t, "30021", event.Actor.ImpersonatingUserID)
		assert.Equal(t, "admin@example.com", event.Actor.ImpersonatingUsername)

		// Check resource
		require.NotNil(t, event.Resource)
		assert.Equal(t, "document", event.Resource.Type)
		assert.Equal(t, "doc123", event.Resource.ID)

		// Check details include both impersonation and custom details
		require.NotNil(t, event.Details)
		assert.Equal(t, "1", event.Details["impersonated_user_id"])
		assert.Equal(t, "user@example.com", event.Details["impersonated_username"])
		assert.Equal(t, "support ticket #123", event.Details["impersonation_reason"])
		assert.Equal(t, "read", event.Details["action_detail"])
	})

	t.Run("ReturnsNilForNilContext", func(t *testing.T) {
		event, err := BuildAuditEventForImpersonatedOperation(
			nil,
			"test.action",
			AuditEventAuthorization,
			AuditCategorySecurity,
			AuditOutcomeSuccess,
			nil,
			nil,
		)
		require.NoError(t, err)
		assert.Nil(t, event)
	})

	t.Run("RefusesWhenTheCSPRNGFails", func(t *testing.T) {
		broken := breakAuditLogIDEntropy(t)

		event, err := BuildAuditEventForImpersonatedOperation(
			&ImpersonationContext{SessionID: "imp_test123", TenantID: "tenant1"},
			"document.read",
			AuditEventAuthorization,
			AuditCategorySecurity,
			AuditOutcomeSuccess,
			nil,
			nil,
		)

		var idErr *AuditLogIDGenerationError
		require.ErrorAs(t, err, &idErr)
		assert.ErrorIs(t, err, broken)
		assert.Nil(t, event, "an event without a unique ID must not be built")
	})
}
