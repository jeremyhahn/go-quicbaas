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
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func newBatchTenantTestService(t *testing.T) *batchService {
	t.Helper()
	svc := &quicBaaS{logger: createTestLogger(t), config: &ServiceConfig{}, keyspaceResolver: NewKeyspaceResolver()}
	return &batchService{svc: svc}
}

func requireForbiddenMissingTenant(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	require.Equal(t, errors.ErrForbidden, qbErr.Code)
	var missing *tenantctx.MissingTenantError
	require.ErrorAs(t, err, &missing)
}

func TestBatchPut_NoTenantIsForbidden(t *testing.T) {
	bs := newBatchTenantTestService(t)
	resp, err := bs.BatchPut(context.Background(), &BatchPutRequest{
		Requests: []*PutRequest{{EntityType: "user", ID: 1, Data: []byte("{}")}},
	})
	require.Nil(t, resp)
	requireForbiddenMissingTenant(t, err)
}

func TestBatchDelete_NoTenantIsForbidden(t *testing.T) {
	bs := newBatchTenantTestService(t)
	resp, err := bs.BatchDelete(context.Background(), &BatchDeleteRequest{
		Requests: []*DeleteRequest{{EntityType: "user", ID: 1}},
	})
	require.Nil(t, resp)
	requireForbiddenMissingTenant(t, err)
}

func TestBatchPut_ReservedEntityTypeStaysInvalidInput(t *testing.T) {
	bs := newBatchTenantTestService(t)
	resp, err := bs.BatchPut(tenantctx.WithSystem(context.Background()), &BatchPutRequest{
		Requests: []*PutRequest{{EntityType: "__reserved", ID: 1, Data: []byte("{}")}},
	})
	require.Nil(t, resp)
	require.Error(t, err)
	qbErr, ok := err.(*errors.QBError)
	require.True(t, ok, "outermost error is %T, want *errors.QBError", err)
	require.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	require.Contains(t, qbErr.Error(), "validate entity type __reserved")
}
