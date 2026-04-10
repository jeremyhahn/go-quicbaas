//go:build rest && !timeseries

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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func TestTimeseriesNoopHandlers(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()
	server, err := NewServer(mockSvc, DefaultConfig(), log)
	require.NoError(t, err)
	defer server.Close()

	tests := []struct {
		name     string
		method   string
		path     string
		handler  func(w http.ResponseWriter, r *http.Request)
		wantCode int
	}{
		{
			name:     "HandleWritePoint returns not implemented",
			method:   http.MethodPost,
			path:     "/api/v1/timeseries/write",
			handler:  server.HandleWritePoint,
			wantCode: http.StatusNotImplemented,
		},
		{
			name:     "HandleWriteBatch returns not implemented",
			method:   http.MethodPost,
			path:     "/api/v1/timeseries/batch",
			handler:  server.HandleWriteBatch,
			wantCode: http.StatusNotImplemented,
		},
		{
			name:     "HandleTSQuery returns not implemented",
			method:   http.MethodPost,
			path:     "/api/v1/timeseries/query",
			handler:  server.HandleTSQuery,
			wantCode: http.StatusNotImplemented,
		},
		{
			name:     "HandleDeleteSeries returns not implemented",
			method:   http.MethodDelete,
			path:     "/api/v1/timeseries/series/test",
			handler:  server.HandleDeleteSeries,
			wantCode: http.StatusNotImplemented,
		},
		{
			name:     "HandleGetRetentionPolicy returns not implemented",
			method:   http.MethodGet,
			path:     "/api/v1/timeseries/retention",
			handler:  server.HandleGetRetentionPolicy,
			wantCode: http.StatusNotImplemented,
		},
		{
			name:     "HandleSetRetentionPolicy returns not implemented",
			method:   http.MethodPut,
			path:     "/api/v1/timeseries/retention",
			handler:  server.HandleSetRetentionPolicy,
			wantCode: http.StatusNotImplemented,
		},
		{
			name:     "HandleListMetrics returns not implemented",
			method:   http.MethodGet,
			path:     "/api/v1/timeseries/metrics",
			handler:  server.HandleListMetrics,
			wantCode: http.StatusNotImplemented,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			tt.handler(w, req)

			assert.Equal(t, tt.wantCode, w.Code)

			// Verify error response contains the expected message
			var errResp ErrorResponse
			err := json.NewDecoder(w.Body).Decode(&errResp)
			require.NoError(t, err)
			assert.Contains(t, errResp.Error, "timeseries support not compiled")
		})
	}
}

func TestErrTimeseriesNotEnabled(t *testing.T) {
	assert.NotNil(t, ErrTimeseriesNotEnabled)
	assert.Contains(t, ErrTimeseriesNotEnabled.Error(), "timeseries support not compiled")
	assert.Contains(t, ErrTimeseriesNotEnabled.Error(), "-tags timeseries")
}
