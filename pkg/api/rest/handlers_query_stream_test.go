//go:build rest

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// serveAfterStreamClosed serves req while the service double has closed its
// stream but not yet returned, which is the production order of events for a
// failing call. Inside the synctest bubble, synctest.Wait returns only once
// the handler has either answered or is blocked waiting for the verdict, so
// a handler that treats the closed stream as success is caught without
// depending on the scheduler. The service is let go before any failure is
// reported, so the bubble always unwinds.
func serveAfterStreamClosed(t *testing.T, server *Server, req *http.Request, release chan struct{}) *httptest.ResponseRecorder {
	t.Helper()

	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		server.GetRouter().ServeHTTP(w, req)
		close(done)
	}()

	synctest.Wait()
	answeredEarly := false
	select {
	case <-done:
		answeredEarly = true
	default:
	}

	close(release)
	<-done
	if answeredEarly {
		t.Fatalf("answered HTTP %d before the service returned its verdict", w.Code)
	}
	return w
}

func internalServiceError(op string) error {
	return &errors.QBError{
		Code: errors.ErrInternal,
		Op:   op,
		Err:  stderrors.New("internal error"),
	}
}

// TestHandleQuery_ServiceErrorSurvivesAClosedStream: a failing Query closes
// its stream and only then returns its error. HandleQuery used to read the
// closed stream as an empty result and answer HTTP 200.
func TestHandleQuery_ServiceErrorSurvivesAClosedStream(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		mockService.On("Query", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				close(args.Get(2).(chan<- *service.QueryResponse))
				<-release
			}).Return(internalServiceError("Query")).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/query/users/email?value=alice@example.com", nil)
		w := serveAfterStreamClosed(t, server, req, release)

		require.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// TestHandleRangeQuery_ServiceErrorSurvivesAClosedStream is the ScanIndex
// counterpart: HandleRangeQuery used to stop at the closed stream and answer
// HTTP 200 with whatever it had collected.
func TestHandleRangeQuery_ServiceErrorSurvivesAClosedStream(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	body, err := json.Marshal(RangeQueryRequest{EntityType: "users", Index: "age", Start: 20, End: 30})
	require.NoError(t, err)

	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		mockService.On("ScanIndex", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				close(args.Get(2).(chan<- *service.ScanResponse))
				<-release
			}).Return(internalServiceError("ScanIndex")).Once()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/query/range", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := serveAfterStreamClosed(t, server, req, release)

		require.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// TestHandleRangeQuery_AnswersOnceTheDoubleClosesStream pins the ScanIndex
// contract on the func-based double: the call closes stream before it
// returns, and HandleRangeQuery answers with the call's verdict. The request
// carries a deadline inside a synctest bubble, so a double or handler that
// leaves stream open answers 408 at once instead of hanging the package.
func TestHandleRangeQuery_AnswersOnceTheDoubleClosesStream(t *testing.T) {
	tests := []struct {
		name       string
		scanIndex  func(context.Context, *service.ScanRequest, chan<- *service.ScanResponse) error
		wantStatus int
	}{
		{
			name: "service error",
			scanIndex: func(_ context.Context, _ *service.ScanRequest, stream chan<- *service.ScanResponse) error {
				defer close(stream)
				return internalServiceError("ScanIndex")
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "default double",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServerWithFuncMock(t)
			defer cleanup()
			mockService.ScanIndexFunc = tt.scanIndex

			body, err := json.Marshal(RangeQueryRequest{EntityType: "users", Index: "age", Start: 20, End: 30})
			require.NoError(t, err)

			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()

				req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/query/range", bytes.NewReader(body))
				w := httptest.NewRecorder()
				server.HandleRangeQuery(w, req)

				require.Equal(t, tt.wantStatus, w.Code)
			})
		})
	}
}

// TestHandleRangeQuery_ExpiredContextPrecedence pins go-quicbaas#1326 for
// ScanIndex: once the request context has expired, a page already received
// wins, then a service error, then a timeout. The double expires the context
// itself before it returns, so the handler sees both a finished call and an
// expired context; the old final select picked between them at random, so
// each case runs often enough to make a wrong pick certain.
func TestHandleRangeQuery_ExpiredContextPrecedence(t *testing.T) {
	const iterations = 500

	tests := []struct {
		name       string
		scanIndex  func(cancel context.CancelFunc) func(context.Context, *service.ScanRequest, chan<- *service.ScanResponse) error
		wantStatus int
		wantCount  int
	}{
		{
			name: "empty completion after expiry is a timeout",
			scanIndex: func(cancel context.CancelFunc) func(context.Context, *service.ScanRequest, chan<- *service.ScanResponse) error {
				return func(_ context.Context, _ *service.ScanRequest, stream chan<- *service.ScanResponse) error {
					cancel()
					close(stream)
					return nil
				}
			},
			wantStatus: http.StatusRequestTimeout,
		},
		{
			name: "service error beats an expired context",
			scanIndex: func(cancel context.CancelFunc) func(context.Context, *service.ScanRequest, chan<- *service.ScanResponse) error {
				return func(_ context.Context, _ *service.ScanRequest, stream chan<- *service.ScanResponse) error {
					close(stream)
					cancel()
					return internalServiceError("ScanIndex")
				}
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "page received before expiry beats a timeout",
			scanIndex: func(cancel context.CancelFunc) func(context.Context, *service.ScanRequest, chan<- *service.ScanResponse) error {
				return func(_ context.Context, _ *service.ScanRequest, stream chan<- *service.ScanResponse) error {
					stream <- &service.ScanResponse{Entities: []*service.Entity{{ID: 1, EntityType: "users"}}}
					close(stream)
					cancel()
					return nil
				}
			},
			wantStatus: http.StatusOK,
			wantCount:  1,
		},
	}

	body, err := json.Marshal(RangeQueryRequest{EntityType: "users", Index: "age", Start: 20, End: 30})
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServerWithFuncMock(t)
			defer cleanup()

			for i := range iterations {
				ctx, cancel := context.WithCancel(context.Background())
				mockService.ScanIndexFunc = tt.scanIndex(cancel)

				req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/query/range", bytes.NewReader(body))
				w := httptest.NewRecorder()
				server.HandleRangeQuery(w, req)
				cancel()

				if w.Code != tt.wantStatus {
					t.Fatalf("iteration %d: status = %d, want %d", i, w.Code, tt.wantStatus)
				}
				if tt.wantStatus == http.StatusOK {
					var resp QueryResponse
					require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
					require.Equal(t, tt.wantCount, resp.Count, "iteration %d", i)
				}
			}
		})
	}
}
