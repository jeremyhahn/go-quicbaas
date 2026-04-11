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
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// paramManagerMock is a mock implementation of interfaces.ParameterManager.
type paramManagerMock struct {
	mock.Mock
}

func newParamManagerMock() *paramManagerMock {
	return &paramManagerMock{}
}

func (m *paramManagerMock) Store(ctx context.Context, path string, value []byte, metadata *interfaces.ParameterMetadata, overwrite bool) error {
	args := m.Called(ctx, path, value, metadata, overwrite)
	return args.Error(0)
}

func (m *paramManagerMock) StoreWithOptions(ctx context.Context, path string, value []byte, metadata *interfaces.ParameterMetadata, overwrite bool, opts *interfaces.StoreOptions) error {
	args := m.Called(ctx, path, value, metadata, overwrite, opts)
	return args.Error(0)
}

func (m *paramManagerMock) Get(ctx context.Context, path string, withDecryption bool) ([]byte, *interfaces.ParameterMetadata, error) {
	args := m.Called(ctx, path, withDecryption)
	if args.Get(0) == nil {
		return nil, nil, args.Error(2)
	}
	var metadata *interfaces.ParameterMetadata
	if args.Get(1) != nil {
		metadata = args.Get(1).(*interfaces.ParameterMetadata)
	}
	return args.Get(0).([]byte), metadata, args.Error(2)
}

func (m *paramManagerMock) Delete(ctx context.Context, path string) error {
	args := m.Called(ctx, path)
	return args.Error(0)
}

func (m *paramManagerMock) List(ctx context.Context, prefix string, recursive bool, limit int, cursor string) ([]string, string, error) {
	args := m.Called(ctx, prefix, recursive, limit, cursor)
	if args.Get(0) == nil {
		return nil, "", args.Error(2)
	}
	return args.Get(0).([]string), args.String(1), args.Error(2)
}

func (m *paramManagerMock) GetBatch(ctx context.Context, paths []string, withDecryption bool) (map[string]*interfaces.ParameterResult, map[string]error, error) {
	args := m.Called(ctx, paths, withDecryption)
	var results map[string]*interfaces.ParameterResult
	var errs map[string]error
	if args.Get(0) != nil {
		results = args.Get(0).(map[string]*interfaces.ParameterResult)
	}
	if args.Get(1) != nil {
		errs = args.Get(1).(map[string]error)
	}
	return results, errs, args.Error(2)
}

func (m *paramManagerMock) StoreBatch(ctx context.Context, params []*interfaces.ParameterEntry, overwrite bool, opts *interfaces.StoreOptions) error {
	args := m.Called(ctx, params, overwrite, opts)
	return args.Error(0)
}

func (m *paramManagerMock) DeleteBatch(ctx context.Context, paths []string) (map[string]bool, map[string]error, error) {
	args := m.Called(ctx, paths)
	var results map[string]bool
	var errs map[string]error
	if args.Get(0) != nil {
		results = args.Get(0).(map[string]bool)
	}
	if args.Get(1) != nil {
		errs = args.Get(1).(map[string]error)
	}
	return results, errs, args.Error(2)
}

func (m *paramManagerMock) SearchByTags(ctx context.Context, tags map[string]string, prefix string, limit int, cursor string) ([]string, string, error) {
	args := m.Called(ctx, tags, prefix, limit, cursor)
	if args.Get(0) == nil {
		return nil, "", args.Error(2)
	}
	return args.Get(0).([]string), args.String(1), args.Error(2)
}

func (m *paramManagerMock) GetBackendType() string {
	args := m.Called()
	return args.String(0)
}

func (m *paramManagerMock) Close() error {
	args := m.Called()
	return args.Error(0)
}

// paramStoreLoggerMock is a mock implementation of interfaces.Logger.
type paramStoreLoggerMock struct {
	fields map[string]interface{}
}

func newParamStoreLoggerMock() *paramStoreLoggerMock {
	return &paramStoreLoggerMock{
		fields: make(map[string]interface{}),
	}
}

