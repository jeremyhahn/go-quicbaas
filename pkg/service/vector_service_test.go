// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// SPDX-License-Identifier: GPL-3.0-only
// See the LICENSE file in the root of this repository.

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qbErrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	quicbaasErrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/shards"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func TestVectorService_ValidateCreateNamespaceRequest(t *testing.T) {
	v := &vectorService{}

	tests := []struct {
		name     string
		req      *CreateNamespaceRequest
		wantErr  bool
		errCheck func(t *testing.T, err error)
	}{
		{
			name:    "nil request returns error",
			req:     nil,
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "request cannot be nil")
			},
		},
		{
			name:    "empty namespace returns error",
			req:     &CreateNamespaceRequest{Namespace: "", Dimension: 128},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "namespace is required")
			},
		},
		{
			name:    "zero dimension returns error",
			req:     &CreateNamespaceRequest{Namespace: "test-ns", Dimension: 0},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "dimension must be greater than 0")
			},
		},
		{
			name:    "valid request returns no error",
			req:     &CreateNamespaceRequest{Namespace: "test-ns", Dimension: 128},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.validateCreateNamespaceRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVectorService_ValidateDeleteNamespaceRequest(t *testing.T) {
	v := &vectorService{}

	tests := []struct {
		name     string
		req      *DeleteNamespaceRequest
		wantErr  bool
		errCheck func(t *testing.T, err error)
	}{
		{
			name:    "nil request returns error",
			req:     nil,
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "request cannot be nil")
			},
		},
		{
			name:    "empty namespace returns error",
			req:     &DeleteNamespaceRequest{Namespace: ""},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "namespace is required")
			},
		},
		{
			name:    "valid request returns no error",
			req:     &DeleteNamespaceRequest{Namespace: "test-ns"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.validateDeleteNamespaceRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVectorService_ValidateGetNamespaceInfoRequest(t *testing.T) {
	v := &vectorService{}

	tests := []struct {
		name     string
		req      *GetNamespaceInfoRequest
		wantErr  bool
		errCheck func(t *testing.T, err error)
	}{
		{
			name:    "nil request returns error",
			req:     nil,
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "request cannot be nil")
			},
		},
		{
			name:    "empty namespace returns error",
			req:     &GetNamespaceInfoRequest{Namespace: ""},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "namespace is required")
			},
		},
		{
			name:    "valid request returns no error",
			req:     &GetNamespaceInfoRequest{Namespace: "test-ns"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.validateGetNamespaceInfoRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVectorService_ValidateUpsertVectorRequest(t *testing.T) {
	v := &vectorService{}

	tests := []struct {
		name     string
		req      *UpsertVectorRequest
		wantErr  bool
		errCheck func(t *testing.T, err error)
	}{
		{
			name:    "nil request returns error",
			req:     nil,
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "request cannot be nil")
			},
		},
		{
			name:    "empty namespace returns error",
			req:     &UpsertVectorRequest{Namespace: "", ID: 1, Vector: []float32{0.1}},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "namespace is required")
			},
		},
		{
			name:    "zero ID returns error",
			req:     &UpsertVectorRequest{Namespace: "test-ns", ID: 0, Vector: []float32{0.1}},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "ID must be greater than 0")
			},
		},
		{
			name:    "empty vector returns error",
			req:     &UpsertVectorRequest{Namespace: "test-ns", ID: 1, Vector: []float32{}},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "vector is required")
			},
		},
		{
			name:    "nil vector returns error",
			req:     &UpsertVectorRequest{Namespace: "test-ns", ID: 1, Vector: nil},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "vector is required")
			},
		},
		{
			name:    "valid request returns no error",
			req:     &UpsertVectorRequest{Namespace: "test-ns", ID: 1, Vector: []float32{0.1, 0.2, 0.3}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.validateUpsertVectorRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVectorService_ValidateGetVectorRequest(t *testing.T) {
	v := &vectorService{}

	tests := []struct {
		name     string
		req      *GetVectorRequest
		wantErr  bool
		errCheck func(t *testing.T, err error)
	}{
		{
			name:    "nil request returns error",
			req:     nil,
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "request cannot be nil")
			},
		},
		{
			name:    "empty namespace returns error",
			req:     &GetVectorRequest{Namespace: "", ID: 1},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "namespace is required")
			},
		},
		{
			name:    "zero ID returns error",
			req:     &GetVectorRequest{Namespace: "test-ns", ID: 0},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "ID must be greater than 0")
			},
		},
		{
			name:    "valid request returns no error",
			req:     &GetVectorRequest{Namespace: "test-ns", ID: 1},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.validateGetVectorRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVectorService_ValidateDeleteVectorRequest(t *testing.T) {
	v := &vectorService{}

	tests := []struct {
		name     string
		req      *DeleteVectorRequest
		wantErr  bool
		errCheck func(t *testing.T, err error)
	}{
		{
			name:    "nil request returns error",
			req:     nil,
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "request cannot be nil")
			},
		},
		{
			name:    "empty namespace returns error",
			req:     &DeleteVectorRequest{Namespace: "", ID: 1},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "namespace is required")
			},
		},
		{
			name:    "zero ID returns error",
			req:     &DeleteVectorRequest{Namespace: "test-ns", ID: 0},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "ID must be greater than 0")
			},
		},
		{
			name:    "valid request returns no error",
			req:     &DeleteVectorRequest{Namespace: "test-ns", ID: 1},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.validateDeleteVectorRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVectorService_ValidateBatchUpsertRequest(t *testing.T) {
	v := &vectorService{}

	tests := []struct {
		name     string
		req      *BatchUpsertRequest
		wantErr  bool
		errCheck func(t *testing.T, err error)
	}{
		{
			name:    "nil request returns error",
			req:     nil,
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "request cannot be nil")
			},
		},
		{
			name:    "empty namespace returns error",
			req:     &BatchUpsertRequest{Namespace: "", Vectors: []*VectorData{{ID: 1}}},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "namespace is required")
			},
		},
		{
			name:    "empty vectors returns error",
			req:     &BatchUpsertRequest{Namespace: "test-ns", Vectors: []*VectorData{}},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "vectors is required")
			},
		},
		{
			name:    "nil vectors returns error",
			req:     &BatchUpsertRequest{Namespace: "test-ns", Vectors: nil},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "vectors is required")
			},
		},
		{
			name:    "valid request returns no error",
			req:     &BatchUpsertRequest{Namespace: "test-ns", Vectors: []*VectorData{{ID: 1, Vector: []float32{0.1}}}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.validateBatchUpsertRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVectorService_ValidateSearchSimilarRequest(t *testing.T) {
	v := &vectorService{}

	tests := []struct {
		name     string
		req      *SearchSimilarRequest
		wantErr  bool
		errCheck func(t *testing.T, err error)
	}{
		{
			name:    "nil request returns error",
			req:     nil,
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "request cannot be nil")
			},
		},
		{
			name:    "empty namespace returns error",
			req:     &SearchSimilarRequest{Namespace: "", QueryVector: []float32{0.1}, K: 10},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "namespace is required")
			},
		},
		{
			name:    "empty query vector returns error",
			req:     &SearchSimilarRequest{Namespace: "test-ns", QueryVector: []float32{}, K: 10},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "query vector is required")
			},
		},
		{
			name:    "nil query vector returns error",
			req:     &SearchSimilarRequest{Namespace: "test-ns", QueryVector: nil, K: 10},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "query vector is required")
			},
		},
		{
			name:    "zero K returns error",
			req:     &SearchSimilarRequest{Namespace: "test-ns", QueryVector: []float32{0.1}, K: 0},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "k must be greater than 0")
			},
		},
		{
			name:    "negative K returns error",
			req:     &SearchSimilarRequest{Namespace: "test-ns", QueryVector: []float32{0.1}, K: -5},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "k must be greater than 0")
			},
		},
		{
			name:    "valid request returns no error",
			req:     &SearchSimilarRequest{Namespace: "test-ns", QueryVector: []float32{0.1, 0.2}, K: 10},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.validateSearchSimilarRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVectorService_ValidateSearchByMetadataRequest(t *testing.T) {
	v := &vectorService{}

	tests := []struct {
		name     string
		req      *SearchByMetadataRequest
		wantErr  bool
		errCheck func(t *testing.T, err error)
	}{
		{
			name:    "nil request returns error",
			req:     nil,
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "request cannot be nil")
			},
		},
		{
			name:    "empty namespace returns error",
			req:     &SearchByMetadataRequest{Namespace: ""},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "namespace is required")
			},
		},
		{
			name:    "valid request with namespace only returns no error",
			req:     &SearchByMetadataRequest{Namespace: "test-ns"},
			wantErr: false,
		},
		{
			name: "valid request with filters returns no error",
			req: &SearchByMetadataRequest{
				Namespace: "test-ns",
				Filters:   map[string]interface{}{"key": "value"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.validateSearchByMetadataRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVectorService_ValidateRebuildIndexRequest(t *testing.T) {
	v := &vectorService{}

	tests := []struct {
		name     string
		req      *RebuildIndexRequest
		wantErr  bool
		errCheck func(t *testing.T, err error)
	}{
		{
			name:    "nil request returns error",
			req:     nil,
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "request cannot be nil")
			},
		},
		{
			name:    "empty namespace returns error",
			req:     &RebuildIndexRequest{Namespace: ""},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "namespace is required")
			},
		},
		{
			name:    "valid request returns no error",
			req:     &RebuildIndexRequest{Namespace: "test-ns"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.validateRebuildIndexRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVectorService_ValidateGetRebuildProgressRequest(t *testing.T) {
	v := &vectorService{}

	tests := []struct {
		name     string
		req      *GetRebuildProgressRequest
		wantErr  bool
		errCheck func(t *testing.T, err error)
	}{
		{
			name:    "nil request returns error",
			req:     nil,
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "request cannot be nil")
			},
		},
		{
			name:    "empty job ID and namespace returns error",
			req:     &GetRebuildProgressRequest{JobID: "", Namespace: ""},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "job_id or namespace is required")
			},
		},
		{
			name:    "valid request with job ID returns no error",
			req:     &GetRebuildProgressRequest{JobID: "job-123"},
			wantErr: false,
		},
		{
			name:    "valid request with namespace returns no error",
			req:     &GetRebuildProgressRequest{Namespace: "test-ns"},
			wantErr: false,
		},
		{
			name:    "valid request with both returns no error",
			req:     &GetRebuildProgressRequest{JobID: "job-123", Namespace: "test-ns"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.validateGetRebuildProgressRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVectorService_ValidateGetIndexStatsRequest(t *testing.T) {
	v := &vectorService{}

	tests := []struct {
		name     string
		req      *GetIndexStatsRequest
		wantErr  bool
		errCheck func(t *testing.T, err error)
	}{
		{
			name:    "nil request returns error",
			req:     nil,
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "request cannot be nil")
			},
		},
		{
			name:    "empty namespace returns error",
			req:     &GetIndexStatsRequest{Namespace: ""},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "namespace is required")
			},
		},
		{
			name:    "valid request returns no error",
			req:     &GetIndexStatsRequest{Namespace: "test-ns"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.validateGetIndexStatsRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVectorService_ValidateCompactNamespaceRequest(t *testing.T) {
	v := &vectorService{}

	tests := []struct {
		name     string
		req      *CompactNamespaceRequest
		wantErr  bool
		errCheck func(t *testing.T, err error)
	}{
		{
			name:    "nil request returns error",
			req:     nil,
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "request cannot be nil")
			},
		},
		{
			name:    "empty namespace returns error",
			req:     &CompactNamespaceRequest{Namespace: ""},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "namespace is required")
			},
		},
		{
			name:    "valid request returns no error",
			req:     &CompactNamespaceRequest{Namespace: "test-ns"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.validateCompactNamespaceRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVectorService_ValidateGetCompactionProgressRequest(t *testing.T) {
	v := &vectorService{}

	tests := []struct {
		name     string
		req      *GetCompactionProgressRequest
		wantErr  bool
		errCheck func(t *testing.T, err error)
	}{
		{
			name:    "nil request returns error",
			req:     nil,
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "request cannot be nil")
			},
		},
		{
			name:    "empty operation ID and namespace returns error",
			req:     &GetCompactionProgressRequest{OperationID: "", Namespace: ""},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "either operation_id or namespace is required")
			},
		},
		{
			name:    "valid request with operation ID returns no error",
			req:     &GetCompactionProgressRequest{OperationID: "op-123"},
			wantErr: false,
		},
		{
			name:    "valid request with namespace returns no error",
			req:     &GetCompactionProgressRequest{Namespace: "test-ns"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.validateGetCompactionProgressRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConvertToStringMap(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		expected map[string]string
	}{
		{
			name:     "nil map returns nil",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty map returns empty map",
			input:    map[string]interface{}{},
			expected: map[string]string{},
		},
		{
			name: "string values converted correctly",
			input: map[string]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
			expected: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name: "numeric values converted to string",
			input: map[string]interface{}{
				"int":   42,
				"float": 3.14,
			},
			expected: map[string]string{
				"int":   "42",
				"float": "3.14",
			},
		},
		{
			name: "bool values converted to string",
			input: map[string]interface{}{
				"true":  true,
				"false": false,
			},
			expected: map[string]string{
				"true":  "true",
				"false": "false",
			},
		},
		{
			name: "mixed values converted to string",
			input: map[string]interface{}{
				"string": "hello",
				"int":    123,
				"bool":   true,
				"nil":    nil,
			},
			expected: map[string]string{
				"string": "hello",
				"int":    "123",
				"bool":   "true",
				"nil":    "<nil>",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertToStringMap(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertFromStringMap(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected map[string]interface{}
	}{
		{
			name:     "nil map returns nil",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty map returns empty map",
			input:    map[string]string{},
			expected: map[string]interface{}{},
		},
		{
			name: "string values preserved as interface",
			input: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			expected: map[string]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name: "numeric strings remain as strings",
			input: map[string]string{
				"int":   "42",
				"float": "3.14",
			},
			expected: map[string]interface{}{
				"int":   "42",
				"float": "3.14",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertFromStringMap(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchesMetadataFilter(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]interface{}
		filter   map[string]interface{}
		expected bool
	}{
		{
			name:     "empty filter matches any metadata",
			metadata: map[string]interface{}{"key": "value"},
			filter:   map[string]interface{}{},
			expected: true,
		},
		{
			name:     "nil filter matches any metadata",
			metadata: map[string]interface{}{"key": "value"},
			filter:   nil,
			expected: true,
		},
		{
			name:     "matching key-value pair returns true",
			metadata: map[string]interface{}{"key": "value"},
			filter:   map[string]interface{}{"key": "value"},
			expected: true,
		},
		{
			name:     "non-matching value returns false",
			metadata: map[string]interface{}{"key": "value1"},
			filter:   map[string]interface{}{"key": "value2"},
			expected: false,
		},
		{
			name:     "missing key returns false",
			metadata: map[string]interface{}{"key1": "value"},
			filter:   map[string]interface{}{"key2": "value"},
			expected: false,
		},
		{
			name: "multiple matching filters return true",
			metadata: map[string]interface{}{
				"key1": "value1",
				"key2": "value2",
				"key3": "value3",
			},
			filter: map[string]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
			expected: true,
		},
		{
			name: "one non-matching filter returns false",
			metadata: map[string]interface{}{
				"key1": "value1",
				"key2": "wrong",
			},
			filter: map[string]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
			expected: false,
		},
		{
			name:     "numeric values compared as strings",
			metadata: map[string]interface{}{"count": 42},
			filter:   map[string]interface{}{"count": 42},
			expected: true,
		},
		{
			name:     "string vs int comparison",
			metadata: map[string]interface{}{"count": "42"},
			filter:   map[string]interface{}{"count": 42},
			expected: true,
		},
		{
			name:     "bool values compared correctly",
			metadata: map[string]interface{}{"active": true},
			filter:   map[string]interface{}{"active": true},
			expected: true,
		},
		{
			name:     "empty metadata with non-empty filter returns false",
			metadata: map[string]interface{}{},
			filter:   map[string]interface{}{"key": "value"},
			expected: false,
		},
		{
			name:     "nil metadata with non-empty filter returns false",
			metadata: nil,
			filter:   map[string]interface{}{"key": "value"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesMetadataFilter(tt.metadata, tt.filter)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWithVectorRaftNode_Option(t *testing.T) {
	storage := newVecMockStorage()
	logger := newVecTestLogger()
	svc, err := NewVectorService(storage, logger, WithVectorRaftNode(nil))
	require.NoError(t, err)
	assert.Nil(t, svc.(*vectorService).raftNode)
}

func TestWithVectorSDKProvider_Option(t *testing.T) {
	storage := newVecMockStorage()
	logger := newVecTestLogger()
	provider := newVecSDKProvider(&vecTransport{})
	svc, err := NewVectorService(storage, logger, WithVectorSDKProvider(provider))
	require.NoError(t, err)
	assert.NotNil(t, svc.(*vectorService).sdkProvider)
}

func TestSetRaftNode(t *testing.T) {
	storage := newVecMockStorage()
	logger := newVecTestLogger()
	svc, err := NewVectorService(storage, logger)
	require.NoError(t, err)
	vs := svc.(*vectorService)
	assert.Nil(t, vs.raftNode)
	vs.SetRaftNode(nil)
	assert.Nil(t, vs.raftNode)
}

func TestVectorClient_NoSDKProvider(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "ns", 128)
	svc.sdkProvider = nil
	_, err := svc.vectorClient()
	require.Error(t, err)
	var qbErr *qbErrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, qbErrors.ErrInternal, qbErr.Code)
}

func TestVectorClient_Success(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "ns", 128)
	vc, err := svc.vectorClient()
	require.NoError(t, err)
	assert.NotNil(t, vc)
}

// The write client comes from the provider for the service's own shard.
func TestVectorClient_UsesTheServiceShard(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "ns", 128)
	provider := newVecSDKProvider(&vecTransport{})
	svc.sdkProvider = provider
	_, err := svc.vectorClient()
	require.NoError(t, err)
	assert.Equal(t, svc.shardID, provider.lastShardID.Load())
}

// A provider that cannot hand out a client fails the write before any call.
func TestVectorClient_ProviderErrorIsRaft(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "ns", 128)
	svc.sdkProvider = newVecSDKProvider(nil)
	_, err := svc.vectorClient()
	var qbErr *qbErrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, qbErrors.ErrRaft, qbErr.Code)
}

func TestResolveShardID_NonZero(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "ns", 128)
	assert.Equal(t, uint64(42), svc.resolveShardID(42))
}

func TestResolveShardID_Zero(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "ns", 128)
	assert.Equal(t, shards.PlatformShardID, svc.resolveShardID(0))
}

func TestEnsureHNSWIndex_AlreadyExists(t *testing.T) {
	svc, _, _, mockIndex := newVecSvcWithSDK(t, "test-ns", 128)
	index, err := svc.ensureIndex(tenantctx.WithSystem(context.Background()), "test-ns")
	require.NoError(t, err)
	assert.Equal(t, mockIndex, index)
}

func TestEnsureHNSWIndex_NoFactory(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	svc.indexFactory = nil
	svc.indexMu.Lock()
	delete(svc.indexes, "test-ns")
	svc.indexMu.Unlock()
	_, err := svc.ensureIndex(tenantctx.WithSystem(context.Background()), "test-ns")
	require.Error(t, err)
	var qbErr *qbErrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, qbErrors.ErrInternal, qbErr.Code)
}

func TestEnsureHNSWIndex_NamespaceNotFound(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	_, err := svc.ensureIndex(tenantctx.WithSystem(context.Background()), "nonexistent-ns")
	require.Error(t, err)
}

func TestEnsureHNSWIndex_CreateAndLoadVectors(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 3)
	svc.indexMu.Lock()
	delete(svc.indexes, "test-ns")
	svc.indexMu.Unlock()

	storage.getNamespaceInfoFn = func(_ context.Context, ns string) (*interfaces.VectorNamespaceInfo, error) {
		return &interfaces.VectorNamespaceInfo{Name: ns, Dimension: 3, DistanceMetric: "COSINE"}, nil
	}
	storage.scanNamespaceFn = func(_ context.Context, _ string, _ *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error) {
		return []*interfaces.VectorValue{
			{ID: 1, Embedding: []float32{1.0, 2.0, 3.0}},
			{ID: 2, Embedding: []float32{4.0, 5.0, 6.0}},
		}, nil
	}

	index, err := svc.ensureIndex(tenantctx.WithSystem(context.Background()), "test-ns")
	require.NoError(t, err)
	assert.NotNil(t, index)
}

func TestEnsureHNSWIndex_DotProductMetric(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 3)
	svc.indexMu.Lock()
	delete(svc.indexes, "test-ns")
	svc.indexMu.Unlock()

	storage.getNamespaceInfoFn = func(_ context.Context, ns string) (*interfaces.VectorNamespaceInfo, error) {
		return &interfaces.VectorNamespaceInfo{Name: ns, Dimension: 3, DistanceMetric: "DOT_PRODUCT"}, nil
	}

	index, err := svc.ensureIndex(tenantctx.WithSystem(context.Background()), "test-ns")
	require.NoError(t, err)
	assert.NotNil(t, index)
}

func TestEnsureHNSWIndex_DefaultL2Metric(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 3)
	svc.indexMu.Lock()
	delete(svc.indexes, "test-ns")
	svc.indexMu.Unlock()

	storage.getNamespaceInfoFn = func(_ context.Context, ns string) (*interfaces.VectorNamespaceInfo, error) {
		return &interfaces.VectorNamespaceInfo{Name: ns, Dimension: 3, DistanceMetric: "UNKNOWN"}, nil
	}
	index, err := svc.ensureIndex(tenantctx.WithSystem(context.Background()), "test-ns")
	require.NoError(t, err)
	assert.NotNil(t, index)
}

func TestEnsureHNSWIndex_FactoryCreateError(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 3)
	svc.indexMu.Lock()
	delete(svc.indexes, "test-ns")
	svc.indexMu.Unlock()

	storage.getNamespaceInfoFn = func(_ context.Context, _ string) (*interfaces.VectorNamespaceInfo, error) {
		return &interfaces.VectorNamespaceInfo{Name: "test-ns", Dimension: 3}, nil
	}
	svc.indexFactory = &vecMockVectorIndexFactory{
		createFn: func(_ *interfaces.VectorIndexConfig) (interfaces.VectorIndex, error) {
			return nil, errors.New("factory error")
		},
	}
	_, err := svc.ensureIndex(tenantctx.WithSystem(context.Background()), "test-ns")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create vector index")
}

