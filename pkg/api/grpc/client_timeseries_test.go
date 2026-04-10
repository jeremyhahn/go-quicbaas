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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
)

// mockTSClient mocks the QuicBaaS timeseries client for testing
type mockTSClient struct {
	pb.QuicBaaSClient
	writePointFunc         func(ctx context.Context, in *pb.TSWritePointRequest, opts ...grpc.CallOption) (*pb.TSWritePointResponse, error)
	writeBatchFunc         func(ctx context.Context, in *pb.TSWriteBatchRequest, opts ...grpc.CallOption) (*pb.TSWriteBatchResponse, error)
	queryFunc              func(ctx context.Context, in *pb.TSQueryRequest, opts ...grpc.CallOption) (*pb.TSQueryResponse, error)
	deleteSeriesFunc       func(ctx context.Context, in *pb.TSDeleteSeriesRequest, opts ...grpc.CallOption) (*pb.TSDeleteSeriesResponse, error)
	getRetentionPolicyFunc func(ctx context.Context, in *pb.TSGetRetentionPolicyRequest, opts ...grpc.CallOption) (*pb.TSGetRetentionPolicyResponse, error)
	setRetentionPolicyFunc func(ctx context.Context, in *pb.TSSetRetentionPolicyRequest, opts ...grpc.CallOption) (*pb.TSSetRetentionPolicyResponse, error)
	listMetricsFunc        func(ctx context.Context, in *pb.TSListMetricsRequest, opts ...grpc.CallOption) (*pb.TSListMetricsResponse, error)
}

func (m *mockTSClient) TSWritePoint(ctx context.Context, in *pb.TSWritePointRequest, opts ...grpc.CallOption) (*pb.TSWritePointResponse, error) {
	if m.writePointFunc != nil {
		return m.writePointFunc(ctx, in, opts...)
	}
	return &pb.TSWritePointResponse{Success: true, Timestamp: time.Now().UnixNano()}, nil
}

func (m *mockTSClient) TSWriteBatch(ctx context.Context, in *pb.TSWriteBatchRequest, opts ...grpc.CallOption) (*pb.TSWriteBatchResponse, error) {
	if m.writeBatchFunc != nil {
		return m.writeBatchFunc(ctx, in, opts...)
	}
	return &pb.TSWriteBatchResponse{Success: true, PointsWritten: int32(len(in.Points)), Timestamp: time.Now().UnixNano()}, nil
}