func (l *paramStoreLoggerMock) Debug(msg string, args ...any)                                           {}
func (l *paramStoreLoggerMock) Debugf(format string, args ...interface{})                  {}
func (l *paramStoreLoggerMock) Info(msg string, args ...any)                                            {}
func (l *paramStoreLoggerMock) Infof(format string, args ...interface{})                   {}
func (l *paramStoreLoggerMock) Warn(msg string, args ...any)                                            {}
func (l *paramStoreLoggerMock) Warnf(format string, args ...interface{})                   {}
func (l *paramStoreLoggerMock) Error(msg string, args ...any)                                           {}
func (l *paramStoreLoggerMock) Errorf(format string, args ...interface{})                  {}
func (l *paramStoreLoggerMock) Fatal(msg string)                                           {}
func (l *paramStoreLoggerMock) Fatalf(format string, args ...interface{})                  {}
func (l *paramStoreLoggerMock) WithField(key string, value interface{}) interfaces.Logger  { return l }
func (l *paramStoreLoggerMock) WithFields(fields map[string]interface{}) interfaces.Logger { return l }
func (l *paramStoreLoggerMock) WithError(err error) interfaces.Logger                      { return l }

// TestNewParameterStoreService tests service creation.
func TestNewParameterStoreService(t *testing.T) {
	t.Run("creates service successfully", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()

		svc, err := NewParameterStoreService(pm, logger)

		require.NoError(t, err)
		require.NotNil(t, svc)
	})

	t.Run("returns error when parameter manager is nil", func(t *testing.T) {
		logger := newParamStoreLoggerMock()

		svc, err := NewParameterStoreService(nil, logger)

		require.Error(t, err)
		assert.Nil(t, svc)
		assert.Contains(t, err.Error(), "parameter manager must not be nil")
	})

	t.Run("returns error when logger is nil", func(t *testing.T) {
		pm := newParamManagerMock()

		svc, err := NewParameterStoreService(pm, nil)

		require.Error(t, err)
		assert.Nil(t, svc)
		assert.Contains(t, err.Error(), "logger must not be nil")
	})
}

