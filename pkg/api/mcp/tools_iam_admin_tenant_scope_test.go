//go:build mcp

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

package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// tenantObjects holds one admin assignment, one active impersonation session,
// one export and one deletion request, all in a single tenant.
type tenantObjects struct {
	reg          *ToolRegistry
	adminMgr     *iam.AdminManager
	gdprMgr      *iam.GDPRManager
	assignmentID string
	sessionID    string
	exportID     string
	deletionID   string
}

// adminAndGDPRMock serves both the admin and the GDPR manager.
type adminAndGDPRMock struct {
	mockService
	adminMgr *iam.AdminManager
	gdprMgr  *iam.GDPRManager
}

func (m *adminAndGDPRMock) GetAdminManager(_ context.Context) (*iam.AdminManager, error) {
	return m.adminMgr, nil
}

func (m *adminAndGDPRMock) GetGDPRManager(_ context.Context) (*iam.GDPRManager, error) {
	return m.gdprMgr, nil
}

// newTenantObjects seeds the objects in owner's tenant straight through the
// managers and storage, so no transport check takes part in the seeding.
func newTenantObjects(t *testing.T, owner string) *tenantObjects {
	t.Helper()
	seed := context.Background()

	sessions := iam.NewMemoryImpersonationStorage()
	adminMgr, err := iam.NewAdminManager(iam.AdminManagerOptions{
		AdminStorage:         iam.NewMemoryAdminStorage(),
		ImpersonationStorage: sessions,
		TargetResolver:       mcpAllowAllTargetResolver{},
	})
	require.NoError(t, err)
	gdprMgr := newGDPRMockWithManager(t).gdprMgr

	assignment, err := adminMgr.CreateAssignment(seed, iam.CreateAdminAssignmentOptions{
		UserID: 30001, AdminRole: iam.AdminRoleTenantAdmin, TenantIDs: []string{owner}, GrantedBy: "30021",
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	session := &iam.ImpersonationSession{
		ID: "imp_" + owner, AdminUserID: 30021, TargetUserID: 30001, TenantID: owner,
		Reason: "support", StartedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	require.NoError(t, sessions.CreateImpersonationSession(seed, session))

	reg := &ToolRegistry{service: &adminAndGDPRMock{adminMgr: adminMgr, gdprMgr: gdprMgr}, logger: newTestLogger(t)}
	ownerCtx := tenantctx.WithTenant(seed, owner)
	exported, err := reg.executeIAMGDPRExportRequest(ownerCtx, map[string]interface{}{
		"tenant_id": owner, "user_id": "30001", "requested_by": "50100", "format": "json",
	})
	require.NoError(t, err)
	deleted, err := reg.executeIAMGDPRDeletionRequest(ownerCtx, map[string]interface{}{
		"tenant_id": owner, "user_id": "30001", "requested_by": "50100", "reason": "user asked",
	})
	require.NoError(t, err)

	return &tenantObjects{
		reg: reg, adminMgr: adminMgr, gdprMgr: gdprMgr,
		assignmentID: assignment.ID, sessionID: session.ID,
		exportID:   exported.(map[string]interface{})["id"].(string),
		deletionID: deleted.(map[string]interface{})["id"].(string),
	}
}

// TestIAMAdminAndGDPRTools_AnotherTenantsObjectIsNotFound proves the MCP
// delegated-admin, impersonation and data-request tools answer another
// tenant's object as not found without writing it, refuse a request naming
// another tenant, and leave another tenant's objects out of their lists.
func TestIAMAdminAndGDPRTools_AnotherTenantsObjectIsNotFound(t *testing.T) {
	objs := newTenantObjects(t, "tenant-b")
	reg := objs.reg
	intruder := tenantctx.WithTenant(mcpCtxWithActor(30021), "tenant-a")

	notFound := []struct {
		name string
		want error
		call func() error
	}{
		{"assignment get", iam.ErrAdminAssignmentNotFound, func() error {
			_, err := reg.executeIAMAdminAssignmentGet(intruder, map[string]interface{}{"assignment_id": objs.assignmentID})
			return err
		}},
		{"assignment update", iam.ErrAdminAssignmentNotFound, func() error {
			_, err := reg.executeIAMAdminAssignmentUpdate(intruder, map[string]interface{}{"assignment_id": objs.assignmentID, "active": false})
			return err
		}},
		{"assignment delete", iam.ErrAdminAssignmentNotFound, func() error {
			_, err := reg.executeIAMAdminAssignmentDelete(intruder, map[string]interface{}{"assignment_id": objs.assignmentID})
			return err
		}},
		{"impersonation end", iam.ErrImpersonationSessionNotFound, func() error {
			_, err := reg.executeIAMImpersonationEnd(intruder, map[string]interface{}{"session_id": objs.sessionID})
			return err
		}},
		{"export status", iam.ErrDataExportNotFound, func() error {
			_, err := reg.executeIAMGDPRExportStatus(intruder, map[string]interface{}{"export_id": objs.exportID})
			return err
		}},
		{"export generate", iam.ErrDataExportNotFound, func() error {
			_, err := reg.executeIAMGDPRExportGenerate(intruder, map[string]interface{}{"export_id": objs.exportID})
			return err
		}},
		{"deletion cancel", iam.ErrDeletionNotFound, func() error {
			_, err := reg.executeIAMGDPRDeletionCancel(intruder, map[string]interface{}{"deletion_id": objs.deletionID, "cancelled_by": "30021"})
			return err
		}},
		{"deletion process", iam.ErrDeletionNotFound, func() error {
			_, err := reg.executeIAMGDPRDeletionProcess(intruder, map[string]interface{}{"deletion_id": objs.deletionID})
			return err
		}},
		{"deletion status", iam.ErrDeletionNotFound, func() error {
			_, err := reg.executeIAMGDPRDeletionStatus(intruder, map[string]interface{}{"deletion_id": objs.deletionID})
			return err
		}},
	}
	for _, tc := range notFound {
		assert.ErrorIs(t, tc.call(), tc.want, "%s on another tenant's object", tc.name)
	}

	refused := map[string]func() error{
		"assignment create": func() error {
			_, err := reg.executeIAMAdminAssignmentCreate(intruder, map[string]interface{}{
				"user_id": "30001", "admin_role": string(iam.AdminRoleTenantAdmin), "granted_by": "30021",
				"tenant_ids": []interface{}{"tenant-b"},
			})
			return err
		},
		"assignment list": func() error {
			_, err := reg.executeIAMAdminAssignmentList(intruder, map[string]interface{}{"tenant_id": "tenant-b"})
			return err
		},
		"check permission": func() error {
			_, err := reg.executeIAMAdminCheckPermission(intruder, map[string]interface{}{
				"user_id": "30001", "permission": string(iam.PermUserImpersonate), "tenant_id": "tenant-b",
			})
			return err
		},
		"impersonation start": func() error {
			_, err := reg.executeIAMImpersonationStart(intruder, map[string]interface{}{
				"target_user_id": "30001", "tenant_id": "tenant-b", "reason": "support",
			})
			return err
		},
	}
	for name, call := range refused {
		var cross *tenantctx.CrossTenantError
		assert.True(t, errors.As(call(), &cross), "%s naming another tenant must be refused", name)
	}

	listed, err := reg.executeIAMAdminAssignmentList(intruder, map[string]interface{}{})
	require.NoError(t, err)
	assert.Empty(t, listed.(map[string]interface{})["assignments"], "another tenant's assignment must not be listed")
	active, err := reg.executeIAMImpersonationList(intruder, map[string]interface{}{"admin_user_id": "30021"})
	require.NoError(t, err)
	assert.Empty(t, active.(map[string]interface{})["sessions"], "another tenant's session must not be listed")

	seed := context.Background()
	kept, err := objs.adminMgr.GetAssignment(seed, objs.assignmentID)
	require.NoError(t, err, "another tenant's assignment must survive")
	assert.True(t, kept.Active)
	session, err := objs.adminMgr.GetImpersonationSession(seed, objs.sessionID)
	require.NoError(t, err)
	assert.Nil(t, session.EndedAt, "another tenant's session must stay open")
	deletion, err := objs.gdprMgr.GetDeletionStatus(seed, objs.deletionID)
	require.NoError(t, err)
	assert.Equal(t, iam.DeletionStatusPending, deletion.Status, "another tenant's deletion must stay pending")
}

// TestIAMAdminAndGDPRTools_OwnTenantObjectsAreServed is the owning-tenant
// twin: the same objects in the caller's tenant are served and written.
func TestIAMAdminAndGDPRTools_OwnTenantObjectsAreServed(t *testing.T) {
	objs := newTenantObjects(t, "tenant-a")
	reg := objs.reg
	owner := tenantctx.WithTenant(mcpCtxWithActor(30021), "tenant-a")

	created, err := reg.executeIAMAdminAssignmentCreate(owner, map[string]interface{}{
		"user_id": "30002", "admin_role": string(iam.AdminRoleTenantAdmin), "granted_by": "30021",
	})
	require.NoError(t, err)
	assert.NotNil(t, created)

	_, err = reg.executeIAMAdminAssignmentGet(owner, map[string]interface{}{"assignment_id": objs.assignmentID})
	require.NoError(t, err)
	_, err = reg.executeIAMAdminAssignmentUpdate(owner, map[string]interface{}{"assignment_id": objs.assignmentID, "active": false})
	require.NoError(t, err)
	listed, err := reg.executeIAMAdminAssignmentList(owner, map[string]interface{}{"tenant_id": "tenant-a"})
	require.NoError(t, err)
	assert.NotEmpty(t, listed.(map[string]interface{})["assignments"])
	_, err = reg.executeIAMAdminCheckPermission(owner, map[string]interface{}{
		"user_id": "30001", "permission": string(iam.PermUserImpersonate), "tenant_id": "tenant-a",
	})
	require.NoError(t, err)
	_, err = reg.executeIAMAdminAssignmentDelete(owner, map[string]interface{}{"assignment_id": objs.assignmentID})
	require.NoError(t, err)

	active, err := reg.executeIAMImpersonationList(owner, map[string]interface{}{"admin_user_id": "30021"})
	require.NoError(t, err)
	assert.NotEmpty(t, active.(map[string]interface{})["sessions"])
	_, err = reg.executeIAMImpersonationEnd(owner, map[string]interface{}{"session_id": objs.sessionID})
	require.NoError(t, err)

	_, err = reg.executeIAMGDPRExportStatus(owner, map[string]interface{}{"export_id": objs.exportID})
	require.NoError(t, err)
	_, err = reg.executeIAMGDPRDeletionStatus(owner, map[string]interface{}{"deletion_id": objs.deletionID})
	require.NoError(t, err)
	_, err = reg.executeIAMGDPRDeletionCancel(owner, map[string]interface{}{"deletion_id": objs.deletionID, "cancelled_by": "30021"})
	require.NoError(t, err)
}
