//go:build timeseries

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
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	stderrors "errors"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func createTSTestServer(t *testing.T) (*Server, *testutil.MockQuicBaaS) {
	t.Helper()
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := testutil.NewMockQuicBaaS()

	srv := &Server{
		service: mockSvc,
		logger:  log,
	}

	return srv, mockSvc
}

func TestTSWritePoint(t *testing.T) {
	t.Run("successful_write_point", func(t *testing.T) {
		srv, mockSvc := createTSTestServer(t)

		mockSvc.On("WritePoint", mock.Anything, mock.Anything).
			Return(&service.WritePointResponse{Success: true}, nil).Once()

		req := &pb.TSWritePointRequest{
			Metric:    "cpu.usage",
			Tags:      map[string]string{"host": "server1"},
			Value:     75.5,
			Timestamp: time.Now().UnixNano(),
		}

		resp, err := srv.TSWritePoint(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.True(t, resp.Success)
		mockSvc.AssertExpectations(t)
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTSTestServer(t)

		_, err := srv.TSWritePoint(context.Background(), nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_metric", func(t *testing.T) {
		srv, _ := createTSTestServer(t)

		req := &pb.TSWritePointRequest{
			Value:     75.5,
			Timestamp: time.Now().UnixNano(),
		}

		_, err := srv.TSWritePoint(context.Background(), req)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTSTestServer(t)

		mockSvc.On("WritePoint", mock.Anything, mock.Anything).
			Return(nil, errors.New(errors.ErrInternal, "write", "internal error")).Once()

		req := &pb.TSWritePointRequest{
			Metric:    "cpu.usage",
			Value:     75.5,
			Timestamp: time.Now().UnixNano(),
		}

		_, err := srv.TSWritePoint(context.Background(), req)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Internal, st.Code())
	})
}

func TestTSWriteBatch(t *testing.T) {
	t.Run("successful_write_batch", func(t *testing.T) {
		srv, mockSvc := createTSTestServer(t)

		mockSvc.On("WriteBatch", mock.Anything, mock.Anything).
			Return(&service.WriteBatchResponse{Success: true, PointsWritten: 2}, nil).Once()

		req := &pb.TSWriteBatchRequest{
			Points: []*pb.TSDataPoint{
				{Metric: "cpu.usage", Value: 75.5, Timestamp: time.Now().UnixNano()},
				{Metric: "cpu.usage", Value: 80.0, Timestamp: time.Now().UnixNano()},
			},
		}

		resp, err := srv.TSWriteBatch(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.True(t, resp.Success)
		assert.Equal(t, int32(2), resp.PointsWritten)
		mockSvc.AssertExpectations(t)
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTSTestServer(t)

		_, err := srv.TSWriteBatch(context.Background(), nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty_points", func(t *testing.T) {
		srv, _ := createTSTestServer(t)

		req := &pb.TSWriteBatchRequest{
			Points: []*pb.TSDataPoint{},
		}

		_, err := srv.TSWriteBatch(context.Background(), req)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("points_written_out_of_range", func(t *testing.T) {
		srv, mockSvc := createTSTestServer(t)

		mockSvc.On("WriteBatch", mock.Anything, mock.Anything).
			Return(&service.WriteBatchResponse{Success: true, PointsWritten: math.MaxInt32 + 1}, nil).Once()

		req := &pb.TSWriteBatchRequest{
			Points: []*pb.TSDataPoint{{Metric: "cpu", Value: 1, Timestamp: time.Now().UnixNano()}},
		}

		_, err := srv.TSWriteBatch(context.Background(), req)
		requireRangeStatus(t, err, codes.Internal, "points_written")
	})
}

func TestTSQuery(t *testing.T) {
	now := time.Now()
	startTime := now.Add(-1 * time.Hour)

	t.Run("successful_query", func(t *testing.T) {
		srv, mockSvc := createTSTestServer(t)

		mockSvc.On("TSQuery", mock.Anything, mock.Anything).
			Return(&service.TSQueryResponse{
				Metric: "cpu.usage",
				Points: []*service.TSDataPoint{
					{Metric: "cpu.usage", Value: 75.5, Timestamp: startTime.Add(30 * time.Minute)},
				},
			}, nil).Once()

		req := &pb.TSQueryRequest{
			Metric:    "cpu.usage",
			StartTime: startTime.UnixNano(),
			EndTime:   now.UnixNano(),
		}

		resp, err := srv.TSQuery(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "cpu.usage", resp.Metric)
		assert.Len(t, resp.Points, 1)
		mockSvc.AssertExpectations(t)
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTSTestServer(t)

		_, err := srv.TSQuery(context.Background(), nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_metric", func(t *testing.T) {
		srv, _ := createTSTestServer(t)

		req := &pb.TSQueryRequest{
			StartTime: startTime.UnixNano(),
			EndTime:   now.UnixNano(),
		}

		_, err := srv.TSQuery(context.Background(), req)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_start_time", func(t *testing.T) {
		srv, _ := createTSTestServer(t)

		req := &pb.TSQueryRequest{
			Metric:  "cpu.usage",
			EndTime: now.UnixNano(),
		}

		_, err := srv.TSQuery(context.Background(), req)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_end_time", func(t *testing.T) {
		srv, _ := createTSTestServer(t)

		req := &pb.TSQueryRequest{
			Metric:    "cpu.usage",
			StartTime: startTime.UnixNano(),
		}

		_, err := srv.TSQuery(context.Background(), req)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestTSDeleteSeries(t *testing.T) {
	t.Run("successful_delete", func(t *testing.T) {
		srv, mockSvc := createTSTestServer(t)

		mockSvc.On("DeleteSeries", mock.Anything, mock.Anything).
			Return(&service.TSDeleteResponse{Success: true}, nil).Once()

		req := &pb.TSDeleteSeriesRequest{
			Metric: "cpu.usage",
			Tags:   map[string]string{"host": "server1"},
		}

		resp, err := srv.TSDeleteSeries(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.True(t, resp.Success)
		mockSvc.AssertExpectations(t)
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTSTestServer(t)

		_, err := srv.TSDeleteSeries(context.Background(), nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_metric", func(t *testing.T) {
		srv, _ := createTSTestServer(t)

		req := &pb.TSDeleteSeriesRequest{
			Tags: map[string]string{"host": "server1"},
		}

		_, err := srv.TSDeleteSeries(context.Background(), req)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestTSGetRetentionPolicy(t *testing.T) {
	t.Run("successful_get_retention", func(t *testing.T) {
		srv, mockSvc := createTSTestServer(t)

		mockSvc.On("GetRetentionPolicy", mock.Anything, mock.Anything).
			Return(&service.TSGetRetentionPolicyResponse{
				Metric: "cpu.usage",
				Policy: &service.TSRetentionPolicy{
					Duration:    30 * 24 * time.Hour,
					Resolution:  5 * time.Minute,
					Aggregation: "avg",
				},
			}, nil).Once()

		req := &pb.TSGetRetentionPolicyRequest{
			Metric: "cpu.usage",
		}

		resp, err := srv.TSGetRetentionPolicy(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "cpu.usage", resp.Metric)
		require.NotNil(t, resp.Policy)
		mockSvc.AssertExpectations(t)
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTSTestServer(t)

		_, err := srv.TSGetRetentionPolicy(context.Background(), nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_metric", func(t *testing.T) {
		srv, _ := createTSTestServer(t)

		req := &pb.TSGetRetentionPolicyRequest{}

		_, err := srv.TSGetRetentionPolicy(context.Background(), req)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestTSSetRetentionPolicy(t *testing.T) {
	t.Run("successful_set_retention", func(t *testing.T) {
		srv, mockSvc := createTSTestServer(t)

		mockSvc.On("SetRetentionPolicy", mock.Anything, mock.Anything).
			Return(&service.TSSetRetentionPolicyResponse{Success: true}, nil).Once()

		req := &pb.TSSetRetentionPolicyRequest{
			Metric: "cpu.usage",
			Policy: &pb.TSRetentionPolicy{
				DurationSeconds:   2592000, // 30 days
				ResolutionSeconds: 300,     // 5 minutes
				Aggregation:       "avg",
			},
		}

		resp, err := srv.TSSetRetentionPolicy(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.True(t, resp.Success)
		mockSvc.AssertExpectations(t)
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTSTestServer(t)

		_, err := srv.TSSetRetentionPolicy(context.Background(), nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_metric", func(t *testing.T) {
		srv, _ := createTSTestServer(t)

		req := &pb.TSSetRetentionPolicyRequest{
			Policy: &pb.TSRetentionPolicy{DurationSeconds: 2592000},
		}

		_, err := srv.TSSetRetentionPolicy(context.Background(), req)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_policy", func(t *testing.T) {
		srv, _ := createTSTestServer(t)

		req := &pb.TSSetRetentionPolicyRequest{
			Metric: "cpu.usage",
		}

		_, err := srv.TSSetRetentionPolicy(context.Background(), req)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestTSListMetrics(t *testing.T) {
	t.Run("successful_list_metrics", func(t *testing.T) {
		srv, mockSvc := createTSTestServer(t)

		mockSvc.On("ListMetrics", mock.Anything, mock.Anything).
			Return(&service.TSListMetricsResponse{
				Metrics: []string{"cpu.usage", "memory.used", "disk.free"},
			}, nil).Once()

		req := &pb.TSListMetricsRequest{}

		resp, err := srv.TSListMetrics(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Metrics, 3)
		mockSvc.AssertExpectations(t)
	})

	t.Run("list_metrics_with_prefix", func(t *testing.T) {
		srv, mockSvc := createTSTestServer(t)

		mockSvc.On("ListMetrics", mock.Anything, mock.Anything).
			Return(&service.TSListMetricsResponse{
				Metrics: []string{"cpu.usage", "cpu.load"},
			}, nil).Once()

		req := &pb.TSListMetricsRequest{
			Prefix: "cpu",
		}

		resp, err := srv.TSListMetrics(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Metrics, 2)
		mockSvc.AssertExpectations(t)
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTSTestServer(t)

		_, err := srv.TSListMetrics(context.Background(), nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestTSQueryWithAggregation(t *testing.T) {
	srv, mockSvc := createTSTestServer(t)
	now := time.Now()
	startTime := now.Add(-1 * time.Hour)

	aggValue := 77.5
	mockSvc.On("TSQuery", mock.Anything, mock.MatchedBy(func(req *service.TSQueryRequest) bool {
		return req.Aggregation == "avg" && req.IntervalSec == 300
	})).Return(&service.TSQueryResponse{
		Metric:          "cpu.usage",
		AggregatedValue: &aggValue,
		Points: []*service.TSDataPoint{
			{Metric: "cpu.usage", Value: 77.5, Timestamp: startTime},
		},
	}, nil).Once()

	req := &pb.TSQueryRequest{
		Metric:          "cpu.usage",
		StartTime:       startTime.UnixNano(),
		EndTime:         now.UnixNano(),
		Aggregation:     "avg",
		IntervalSeconds: 300,
	}

	resp, err := srv.TSQuery(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.HasAggregatedValue)
	assert.Equal(t, 77.5, resp.AggregatedValue)
	mockSvc.AssertExpectations(t)
}

func TestTSQueryWithGroupBy(t *testing.T) {
	srv, mockSvc := createTSTestServer(t)
	now := time.Now()
	startTime := now.Add(-1 * time.Hour)

	aggValue1 := 75.5
	aggValue2 := 82.3

	mockSvc.On("TSQuery", mock.Anything, mock.Anything).
		Return(&service.TSQueryResponse{
			Metric: "cpu.usage",
			GroupedResults: []*service.TSGroupedResult{
				{
					Tags:            map[string]string{"host": "server1"},
					AggregatedValue: &aggValue1,
					Points:          []*service.TSDataPoint{},
				},
				{
					Tags:            map[string]string{"host": "server2"},
					AggregatedValue: &aggValue2,
					Points:          []*service.TSDataPoint{},
				},
			},
		}, nil).Once()

	req := &pb.TSQueryRequest{
		Metric:      "cpu.usage",
		StartTime:   startTime.UnixNano(),
		EndTime:     now.UnixNano(),
		Aggregation: "avg",
		GroupBy:     []string{"host"},
	}

	resp, err := srv.TSQuery(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.GroupedResults, 2)
	mockSvc.AssertExpectations(t)
}

func TestTSDeleteSeriesWithTimeRange(t *testing.T) {
	srv, mockSvc := createTSTestServer(t)
	now := time.Now()
	startTime := now.Add(-24 * time.Hour)

	mockSvc.On("DeleteSeries", mock.Anything, mock.MatchedBy(func(req *service.TSDeleteRequest) bool {
		return req.StartTime != nil && req.EndTime != nil
	})).Return(&service.TSDeleteResponse{Success: true}, nil).Once()

	req := &pb.TSDeleteSeriesRequest{
		Metric:       "cpu.usage",
		HasTimeRange: true,
		StartTime:    startTime.UnixNano(),
		EndTime:      now.UnixNano(),
	}

	resp, err := srv.TSDeleteSeries(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	mockSvc.AssertExpectations(t)
}

// Note: TestTimeseriesServiceNotAvailable was removed because QuicBaaS
// already embeds TimeSeriesService, so any valid implementation of QuicBaaS
// will always have timeseries capability. The type assertion pattern is preserved
// for forward compatibility if the interface is refactored to make timeseries optional.

// --- Merged from coverage_91_target_timeseries_test.go ---

// =============================================================================
// TSQuery with GroupedResults and AggregatedValue
// =============================================================================

func TestServer_TSQuery_WithGroupedResults(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	aggVal := 42.5
	groupAggVal := 21.25
	now := time.Now()

	mockSvc.On("TSQuery", mock.Anything, mock.Anything).
		Return(&service.TSQueryResponse{
			Metric: "cpu",
			Tags:   map[string]string{"host": "srv1"},
			Points: []*service.TSDataPoint{
				{Metric: "cpu", Value: 99.0, Timestamp: now},
			},
			AggregatedValue: &aggVal,
			GroupedResults: []*service.TSGroupedResult{
				{
					Tags: map[string]string{"region": "us-east"},
					Points: []*service.TSDataPoint{
						{Metric: "cpu", Value: 95.0, Timestamp: now},
					},
					AggregatedValue: &groupAggVal,
				},
				{
					Tags: map[string]string{"region": "eu-west"},
					Points: []*service.TSDataPoint{
						{Metric: "cpu", Value: 88.0, Timestamp: now},
					},
					AggregatedValue: nil,
				},
			},
			Timestamp: now,
		}, nil)

	resp, err := srv.TSQuery(context.Background(), &pb.TSQueryRequest{
		Metric:    "cpu",
		StartTime: 1,
		EndTime:   time.Now().UnixNano(),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "cpu", resp.Metric)
	assert.True(t, resp.HasAggregatedValue)
	assert.Equal(t, 42.5, resp.AggregatedValue)
	assert.Len(t, resp.Points, 1)
	require.Len(t, resp.GroupedResults, 2)
	assert.True(t, resp.GroupedResults[0].HasAggregatedValue)
	assert.False(t, resp.GroupedResults[1].HasAggregatedValue)
}

// =============================================================================
// Timeseries service error paths
// Covers the if err != nil return paths for each TS RPC method.
// =============================================================================

func TestServer_TSWriteBatch_ServiceError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("WriteBatch", mock.Anything, mock.Anything).
		Return((*service.WriteBatchResponse)(nil), stderrors.New("write batch failed"))

	_, err := srv.TSWriteBatch(context.Background(), &pb.TSWriteBatchRequest{
		Points: []*pb.TSDataPoint{
			{Metric: "cpu", Value: 1.0, Timestamp: time.Now().UnixNano()},
		},
	})
	require.Error(t, err)
}

func TestServer_TSQuery_ServiceError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("TSQuery", mock.Anything, mock.Anything).
		Return((*service.TSQueryResponse)(nil), stderrors.New("query failed"))

	_, err := srv.TSQuery(context.Background(), &pb.TSQueryRequest{
		Metric:    "cpu",
		StartTime: 1,
		EndTime:   time.Now().UnixNano(),
	})
	require.Error(t, err)
}

func TestServer_TSDeleteSeries_ServiceError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("DeleteSeries", mock.Anything, mock.Anything).
		Return((*service.TSDeleteResponse)(nil), stderrors.New("delete failed"))

	_, err := srv.TSDeleteSeries(context.Background(), &pb.TSDeleteSeriesRequest{
		Metric: "cpu",
	})
	require.Error(t, err)
}

func TestServer_TSGetRetentionPolicy_ServiceError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("GetRetentionPolicy", mock.Anything, mock.Anything).
		Return((*service.TSGetRetentionPolicyResponse)(nil), stderrors.New("policy error"))

	_, err := srv.TSGetRetentionPolicy(context.Background(), &pb.TSGetRetentionPolicyRequest{
		Metric: "cpu",
	})
	require.Error(t, err)
}

func TestServer_TSSetRetentionPolicy_ServiceError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("SetRetentionPolicy", mock.Anything, mock.Anything).
		Return((*service.TSSetRetentionPolicyResponse)(nil), stderrors.New("set policy error"))

	_, err := srv.TSSetRetentionPolicy(context.Background(), &pb.TSSetRetentionPolicyRequest{
		Metric: "cpu",
		Policy: &pb.TSRetentionPolicy{DurationSeconds: 3600},
	})
	require.Error(t, err)
}

func TestServer_TSListMetrics_ServiceError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("ListMetrics", mock.Anything, mock.Anything).
		Return((*service.TSListMetricsResponse)(nil), stderrors.New("list metrics error"))

	_, err := srv.TSListMetrics(context.Background(), &pb.TSListMetricsRequest{})
	require.Error(t, err)
}

// --- Merged from remaining_coverage_91_extended_withtimeseries_test.go ---

// =============================================================================
// Server Timeseries - Nil Request / Validation Tests (with timeseries tag)
// With the timeseries build tag, the real TS* implementations are compiled.
// Nil requests and validation failures return codes.InvalidArgument before
// the service type assertion check.
// =============================================================================

func TestGRPCRemaining91_ServerTimeseries_NilRequests(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := &Server{logger: log, service: nil}
	ctx := context.Background()

	t.Run("TSWritePoint_Nil", func(t *testing.T) {
		_, err := srv.TSWritePoint(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("TSWriteBatch_Nil", func(t *testing.T) {
		_, err := srv.TSWriteBatch(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("TSQuery_Nil", func(t *testing.T) {
		_, err := srv.TSQuery(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("TSDeleteSeries_Nil", func(t *testing.T) {
		_, err := srv.TSDeleteSeries(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("TSGetRetentionPolicy_Nil", func(t *testing.T) {
		_, err := srv.TSGetRetentionPolicy(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("TSSetRetentionPolicy_Nil", func(t *testing.T) {
		_, err := srv.TSSetRetentionPolicy(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("TSListMetrics_Nil", func(t *testing.T) {
		_, err := srv.TSListMetrics(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestGRPCRemaining91_ServerTimeseries_ValidationErrors(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := &Server{logger: log, service: nil}
	ctx := context.Background()

	t.Run("TSWritePoint_EmptyMetric", func(t *testing.T) {
		_, err := srv.TSWritePoint(ctx, &pb.TSWritePointRequest{Metric: ""})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("TSWriteBatch_EmptyPoints", func(t *testing.T) {
		_, err := srv.TSWriteBatch(ctx, &pb.TSWriteBatchRequest{Points: nil})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("TSQuery_EmptyMetric", func(t *testing.T) {
		_, err := srv.TSQuery(ctx, &pb.TSQueryRequest{Metric: ""})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("TSQuery_NoStartTime", func(t *testing.T) {
		_, err := srv.TSQuery(ctx, &pb.TSQueryRequest{Metric: "m", StartTime: 0})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("TSQuery_NoEndTime", func(t *testing.T) {
		_, err := srv.TSQuery(ctx, &pb.TSQueryRequest{Metric: "m", StartTime: 1, EndTime: 0})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("TSDeleteSeries_EmptyMetric", func(t *testing.T) {
		_, err := srv.TSDeleteSeries(ctx, &pb.TSDeleteSeriesRequest{Metric: ""})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("TSGetRetentionPolicy_EmptyMetric", func(t *testing.T) {
		_, err := srv.TSGetRetentionPolicy(ctx, &pb.TSGetRetentionPolicyRequest{Metric: ""})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("TSSetRetentionPolicy_EmptyMetric", func(t *testing.T) {
		_, err := srv.TSSetRetentionPolicy(ctx, &pb.TSSetRetentionPolicyRequest{Metric: ""})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("TSSetRetentionPolicy_NilPolicy", func(t *testing.T) {
		_, err := srv.TSSetRetentionPolicy(ctx, &pb.TSSetRetentionPolicyRequest{Metric: "m", Policy: nil})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}