// TestParameterStoreService_GetParameter tests GetParameter method.
func TestParameterStoreService_GetParameter(t *testing.T) {
	t.Run("gets parameter successfully", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		metadata := &interfaces.ParameterMetadata{
			Type:        "string",
			Description: "test parameter",
			Tags:        map[string]string{"env": "test"},
			IsEncrypted: false,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		pm.On("Get", mock.Anything, "/app/config", true).
			Return([]byte("test-value"), metadata, nil)

		resp, err := svc.GetParameter(context.Background(), &interfaces.GetParameterRequest{
			Path:           "/app/config",
			WithDecryption: true,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "/app/config", resp.Parameter.Path)
		assert.Equal(t, []byte("test-value"), resp.Parameter.Value)
		pm.AssertExpectations(t)
	})

	t.Run("gets encrypted parameter", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		metadata := &interfaces.ParameterMetadata{
			Type:        "securestring",
			IsEncrypted: true,
			KMSKeyID:    "kms-key-123",
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		pm.On("Get", mock.Anything, "/app/secret", true).
			Return([]byte("decrypted-value"), metadata, nil)

		resp, err := svc.GetParameter(context.Background(), &interfaces.GetParameterRequest{
			Path:           "/app/secret",
			WithDecryption: true,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.True(t, resp.Parameter.IsEncrypted)
		pm.AssertExpectations(t)
	})

	t.Run("returns error when request is nil", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		resp, err := svc.GetParameter(context.Background(), nil)

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "request is nil")
	})

	t.Run("returns error when path is empty", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		resp, err := svc.GetParameter(context.Background(), &interfaces.GetParameterRequest{
			Path: "",
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "path is required")
	})

	t.Run("returns error when service is closed", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		err = svc.(*parameterStoreService).Close()
		require.NoError(t, err)

		resp, err := svc.GetParameter(context.Background(), &interfaces.GetParameterRequest{
			Path: "/app/config",
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "service is closed")
	})

	t.Run("propagates manager error", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		pm.On("Get", mock.Anything, "/app/missing", true).
			Return(nil, nil, &dberrors.QBError{
				Code: dberrors.ErrNotFound,
				Op:   "test",
				Err:  nil,
			})

		resp, err := svc.GetParameter(context.Background(), &interfaces.GetParameterRequest{
			Path:           "/app/missing",
			WithDecryption: true,
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		pm.AssertExpectations(t)
	})
}

// TestParameterStoreService_SetParameter tests SetParameter method.
func TestParameterStoreService_SetParameter(t *testing.T) {
	t.Run("sets parameter successfully", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		// Mock Get to return error (parameter not found) - this means Created=true
		pm.On("Get", mock.Anything, "/app/config", false).
			Return(nil, nil, stderrors.New("not found"))
		pm.On("Store", mock.Anything, "/app/config", []byte("test-value"), mock.AnythingOfType("*interfaces.ParameterMetadata"), true).
			Return(nil)

		resp, err := svc.SetParameter(context.Background(), &interfaces.SetParameterRequest{
			Path:      "/app/config",
			Value:     []byte("test-value"),
			Type:      "string",
			Overwrite: true,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "/app/config", resp.Path)
		assert.True(t, resp.Created)
		pm.AssertExpectations(t)
	})

	t.Run("sets encrypted parameter", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		// Mock Get to return error (parameter not found) - this means Created=true
		pm.On("Get", mock.Anything, "/app/secret", false).
			Return(nil, nil, stderrors.New("not found"))
		pm.On("StoreWithOptions", mock.Anything, "/app/secret", []byte("secret-value"),
			mock.AnythingOfType("*interfaces.ParameterMetadata"), true,
			mock.AnythingOfType("*interfaces.StoreOptions")).
			Return(nil)

		resp, err := svc.SetParameter(context.Background(), &interfaces.SetParameterRequest{
			Path:      "/app/secret",
			Value:     []byte("secret-value"),
			Type:      "securestring",
			KMSKeyID:  "kms-key-123",
			Overwrite: true,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		pm.AssertExpectations(t)
	})

	t.Run("returns error when request is nil", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		resp, err := svc.SetParameter(context.Background(), nil)

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "request is nil")
	})

	t.Run("returns error when path is empty", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		resp, err := svc.SetParameter(context.Background(), &interfaces.SetParameterRequest{
			Path:  "",
			Value: []byte("value"),
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "path is required")
	})

	t.Run("returns error when value is empty", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		resp, err := svc.SetParameter(context.Background(), &interfaces.SetParameterRequest{
			Path:  "/app/config",
			Value: []byte{},
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "value is required")
	})

	t.Run("returns error when service is closed", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		err = svc.(*parameterStoreService).Close()
		require.NoError(t, err)

		resp, err := svc.SetParameter(context.Background(), &interfaces.SetParameterRequest{
			Path:  "/app/config",
			Value: []byte("value"),
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "service is closed")
	})

	t.Run("propagates manager error", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		// Mock Get to return error (parameter not found)
		pm.On("Get", mock.Anything, "/app/config", false).
			Return(nil, nil, stderrors.New("not found"))
		pm.On("Store", mock.Anything, "/app/config", []byte("value"), mock.AnythingOfType("*interfaces.ParameterMetadata"), false).
			Return(&dberrors.QBError{
				Code: dberrors.ErrAlreadyExists,
				Op:   "test",
			})

		resp, err := svc.SetParameter(context.Background(), &interfaces.SetParameterRequest{
			Path:      "/app/config",
			Value:     []byte("value"),
			Overwrite: false,
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		pm.AssertExpectations(t)
	})
}

// TestParameterStoreService_DeleteParameter tests DeleteParameter method.
func TestParameterStoreService_DeleteParameter(t *testing.T) {
	t.Run("deletes parameter successfully", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		pm.On("Delete", mock.Anything, "/app/config").Return(nil)

		resp, err := svc.DeleteParameter(context.Background(), &interfaces.DeleteParameterRequest{
			Path: "/app/config",
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "/app/config", resp.Path)
		assert.True(t, resp.Deleted)
		pm.AssertExpectations(t)
	})

	t.Run("returns error when request is nil", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		resp, err := svc.DeleteParameter(context.Background(), nil)

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "request is nil")
	})

	t.Run("returns error when path is empty", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		resp, err := svc.DeleteParameter(context.Background(), &interfaces.DeleteParameterRequest{
			Path: "",
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "path is required")
	})

	t.Run("returns error when service is closed", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		err = svc.(*parameterStoreService).Close()
		require.NoError(t, err)

		resp, err := svc.DeleteParameter(context.Background(), &interfaces.DeleteParameterRequest{
			Path: "/app/config",
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "service is closed")
	})

	t.Run("propagates manager error", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		pm.On("Delete", mock.Anything, "/app/missing").
			Return(&dberrors.QBError{
				Code: dberrors.ErrNotFound,
				Op:   "test",
			})

		resp, err := svc.DeleteParameter(context.Background(), &interfaces.DeleteParameterRequest{
			Path: "/app/missing",
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		pm.AssertExpectations(t)
	})
}

// TestParameterStoreService_GetParameters tests GetParameters method (batch get).
func TestParameterStoreService_GetParameters(t *testing.T) {
	t.Run("gets multiple parameters successfully", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		results := map[string]*interfaces.ParameterResult{
			"/app/config1": {
				Value:    []byte("value1"),
				Metadata: &interfaces.ParameterMetadata{Type: "string"},
			},
			"/app/config2": {
				Value:    []byte("value2"),
				Metadata: &interfaces.ParameterMetadata{Type: "string"},
			},
		}

		pm.On("GetBatch", mock.Anything, []string{"/app/config1", "/app/config2"}, true).
			Return(results, map[string]error(nil), nil)

		resp, err := svc.GetParameters(context.Background(), &interfaces.GetParametersRequest{
			Paths:          []string{"/app/config1", "/app/config2"},
			WithDecryption: true,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Parameters, 2)
		pm.AssertExpectations(t)
	})

	t.Run("returns empty response when paths is empty", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		resp, err := svc.GetParameters(context.Background(), &interfaces.GetParametersRequest{
			Paths: []string{},
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Empty(t, resp.Parameters)
	})

	t.Run("returns error when request is nil", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		resp, err := svc.GetParameters(context.Background(), nil)

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "request is nil")
	})

	t.Run("returns error when service is closed", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		err = svc.(*parameterStoreService).Close()
		require.NoError(t, err)

		resp, err := svc.GetParameters(context.Background(), &interfaces.GetParametersRequest{
			Paths: []string{"/app/config"},
		})

		require.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("includes partial errors in response", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		results := map[string]*interfaces.ParameterResult{
			"/app/config1": {
				Value:    []byte("value1"),
				Metadata: &interfaces.ParameterMetadata{Type: "string"},
			},
		}
		errs := map[string]error{
			"/app/config2": &dberrors.QBError{Code: dberrors.ErrNotFound},
		}

		pm.On("GetBatch", mock.Anything, []string{"/app/config1", "/app/config2"}, true).
			Return(results, errs, nil)

		resp, err := svc.GetParameters(context.Background(), &interfaces.GetParametersRequest{
			Paths:          []string{"/app/config1", "/app/config2"},
			WithDecryption: true,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Parameters, 1)
		assert.Contains(t, resp.Errors, "/app/config2")
		pm.AssertExpectations(t)
	})
}

// TestParameterStoreService_SetParameters tests SetParameters method (batch set).
func TestParameterStoreService_SetParameters(t *testing.T) {
	t.Run("sets multiple parameters successfully", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		pm.On("StoreBatch", mock.Anything, mock.AnythingOfType("[]*interfaces.ParameterEntry"), true, mock.Anything).
			Return(nil)

		resp, err := svc.SetParameters(context.Background(), &interfaces.SetParametersRequest{
			Parameters: []*interfaces.SetParameterRequest{
				{Path: "/app/config1", Value: []byte("value1")},
				{Path: "/app/config2", Value: []byte("value2")},
			},
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Results, 2)
		for _, r := range resp.Results {
			assert.True(t, r.Success)
		}
		pm.AssertExpectations(t)
	})

	t.Run("returns empty response when parameters is empty", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		resp, err := svc.SetParameters(context.Background(), &interfaces.SetParametersRequest{
			Parameters: []*interfaces.SetParameterRequest{},
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Empty(t, resp.Results)
	})

	t.Run("returns error when request is nil", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		resp, err := svc.SetParameters(context.Background(), nil)

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "request is nil")
	})

	t.Run("returns error when service is closed", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		err = svc.(*parameterStoreService).Close()
		require.NoError(t, err)

		resp, err := svc.SetParameters(context.Background(), &interfaces.SetParametersRequest{
			Parameters: []*interfaces.SetParameterRequest{
				{Path: "/app/config", Value: []byte("value")},
			},
		})

		require.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("validates individual parameters in batch", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		resp, err := svc.SetParameters(context.Background(), &interfaces.SetParametersRequest{
			Parameters: []*interfaces.SetParameterRequest{
				{Path: "/app/valid", Value: []byte("value")},
				{Path: "", Value: []byte("value")}, // Invalid: empty path
			},
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "path is required")
	})
}

// TestParameterStoreService_DeleteParameters tests DeleteParameters method (batch delete).
func TestParameterStoreService_DeleteParameters(t *testing.T) {
	t.Run("deletes multiple parameters successfully", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		results := map[string]bool{
			"/app/config1": true,
			"/app/config2": true,
		}

		pm.On("DeleteBatch", mock.Anything, []string{"/app/config1", "/app/config2"}).
			Return(results, map[string]error(nil), nil)

		resp, err := svc.DeleteParameters(context.Background(), &interfaces.DeleteParametersRequest{
			Paths: []string{"/app/config1", "/app/config2"},
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Results, 2)
		assert.Equal(t, 2, resp.DeletedCount)
		pm.AssertExpectations(t)
	})

	t.Run("returns empty response when paths is empty", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		resp, err := svc.DeleteParameters(context.Background(), &interfaces.DeleteParametersRequest{
			Paths: []string{},
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Empty(t, resp.Results)
		assert.Equal(t, 0, resp.DeletedCount)
	})

	t.Run("returns error when request is nil", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		resp, err := svc.DeleteParameters(context.Background(), nil)

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "request is nil")
	})

	t.Run("returns error when service is closed", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		err = svc.(*parameterStoreService).Close()
		require.NoError(t, err)

		resp, err := svc.DeleteParameters(context.Background(), &interfaces.DeleteParametersRequest{
			Paths: []string{"/app/config"},
		})

		require.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("includes partial errors in response", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		results := map[string]bool{
			"/app/config1": true,
			"/app/config2": false,
		}
		errs := map[string]error{
			"/app/config2": &dberrors.QBError{Code: dberrors.ErrNotFound},
		}

		pm.On("DeleteBatch", mock.Anything, []string{"/app/config1", "/app/config2"}).
			Return(results, errs, nil)

		resp, err := svc.DeleteParameters(context.Background(), &interfaces.DeleteParametersRequest{
			Paths: []string{"/app/config1", "/app/config2"},
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, 1, resp.DeletedCount)
		pm.AssertExpectations(t)
	})
}

// TestParameterStoreService_ListParameters tests ListParameters method.
func TestParameterStoreService_ListParameters(t *testing.T) {
	t.Run("lists parameters successfully", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		pm.On("List", mock.Anything, "/app/", true, 100, "").
			Return([]string{"/app/config1", "/app/config2"}, "", nil)

		resp, err := svc.ListParameters(context.Background(), &interfaces.ListParametersRequest{
			Prefix:    "/app/",
			Recursive: true,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Parameters, 2)
		assert.False(t, resp.HasMore)
		pm.AssertExpectations(t)
	})

	t.Run("lists parameters with values", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		pm.On("List", mock.Anything, "/app/", true, 100, "").
			Return([]string{"/app/config1", "/app/config2"}, "", nil)

		results := map[string]*interfaces.ParameterResult{
			"/app/config1": {
				Value:    []byte("value1"),
				Metadata: &interfaces.ParameterMetadata{Type: "string"},
			},
			"/app/config2": {
				Value:    []byte("value2"),
				Metadata: &interfaces.ParameterMetadata{Type: "string"},
			},
		}

		pm.On("GetBatch", mock.Anything, []string{"/app/config1", "/app/config2"}, false).
			Return(results, map[string]error(nil), nil)

		resp, err := svc.ListParameters(context.Background(), &interfaces.ListParametersRequest{
			Prefix:     "/app/",
			Recursive:  true,
			WithValues: true,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Parameters, 2)
		pm.AssertExpectations(t)
	})

	t.Run("returns pagination cursor", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		pm.On("List", mock.Anything, "/app/", true, 10, "").
			Return([]string{"/app/config1"}, "next-cursor", nil)

		resp, err := svc.ListParameters(context.Background(), &interfaces.ListParametersRequest{
			Prefix:    "/app/",
			Recursive: true,
			Limit:     10,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "next-cursor", resp.NextCursor)
		assert.True(t, resp.HasMore)
		pm.AssertExpectations(t)
	})

	t.Run("returns error when request is nil", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		resp, err := svc.ListParameters(context.Background(), nil)

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "request is nil")
	})

	t.Run("returns error when service is closed", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		err = svc.(*parameterStoreService).Close()
		require.NoError(t, err)

		resp, err := svc.ListParameters(context.Background(), &interfaces.ListParametersRequest{
			Prefix: "/app/",
		})

		require.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("uses default limit when not specified", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		pm.On("List", mock.Anything, "/app/", false, 100, "").
			Return([]string{}, "", nil)

		resp, err := svc.ListParameters(context.Background(), &interfaces.ListParametersRequest{
			Prefix: "/app/",
			Limit:  0, // Should default to 100
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		pm.AssertExpectations(t)
	})
}

// TestParameterStoreService_SearchByTags tests SearchByTags method.
func TestParameterStoreService_SearchByTags(t *testing.T) {
	t.Run("searches parameters by tags successfully", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		tags := map[string]string{"env": "production"}
		pm.On("SearchByTags", mock.Anything, tags, "", 100, "").
			Return([]string{"/app/config1", "/app/config2"}, "", nil)

		resp, err := svc.SearchByTags(context.Background(), &interfaces.SearchByTagsRequest{
			Tags: tags,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Parameters, 2)
		pm.AssertExpectations(t)
	})

	t.Run("searches with prefix filter", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		tags := map[string]string{"env": "production"}
		pm.On("SearchByTags", mock.Anything, tags, "/app/", 100, "").
			Return([]string{"/app/config1"}, "", nil)

		resp, err := svc.SearchByTags(context.Background(), &interfaces.SearchByTagsRequest{
			Tags:   tags,
			Prefix: "/app/",
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Parameters, 1)
		pm.AssertExpectations(t)
	})

	t.Run("searches with values", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		tags := map[string]string{"env": "production"}
		pm.On("SearchByTags", mock.Anything, tags, "", 100, "").
			Return([]string{"/app/config1"}, "", nil)

		results := map[string]*interfaces.ParameterResult{
			"/app/config1": {
				Value:    []byte("value1"),
				Metadata: &interfaces.ParameterMetadata{Type: "string"},
			},
		}
		pm.On("GetBatch", mock.Anything, []string{"/app/config1"}, false).
			Return(results, map[string]error(nil), nil)

		resp, err := svc.SearchByTags(context.Background(), &interfaces.SearchByTagsRequest{
			Tags:       tags,
			WithValues: true,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Parameters, 1)
		pm.AssertExpectations(t)
	})

	t.Run("returns error when request is nil", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		resp, err := svc.SearchByTags(context.Background(), nil)

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "request is nil")
	})

	t.Run("returns error when tags is empty", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		resp, err := svc.SearchByTags(context.Background(), &interfaces.SearchByTagsRequest{
			Tags: map[string]string{},
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "at least one tag is required")
	})

	t.Run("returns error when service is closed", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		err = svc.(*parameterStoreService).Close()
		require.NoError(t, err)

		resp, err := svc.SearchByTags(context.Background(), &interfaces.SearchByTagsRequest{
			Tags: map[string]string{"env": "prod"},
		})

		require.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestParameterStoreService_Close tests Close method.
func TestParameterStoreService_Close(t *testing.T) {
	t.Run("closes service successfully", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		err = svc.(*parameterStoreService).Close()
		require.NoError(t, err)
	})

	t.Run("returns error when closing already closed service", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		err = svc.(*parameterStoreService).Close()
		require.NoError(t, err)

		err = svc.(*parameterStoreService).Close()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already closed")
	})
}