func (m *mockTSClient) TSQuery(ctx context.Context, in *pb.TSQueryRequest, opts ...grpc.CallOption) (*pb.TSQueryResponse, error) {
	if m.queryFunc != nil {
		return m.queryFunc(ctx, in, opts...)
	}
	return &pb.TSQueryResponse{
		Metric:    in.Metric,
		Tags:      in.Tags,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

func (m *mockTSClient) TSDeleteSeries(ctx context.Context, in *pb.TSDeleteSeriesRequest, opts ...grpc.CallOption) (*pb.TSDeleteSeriesResponse, error) {
	if m.deleteSeriesFunc != nil {
		return m.deleteSeriesFunc(ctx, in, opts...)
	}
	return &pb.TSDeleteSeriesResponse{Success: true, Timestamp: time.Now().UnixNano()}, nil
}

func (m *mockTSClient) TSGetRetentionPolicy(ctx context.Context, in *pb.TSGetRetentionPolicyRequest, opts ...grpc.CallOption) (*pb.TSGetRetentionPolicyResponse, error) {
	if m.getRetentionPolicyFunc != nil {
		return m.getRetentionPolicyFunc(ctx, in, opts...)
	}
	return &pb.TSGetRetentionPolicyResponse{
		Metric:    in.Metric,
		Timestamp: time.Now().UnixNano(),
		Policy: &pb.TSRetentionPolicy{
			DurationSeconds:   86400,
			ResolutionSeconds: 60,
			Aggregation:       "avg",
		},
	}, nil
}

func (m *mockTSClient) TSSetRetentionPolicy(ctx context.Context, in *pb.TSSetRetentionPolicyRequest, opts ...grpc.CallOption) (*pb.TSSetRetentionPolicyResponse, error) {
	if m.setRetentionPolicyFunc != nil {
		return m.setRetentionPolicyFunc(ctx, in, opts...)
	}
	return &pb.TSSetRetentionPolicyResponse{Success: true, Timestamp: time.Now().UnixNano()}, nil
}

func (m *mockTSClient) TSListMetrics(ctx context.Context, in *pb.TSListMetricsRequest, opts ...grpc.CallOption) (*pb.TSListMetricsResponse, error) {
	if m.listMetricsFunc != nil {
		return m.listMetricsFunc(ctx, in, opts...)
	}
	return &pb.TSListMetricsResponse{
		Metrics:    []string{"metric1", "metric2"},
		NextCursor: "",
		Timestamp:  time.Now().UnixNano(),
	}, nil
}

func TestClient_TSWritePoint_Success(t *testing.T) {
	mock := &mockTSClient{}
	client := &Client{
		conn:   &grpc.ClientConn{},
		client: mock,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      3,
			retryBackoff:    100 * time.Millisecond,
			maxRetryBackoff: 1 * time.Second,
		},
		connected: true,
	}

	ctx := context.Background()
	req := &cli.WritePointRequest{
		Metric:    "test.metric",
		Tags:      map[string]string{"host": "server1"},
		Value:     42.5,
		Timestamp: time.Now(),
	}

	resp, err := client.TSWritePoint(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Success)
}

func TestClient_TSWritePoint_NotConnected(t *testing.T) {
	client := &Client{
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      3,
			retryBackoff:    100 * time.Millisecond,
			maxRetryBackoff: 1 * time.Second,
		},
		connected: false,
	}

	ctx := context.Background()
	req := &cli.WritePointRequest{
		Metric:    "test.metric",
		Value:     42.5,
		Timestamp: time.Now(),
	}

	resp, err := client.TSWritePoint(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClient_TSWritePoint_Error(t *testing.T) {
	mock := &mockTSClient{
		writePointFunc: func(ctx context.Context, in *pb.TSWritePointRequest, opts ...grpc.CallOption) (*pb.TSWritePointResponse, error) {
			return nil, status.Error(codes.Internal, "write failed")
		},
	}

	client := &Client{
		conn:   &grpc.ClientConn{},
		client: mock,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      1,
			retryBackoff:    10 * time.Millisecond,
			maxRetryBackoff: 100 * time.Millisecond,
		},
		connected: true,
	}

	ctx := context.Background()
	req := &cli.WritePointRequest{
		Metric:    "test.metric",
		Value:     42.5,
		Timestamp: time.Now(),
	}

	resp, err := client.TSWritePoint(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClient_TSWriteBatch_Success(t *testing.T) {
	mock := &mockTSClient{}
	client := &Client{
		conn:   &grpc.ClientConn{},
		client: mock,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      3,
			retryBackoff:    100 * time.Millisecond,
			maxRetryBackoff: 1 * time.Second,
		},
		connected: true,
	}

	ctx := context.Background()
	now := time.Now()
	req := &cli.WriteBatchRequest{
		Points: []cli.WritePointRequest{
			{Metric: "test.metric1", Value: 1.0, Timestamp: now},
			{Metric: "test.metric2", Value: 2.0, Timestamp: now},
		},
	}

	resp, err := client.TSWriteBatch(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, 2, resp.PointsWritten)
}

func TestClient_TSWriteBatch_Error(t *testing.T) {
	mock := &mockTSClient{
		writeBatchFunc: func(ctx context.Context, in *pb.TSWriteBatchRequest, opts ...grpc.CallOption) (*pb.TSWriteBatchResponse, error) {
			return nil, errors.New("batch write failed")
		},
	}

	client := &Client{
		conn:   &grpc.ClientConn{},
		client: mock,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      1,
			retryBackoff:    10 * time.Millisecond,
			maxRetryBackoff: 100 * time.Millisecond,
		},
		connected: true,
	}

	ctx := context.Background()
	req := &cli.WriteBatchRequest{
		Points: []cli.WritePointRequest{
			{Metric: "test.metric", Value: 1.0, Timestamp: time.Now()},
		},
	}

	resp, err := client.TSWriteBatch(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClient_TSQuery_Success(t *testing.T) {
	mock := &mockTSClient{
		queryFunc: func(ctx context.Context, in *pb.TSQueryRequest, opts ...grpc.CallOption) (*pb.TSQueryResponse, error) {
			return &pb.TSQueryResponse{
				Metric: in.Metric,
				Tags:   in.Tags,
				Points: []*pb.TSDataPoint{
					{Metric: "test.metric", Value: 1.0, Timestamp: time.Now().UnixNano()},
					{Metric: "test.metric", Value: 2.0, Timestamp: time.Now().UnixNano()},
				},
				Timestamp: time.Now().UnixNano(),
			}, nil
		},
	}

	client := &Client{
		conn:   &grpc.ClientConn{},
		client: mock,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      3,
			retryBackoff:    100 * time.Millisecond,
			maxRetryBackoff: 1 * time.Second,
		},
		connected: true,
	}

	ctx := context.Background()
	req := &cli.TSQueryRequest{
		Metric:    "test.metric",
		StartTime: time.Now().Add(-1 * time.Hour),
		EndTime:   time.Now(),
		Limit:     100,
	}

	resp, err := client.TSQuery(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test.metric", resp.Metric)
	assert.Len(t, resp.Points, 2)
}

func TestClient_TSQuery_WithGroupedResults(t *testing.T) {
	mock := &mockTSClient{
		queryFunc: func(ctx context.Context, in *pb.TSQueryRequest, opts ...grpc.CallOption) (*pb.TSQueryResponse, error) {
			aggValue := 1.5
			return &pb.TSQueryResponse{
				Metric: in.Metric,
				GroupedResults: []*pb.TSGroupedResult{
					{
						Tags: map[string]string{"host": "server1"},
						Points: []*pb.TSDataPoint{
							{Metric: "test.metric", Value: 1.0, Timestamp: time.Now().UnixNano()},
						},
						AggregatedValue:    aggValue,
						HasAggregatedValue: true,
					},
				},
				AggregatedValue:    aggValue,
				HasAggregatedValue: true,
				Timestamp:          time.Now().UnixNano(),
			}, nil
		},
	}

	client := &Client{
		conn:   &grpc.ClientConn{},
		client: mock,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      3,
			retryBackoff:    100 * time.Millisecond,
			maxRetryBackoff: 1 * time.Second,
		},
		connected: true,
	}

	ctx := context.Background()
	req := &cli.TSQueryRequest{
		Metric:      "test.metric",
		StartTime:   time.Now().Add(-1 * time.Hour),
		EndTime:     time.Now(),
		Aggregation: "avg",
		GroupBy:     []string{"host"},
	}

	resp, err := client.TSQuery(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.GroupedResults, 1)
	assert.NotNil(t, resp.AggregatedValue)
	assert.Equal(t, 1.5, *resp.AggregatedValue)
}

func TestClient_TSQuery_LimitClamping(t *testing.T) {
	mock := &mockTSClient{}
	client := &Client{
		conn:   &grpc.ClientConn{},
		client: mock,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      3,
			retryBackoff:    100 * time.Millisecond,
			maxRetryBackoff: 1 * time.Second,
		},
		connected: true,
	}

	ctx := context.Background()

	// Test limit > max int32
	req := &cli.TSQueryRequest{
		Metric:    "test.metric",
		StartTime: time.Now().Add(-1 * time.Hour),
		EndTime:   time.Now(),
		Limit:     0xFFFFFFFF, // Greater than max int32
	}

	resp, err := client.TSQuery(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)

	// Test negative limit
	req.Limit = -1
	resp, err = client.TSQuery(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestClient_TSDeleteSeries_Success(t *testing.T) {
	mock := &mockTSClient{}
	client := &Client{
		conn:   &grpc.ClientConn{},
		client: mock,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      3,
			retryBackoff:    100 * time.Millisecond,
			maxRetryBackoff: 1 * time.Second,
		},
		connected: true,
	}

	ctx := context.Background()
	startTime := time.Now().Add(-1 * time.Hour)
	endTime := time.Now()
	req := &cli.TSDeleteRequest{
		Metric:    "test.metric",
		Tags:      map[string]string{"host": "server1"},
		StartTime: &startTime,
		EndTime:   &endTime,
	}

	resp, err := client.TSDeleteSeries(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Success)
}

func TestClient_TSDeleteSeries_WithoutTimeRange(t *testing.T) {
	mock := &mockTSClient{}
	client := &Client{
		conn:   &grpc.ClientConn{},
		client: mock,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      3,
			retryBackoff:    100 * time.Millisecond,
			maxRetryBackoff: 1 * time.Second,
		},
		connected: true,
	}

	ctx := context.Background()
	req := &cli.TSDeleteRequest{
		Metric: "test.metric",
	}

	resp, err := client.TSDeleteSeries(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Success)
}

func TestClient_TSGetRetentionPolicy_Success(t *testing.T) {
	mock := &mockTSClient{}
	client := &Client{
		conn:   &grpc.ClientConn{},
		client: mock,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      3,
			retryBackoff:    100 * time.Millisecond,
			maxRetryBackoff: 1 * time.Second,
		},
		connected: true,
	}

	ctx := context.Background()
	req := &cli.TSGetRetentionPolicyRequest{
		Metric: "test.metric",
	}

	resp, err := client.TSGetRetentionPolicy(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.Policy)
	assert.Equal(t, "test.metric", resp.Metric)
	assert.Equal(t, 24*time.Hour, resp.Policy.Duration)
}

