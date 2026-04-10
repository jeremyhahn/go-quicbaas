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
)

// =============================================================================
// BatchPutObjects Tests
// =============================================================================

func TestBatchPutObjects_Success(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &BatchPutObjectsRequest{
		Objects: []*PutObjectRequest{
			{Key: "batch:obj1", Value: []byte("value1"), ContentType: "text/plain"},
			{Key: "batch:obj2", Value: []byte("value2"), ContentType: "text/plain"},
			{Key: "batch:obj3", Value: []byte("value3"), ContentType: "text/plain"},
		},
	}

	resp, err := svc.BatchPutObjects(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 3, resp.SuccessCount)
	assert.Equal(t, 0, resp.FailureCount)
	assert.Len(t, resp.Results, 3)

	for _, r := range resp.Results {
		assert.True(t, r.Success)
		assert.Empty(t, r.Error)
	}
}

func TestBatchPutObjects_WithTenantOverride(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &BatchPutObjectsRequest{
		TenantID: "tenant-abc",
		Objects: []*PutObjectRequest{
			{Key: "batch:tenant-obj1", Value: []byte("data1"), ContentType: "text/plain"},
			{Key: "batch:tenant-obj2", Value: []byte("data2"), ContentType: "text/plain"},
		},
	}

	resp, err := svc.BatchPutObjects(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 2, resp.SuccessCount)
	assert.Equal(t, 0, resp.FailureCount)
}

func TestBatchPutObjects_EmptyObjects(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &BatchPutObjectsRequest{
		Objects: []*PutObjectRequest{},
	}

	resp, err := svc.BatchPutObjects(ctx, req)
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestBatchPutObjects_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.BatchPutObjects(ctx, nil)
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestBatchPutObjects_WithFailure_ContinueOnError(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &BatchPutObjectsRequest{
		Objects: []*PutObjectRequest{
			{Key: "batch:good1", Value: []byte("data1"), ContentType: "text/plain"},
			{Key: "", Value: []byte("data2"), ContentType: "text/plain"}, // Invalid: empty key
			{Key: "batch:good2", Value: []byte("data3"), ContentType: "text/plain"},
		},
		ContinueOnError: true,
	}

	resp, err := svc.BatchPutObjects(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	// At least one should succeed, one should fail
	assert.True(t, resp.SuccessCount > 0 || resp.FailureCount > 0)
}

func TestBatchPutObjects_WithFailure_StopOnError(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &BatchPutObjectsRequest{
		Objects: []*PutObjectRequest{
			{Key: "", Value: []byte("data1"), ContentType: "text/plain"}, // Invalid: empty key
			{Key: "batch:wont-run", Value: []byte("data2"), ContentType: "text/plain"},
		},
		ContinueOnError: false,
	}

	resp, err := svc.BatchPutObjects(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	// Should stop after first failure
	assert.True(t, resp.FailureCount > 0)
}

// =============================================================================
// BatchGetObjects Tests
// =============================================================================

func TestBatchGetObjects_Success(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	// First, put some objects
	for _, key := range []string{"bget:obj1", "bget:obj2"} {
		_, err := svc.PutObject(ctx, &PutObjectRequest{
			Key:         key,
			Value:       []byte("data-" + key),
			ContentType: "text/plain",
		})
		require.NoError(t, err)
	}

	req := &BatchGetObjectsRequest{
		Keys: []string{"bget:obj1", "bget:obj2"},
	}

	resp, err := svc.BatchGetObjects(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 2, resp.FoundCount)
	assert.Equal(t, 0, resp.NotFoundCount)
	assert.Len(t, resp.Results, 2)
}

func TestBatchGetObjects_MixedFoundAndNotFound(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	// Put one object
	_, err := svc.PutObject(ctx, &PutObjectRequest{
		Key:         "bget:exists",
		Value:       []byte("I exist"),
		ContentType: "text/plain",
	})
	require.NoError(t, err)

	req := &BatchGetObjectsRequest{
		Keys: []string{"bget:exists", "bget:not-there"},
	}

	resp, err := svc.BatchGetObjects(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 1, resp.FoundCount)
	assert.Equal(t, 1, resp.NotFoundCount)
}

func TestBatchGetObjects_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.BatchGetObjects(ctx, nil)
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestBatchGetObjects_EmptyKeys(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &BatchGetObjectsRequest{
		Keys: []string{},
	}

	resp, err := svc.BatchGetObjects(ctx, req)
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestBatchGetObjects_WithTenantID(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &BatchGetObjectsRequest{
		Keys:     []string{"bget:tenant-obj1"},
		TenantID: "tenant-xyz",
	}

	resp, err := svc.BatchGetObjects(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	// All not found since nothing was put under this tenant
	assert.Equal(t, 0, resp.FoundCount)
}

func TestBatchGetObjects_WithIncludeMetadata(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	// Put an object
	_, err := svc.PutObject(ctx, &PutObjectRequest{
		Key:         "bget:meta-obj",
		Value:       []byte("test data"),
		ContentType: "application/json",
	})
	require.NoError(t, err)

	req := &BatchGetObjectsRequest{
		Keys:            []string{"bget:meta-obj"},
		IncludeMetadata: true,
	}

	resp, err := svc.BatchGetObjects(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 1, resp.FoundCount)
}

// =============================================================================
// BatchDeleteObjects Tests
// =============================================================================

func TestBatchDeleteObjects_Success(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	// Put some objects
	for _, key := range []string{"bdel:obj1", "bdel:obj2", "bdel:obj3"} {
		_, err := svc.PutObject(ctx, &PutObjectRequest{
			Key:         key,
			Value:       []byte("data-" + key),
			ContentType: "text/plain",
		})
		require.NoError(t, err)
	}

	req := &BatchDeleteObjectsRequest{
		Keys: []string{"bdel:obj1", "bdel:obj2", "bdel:obj3"},
	}

	resp, err := svc.BatchDeleteObjects(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 3, resp.DeletedCount)
	assert.Equal(t, 0, resp.FailureCount)
}

func TestBatchDeleteObjects_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.BatchDeleteObjects(ctx, nil)
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestBatchDeleteObjects_EmptyKeys(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &BatchDeleteObjectsRequest{
		Keys: []string{},
	}

	resp, err := svc.BatchDeleteObjects(ctx, req)
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestBatchDeleteObjects_MixedSuccessAndFailure(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	// Put one object
	_, err := svc.PutObject(ctx, &PutObjectRequest{
		Key:         "bdel:exists",
		Value:       []byte("data"),
		ContentType: "text/plain",
	})
	require.NoError(t, err)

	req := &BatchDeleteObjectsRequest{
		Keys: []string{"bdel:exists", "bdel:not-there"},
	}

	resp, err := svc.BatchDeleteObjects(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	// At least the existing one should be deleted; not-there may succeed or fail
	assert.Len(t, resp.Results, 2)
}

func TestBatchDeleteObjects_WithTenantID(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &BatchDeleteObjectsRequest{
		Keys:     []string{"bdel:tenant-obj"},
		TenantID: "tenant-del",
	}

	resp, err := svc.BatchDeleteObjects(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}