// TestValidateParameterPath tests path validation.
func TestValidateParameterPath(t *testing.T) {
	t.Run("valid paths", func(t *testing.T) {
		validPaths := []string{
			"/",
			"/app",
			"/app/config",
			"/app/config/database",
			"/app/config/database/host",
			"/a",
			"/app-name",
			"/app_name",
			"/app.name",
		}

		for _, path := range validPaths {
			err := validateParameterPath(path)
			assert.NoError(t, err, "path should be valid: %s", path)
		}
	})

	t.Run("path must start with /", func(t *testing.T) {
		err := validateParameterPath("app/config")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path must start with /")
	})

	t.Run("path cannot end with /", func(t *testing.T) {
		err := validateParameterPath("/app/config/")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path cannot end with /")
	})

	t.Run("path cannot contain //", func(t *testing.T) {
		err := validateParameterPath("/app//config")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path cannot contain //")
	})

	t.Run("empty path is invalid", func(t *testing.T) {
		err := validateParameterPath("")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path is required")
	})

	t.Run("root path is valid", func(t *testing.T) {
		err := validateParameterPath("/")
		assert.NoError(t, err)
	})
}

// TestBuildMetadata tests metadata building.
func TestBuildMetadata(t *testing.T) {
	t.Run("builds metadata with all fields", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		service := svc.(*parameterStoreService)

		req := &interfaces.SetParameterRequest{
			Path:         "/app/config",
			Value:        []byte("value"),
			Type:         "json",
			Description:  "Test config",
			KMSKeyID:     "kms-key-123",
			Tags:         map[string]string{"env": "test"},
			TTLSeconds:   3600,
			AuditEnabled: true,
		}

		metadata := service.buildMetadata(context.Background(), req)

		assert.Equal(t, "json", metadata.Type)
		assert.Equal(t, "Test config", metadata.Description)
		assert.Equal(t, "kms-key-123", metadata.KMSKeyID)
		assert.True(t, metadata.IsEncrypted)
		assert.Equal(t, int64(3600), metadata.TTLSeconds)
		assert.True(t, metadata.AuditEnabled)
		assert.NotNil(t, metadata.ExpiresAt)
		assert.NotZero(t, metadata.CreatedAt)
		assert.NotZero(t, metadata.UpdatedAt)
	})

	t.Run("defaults type to string", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		service := svc.(*parameterStoreService)

		req := &interfaces.SetParameterRequest{
			Path:  "/app/config",
			Value: []byte("value"),
			Type:  "", // Empty
		}

		metadata := service.buildMetadata(context.Background(), req)

		assert.Equal(t, "string", metadata.Type)
	})

	t.Run("extracts user from context", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		service := svc.(*parameterStoreService)
		ctx := iam.ContextWithUser(context.Background(), "user-123")

		req := &interfaces.SetParameterRequest{
			Path:  "/app/config",
			Value: []byte("value"),
		}

		metadata := service.buildMetadata(ctx, req)

		assert.Equal(t, "user-123", metadata.CreatedBy)
		assert.Equal(t, "user-123", metadata.UpdatedBy)
	})

	t.Run("no expiration when TTL is zero", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		service := svc.(*parameterStoreService)

		req := &interfaces.SetParameterRequest{
			Path:       "/app/config",
			Value:      []byte("value"),
			TTLSeconds: 0,
		}

		metadata := service.buildMetadata(context.Background(), req)

		assert.Nil(t, metadata.ExpiresAt)
	})
}