func TestClient_TSGetRetentionPolicy_NilPolicy(t *testing.T) {
	mock := &mockTSClient{
		getRetentionPolicyFunc: func(ctx context.Context, in *pb.TSGetRetentionPolicyRequest, opts ...grpc.CallOption) (*pb.TSGetRetentionPolicyResponse, error) {
			return &pb.TSGetRetentionPolicyResponse{
				Metric:    in.Metric,
				Policy:    nil,
				Timestamp: time.Now().UnixNano(),
			}, nil
		},
	}

	client := &Client{
		conn:   &grpc.ClientConn{},
		client: mock,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      3,
			retryBackoff:    100 * time.Millisecond,
			maxRetryBackoff: 1 * time.Second,
		},
		connected: true,
	}

	ctx := context.Background()
	req := &cli.TSGetRetentionPolicyRequest{
		Metric: "test.metric",
	}

	resp, err := client.TSGetRetentionPolicy(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Nil(t, resp.Policy)
}

func TestClient_TSSetRetentionPolicy_Success(t *testing.T) {
	mock := &mockTSClient{}
	client := &Client{
		conn:   &grpc.ClientConn{},
		client: mock,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      3,
			retryBackoff:    100 * time.Millisecond,
			maxRetryBackoff: 1 * time.Second,
		},
		connected: true,
	}

	ctx := context.Background()
	req := &cli.TSSetRetentionPolicyRequest{
		Metric: "test.metric",
		Policy: &cli.TSRetentionPolicy{
			Duration:    24 * time.Hour,
			Resolution:  1 * time.Minute,
			Aggregation: "avg",
		},
	}

	resp, err := client.TSSetRetentionPolicy(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Success)
}

