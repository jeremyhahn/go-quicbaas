//go:build rest

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

package rest

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/backup"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

// requireServerFailure asserts w carries the sanitized internal-error
// envelope a handler writes when server data does not fit the type of its
// response field.
func requireServerFailure(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusInternalServerError, w.Code)
	var resp ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, errors.ErrInternal.String(), resp.Code)
	require.Contains(t, resp.Error, ErrMsgServerFailure)
	require.NotContains(t, resp.Error, "overflow", "the conversion cause must reach the log, not the caller")
}

// requireInvalidField asserts w carries the 400 validation envelope a
// handler writes when a request field does not fit the type the service
// request uses, and that the message names the field.
func requireInvalidField(t *testing.T, w *httptest.ResponseRecorder, field string) {
	t.Helper()
	require.Equal(t, http.StatusBadRequest, w.Code)
	var resp ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Contains(t, resp.Error, "invalid "+field)
}

func TestBackupInfoToProto_RefusesShardCountBeyondInt32(t *testing.T) {
	tests := []struct {
		name      string
		numShards int
	}{
		{name: "one past max int32", numShards: math.MaxInt32 + 1},
		{name: "one below min int32", numShards: math.MinInt32 - 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := backupInfoToProto(&backup.Backup{
				ID:       "b1",
				Metadata: &backup.BackupMetadata{NumShards: tt.numShards},
			})

			require.Nil(t, info)
			var qbErr *errors.QBError
			require.ErrorAs(t, err, &qbErr)
			require.Equal(t, errors.ErrInternal, qbErr.Code)
			var overflow *conv.ConversionOverflowError
			require.ErrorAs(t, err, &overflow)
		})
	}
}

func TestBackupInfoToProto_CarriesShardCount(t *testing.T) {
	info, err := backupInfoToProto(&backup.Backup{
		ID:       "b1",
		Metadata: &backup.BackupMetadata{NumShards: math.MaxInt32},
	})

	require.NoError(t, err)
	require.Equal(t, "b1", info.BackupId)
	require.Equal(t, int32(math.MaxInt32), info.NumShards)
}

func TestKMSHandlers_RefuseKeyVersionBeyondInt(t *testing.T) {
	huge := &service.KeyHandle{KeyID: "k1", Version: math.MaxUint64}
	fine := &service.KeyHandle{KeyID: "k1", Version: 1}
	hugeMetadata := &service.KeyMetadata{KeyID: "k1", Version: math.MaxUint64}
	fineMetadata := &service.KeyMetadata{KeyID: "k1", Version: 1}

	tests := []struct {
		name      string
		method    string
		path      string
		body      string
		setupMock func(*testutil.MockQuicBaaS)
	}{
		{
			name:   "get key with key version beyond int",
			method: http.MethodGet,
			path:   "/api/v1/kms/keys/k1",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetKey", mock.Anything, mock.Anything).
					Return(&service.GetKeyResponse{Key: huge, Metadata: fineMetadata}, nil).Once()
			},
		},
		{
			name:   "get key with metadata version beyond int",
			method: http.MethodGet,
			path:   "/api/v1/kms/keys/k1",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetKey", mock.Anything, mock.Anything).
					Return(&service.GetKeyResponse{Key: fine, Metadata: hugeMetadata}, nil).Once()
			},
		},
		{
			name:   "list keys with a key version beyond int",
			method: http.MethodGet,
			path:   "/api/v1/kms/keys",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ListKeys", mock.Anything, mock.Anything).
					Return(&service.ListKeysResponse{Keys: []*service.KeyHandle{fine, huge}}, nil).Once()
			},
		},
		{
			name:   "key metadata version beyond int",
			method: http.MethodGet,
			path:   "/api/v1/kms/keys/k1/metadata",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetKeyMetadata", mock.Anything, mock.Anything).
					Return(&service.GetKeyMetadataResponse{Metadata: hugeMetadata}, nil).Once()
			},
		},
		{
			name:   "unwrapped key version beyond int",
			method: http.MethodPost,
			path:   "/api/v1/kms/keys/unwrap",
			body:   `{"wrapped_key":"AQID","wrapping_key_id":"wrap-1"}`,
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("UnwrapKey", mock.Anything, mock.Anything).
					Return(&service.UnwrapKeyResponse{KeyID: "k1", Key: huge}, nil).Once()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			w := httptest.NewRecorder()
			server.GetRouter().ServeHTTP(w, req)

			requireServerFailure(t, w)
		})
	}
}

func TestHandleHealth_RefusesUptimeBeyondDuration(t *testing.T) {
	tests := []struct {
		name          string
		uptimeSeconds uint64
	}{
		{name: "beyond int64", uptimeSeconds: math.MaxUint64},
		{name: "fits int64 but not a duration", uptimeSeconds: math.MaxInt64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()

			mockService.On("Health", mock.Anything, mock.Anything).Return(&service.HealthResponse{
				Status:        service.HealthStatusHealthy,
				UptimeSeconds: tt.uptimeSeconds,
			}, nil).Once()

			w := httptest.NewRecorder()
			server.HandleHealth(w, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

			requireServerFailure(t, w)
		})
	}
}