// TestSetParameterContext tests ABAC context setting.
func TestSetParameterContext(t *testing.T) {
	t.Run("sets parameter context with all fields", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		service := svc.(*parameterStoreService)

		ctx := iam.ContextWithTenant(context.Background(), "tenant-123")
		ctx = iam.ContextWithUser(ctx, "user-456")

		tags := map[string]string{"env": "prod"}
		resultCtx := service.setParameterContext(ctx, "/app/config", "string", iam.ParameterOperationSet, true, tags)

		paramCtx := iam.ParameterContextFromContext(resultCtx)
		require.NotNil(t, paramCtx)
		assert.Equal(t, "/app/config", paramCtx.Path)
		assert.Equal(t, "string", paramCtx.Type)
		assert.Equal(t, iam.ParameterOperationSet, paramCtx.Operation)
		assert.True(t, paramCtx.IsEncrypted)
		assert.Equal(t, "tenant-123", paramCtx.TenantID)
		assert.Equal(t, "user-456", paramCtx.Owner)
		assert.Equal(t, tags, paramCtx.Tags)
	})

	t.Run("handles missing tenant context", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		service := svc.(*parameterStoreService)

		resultCtx := service.setParameterContext(context.Background(), "/app/config", "", iam.ParameterOperationGet, false, nil)

		paramCtx := iam.ParameterContextFromContext(resultCtx)
		require.NotNil(t, paramCtx)
		assert.Empty(t, paramCtx.TenantID)
		assert.Empty(t, paramCtx.Owner)
	})
}

