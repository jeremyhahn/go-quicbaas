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
	stderrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// SetParameters - Batch set tests
// =============================================================================

func TestSetParameters_NilRequest(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	resp, err := svc.SetParameters(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dragonErr *dberrors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, dberrors.ErrInvalidInput, dragonErr.Code)
}

func TestSetParameters_EmptyParameters(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	resp, err := svc.SetParameters(context.Background(), &interfaces.SetParametersRequest{
		Parameters: []*interfaces.SetParameterRequest{},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Results, 0)
}

func TestSetParameters_Success(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	pm.On("StoreBatch", mock.Anything, mock.AnythingOfType("[]*interfaces.ParameterEntry"), true, (*interfaces.StoreOptions)(nil)).
		Return(nil).Once()

	resp, err := svc.SetParameters(context.Background(), &interfaces.SetParametersRequest{
		Parameters: []*interfaces.SetParameterRequest{
			{Path: "/app/config/key1", Value: []byte("value1"), Type: "string"},
			{Path: "/app/config/key2", Value: []byte("value2"), Type: "string"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Results, 2)
	for _, r := range resp.Results {
		assert.True(t, r.Success)
	}
	pm.AssertExpectations(t)
}

func TestSetParameters_StoreBatchError(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	pm.On("StoreBatch", mock.Anything, mock.AnythingOfType("[]*interfaces.ParameterEntry"), true, (*interfaces.StoreOptions)(nil)).
		Return(stderrors.New("storage error")).Once()

	resp, err := svc.SetParameters(context.Background(), &interfaces.SetParametersRequest{
		Parameters: []*interfaces.SetParameterRequest{
			{Path: "/app/config/key1", Value: []byte("value1"), Type: "string"},
		},
	})
	// Returns both partial results and an error
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Results, 1)
	assert.False(t, resp.Results[0].Success)
	assert.NotEmpty(t, resp.Results[0].Error)
	pm.AssertExpectations(t)
}

func TestSetParameters_WithEncryption(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	pm.On("StoreBatch", mock.Anything, mock.AnythingOfType("[]*interfaces.ParameterEntry"), true, mock.AnythingOfType("*interfaces.StoreOptions")).
		Return(nil).Once()

	resp, err := svc.SetParameters(context.Background(), &interfaces.SetParametersRequest{
		Parameters: []*interfaces.SetParameterRequest{
			{Path: "/app/secret/key1", Value: []byte("secret"), Type: "securestring", KMSKeyID: "kms-key-123"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Results, 1)
	assert.True(t, resp.Results[0].Success)
	pm.AssertExpectations(t)
}

func TestSetParameters_InvalidParameter(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	resp, err := svc.SetParameters(context.Background(), &interfaces.SetParametersRequest{
		Parameters: []*interfaces.SetParameterRequest{
			{Path: "", Value: []byte("value1"), Type: "string"}, // empty path
		},
	})
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestSetParameters_ClosedService(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	pm.On("Close").Return(nil).Once()
	err = svc.(*parameterStoreService).Close()
	require.NoError(t, err)

	resp, err := svc.SetParameters(context.Background(), &interfaces.SetParametersRequest{
		Parameters: []*interfaces.SetParameterRequest{
			{Path: "/app/config/key1", Value: []byte("value1"), Type: "string"},
		},
	})
	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// ListParameters - List tests
// =============================================================================

func TestListParameters_NilRequest(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	resp, err := svc.ListParameters(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dragonErr *dberrors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, dberrors.ErrInvalidInput, dragonErr.Code)
}

func TestListParameters_Success_PathsOnly(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	pm.On("List", mock.Anything, "/app/", true, 100, "").
		Return([]string{"/app/key1", "/app/key2"}, "", nil).Once()

	resp, err := svc.ListParameters(context.Background(), &interfaces.ListParametersRequest{
		Prefix:    "/app/",
		Recursive: true,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Parameters, 2)
	assert.Equal(t, "/app/key1", resp.Parameters[0].Path)
	assert.Equal(t, "/app/key2", resp.Parameters[1].Path)
	assert.False(t, resp.HasMore)
	pm.AssertExpectations(t)
}

func TestListParameters_Success_WithValues(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	pm.On("List", mock.Anything, "/app/", false, 100, "").
		Return([]string{"/app/key1"}, "", nil).Once()

	pm.On("GetBatch", mock.Anything, []string{"/app/key1"}, false).
		Return(map[string]*interfaces.ParameterResult{
			"/app/key1": {
				Value: []byte("value1"),
				Metadata: &interfaces.ParameterMetadata{
					Type:      "string",
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				},
			},
		}, (map[string]error)(nil), nil).Once()

	resp, err := svc.ListParameters(context.Background(), &interfaces.ListParametersRequest{
		Prefix:     "/app/",
		WithValues: true,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Parameters, 1)
	pm.AssertExpectations(t)
}

func TestListParameters_WithPagination(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	pm.On("List", mock.Anything, "/app/", false, 10, "").
		Return([]string{"/app/key1"}, "next-cursor-123", nil).Once()

	resp, err := svc.ListParameters(context.Background(), &interfaces.ListParametersRequest{
		Prefix: "/app/",
		Limit:  10,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.HasMore)
	assert.Equal(t, "next-cursor-123", resp.NextCursor)
	pm.AssertExpectations(t)
}

func TestListParameters_StorageError(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	pm.On("List", mock.Anything, "/app/", false, 100, "").
		Return(nil, "", stderrors.New("storage read error")).Once()

	resp, err := svc.ListParameters(context.Background(), &interfaces.ListParametersRequest{
		Prefix: "/app/",
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	pm.AssertExpectations(t)
}

func TestListParameters_GetBatchError(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	pm.On("List", mock.Anything, "/app/", false, 100, "").
		Return([]string{"/app/key1"}, "", nil).Once()

	pm.On("GetBatch", mock.Anything, []string{"/app/key1"}, false).
		Return(nil, nil, stderrors.New("batch get error")).Once()

	resp, err := svc.ListParameters(context.Background(), &interfaces.ListParametersRequest{
		Prefix:     "/app/",
		WithValues: true,
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	pm.AssertExpectations(t)
}

func TestListParameters_ClosedService(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	pm.On("Close").Return(nil).Once()
	err = svc.(*parameterStoreService).Close()
	require.NoError(t, err)

	resp, err := svc.ListParameters(context.Background(), &interfaces.ListParametersRequest{
		Prefix: "/app/",
	})
	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// SearchByTags - Search tests
// =============================================================================

func TestSearchByTags_NilRequest(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	resp, err := svc.SearchByTags(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dragonErr *dberrors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, dberrors.ErrInvalidInput, dragonErr.Code)
}

func TestSearchByTags_EmptyTags(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	resp, err := svc.SearchByTags(context.Background(), &interfaces.SearchByTagsRequest{
		Tags: map[string]string{},
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var dragonErr *dberrors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, dberrors.ErrInvalidInput, dragonErr.Code)
}

func TestSearchByTags_Success_PathsOnly(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	pm.On("SearchByTags", mock.Anything, map[string]string{"env": "prod"}, "", 100, "").
		Return([]string{"/app/key1", "/app/key2"}, "", nil).Once()

	resp, err := svc.SearchByTags(context.Background(), &interfaces.SearchByTagsRequest{
		Tags: map[string]string{"env": "prod"},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Parameters, 2)
	assert.False(t, resp.HasMore)
	pm.AssertExpectations(t)
}

func TestSearchByTags_Success_WithValues(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	pm.On("SearchByTags", mock.Anything, map[string]string{"env": "prod"}, "/app/", 50, "").
		Return([]string{"/app/key1"}, "", nil).Once()

	pm.On("GetBatch", mock.Anything, []string{"/app/key1"}, true).
		Return(map[string]*interfaces.ParameterResult{
			"/app/key1": {
				Value: []byte("secret-value"),
				Metadata: &interfaces.ParameterMetadata{
					Type:      "securestring",
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				},
			},
		}, (map[string]error)(nil), nil).Once()

	resp, err := svc.SearchByTags(context.Background(), &interfaces.SearchByTagsRequest{
		Tags:           map[string]string{"env": "prod"},
		Prefix:         "/app/",
		WithValues:     true,
		WithDecryption: true,
		Limit:          50,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Parameters, 1)
	pm.AssertExpectations(t)
}

func TestSearchByTags_StorageError(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	pm.On("SearchByTags", mock.Anything, map[string]string{"env": "prod"}, "", 100, "").
		Return(nil, "", stderrors.New("search error")).Once()

	resp, err := svc.SearchByTags(context.Background(), &interfaces.SearchByTagsRequest{
		Tags: map[string]string{"env": "prod"},
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	pm.AssertExpectations(t)
}

func TestSearchByTags_GetBatchError(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	pm.On("SearchByTags", mock.Anything, map[string]string{"env": "prod"}, "", 100, "").
		Return([]string{"/app/key1"}, "", nil).Once()

	pm.On("GetBatch", mock.Anything, []string{"/app/key1"}, false).
		Return(nil, nil, stderrors.New("batch get error")).Once()

	resp, err := svc.SearchByTags(context.Background(), &interfaces.SearchByTagsRequest{
		Tags:       map[string]string{"env": "prod"},
		WithValues: true,
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	pm.AssertExpectations(t)
}

func TestSearchByTags_WithPagination(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	pm.On("SearchByTags", mock.Anything, map[string]string{"env": "prod"}, "", 10, "cursor-abc").
		Return([]string{"/app/key3"}, "next-cursor-def", nil).Once()

	resp, err := svc.SearchByTags(context.Background(), &interfaces.SearchByTagsRequest{
		Tags:   map[string]string{"env": "prod"},
		Limit:  10,
		Cursor: "cursor-abc",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.HasMore)
	assert.Equal(t, "next-cursor-def", resp.NextCursor)
	pm.AssertExpectations(t)
}

func TestSearchByTags_ClosedService(t *testing.T) {
	pm := newParamManagerMock()
	logger := newParamStoreLoggerMock()
	svc, err := NewParameterStoreService(pm, logger)
	require.NoError(t, err)

	pm.On("Close").Return(nil).Once()
	err = svc.(*parameterStoreService).Close()
	require.NoError(t, err)

	resp, err := svc.SearchByTags(context.Background(), &interfaces.SearchByTagsRequest{
		Tags: map[string]string{"env": "prod"},
	})
	require.Error(t, err)
	assert.Nil(t, resp)
}
