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

package grpc

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// deploymentDetail stands in for the server-side cause a server-class error
// carries; it must reach the log and never the caller.
const deploymentDetail = "open /var/lib/quicbaas/shard-3: permission denied"

// requireStatus asserts err is a gRPC status with the wanted code and returns
// its message.
func requireStatus(t *testing.T, err error, want codes.Code) string {
	t.Helper()
	require.Error(t, err, "a failed service call must not be reported as success")
	st, ok := status.FromError(err)
	require.True(t, ok, "error must be a gRPC status: %v", err)
	assert.Equal(t, want, st.Code())
	return st.Message()
}

// TestList_ServiceErrorReturnsStatus: a List whose service call fails after
// streaming one page must still fail the RPC, so the caller cannot mistake a
// truncated listing for a complete one. A server-class cause is not echoed.
func TestList_ServiceErrorReturnsStatus(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)
	mockSvc.On("List", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.ListResponse)
			stream <- &service.ListResponse{
				Entities: []*service.Entity{{EntityType: "users", ID: 1}},
			}
			close(stream)
		}).
		Return(&errors.QBError{
			Code: errors.ErrResourceExhausted,
			Op:   "kv.List",
			Err:  stderrors.New(deploymentDetail),
		})

	stream := NewMockListStreamHandler(context.Background())
	err := srv.List(&pb.ListRequest{EntityType: "users", PageSize: 100}, stream)

	msg := requireStatus(t, err, codes.ResourceExhausted)
	assert.Equal(t, errMsgServerFailure, msg)
	assert.Len(t, stream.GetMessages(), 1, "the page streamed before the failure is still delivered")
	mockSvc.AssertExpectations(t)
}

// TestQuery_ServiceErrorReturnsStatus: a request-class failure keeps its code
// and its text, so the caller can correct the request.
func TestQuery_ServiceErrorReturnsStatus(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)
	mockSvc.On("Query", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			close(args.Get(2).(chan<- *service.QueryResponse))
		}).
		Return(&errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "query.Query",
			Err:  stderrors.New("index email_idx does not exist"),
		})

	stream := NewMockQueryStreamHandler(context.Background())
	err := srv.Query(&pb.QueryRequest{
		EntityType: "users",
		IndexName:  "email_idx",
		Value:      []byte("alice@example.com"),
	}, stream)

	msg := requireStatus(t, err, codes.InvalidArgument)
	assert.Contains(t, msg, "index email_idx does not exist")
	assert.Empty(t, stream.GetMessages())
	mockSvc.AssertExpectations(t)
}

// TestScanIndex_ServiceErrorReturnsStatus: an untyped service error is a
// server failure: Internal, with the generic message.
func TestScanIndex_ServiceErrorReturnsStatus(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)
	mockSvc.On("ScanIndex", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			close(args.Get(2).(chan<- *service.ScanResponse))
		}).
		Return(stderrors.New(deploymentDetail))

	stream := NewMockScanStreamHandler(context.Background())
	err := srv.ScanIndex(&pb.ScanRequest{EntityType: "users", IndexName: "email_idx"}, stream)

	msg := requireStatus(t, err, codes.Internal)
	assert.Equal(t, errMsgServerFailure, msg)
	assert.Empty(t, stream.GetMessages())
	mockSvc.AssertExpectations(t)
}

// TestTimeSeriesQuery_ServiceErrorReturnsStatus: a server-class QBError keeps
// its code, so a caller can still retry an unavailable shard.
func TestTimeSeriesQuery_ServiceErrorReturnsStatus(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)
	mockSvc.On("TimeSeriesQuery", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			close(args.Get(2).(chan<- *service.TimeSeriesQueryResponse))
		}).
		Return(&errors.QBError{
			Code: errors.ErrUnavailable,
			Op:   "timeseries.Query",
			Err:  stderrors.New(deploymentDetail),
		})

	stream := NewMockTimeSeriesStreamHandler(context.Background())
	err := srv.TimeSeriesQuery(&pb.TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  1000,
		EndTime:    2000,
	}, stream)

	msg := requireStatus(t, err, codes.Unavailable)
	assert.Equal(t, errMsgServerFailure, msg)
	assert.Empty(t, stream.GetMessages())
	mockSvc.AssertExpectations(t)
}

func TestToSanitizedServiceError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
		wantMsg  string
	}{
		{
			name:     "request class echoes its text",
			err:      &errors.QBError{Code: errors.ErrNotFound, Op: "kv.Get", Err: stderrors.New("entity users/7")},
			wantCode: codes.NotFound,
			wantMsg:  "entity users/7",
		},
		{
			name:     "server class keeps its code and hides its cause",
			err:      &errors.QBError{Code: errors.ErrDataLoss, Op: "kv.List", Err: stderrors.New(deploymentDetail)},
			wantCode: codes.DataLoss,
			wantMsg:  errMsgServerFailure,
		},
		{
			name:     "untyped error is internal",
			err:      stderrors.New(deploymentDetail),
			wantCode: codes.Internal,
			wantMsg:  errMsgServerFailure,
		},
		{
			name:     "canceled context maps to Canceled",
			err:      context.Canceled,
			wantCode: codes.Canceled,
			wantMsg:  context.Canceled.Error(),
		},
		{
			name:     "existing status passes through",
			err:      status.Error(codes.Aborted, "retry the transaction"),
			wantCode: codes.Aborted,
			wantMsg:  "retry the transaction",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := requireStatus(t, toSanitizedServiceError(tt.err), tt.wantCode)
			assert.Contains(t, msg, tt.wantMsg)
			if tt.wantMsg == errMsgServerFailure {
				assert.NotContains(t, msg, deploymentDetail)
			}
		})
	}
}

func TestToSanitizedServiceError_Nil(t *testing.T) {
	assert.NoError(t, toSanitizedServiceError(nil))
}

func TestStartServiceStream(t *testing.T) {
	t.Run("results then nil error", func(t *testing.T) {
		results, errChan := startServiceStream(func(out chan<- int) error {
			defer close(out)
			out <- 1
			out <- 2
			return nil
		})
		var got []int
		for r := range results {
			got = append(got, r)
		}
		assert.Equal(t, []int{1, 2}, got)
		assert.NoError(t, <-errChan)
	})

	t.Run("error is delivered after the results close", func(t *testing.T) {
		callErr := stderrors.New("scan failed")
		results, errChan := startServiceStream(func(out chan<- int) error {
			close(out)
			return callErr
		})
		for range results {
			t.Fatal("no result expected")
		}
		assert.ErrorIs(t, <-errChan, callErr)
	})
}