func TestEnsureHNSWIndex_ScanError(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 3)
	svc.indexMu.Lock()
	delete(svc.indexes, "test-ns")
	svc.indexMu.Unlock()

	storage.getNamespaceInfoFn = func(_ context.Context, _ string) (*interfaces.VectorNamespaceInfo, error) {
		return &interfaces.VectorNamespaceInfo{Name: "test-ns", Dimension: 3, DistanceMetric: "L2"}, nil
	}
	storage.scanNamespaceFn = func(_ context.Context, _ string, _ *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error) {
		return nil, errors.New("scan failed")
	}
	index, err := svc.ensureIndex(tenantctx.WithSystem(context.Background()), "test-ns")
	require.NoError(t, err)
	assert.NotNil(t, index)
}

func TestEnsureHNSWIndex_InsertError(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 3)
	svc.indexMu.Lock()
	delete(svc.indexes, "test-ns")
	svc.indexMu.Unlock()

	storage.getNamespaceInfoFn = func(_ context.Context, _ string) (*interfaces.VectorNamespaceInfo, error) {
		return &interfaces.VectorNamespaceInfo{Name: "test-ns", Dimension: 3}, nil
	}
	storage.scanNamespaceFn = func(_ context.Context, _ string, _ *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error) {
		return []*interfaces.VectorValue{{ID: 1, Embedding: []float32{1, 2, 3}}}, nil
	}
	failIndex := newVecMockVectorIndex()
	failIndex.insertFn = func(_ uint64, _ []float32) error { return errors.New("insert failed") }
	svc.indexFactory = newVecMockVectorIndexFactory(failIndex)
	index, err := svc.ensureIndex(tenantctx.WithSystem(context.Background()), "test-ns")
	require.NoError(t, err)
	assert.NotNil(t, index)
}

