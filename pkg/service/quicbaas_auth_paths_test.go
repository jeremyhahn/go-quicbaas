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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// authTestService creates a quicBaaS service with auth enabled for testing
// auth-related error paths on functions that have checkAuth calls.
func authTestService() *quicBaaS {
	svc := &quicBaaS{
		config: &ServiceConfig{
			EnableAuth:    true,
			EnableMetrics: false,
		},
		raftNode:         &mockRaftNode{},
		authManager:      &mockAuthManager{},
		keyspaceResolver: NewKeyspaceResolver(),
	}
	svc.kvService = &kvService{svc: svc}
	svc.batchService = &batchService{svc: svc}
	svc.transactionService = &transactionService{svc: svc}
	svc.queryService = &queryService{svc: svc}
	return svc
}

// authTestContext creates a context with a Bearer token in gRPC metadata.
func authTestContext(token string) context.Context {
	md := metadata.New(map[string]string{
		"authorization": "Bearer " + token,
	})
	return metadata.NewIncomingContext(context.Background(), md)
}

// noTokenContext creates a context with gRPC metadata but no auth token.
func noTokenContext() context.Context {
	md := metadata.New(map[string]string{})
	return metadata.NewIncomingContext(context.Background(), md)
}

// =============================================================================
// Put - Auth checks (line 658-666)
// =============================================================================

func TestPut_AuthEnabled_NoToken(t *testing.T) {
	svc := authTestService()
	resp, err := svc.kvService.Put(noTokenContext(), &PutRequest{EntityType: "users", ID: 1, Data: []byte(`{}`)})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnauthorized, qbErr.Code)
}

// =============================================================================
// Get - Auth checks (line 743-751)
// =============================================================================

func TestGet_AuthEnabled_NoToken(t *testing.T) {
	svc := authTestService()
	resp, err := svc.kvService.Get(noTokenContext(), &GetRequest{EntityType: "users", ID: 1})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnauthorized, qbErr.Code)
}

// =============================================================================
// Delete - Auth checks (line 867-875)
// =============================================================================

func TestDelete_AuthEnabled_NoToken(t *testing.T) {
	svc := authTestService()
	resp, err := svc.kvService.Delete(noTokenContext(), &DeleteRequest{EntityType: "users", ID: 1})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnauthorized, qbErr.Code)
}

func TestDelete_AuthEnabled_AuthorizationFailure(t *testing.T) {
	svc := authTestService()
	svc.authManager = &mockAuthManager{
		authorizeFn: func(ctx context.Context, token, resource, action string) error {
			return assert.AnError
		},
	}
	ctx := authTestContext("valid-token")
	resp, err := svc.kvService.Delete(ctx, &DeleteRequest{EntityType: "users", ID: 1})
	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// List - Auth checks (line 943-951)
// =============================================================================

func TestList_AuthEnabled_NoToken(t *testing.T) {
	svc := authTestService()
	ch := make(chan *ListResponse, 1)
	err := svc.kvService.List(noTokenContext(), &ListRequest{EntityType: "users"}, ch)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnauthorized, qbErr.Code)
}

// =============================================================================
// ListEntityTypes - Auth checks (line 1243-1251)
// =============================================================================

func TestListEntityTypes_AuthEnabled_NoToken(t *testing.T) {
	svc := authTestService()
	resp, err := svc.kvService.ListEntityTypes(noTokenContext(), &ListEntityTypesRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnauthorized, qbErr.Code)
}

// =============================================================================
// BatchPut - Auth checks (line 1398-1405)
// =============================================================================

func TestBatchPut_AuthEnabled_NoToken(t *testing.T) {
	svc := authTestService()
	resp, err := svc.batchService.BatchPut(noTokenContext(), &BatchPutRequest{
		Requests: []*PutRequest{{EntityType: "users", ID: 1, Data: []byte(`{}`)}},
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnauthorized, qbErr.Code)
}

// =============================================================================
// BatchDelete - Auth checks (line 1504-1511)
// =============================================================================

func TestBatchDelete_AuthEnabled_NoToken(t *testing.T) {
	svc := authTestService()
	resp, err := svc.batchService.BatchDelete(noTokenContext(), &BatchDeleteRequest{
		Requests: []*DeleteRequest{{EntityType: "users", ID: 1}},
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnauthorized, qbErr.Code)
}

func TestBatchDelete_AuthEnabled_AuthorizationFailure(t *testing.T) {
	svc := authTestService()
	svc.authManager = &mockAuthManager{
		authorizeFn: func(ctx context.Context, token, resource, action string) error {
			return assert.AnError
		},
	}
	ctx := authTestContext("valid-token")
	resp, err := svc.batchService.BatchDelete(ctx, &BatchDeleteRequest{
		Requests: []*DeleteRequest{{EntityType: "users", ID: 1}},
	})
	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// BatchGet - Auth checks (line 1573-1580)
// =============================================================================

func TestBatchGet_AuthEnabled_NoToken(t *testing.T) {
	svc := authTestService()
	resp, err := svc.batchService.BatchGet(noTokenContext(), &BatchGetRequest{
		Requests: []*GetRequest{{EntityType: "users", ID: 1}},
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnauthorized, qbErr.Code)
}

// =============================================================================
// Transaction - Auth checks (line 1671-1678)
// =============================================================================

func TestTransaction_AuthEnabled_NoToken(t *testing.T) {
	svc := authTestService()
	resp, err := svc.transactionService.Transaction(noTokenContext(), &TransactionRequest{
		Compare: []*CompareCondition{
			{EntityType: "users", ID: 1, Target: CompareTargetVersion, Operator: CompareOperatorEqual, Version: 1},
		},
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnauthorized, qbErr.Code)
}

// =============================================================================
// Query - Auth checks (line 1935-1942)
// =============================================================================

func TestQuery_AuthEnabled_NoToken(t *testing.T) {
	svc := authTestService()
	ch := make(chan *QueryResponse, 1) // bidirectional for defer close(stream)
	err := svc.queryService.Query(noTokenContext(), &QueryRequest{EntityType: "users"}, ch)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnauthorized, qbErr.Code)
}