// TestMetadataToParameterValue tests conversion from metadata to parameter value.
func TestMetadataToParameterValue(t *testing.T) {
	t.Run("converts all fields correctly", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		service := svc.(*parameterStoreService)

		now := time.Now()
		expiresAt := now.Add(time.Hour)
		metadata := &interfaces.ParameterMetadata{
			Type:         "json",
			Description:  "Test",
			Tags:         map[string]string{"env": "test"},
			KMSKeyID:     "kms-123",
			IsEncrypted:  true,
			ExpiresAt:    &expiresAt,
			AuditEnabled: true,
			CreatedAt:    now,
			UpdatedAt:    now,
			CreatedBy:    "user-1",
			UpdatedBy:    "user-2",
		}

		pv := service.metadataToParameterValue("/app/config", []byte("value"), metadata)

		assert.Equal(t, "/app/config", pv.Path)
		assert.Equal(t, []byte("value"), pv.Value)
		assert.Equal(t, "json", pv.Type)
		assert.Equal(t, "Test", pv.Description)
		assert.Equal(t, metadata.Tags, pv.Tags)
		assert.Equal(t, "kms-123", pv.KMSKeyID)
		assert.True(t, pv.IsEncrypted)
		assert.Equal(t, &expiresAt, pv.ExpiresAt)
		assert.True(t, pv.AuditEnabled)
		assert.Equal(t, now, pv.CreatedAt)
		assert.Equal(t, now, pv.UpdatedAt)
		assert.Equal(t, "user-1", pv.CreatedBy)
		assert.Equal(t, "user-2", pv.UpdatedBy)
	})
}

// TestParameterStoreService_ConcurrentOperations tests thread safety.
func TestParameterStoreService_ConcurrentOperations(t *testing.T) {
	t.Run("handles concurrent operations safely", func(t *testing.T) {
		pm := newParamManagerMock()
		logger := newParamStoreLoggerMock()
		svc, err := NewParameterStoreService(pm, logger)
		require.NoError(t, err)

		// Setup mocks for concurrent operations
		pm.On("Get", mock.Anything, mock.AnythingOfType("string"), true).
			Return([]byte("value"), &interfaces.ParameterMetadata{Type: "string"}, nil)

		done := make(chan bool)
		for i := 0; i < 10; i++ {
			go func(idx int) {
				ctx := context.Background()
				_, _ = svc.GetParameter(ctx, &interfaces.GetParameterRequest{
					Path:           "/app/config",
					WithDecryption: true,
				})
				done <- true
			}(i)
		}

		// Wait for all goroutines
		for i := 0; i < 10; i++ {
			<-done
		}
	})
}