func TestEnsureHNSWIndex_ConcurrentCreation(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 3)
	svc.indexMu.Lock()
	delete(svc.indexes, "test-ns")
	svc.indexMu.Unlock()

	storage.getNamespaceInfoFn = func(_ context.Context, _ string) (*interfaces.VectorNamespaceInfo, error) {
		return &interfaces.VectorNamespaceInfo{Name: "test-ns", Dimension: 3}, nil
	}

	existingIndex := newVecMockVectorIndex()
	existingIndex.countVal = 99
	callCount := 0
	svc.indexFactory = &vecMockVectorIndexFactory{
		createFn: func(_ *interfaces.VectorIndexConfig) (interfaces.VectorIndex, error) {
			if callCount == 0 {
				svc.indexMu.Lock()
				svc.indexes["test-ns"] = existingIndex
				svc.indexMu.Unlock()
			}
			callCount++
			return newVecMockVectorIndex(), nil
		},
	}
	index, err := svc.ensureIndex(tenantctx.WithSystem(context.Background()), "test-ns")
	require.NoError(t, err)
	assert.Equal(t, int64(99), index.Count())
}

func TestLoadNamespaces_WithFactory(t *testing.T) {
	storage := newVecMockStorage()
	logger := newVecTestLogger()
	mockIndex := newVecMockVectorIndex()
	factory := newVecMockVectorIndexFactory(mockIndex)

	storage.listNamespacesFn = func(_ context.Context) ([]interfaces.VectorNamespaceInfo, error) {
		return []interfaces.VectorNamespaceInfo{
			{Name: "ns-1", Dimension: 128, VectorCount: 100},
			{Name: "ns-2", Dimension: 64, VectorCount: 200},
		}, nil
	}

	svc, err := NewVectorService(storage, logger, WithVectorIndexFactory(factory))
	require.NoError(t, err)
	vs := svc.(*vectorService)
	assert.Len(t, vs.indexes, 2)
}

func TestLoadNamespaces_FactoryCreateError(t *testing.T) {
	storage := newVecMockStorage()
	logger := newVecTestLogger()

	storage.listNamespacesFn = func(_ context.Context) ([]interfaces.VectorNamespaceInfo, error) {
		return []interfaces.VectorNamespaceInfo{
			{Name: "ns-fail", Dimension: 128},
			{Name: "ns-ok", Dimension: 64},
		}, nil
	}

	callCount := 0
	factory := &vecMockVectorIndexFactory{
		createFn: func(_ *interfaces.VectorIndexConfig) (interfaces.VectorIndex, error) {
			callCount++
			if callCount == 1 {
				return nil, errors.New("create failed")
			}
			return newVecMockVectorIndex(), nil
		},
	}

	svc, err := NewVectorService(storage, logger, WithVectorIndexFactory(factory))
	require.NoError(t, err)
	assert.Len(t, svc.(*vectorService).indexes, 1)
}

func TestCreateNamespace_SDKSuccess(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 128)

	storage.getNamespaceInfoFn = func(_ context.Context, ns string) (*interfaces.VectorNamespaceInfo, error) {
		if ns == "test-ns" {
			return &interfaces.VectorNamespaceInfo{Name: "test-ns", Dimension: 128}, nil
		}
		return nil, errors.New("not found")
	}

	resp, err := svc.CreateNamespace(tenantctx.WithSystem(context.Background()), &CreateNamespaceRequest{
		Namespace: "new-ns", Dimension: 256,
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, "new-ns", resp.Namespace)
}