func TestClient_TSSetRetentionPolicy_NilPolicy(t *testing.T) {
	mock := &mockTSClient{}
	client := &Client{
		conn:   &grpc.ClientConn{},
		client: mock,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      3,
			retryBackoff:    100 * time.Millisecond,
			maxRetryBackoff: 1 * time.Second,
		},
		connected: true,
	}

	ctx := context.Background()
	req := &cli.TSSetRetentionPolicyRequest{
		Metric: "test.metric",
		Policy: nil,
	}

	resp, err := client.TSSetRetentionPolicy(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestClient_TSListMetrics_Success(t *testing.T) {
	mock := &mockTSClient{}
	client := &Client{
		conn:   &grpc.ClientConn{},
		client: mock,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      3,
			retryBackoff:    100 * time.Millisecond,
			maxRetryBackoff: 1 * time.Second,
		},
		connected: true,
	}

	ctx := context.Background()
	req := &cli.TSListMetricsRequest{
		Prefix: "test.",
		Limit:  100,
	}

	resp, err := client.TSListMetrics(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.Metrics, 2)
}

func TestClient_TSListMetrics_LimitClamping(t *testing.T) {
	mock := &mockTSClient{}
	client := &Client{
		conn:   &grpc.ClientConn{},
		client: mock,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      3,
			retryBackoff:    100 * time.Millisecond,
			maxRetryBackoff: 1 * time.Second,
		},
		connected: true,
	}

	ctx := context.Background()

	// Test limit > max int32
	req := &cli.TSListMetricsRequest{
		Limit: 0xFFFFFFFF,
	}

	resp, err := client.TSListMetrics(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)

	// Test negative limit
	req.Limit = -1
	resp, err = client.TSListMetrics(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestClient_TSListMetrics_WithCursor(t *testing.T) {
	mock := &mockTSClient{
		listMetricsFunc: func(ctx context.Context, in *pb.TSListMetricsRequest, opts ...grpc.CallOption) (*pb.TSListMetricsResponse, error) {
			return &pb.TSListMetricsResponse{
				Metrics:    []string{"metric1", "metric2"},
				NextCursor: "cursor123",
				Timestamp:  time.Now().UnixNano(),
			}, nil
		},
	}

	client := &Client{
		conn:   &grpc.ClientConn{},
		client: mock,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      3,
			retryBackoff:    100 * time.Millisecond,
			maxRetryBackoff: 1 * time.Second,
		},
		connected: true,
	}

	ctx := context.Background()
	req := &cli.TSListMetricsRequest{
		Cursor: "previous_cursor",
		Limit:  50,
	}

	resp, err := client.TSListMetrics(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "cursor123", resp.NextCursor)
}

func TestClient_TSOperations_WithTimeout(t *testing.T) {
	mock := &mockTSClient{
		writePointFunc: func(ctx context.Context, in *pb.TSWritePointRequest, opts ...grpc.CallOption) (*pb.TSWritePointResponse, error) {
			// Check that context has a deadline
			_, hasDeadline := ctx.Deadline()
			assert.True(t, hasDeadline)
			return &pb.TSWritePointResponse{Success: true, Timestamp: time.Now().UnixNano()}, nil
		},
	}

	client := &Client{
		conn:   &grpc.ClientConn{},
		client: mock,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      3,
			retryBackoff:    100 * time.Millisecond,
			maxRetryBackoff: 1 * time.Second,
		},
		connected: true,
	}

	// Test with context that already has deadline
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &cli.WritePointRequest{
		Metric:    "test.metric",
		Value:     42.5,
		Timestamp: time.Now(),
	}

	resp, err := client.TSWritePoint(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
}
