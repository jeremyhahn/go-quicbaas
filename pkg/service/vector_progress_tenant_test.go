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

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qbErrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// Tenant A ("tenant-a") owns one rebuild job and one compaction job on its
// namespace "docs", stored under the scoped name "tenant/tenant-a/docs".
const (
	progressTenantA        = "tenant-a"
	progressTenantB        = "tenant-b"
	progressNamespace      = "docs"
	progressScopedA        = "tenant/tenant-a/docs"
	progressRebuildJobID   = "rebuild-a"
	progressCompactionOpID = "compact-a"
)

func newProgressTenantService(t *testing.T) *vectorService {
	t.Helper()
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	svc.jobMu.Lock()
	svc.rebuildJobs[progressRebuildJobID] = &RebuildJobState{
		JobID:      progressRebuildJobID,
		Namespace:  progressScopedA,
		InProgress: true,
		StartedAt:  time.Now(),
	}
	svc.compactionJobs[progressCompactionOpID] = &CompactionJobState{
		OperationID: progressCompactionOpID,
		Namespace:   progressScopedA,
		InProgress:  true,
		StartedAt:   time.Now(),
	}
	svc.jobMu.Unlock()
	return svc
}

func requireProgressNotFound(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var qbErr *qbErrors.QBError
	require.True(t, errors.As(err, &qbErr), "expected a QBError, got %T", err)
	assert.Equal(t, qbErrors.ErrNotFound, qbErr.Code)
}

func TestGetRebuildProgress_TenantFindsOwnJobByNamespace(t *testing.T) {
	svc := newProgressTenantService(t)
	ctx := tenantctx.WithTenant(context.Background(), progressTenantA)

	resp, err := svc.GetRebuildProgress(ctx, &GetRebuildProgressRequest{Namespace: progressNamespace})
	require.NoError(t, err)
	assert.Equal(t, progressRebuildJobID, resp.JobID)
	assert.Equal(t, progressNamespace, resp.Namespace)
}

func TestGetRebuildProgress_OtherTenantCannotReadByNamespace(t *testing.T) {
	svc := newProgressTenantService(t)
	ctx := tenantctx.WithTenant(context.Background(), progressTenantB)

	for _, ns := range []string{progressNamespace, progressScopedA} {
		_, err := svc.GetRebuildProgress(ctx, &GetRebuildProgressRequest{Namespace: ns})
		requireProgressNotFound(t, err)
	}
}

func TestGetRebuildProgress_OtherTenantCannotReadByJobID(t *testing.T) {
	svc := newProgressTenantService(t)
	ctx := tenantctx.WithTenant(context.Background(), progressTenantB)

	_, err := svc.GetRebuildProgress(ctx, &GetRebuildProgressRequest{JobID: progressRebuildJobID})
	requireProgressNotFound(t, err)
}

func TestGetRebuildProgress_TenantReadsOwnJobByID(t *testing.T) {
	svc := newProgressTenantService(t)
	ctx := tenantctx.WithTenant(context.Background(), progressTenantA)

	resp, err := svc.GetRebuildProgress(ctx, &GetRebuildProgressRequest{JobID: progressRebuildJobID})
	require.NoError(t, err)
	assert.Equal(t, progressRebuildJobID, resp.JobID)
	assert.Equal(t, progressNamespace, resp.Namespace)
}

func TestGetCompactionProgress_TenantFindsOwnJobByNamespace(t *testing.T) {
	svc := newProgressTenantService(t)
	ctx := tenantctx.WithTenant(context.Background(), progressTenantA)

	resp, err := svc.GetCompactionProgress(ctx, &GetCompactionProgressRequest{Namespace: progressNamespace})
	require.NoError(t, err)
	assert.Equal(t, progressCompactionOpID, resp.OperationID)
	assert.Equal(t, progressNamespace, resp.Namespace)
}

func TestGetCompactionProgress_OtherTenantCannotReadByNamespace(t *testing.T) {
	svc := newProgressTenantService(t)
	ctx := tenantctx.WithTenant(context.Background(), progressTenantB)

	for _, ns := range []string{progressNamespace, progressScopedA} {
		_, err := svc.GetCompactionProgress(ctx, &GetCompactionProgressRequest{Namespace: ns})
		requireProgressNotFound(t, err)
	}
}

func TestGetCompactionProgress_OtherTenantCannotReadByOperationID(t *testing.T) {
	svc := newProgressTenantService(t)
	ctx := tenantctx.WithTenant(context.Background(), progressTenantB)

	_, err := svc.GetCompactionProgress(ctx, &GetCompactionProgressRequest{OperationID: progressCompactionOpID})
	requireProgressNotFound(t, err)
}

func TestGetCompactionProgress_TenantReadsOwnJobByOperationID(t *testing.T) {
	svc := newProgressTenantService(t)
	ctx := tenantctx.WithTenant(context.Background(), progressTenantA)

	resp, err := svc.GetCompactionProgress(ctx, &GetCompactionProgressRequest{OperationID: progressCompactionOpID})
	require.NoError(t, err)
	assert.Equal(t, progressCompactionOpID, resp.OperationID)
	assert.Equal(t, progressNamespace, resp.Namespace)
}