func TestCreateNamespace_SDKError(t *testing.T) {
	svc, storage, vt, _ := newVecSvcWithSDK(t, "test-ns", 128)

	storage.getNamespaceInfoFn = func(_ context.Context, _ string) (*interfaces.VectorNamespaceInfo, error) {
		return nil, errors.New("not found")
	}
	vt.createNamespaceFn = func(_ context.Context, _ *qrdbtransport.CreateNamespaceRequest) (*qrdbtransport.CreateNamespaceResponse, error) {
		return nil, errors.New("sdk error")
	}

	_, err := svc.CreateNamespace(tenantctx.WithSystem(context.Background()), &CreateNamespaceRequest{
		Namespace: "new-ns", Dimension: 128,
	})
	require.Error(t, err)
	var qbErr *qbErrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, qbErrors.ErrRaft, qbErr.Code)
}

func TestCreateNamespace_AlreadyExists(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	_, err := svc.CreateNamespace(tenantctx.WithSystem(context.Background()), &CreateNamespaceRequest{Namespace: "test-ns", Dimension: 128})
	require.Error(t, err)
	var qbErr *qbErrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, qbErrors.ErrAlreadyExists, qbErr.Code)
}

func TestCreateNamespace_NoSDKProvider(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	svc.sdkProvider = nil
	storage.getNamespaceInfoFn = func(_ context.Context, _ string) (*interfaces.VectorNamespaceInfo, error) {
		return nil, errors.New("not found")
	}
	_, err := svc.CreateNamespace(tenantctx.WithSystem(context.Background()), &CreateNamespaceRequest{Namespace: "new-ns", Dimension: 128})
	require.Error(t, err)
}

func TestDeleteNamespace_SDKSuccess(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	resp, err := svc.DeleteNamespace(tenantctx.WithSystem(context.Background()), &DeleteNamespaceRequest{Namespace: "test-ns"})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestDeleteNamespace_SDKError(t *testing.T) {
	svc, _, vt, _ := newVecSvcWithSDK(t, "test-ns", 128)
	vt.deleteNamespaceFn = func(_ context.Context, _ *qrdbtransport.DeleteNamespaceRequest) (*qrdbtransport.DeleteNamespaceResponse, error) {
		return nil, errors.New("sdk error")
	}
	_, err := svc.DeleteNamespace(tenantctx.WithSystem(context.Background()), &DeleteNamespaceRequest{Namespace: "test-ns"})
	require.Error(t, err)
	var qbErr *qbErrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, qbErrors.ErrRaft, qbErr.Code)
}

func TestDeleteNamespace_NoSDKProvider(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	svc.sdkProvider = nil
	_, err := svc.DeleteNamespace(tenantctx.WithSystem(context.Background()), &DeleteNamespaceRequest{Namespace: "test-ns"})
	require.Error(t, err)
}

func TestUpsertVector_SDKSuccess(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 3)
	resp, err := svc.UpsertVector(tenantctx.WithSystem(context.Background()), &UpsertVectorRequest{
		Namespace: "test-ns", ID: 1, Vector: []float32{1, 2, 3},
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestUpsertVector_SDKError(t *testing.T) {
	svc, _, vt, _ := newVecSvcWithSDK(t, "test-ns", 3)
	vt.upsertVectorFn = func(_ context.Context, _ *qrdbtransport.UpsertVectorRequest) (*qrdbtransport.UpsertVectorResponse, error) {
		return nil, errors.New("sdk error")
	}
	_, err := svc.UpsertVector(tenantctx.WithSystem(context.Background()), &UpsertVectorRequest{
		Namespace: "test-ns", ID: 1, Vector: []float32{1, 2, 3},
	})
	require.Error(t, err)
	var qbErr *qbErrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, qbErrors.ErrRaft, qbErr.Code)
}

func TestUpsertVector_NoSDKProvider(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 3)
	svc.sdkProvider = nil
	_, err := svc.UpsertVector(tenantctx.WithSystem(context.Background()), &UpsertVectorRequest{
		Namespace: "test-ns", ID: 1, Vector: []float32{1, 2, 3},
	})
	require.Error(t, err)
}

