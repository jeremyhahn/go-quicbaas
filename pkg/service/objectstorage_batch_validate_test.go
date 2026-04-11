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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// BatchPutObjectsRequest.Validate Tests
// =============================================================================

func TestBatchPutObjectsRequest_Validate_NilRequest(t *testing.T) {
	var req *BatchPutObjectsRequest
	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request cannot be nil")

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
}

func TestBatchPutObjectsRequest_Validate_EmptyObjects(t *testing.T) {
	req := &BatchPutObjectsRequest{
		Objects: []*PutObjectRequest{},
	}
	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "objects cannot be empty")
}

func TestBatchPutObjectsRequest_Validate_Success(t *testing.T) {
	req := &BatchPutObjectsRequest{
		Objects: []*PutObjectRequest{
			{Key: "test-key", Value: []byte("test-value")},
		},
	}
	err := req.Validate()
	require.NoError(t, err)
}

func TestBatchPutObjectsRequest_Validate_MultipleObjects(t *testing.T) {
	req := &BatchPutObjectsRequest{
		Objects: []*PutObjectRequest{
			{Key: "key-1", Value: []byte("val-1")},
			{Key: "key-2", Value: []byte("val-2")},
			{Key: "key-3", Value: []byte("val-3")},
		},
		TenantID:        "tenant-1",
		ContinueOnError: true,
	}
	err := req.Validate()
	require.NoError(t, err)
}

// =============================================================================
// BatchGetObjectsRequest.Validate Tests
// =============================================================================

func TestBatchGetObjectsRequest_Validate_NilRequest(t *testing.T) {
	var req *BatchGetObjectsRequest
	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request cannot be nil")

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
}

func TestBatchGetObjectsRequest_Validate_EmptyKeys(t *testing.T) {
	req := &BatchGetObjectsRequest{
		Keys: []string{},
	}
	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keys cannot be empty")
}

func TestBatchGetObjectsRequest_Validate_Success(t *testing.T) {
	req := &BatchGetObjectsRequest{
		Keys: []string{"key1", "key2"},
	}
	err := req.Validate()
	require.NoError(t, err)
}

func TestBatchGetObjectsRequest_Validate_WithOptions(t *testing.T) {
	req := &BatchGetObjectsRequest{
		Keys:            []string{"key1"},
		TenantID:        "tenant-1",
		IncludeMetadata: true,
	}
	err := req.Validate()
	require.NoError(t, err)
}

// =============================================================================
// BatchDeleteObjectsRequest.Validate Tests
// =============================================================================

func TestBatchDeleteObjectsRequest_Validate_NilRequest(t *testing.T) {
	var req *BatchDeleteObjectsRequest
	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request cannot be nil")

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
}

func TestBatchDeleteObjectsRequest_Validate_EmptyKeys(t *testing.T) {
	req := &BatchDeleteObjectsRequest{
		Keys: []string{},
	}
	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keys cannot be empty")
}

func TestBatchDeleteObjectsRequest_Validate_Success(t *testing.T) {
	req := &BatchDeleteObjectsRequest{
		Keys: []string{"key1", "key2"},
	}
	err := req.Validate()
	require.NoError(t, err)
}

func TestBatchDeleteObjectsRequest_Validate_WithOptions(t *testing.T) {
	req := &BatchDeleteObjectsRequest{
		Keys:            []string{"key1"},
		TenantID:        "tenant-1",
		ContinueOnError: true,
	}
	err := req.Validate()
	require.NoError(t, err)
}
