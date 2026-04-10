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
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/mock"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/routing"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func newTestServer(t *testing.T) (*Server, *testutil.MockQuicBaaS) {
	t.Helper()

	mockService := testutil.NewMockQuicBaaS()
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json"})
	s, err := NewServer(mockService, &Config{Address: ":0", ExposeShardHeader: true}, log)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return s, mockService
}

func TestHandleList_ShardHeaderAndRouting(t *testing.T) {
	s, mockSvc := newTestServer(t)
	// Configure shard router with first shard = 2
	r := routing.NewWithShards([]uint64{2, 3})
	s.SetShardRouter(r)

	// Setup mock to handle List call
	mockSvc.On("List", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.ListResponse)
			close(stream)
		}).Return(nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/entities/foo?limit=1", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	res := w.Result()
	if got := res.Header.Get("X-QuicBaaS-Shard"); got != "2" {
		t.Fatalf("expected shard header 2, got %q", got)
	}
}

func TestHandlePut_RoutesByID_SetsHeader(t *testing.T) {
	s, mockSvc := newTestServer(t)
	r := routing.NewWithShards([]uint64{1, 2, 3})
	s.SetShardRouter(r)

	// Setup mock to handle Put call
	mockSvc.On("Put", mock.Anything, mock.Anything).Return(&service.PutResponse{
		ID:        123,
		Version:   1,
		Timestamp: 12345,
	}, nil).Once()

	body, _ := json.Marshal(map[string]interface{}{"data": map[string]string{"x": "y"}})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/entities/foo/123", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	res := w.Result()
	if got := res.Header.Get("X-QuicBaaS-Shard"); got != "1" {
		t.Fatalf("expected shard header 1, got %q", got)
	}
}