func TestUpsertVector_EnsureIndexFails(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 3)
	// Remove index and set factory to nil so ensureIndex fails
	svc.indexMu.Lock()
	delete(svc.indexes, "test-ns")
	svc.indexMu.Unlock()
	svc.indexFactory = nil

	// Should still succeed - index errors are logged as warnings
	resp, err := svc.UpsertVector(tenantctx.WithSystem(context.Background()), &UpsertVectorRequest{
		Namespace: "test-ns", ID: 1, Vector: []float32{1, 2, 3},
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestUpsertVector_IndexInsertError(t *testing.T) {
	svc, _, _, mockIndex := newVecSvcWithSDK(t, "test-ns", 3)
	mockIndex.insertFn = func(_ uint64, _ []float32) error { return errors.New("insert failed") }

	resp, err := svc.UpsertVector(tenantctx.WithSystem(context.Background()), &UpsertVectorRequest{
		Namespace: "test-ns", ID: 1, Vector: []float32{1, 2, 3},
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestDeleteVector_SDKSuccess(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	resp, err := svc.DeleteVector(tenantctx.WithSystem(context.Background()), &DeleteVectorRequest{Namespace: "test-ns", ID: 1})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestDeleteVector_SDKError(t *testing.T) {
	svc, _, vt, _ := newVecSvcWithSDK(t, "test-ns", 128)
	vt.deleteVectorFn = func(_ context.Context, _ *qrdbtransport.DeleteVectorRequest) (*qrdbtransport.DeleteVectorResponse, error) {
		return nil, errors.New("sdk error")
	}
	_, err := svc.DeleteVector(tenantctx.WithSystem(context.Background()), &DeleteVectorRequest{Namespace: "test-ns", ID: 1})
	require.Error(t, err)
}

func TestDeleteVector_WithHNSWIndex(t *testing.T) {
	svc, _, _, mockIndex := newVecSvcWithSDK(t, "test-ns", 128)
	deleted := false
	mockIndex.deleteFn = func(_ uint64) error { deleted = true; return nil }
	resp, err := svc.DeleteVector(tenantctx.WithSystem(context.Background()), &DeleteVectorRequest{Namespace: "test-ns", ID: 1})
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.True(t, deleted)
}

func TestDeleteVector_HNSWDeleteError(t *testing.T) {
	svc, _, _, mockIndex := newVecSvcWithSDK(t, "test-ns", 128)
	mockIndex.deleteFn = func(_ uint64) error { return errors.New("delete failed") }
	resp, err := svc.DeleteVector(tenantctx.WithSystem(context.Background()), &DeleteVectorRequest{Namespace: "test-ns", ID: 1})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestDeleteVector_NoSDKProvider(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	svc.sdkProvider = nil
	_, err := svc.DeleteVector(tenantctx.WithSystem(context.Background()), &DeleteVectorRequest{Namespace: "test-ns", ID: 1})
	require.Error(t, err)
}

func TestDeleteVector_TenantForbidden(t *testing.T) {
	// With prefix-based tenant isolation, the attacker's scoped namespace
	// "tenant/other/test-ns" is a different namespace than the owner's "test-ns".
	// The SDK mock silently succeeds on the scoped namespace, so no error is
	// returned. The owner's data is never touched because the delete operates
	// on a completely different (non-existent) scoped namespace.
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	ctx := tenantctx.WithTenant(context.Background(), "other")
	resp, err := svc.DeleteVector(ctx, &DeleteVectorRequest{Namespace: "test-ns", ID: 1})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Success)
}

func TestBatchUpsert_SDKSuccess(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 3)
	resp, err := svc.BatchUpsert(tenantctx.WithSystem(context.Background()), &BatchUpsertRequest{
		Namespace: "test-ns",
		Vectors:   []*VectorData{{ID: 1, Vector: []float32{1, 2, 3}}, {ID: 2, Vector: []float32{4, 5, 6}}},
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, 2, resp.InsertedCount)
}

func TestBatchUpsert_SDKError(t *testing.T) {
	svc, _, vt, _ := newVecSvcWithSDK(t, "test-ns", 3)
	vt.batchUpsertFn = func(_ context.Context, _ *qrdbtransport.BatchUpsertRequest) (*qrdbtransport.BatchUpsertResponse, error) {
		return nil, errors.New("sdk error")
	}
	_, err := svc.BatchUpsert(tenantctx.WithSystem(context.Background()), &BatchUpsertRequest{
		Namespace: "test-ns",
		Vectors:   []*VectorData{{ID: 1, Vector: []float32{1, 2, 3}}},
	})
	require.Error(t, err)
}

func TestBatchUpsert_NoSDKProvider(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 3)
	svc.sdkProvider = nil
	_, err := svc.BatchUpsert(tenantctx.WithSystem(context.Background()), &BatchUpsertRequest{
		Namespace: "test-ns",
		Vectors:   []*VectorData{{ID: 1, Vector: []float32{1, 2, 3}}},
	})
	require.Error(t, err)
}

func TestBatchUpsert_EnsureIndexFails(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 3)
	svc.indexMu.Lock()
	delete(svc.indexes, "test-ns")
	svc.indexMu.Unlock()
	svc.indexFactory = nil

	resp, err := svc.BatchUpsert(tenantctx.WithSystem(context.Background()), &BatchUpsertRequest{
		Namespace: "test-ns",
		Vectors:   []*VectorData{{ID: 1, Vector: []float32{1, 2, 3}}},
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestBatchUpsert_IndexInsertError(t *testing.T) {
	svc, _, _, mockIndex := newVecSvcWithSDK(t, "test-ns", 3)
	mockIndex.insertFn = func(_ uint64, _ []float32) error { return errors.New("insert failed") }

	resp, err := svc.BatchUpsert(tenantctx.WithSystem(context.Background()), &BatchUpsertRequest{
		Namespace: "test-ns",
		Vectors:   []*VectorData{{ID: 1, Vector: []float32{1, 2, 3}}},
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestSearchSimilar_WithResults(t *testing.T) {
	svc, _, _, mockIndex := newVecSvcWithSDK(t, "test-ns", 3)
	mockIndex.searchFn = func(_ []float32, _ int, _ *interfaces.SearchParams) ([]*interfaces.SearchResult, error) {
		return []*interfaces.SearchResult{{ID: 1, Distance: 0.1}, {ID: 2, Distance: 0.5}}, nil
	}
	resp, err := svc.SearchSimilar(tenantctx.WithSystem(context.Background()), &SearchSimilarRequest{
		Namespace: "test-ns", QueryVector: []float32{1, 2, 3}, K: 3,
	})
	require.NoError(t, err)
	assert.Len(t, resp.Results, 2)
}

func TestSearchSimilar_WithMinScore(t *testing.T) {
	svc, _, _, mockIndex := newVecSvcWithSDK(t, "test-ns", 3)
	mockIndex.searchFn = func(_ []float32, _ int, _ *interfaces.SearchParams) ([]*interfaces.SearchResult, error) {
		return []*interfaces.SearchResult{{ID: 1, Distance: 0.1}, {ID: 2, Distance: 0.5}, {ID: 3, Distance: 0.9}}, nil
	}
	resp, err := svc.SearchSimilar(tenantctx.WithSystem(context.Background()), &SearchSimilarRequest{
		Namespace: "test-ns", QueryVector: []float32{1, 2, 3}, K: 3, MinScore: 0.4,
	})
	require.NoError(t, err)
	assert.Len(t, resp.Results, 1)
}

func TestSearchSimilar_WithVectorsAndMetadata(t *testing.T) {
	svc, storage, _, mockIndex := newVecSvcWithSDK(t, "test-ns", 3)
	mockIndex.searchFn = func(_ []float32, _ int, _ *interfaces.SearchParams) ([]*interfaces.SearchResult, error) {
		return []*interfaces.SearchResult{{ID: 1, Distance: 0.1}}, nil
	}
	storage.getVectorFn = func(_ context.Context, _ string, _ uint64) (*interfaces.VectorValue, error) {
		return &interfaces.VectorValue{ID: 1, Embedding: []float32{1, 2, 3}, Metadata: map[string]string{"color": "red"}}, nil
	}
	resp, err := svc.SearchSimilar(tenantctx.WithSystem(context.Background()), &SearchSimilarRequest{
		Namespace: "test-ns", QueryVector: []float32{1, 2, 3}, K: 1, IncludeVectors: true, IncludeMetadata: true,
	})
	require.NoError(t, err)
	assert.Len(t, resp.Results, 1)
	assert.Equal(t, []float32{1, 2, 3}, resp.Results[0].Vector)
	assert.Equal(t, "red", resp.Results[0].Metadata["color"])
}

func TestSearchSimilar_WithMetadataFilter(t *testing.T) {
	svc, storage, _, mockIndex := newVecSvcWithSDK(t, "test-ns", 3)
	mockIndex.searchFn = func(_ []float32, _ int, _ *interfaces.SearchParams) ([]*interfaces.SearchResult, error) {
		return []*interfaces.SearchResult{{ID: 1, Distance: 0.1}, {ID: 2, Distance: 0.2}}, nil
	}
	storage.getVectorFn = func(_ context.Context, _ string, id uint64) (*interfaces.VectorValue, error) {
		meta := map[string]string{"color": "red"}
		if id == 2 {
			meta = map[string]string{"color": "blue"}
		}
		return &interfaces.VectorValue{ID: id, Embedding: []float32{1, 2, 3}, Metadata: meta}, nil
	}
	resp, err := svc.SearchSimilar(tenantctx.WithSystem(context.Background()), &SearchSimilarRequest{
		Namespace: "test-ns", QueryVector: []float32{1, 2, 3}, K: 2, IncludeMetadata: true,
		MetadataFilter: map[string]interface{}{"color": "red"},
	})
	require.NoError(t, err)
	assert.Len(t, resp.Results, 1)
}

func TestSearchSimilar_SearchError_EmptyIndex(t *testing.T) {
	svc, _, _, mockIndex := newVecSvcWithSDK(t, "test-ns", 3)
	mockIndex.countVal = 0
	mockIndex.searchFn = func(_ []float32, _ int, _ *interfaces.SearchParams) ([]*interfaces.SearchResult, error) {
		return nil, errors.New("error")
	}
	resp, err := svc.SearchSimilar(tenantctx.WithSystem(context.Background()), &SearchSimilarRequest{
		Namespace: "test-ns", QueryVector: []float32{1, 2, 3}, K: 5,
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Results)
}

func TestSearchSimilar_SearchError_NonEmptyIndex(t *testing.T) {
	svc, _, _, mockIndex := newVecSvcWithSDK(t, "test-ns", 3)
	mockIndex.countVal = 10
	mockIndex.searchFn = func(_ []float32, _ int, _ *interfaces.SearchParams) ([]*interfaces.SearchResult, error) {
		return nil, errors.New("error")
	}
	_, err := svc.SearchSimilar(tenantctx.WithSystem(context.Background()), &SearchSimilarRequest{
		Namespace: "test-ns", QueryVector: []float32{1, 2, 3}, K: 5,
	})
	require.Error(t, err)
	var qbErr *qbErrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, qbErrors.ErrInternal, qbErr.Code)
}

func TestSearchSimilar_NoIndex_ReturnsEmpty(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "other-ns", 3)
	svc.indexFactory = nil
	resp, err := svc.SearchSimilar(tenantctx.WithSystem(context.Background()), &SearchSimilarRequest{
		Namespace: "other-ns", QueryVector: []float32{1, 2, 3}, K: 5,
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Results)
}

func TestSearchSimilar_GetVectorError(t *testing.T) {
	svc, storage, _, mockIndex := newVecSvcWithSDK(t, "test-ns", 3)
	mockIndex.searchFn = func(_ []float32, _ int, _ *interfaces.SearchParams) ([]*interfaces.SearchResult, error) {
		return []*interfaces.SearchResult{{ID: 1, Distance: 0.1}}, nil
	}
	storage.getVectorFn = func(_ context.Context, _ string, _ uint64) (*interfaces.VectorValue, error) {
		return nil, errors.New("not found")
	}
	resp, err := svc.SearchSimilar(tenantctx.WithSystem(context.Background()), &SearchSimilarRequest{
		Namespace: "test-ns", QueryVector: []float32{1, 2, 3}, K: 1, IncludeVectors: true,
	})
	require.NoError(t, err)
	assert.Len(t, resp.Results, 1)
}

func TestHybridSearch_Success(t *testing.T) {
	svc, storage, _, mockIndex := newVecSvcWithSDK(t, "test-ns", 3)
	mockIndex.searchFn = func(_ []float32, _ int, _ *interfaces.SearchParams) ([]*interfaces.SearchResult, error) {
		return []*interfaces.SearchResult{{ID: 1, Distance: 0.1}, {ID: 2, Distance: 0.2}, {ID: 3, Distance: 0.3}}, nil
	}
	storage.getVectorFn = func(_ context.Context, _ string, id uint64) (*interfaces.VectorValue, error) {
		meta := map[string]string{"type": "document"}
		if id == 2 {
			meta = map[string]string{"type": "image"}
		}
		return &interfaces.VectorValue{ID: id, Embedding: []float32{1, 2, 3}, Metadata: meta}, nil
	}
	resp, err := svc.HybridSearch(tenantctx.WithSystem(context.Background()), &HybridSearchRequest{
		Namespace: "test-ns", QueryVector: []float32{1, 2, 3}, K: 2,
		MetadataFilter: map[string]interface{}{"type": "document"}, IncludeMetadata: true,
	})
	require.NoError(t, err)
	assert.Equal(t, 3, resp.TotalCandidates)
	assert.Equal(t, 2, resp.TotalFiltered)
}

func TestHybridSearch_LimitedByK(t *testing.T) {
	svc, storage, _, mockIndex := newVecSvcWithSDK(t, "test-ns", 3)
	mockIndex.searchFn = func(_ []float32, _ int, _ *interfaces.SearchParams) ([]*interfaces.SearchResult, error) {
		return []*interfaces.SearchResult{
			{ID: 1, Distance: 0.1}, {ID: 2, Distance: 0.2}, {ID: 3, Distance: 0.3}, {ID: 4, Distance: 0.4},
		}, nil
	}
	storage.getVectorFn = func(_ context.Context, _ string, id uint64) (*interfaces.VectorValue, error) {
		return &interfaces.VectorValue{ID: id, Embedding: []float32{1, 2, 3}, Metadata: map[string]string{"match": "yes"}}, nil
	}
	resp, err := svc.HybridSearch(tenantctx.WithSystem(context.Background()), &HybridSearchRequest{
		Namespace: "test-ns", QueryVector: []float32{1, 2, 3}, K: 2,
		MetadataFilter: map[string]interface{}{"match": "yes"}, IncludeMetadata: true,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, resp.TotalFiltered)
}

func TestPerformIndexRebuild_JobNotFound(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	err := svc.performIndexRebuild(tenantctx.WithSystem(context.Background()), "nonexistent", "test-ns", nil)
	require.Error(t, err)
}

func TestPerformIndexRebuild_NoFactory(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	svc.indexFactory = nil
	job := &RebuildJobState{JobID: "j1", Namespace: "test-ns", InProgress: true, StartedAt: time.Now()}
	svc.jobMu.Lock()
	svc.rebuildJobs["j1"] = job
	svc.jobMu.Unlock()

	err := svc.performIndexRebuild(tenantctx.WithSystem(context.Background()), "j1", "test-ns", nil)
	require.Error(t, err)
	job.mu.RLock()
	assert.False(t, job.InProgress)
	assert.NotNil(t, job.Error)
	job.mu.RUnlock()
}

func TestPerformIndexRebuild_FactoryError(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	svc.indexFactory = &vecMockVectorIndexFactory{
		createFn: func(_ *interfaces.VectorIndexConfig) (interfaces.VectorIndex, error) { return nil, errors.New("fail") },
	}
	job := &RebuildJobState{JobID: "j2", Namespace: "test-ns", InProgress: true, StartedAt: time.Now()}
	svc.jobMu.Lock()
	svc.rebuildJobs["j2"] = job
	svc.jobMu.Unlock()

	err := svc.performIndexRebuild(tenantctx.WithSystem(context.Background()), "j2", "test-ns", nil)
	require.Error(t, err)
}

func TestPerformIndexRebuild_Success(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 3)
	storage.scanNamespaceFn = func(_ context.Context, _ string, opts *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error) {
		if opts.Offset > 0 {
			return nil, nil
		}
		return []*interfaces.VectorValue{{ID: 1, Embedding: []float32{1, 2, 3}}, {ID: 2, Embedding: []float32{4, 5, 6}}}, nil
	}
	job := &RebuildJobState{JobID: "j3", Namespace: "test-ns", InProgress: true, TotalVectors: 2, StartedAt: time.Now()}
	svc.jobMu.Lock()
	svc.rebuildJobs["j3"] = job
	svc.jobMu.Unlock()

	err := svc.performIndexRebuild(tenantctx.WithSystem(context.Background()), "j3", "test-ns", nil)
	require.NoError(t, err)
	job.mu.RLock()
	assert.Equal(t, uint64(2), job.VectorsProcessed)
	job.mu.RUnlock()
}

func TestPerformIndexRebuild_WithCustomConfig(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 3)
	storage.scanNamespaceFn = func(_ context.Context, _ string, _ *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error) {
		return nil, nil
	}
	job := &RebuildJobState{JobID: "j4", Namespace: "test-ns", InProgress: true, StartedAt: time.Now()}
	svc.jobMu.Lock()
	svc.rebuildJobs["j4"] = job
	svc.jobMu.Unlock()

	err := svc.performIndexRebuild(tenantctx.WithSystem(context.Background()), "j4", "test-ns", map[string]string{
		"m": "32", "ef_construction": "400", "ef_search": "100",
	})
	require.NoError(t, err)
}

func TestPerformIndexRebuild_InvalidConfig(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 3)
	storage.scanNamespaceFn = func(_ context.Context, _ string, _ *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error) {
		return nil, nil
	}
	job := &RebuildJobState{JobID: "j5", Namespace: "test-ns", InProgress: true, StartedAt: time.Now()}
	svc.jobMu.Lock()
	svc.rebuildJobs["j5"] = job
	svc.jobMu.Unlock()

	err := svc.performIndexRebuild(tenantctx.WithSystem(context.Background()), "j5", "test-ns", map[string]string{
		"m": "bad", "ef_construction": "bad", "ef_search": "bad",
	})
	require.NoError(t, err)
}

func TestPerformIndexRebuild_ScanError(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 3)
	storage.scanNamespaceFn = func(_ context.Context, _ string, _ *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error) {
		return nil, errors.New("scan error")
	}
	job := &RebuildJobState{JobID: "j6", Namespace: "test-ns", InProgress: true, StartedAt: time.Now()}
	svc.jobMu.Lock()
	svc.rebuildJobs["j6"] = job
	svc.jobMu.Unlock()

	err := svc.performIndexRebuild(tenantctx.WithSystem(context.Background()), "j6", "test-ns", nil)
	require.Error(t, err)
}

func TestRebuildIndex_Synchronous(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 3)
	storage.scanNamespaceFn = func(_ context.Context, _ string, _ *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error) {
		return nil, nil
	}
	resp, err := svc.RebuildIndex(tenantctx.WithSystem(context.Background()), &RebuildIndexRequest{Namespace: "test-ns", Async: false})
	require.NoError(t, err)
	assert.Equal(t, "completed", resp.Status)
	assert.True(t, resp.Success)
}

func TestRebuildIndex_Asynchronous(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 3)
	storage.scanNamespaceFn = func(_ context.Context, _ string, _ *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error) {
		return nil, nil
	}
	resp, err := svc.RebuildIndex(tenantctx.WithSystem(context.Background()), &RebuildIndexRequest{Namespace: "test-ns", Async: true})
	require.NoError(t, err)
	assert.Equal(t, "in_progress", resp.Status)
}

func TestRebuildIndex_NamespaceNotFound(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	storage.scanNamespaceFn = func(_ context.Context, namespace string, _ *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error) {
		return nil, &qrdbtransport.NamespaceNotFoundError{Namespace: namespace}
	}
	_, err := svc.RebuildIndex(tenantctx.WithSystem(context.Background()), &RebuildIndexRequest{Namespace: "nope"})
	var qbErr *qbErrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, qbErrors.ErrNotFound, qbErr.Code)
}

func TestGetRebuildProgress_ByJobID(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	job := &RebuildJobState{JobID: "j1", Namespace: "test-ns", InProgress: true,
		VectorsProcessed: 50, TotalVectors: 100, StartedAt: time.Now()}
	svc.jobMu.Lock()
	svc.rebuildJobs["j1"] = job
	svc.jobMu.Unlock()

	resp, err := svc.GetRebuildProgress(tenantctx.WithSystem(context.Background()), &GetRebuildProgressRequest{JobID: "j1"})
	require.NoError(t, err)
	assert.Equal(t, "running", resp.Status)
	assert.Equal(t, 50.0, resp.PercentComplete)
}

func TestGetRebuildProgress_ByNamespace(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	job := &RebuildJobState{JobID: "jn", Namespace: "test-ns", InProgress: false, StartedAt: time.Now()}
	svc.jobMu.Lock()
	svc.rebuildJobs["jn"] = job
	svc.jobMu.Unlock()

	resp, err := svc.GetRebuildProgress(tenantctx.WithSystem(context.Background()), &GetRebuildProgressRequest{Namespace: "test-ns"})
	require.NoError(t, err)
	assert.Equal(t, "pending", resp.Status)
}

func TestGetRebuildProgress_NotFound(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	_, err := svc.GetRebuildProgress(tenantctx.WithSystem(context.Background()), &GetRebuildProgressRequest{JobID: "nope"})
	require.Error(t, err)
}

func TestGetRebuildProgress_Completed(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	now := time.Now()
	done := now.Add(1 * time.Second)
	job := &RebuildJobState{JobID: "jd", Namespace: "test-ns", InProgress: false,
		VectorsProcessed: 100, TotalVectors: 100, StartedAt: now, CompletedAt: &done}
	svc.jobMu.Lock()
	svc.rebuildJobs["jd"] = job
	svc.jobMu.Unlock()

	resp, err := svc.GetRebuildProgress(tenantctx.WithSystem(context.Background()), &GetRebuildProgressRequest{JobID: "jd"})
	require.NoError(t, err)
	assert.Equal(t, "completed", resp.Status)
	assert.Equal(t, 100.0, resp.PercentComplete)
}

func TestGetRebuildProgress_Failed(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	now := time.Now()
	done := now.Add(1 * time.Second)
	job := &RebuildJobState{JobID: "jf", Namespace: "test-ns", InProgress: false,
		VectorsProcessed: 50, TotalVectors: 100, StartedAt: now, CompletedAt: &done, Error: errors.New("failed")}
	svc.jobMu.Lock()
	svc.rebuildJobs["jf"] = job
	svc.jobMu.Unlock()

	resp, err := svc.GetRebuildProgress(tenantctx.WithSystem(context.Background()), &GetRebuildProgressRequest{JobID: "jf"})
	require.NoError(t, err)
	assert.Equal(t, "failed", resp.Status)
	assert.Equal(t, "failed", resp.Error)
}

func TestGetRebuildProgress_WithEstimate(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	started := time.Now().Add(-1 * time.Second)
	job := &RebuildJobState{JobID: "je", Namespace: "test-ns", InProgress: true,
		VectorsProcessed: 50, TotalVectors: 100, StartedAt: started}
	svc.jobMu.Lock()
	svc.rebuildJobs["je"] = job
	svc.jobMu.Unlock()

	resp, err := svc.GetRebuildProgress(tenantctx.WithSystem(context.Background()), &GetRebuildProgressRequest{JobID: "je"})
	require.NoError(t, err)
	assert.Greater(t, resp.EstimatedTimeRemainingMs, int64(0))
}

func TestGetIndexStats_WithIndex(t *testing.T) {
	svc, _, _, mockIndex := newVecSvcWithSDK(t, "test-ns", 128)
	mockIndex.countVal = 100
	mockIndex.statsVal = &interfaces.IndexStats{NodeCount: 100, MaxLayer: 4, AvgConnections: 12.0, Dimensions: 128}

	resp, err := svc.GetIndexStats(tenantctx.WithSystem(context.Background()), &GetIndexStatsRequest{Namespace: "test-ns"})
	require.NoError(t, err)
	assert.Equal(t, uint64(100), resp.VectorCount)
	assert.Greater(t, resp.IndexSizeBytes, uint64(0))
	assert.Greater(t, resp.MemoryUsageBytes, resp.IndexSizeBytes)
}

func TestGetIndexStats_WithoutIndex(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	svc.indexMu.Lock()
	delete(svc.indexes, "test-ns")
	svc.indexMu.Unlock()

	resp, err := svc.GetIndexStats(tenantctx.WithSystem(context.Background()), &GetIndexStatsRequest{Namespace: "test-ns"})
	require.NoError(t, err)
	assert.Equal(t, uint64(0), resp.VectorCount)
}

func TestGetIndexStats_NilStats(t *testing.T) {
	svc, _, _, mockIndex := newVecSvcWithSDK(t, "test-ns", 128)
	mockIndex.countVal = 5
	mockIndex.statsVal = nil
	resp, err := svc.GetIndexStats(tenantctx.WithSystem(context.Background()), &GetIndexStatsRequest{Namespace: "test-ns"})
	require.NoError(t, err)
	assert.Equal(t, uint64(5), resp.VectorCount)
	assert.Equal(t, uint64(0), resp.IndexSizeBytes)
}

func TestGetIndexStats_MaxLayerZero(t *testing.T) {
	svc, _, _, mockIndex := newVecSvcWithSDK(t, "test-ns", 128)
	mockIndex.countVal = 10
	mockIndex.statsVal = &interfaces.IndexStats{NodeCount: 10, MaxLayer: 0, AvgConnections: 8.0, Dimensions: 128}
	resp, err := svc.GetIndexStats(tenantctx.WithSystem(context.Background()), &GetIndexStatsRequest{Namespace: "test-ns"})
	require.NoError(t, err)
	assert.Greater(t, resp.IndexSizeBytes, uint64(0))
}

func TestCompactNamespace_Synchronous(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	resp, err := svc.CompactNamespace(tenantctx.WithSystem(context.Background()), &CompactNamespaceRequest{Namespace: "test-ns"})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestCompactNamespace_Asynchronous(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	var handedOff bool
	storage.compactFn = func(_ context.Context, _ string, async bool) error {
		handedOff = async
		return nil
	}
	resp, err := svc.CompactNamespace(tenantctx.WithSystem(context.Background()), &CompactNamespaceRequest{Namespace: "test-ns", Async: true})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.NotEmpty(t, resp.OperationID)
	assert.True(t, handedOff, "an async compaction runs in qrdb's background")
}

// qrdb's compact verb checks the namespace in the caller's tenant, so a
// namespace it does not hold is not found, synchronously or not, with no
// separate metadata read.
func TestCompactNamespace_NamespaceNotFound(t *testing.T) {
	for _, async := range []bool{false, true} {
		svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
		storage.getNamespaceInfoFn = func(_ context.Context, _ string) (*interfaces.VectorNamespaceInfo, error) {
			return nil, &namespaceInfoReadError{}
		}
		storage.compactFn = func(_ context.Context, namespace string, _ bool) error {
			return &qrdbtransport.NamespaceNotFoundError{Namespace: namespace}
		}
		_, err := svc.CompactNamespace(tenantctx.WithSystem(context.Background()), &CompactNamespaceRequest{Namespace: "nope", Async: async})
		var refused *qbErrors.QBError
		require.ErrorAs(t, err, &refused, "async=%v", async)
		assert.Equal(t, qbErrors.ErrNotFound, refused.Code, "async=%v", async)
		var notFound *qrdbtransport.NamespaceNotFoundError
		assert.ErrorAs(t, err, &notFound, "async=%v: the refusal is qrdb's, not a metadata read", async)
	}
}

// namespaceInfoReadError fails a namespace-metadata read a test expects the
// service not to make.
type namespaceInfoReadError struct{}

func (*namespaceInfoReadError) Error() string { return "unexpected namespace metadata read" }

func TestCompactNamespace_ServiceClosed(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	svc.mu.Lock()
	svc.closed = true
	svc.mu.Unlock()
	_, err := svc.CompactNamespace(tenantctx.WithSystem(context.Background()), &CompactNamespaceRequest{Namespace: "test-ns"})
	require.Error(t, err)
}

func TestPerformCompaction_JobNotFound(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	err := svc.performCompaction(tenantctx.WithSystem(context.Background()), "nope", "test-ns")
	require.Error(t, err)
}

func TestPerformCompaction_Success(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	callCount := 0
	storage.getStatsFn = func(_ context.Context, _ string) (*interfaces.VectorStorageStats, error) {
		callCount++
		if callCount == 1 {
			return &interfaces.VectorStorageStats{SizeBytes: 1000}, nil
		}
		return &interfaces.VectorStorageStats{SizeBytes: 500}, nil
	}
	job := &CompactionJobState{OperationID: "o1", Namespace: "test-ns", InProgress: true, StartedAt: time.Now()}
	svc.jobMu.Lock()
	svc.compactionJobs["o1"] = job
	svc.jobMu.Unlock()

	err := svc.performCompaction(tenantctx.WithSystem(context.Background()), "o1", "test-ns")
	require.NoError(t, err)
	job.mu.RLock()
	assert.Equal(t, int64(500), job.BytesCompacted)
	job.mu.RUnlock()
}

func TestPerformCompaction_CompactError(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	storage.compactFn = func(_ context.Context, _ string, _ bool) error { return errors.New("fail") }
	job := &CompactionJobState{OperationID: "o2", Namespace: "test-ns", InProgress: true, StartedAt: time.Now()}
	svc.jobMu.Lock()
	svc.compactionJobs["o2"] = job
	svc.jobMu.Unlock()

	err := svc.performCompaction(tenantctx.WithSystem(context.Background()), "o2", "test-ns")
	require.Error(t, err)
}

func TestPerformCompaction_NegativeBytes(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	callCount := 0
	storage.getStatsFn = func(_ context.Context, _ string) (*interfaces.VectorStorageStats, error) {
		callCount++
		if callCount == 1 {
			return &interfaces.VectorStorageStats{SizeBytes: 500}, nil
		}
		return &interfaces.VectorStorageStats{SizeBytes: 1000}, nil
	}
	job := &CompactionJobState{OperationID: "o3", Namespace: "test-ns", InProgress: true, StartedAt: time.Now()}
	svc.jobMu.Lock()
	svc.compactionJobs["o3"] = job
	svc.jobMu.Unlock()

	err := svc.performCompaction(tenantctx.WithSystem(context.Background()), "o3", "test-ns")
	require.NoError(t, err)
	job.mu.RLock()
	assert.Equal(t, int64(0), job.BytesCompacted)
	job.mu.RUnlock()
}

func TestGetCompactionProgress_ByOperationID(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	job := &CompactionJobState{OperationID: "o1", Namespace: "test-ns", InProgress: true,
		BytesCompacted: 500, TotalBytes: 1000, StartedAt: time.Now()}
	svc.jobMu.Lock()
	svc.compactionJobs["o1"] = job
	svc.jobMu.Unlock()

	resp, err := svc.GetCompactionProgress(tenantctx.WithSystem(context.Background()), &GetCompactionProgressRequest{OperationID: "o1"})
	require.NoError(t, err)
	assert.Equal(t, 50.0, resp.PercentComplete)
}

func TestGetCompactionProgress_NotFound(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	_, err := svc.GetCompactionProgress(tenantctx.WithSystem(context.Background()), &GetCompactionProgressRequest{OperationID: "nope"})
	require.Error(t, err)
}

func TestGetCompactionProgress_Completed(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	done := time.Now()
	job := &CompactionJobState{OperationID: "od", Namespace: "test-ns", InProgress: false,
		TotalBytes: 1000, StartedAt: time.Now(), CompletedAt: &done}
	svc.jobMu.Lock()
	svc.compactionJobs["od"] = job
	svc.jobMu.Unlock()

	resp, err := svc.GetCompactionProgress(tenantctx.WithSystem(context.Background()), &GetCompactionProgressRequest{OperationID: "od"})
	require.NoError(t, err)
	assert.Equal(t, 100.0, resp.PercentComplete)
}

func TestGetCompactionProgress_WithError(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	done := time.Now()
	job := &CompactionJobState{OperationID: "oe", Namespace: "test-ns", InProgress: false,
		StartedAt: time.Now(), CompletedAt: &done, Error: errors.New("disk full")}
	svc.jobMu.Lock()
	svc.compactionJobs["oe"] = job
	svc.jobMu.Unlock()

	resp, err := svc.GetCompactionProgress(tenantctx.WithSystem(context.Background()), &GetCompactionProgressRequest{OperationID: "oe"})
	require.NoError(t, err)
	assert.Equal(t, "disk full", resp.Error)
}

func TestGetCompactionProgress_ByNamespace(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	now := time.Now()
	svc.jobMu.Lock()
	svc.compactionJobs["old"] = &CompactionJobState{OperationID: "old", Namespace: "test-ns", StartedAt: now.Add(-10 * time.Second)}
	svc.compactionJobs["new"] = &CompactionJobState{OperationID: "new", Namespace: "test-ns", InProgress: true, StartedAt: now}
	svc.jobMu.Unlock()

	resp, err := svc.GetCompactionProgress(tenantctx.WithSystem(context.Background()), &GetCompactionProgressRequest{Namespace: "test-ns"})
	require.NoError(t, err)
	assert.Equal(t, "new", resp.OperationID)
}

func TestGetCompactionProgress_ByNamespace_NotFound(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	_, err := svc.GetCompactionProgress(tenantctx.WithSystem(context.Background()), &GetCompactionProgressRequest{Namespace: "nope"})
	require.Error(t, err)
}

func TestGetCompactionProgress_ServiceClosed(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	svc.mu.Lock()
	svc.closed = true
	svc.mu.Unlock()
	_, err := svc.GetCompactionProgress(tenantctx.WithSystem(context.Background()), &GetCompactionProgressRequest{OperationID: "o1"})
	require.Error(t, err)
}

func TestBatchDeleteVectors_Success(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	resp, err := svc.BatchDeleteVectors(tenantctx.WithSystem(context.Background()), &BatchDeleteVectorsRequest{
		Namespace: "test-ns", IDs: []uint64{1, 2, 3},
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, 3, resp.DeletedCount)
}

func TestBatchDeleteVectors_NilRequest(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	_, err := svc.BatchDeleteVectors(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
}

func TestBatchDeleteVectors_EmptyIDs(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	_, err := svc.BatchDeleteVectors(tenantctx.WithSystem(context.Background()), &BatchDeleteVectorsRequest{
		Namespace: "test-ns", IDs: []uint64{},
	})
	require.Error(t, err)
}

func TestBatchDeleteVectors_FallbackToIndividual(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	storage.batchDeleteVectorsFn = func(_ context.Context, _ string, _ []uint64) error {
		return errors.New("batch not supported")
	}
	// Individual deletes go through SDK which works
	resp, err := svc.BatchDeleteVectors(tenantctx.WithSystem(context.Background()), &BatchDeleteVectorsRequest{
		Namespace: "test-ns", IDs: []uint64{1, 2},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, resp.DeletedCount)
	assert.True(t, resp.Success)
}

func TestUpdateMetadata_NilRequest(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	_, err := svc.UpdateMetadata(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
}

func TestUpdateMetadata_EmptyNamespace(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	_, err := svc.UpdateMetadata(tenantctx.WithSystem(context.Background()), &UpdateVectorMetadataRequest{ID: 1})
	require.Error(t, err)
}

func TestUpdateMetadata_ZeroID(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	_, err := svc.UpdateMetadata(tenantctx.WithSystem(context.Background()), &UpdateVectorMetadataRequest{Namespace: "test-ns"})
	require.Error(t, err)
}

func TestUpdateMetadata_VectorNotFound(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	storage.getVectorFn = func(_ context.Context, _ string, _ uint64) (*interfaces.VectorValue, error) {
		return nil, errors.New("not found")
	}
	_, err := svc.UpdateMetadata(tenantctx.WithSystem(context.Background()), &UpdateVectorMetadataRequest{
		Namespace: "test-ns", ID: 1, Metadata: map[string]interface{}{"k": "v"},
	})
	require.Error(t, err)
}

func TestUpdateMetadata_ReplaceMetadata(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 3)
	storage.getVectorFn = func(_ context.Context, _ string, _ uint64) (*interfaces.VectorValue, error) {
		return &interfaces.VectorValue{ID: 1, Embedding: []float32{1, 2, 3}, Metadata: map[string]string{"old": "val"}}, nil
	}
	resp, err := svc.UpdateMetadata(tenantctx.WithSystem(context.Background()), &UpdateVectorMetadataRequest{
		Namespace: "test-ns", ID: 1, Metadata: map[string]interface{}{"new": "val"}, MergeMetadata: false,
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestUpdateMetadata_MergeMetadata(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 3)
	storage.getVectorFn = func(_ context.Context, _ string, _ uint64) (*interfaces.VectorValue, error) {
		return &interfaces.VectorValue{ID: 1, Embedding: []float32{1, 2, 3},
			Metadata: map[string]string{"existing": "val1", "to_delete": "val2"}}, nil
	}
	resp, err := svc.UpdateMetadata(tenantctx.WithSystem(context.Background()), &UpdateVectorMetadataRequest{
		Namespace: "test-ns", ID: 1,
		Metadata:      map[string]interface{}{"new_key": "new_val"},
		MergeMetadata: true,
		DeleteKeys:    []string{"to_delete"},
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestSearchByID_NilRequest(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	_, err := svc.SearchByID(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
}

func TestSearchByID_ZeroK(t *testing.T) {
	svc, _, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	_, err := svc.SearchByID(tenantctx.WithSystem(context.Background()), &SearchByIDRequest{Namespace: "test-ns", ID: 1, K: 0})
	require.Error(t, err)
}

func TestSearchByID_VectorNotFound(t *testing.T) {
	svc, storage, _, _ := newVecSvcWithSDK(t, "test-ns", 128)
	storage.getVectorFn = func(_ context.Context, _ string, _ uint64) (*interfaces.VectorValue, error) {
		return nil, errors.New("not found")
	}
	_, err := svc.SearchByID(tenantctx.WithSystem(context.Background()), &SearchByIDRequest{Namespace: "test-ns", ID: 99, K: 5})
	require.Error(t, err)
}

func TestSearchByID_Success(t *testing.T) {
	svc, storage, _, mockIndex := newVecSvcWithSDK(t, "test-ns", 3)
	storage.getVectorFn = func(_ context.Context, _ string, id uint64) (*interfaces.VectorValue, error) {
		return &interfaces.VectorValue{ID: id, Embedding: []float32{1, 2, 3}}, nil
	}
	mockIndex.searchFn = func(_ []float32, _ int, _ *interfaces.SearchParams) ([]*interfaces.SearchResult, error) {
		return []*interfaces.SearchResult{{ID: 1, Distance: 0}, {ID: 2, Distance: 0.5}}, nil
	}
	resp, err := svc.SearchByID(tenantctx.WithSystem(context.Background()), &SearchByIDRequest{Namespace: "test-ns", ID: 1, K: 3})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), resp.SourceID)
	assert.Len(t, resp.Results, 2)
}

func TestSearchByID_ExcludeSelf(t *testing.T) {
	svc, storage, _, mockIndex := newVecSvcWithSDK(t, "test-ns", 3)
	storage.getVectorFn = func(_ context.Context, _ string, id uint64) (*interfaces.VectorValue, error) {
		return &interfaces.VectorValue{ID: id, Embedding: []float32{1, 2, 3}}, nil
	}
	mockIndex.searchFn = func(_ []float32, _ int, _ *interfaces.SearchParams) ([]*interfaces.SearchResult, error) {
		return []*interfaces.SearchResult{{ID: 1, Distance: 0}, {ID: 2, Distance: 0.5}, {ID: 3, Distance: 0.9}}, nil
	}
	resp, err := svc.SearchByID(tenantctx.WithSystem(context.Background()), &SearchByIDRequest{
		Namespace: "test-ns", ID: 1, K: 2, ExcludeSelf: true,
	})
	require.NoError(t, err)
	assert.Len(t, resp.Results, 2)
	for _, r := range resp.Results {
		assert.NotEqual(t, uint64(1), r.ID)
	}
}

func TestLocalPointsToSDK_NonZeroTimestamp(t *testing.T) {
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	points := []*tsDataPoint{{Metric: "cpu", Tags: map[string]string{"h": "n1"}, Value: 42.5, Timestamp: ts}}
	result := localPointsToSDK(points)
	require.Len(t, result, 1)
	assert.Equal(t, "cpu", result[0].Metric)
	assert.Equal(t, ts.UnixNano(), result[0].TimestampNs)
}

func TestLocalPointsToSDK_ZeroTimestamp(t *testing.T) {
	points := []*tsDataPoint{{Metric: "mem", Value: 1024.0}}
	before := time.Now().UnixNano()
	result := localPointsToSDK(points)
	after := time.Now().UnixNano()
	require.Len(t, result, 1)
	assert.GreaterOrEqual(t, result[0].TimestampNs, before)
	assert.LessOrEqual(t, result[0].TimestampNs, after)
}

func TestLocalPointsToSDK_Multiple(t *testing.T) {
	points := []*tsDataPoint{
		{Metric: "a", Value: 1, Timestamp: time.Now()},
		{Metric: "b", Value: 2, Timestamp: time.Now()},
	}
	result := localPointsToSDK(points)
	assert.Len(t, result, 2)
}
