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
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

// timeseriesServiceInterface defines the timeseries service capability.
type timeseriesServiceInterface interface {
	WritePoint(ctx context.Context, req *service.WritePointRequest) (*service.WritePointResponse, error)
	WriteBatch(ctx context.Context, req *service.WriteBatchRequest) (*service.WriteBatchResponse, error)
	TSQuery(ctx context.Context, req *service.TSQueryRequest) (*service.TSQueryResponse, error)
	DeleteSeries(ctx context.Context, req *service.TSDeleteRequest) (*service.TSDeleteResponse, error)
	GetRetentionPolicy(ctx context.Context, req *service.TSGetRetentionPolicyRequest) (*service.TSGetRetentionPolicyResponse, error)
	SetRetentionPolicy(ctx context.Context, req *service.TSSetRetentionPolicyRequest) (*service.TSSetRetentionPolicyResponse, error)
	ListMetrics(ctx context.Context, req *service.TSListMetricsRequest) (*service.TSListMetricsResponse, error)
}

// TSWritePoint writes a single data point to time series storage.
func (s *Server) TSWritePoint(ctx context.Context, req *pb.TSWritePointRequest) (*pb.TSWritePointResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Metric == "" {
		return nil, status.Error(codes.InvalidArgument, "metric is required")
	}

	// Check if service supports timeseries
	tsService, ok := s.service.(timeseriesServiceInterface)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "timeseries service not available")
	}

	// Convert to service request
	serviceReq := &service.WritePointRequest{
		Metric:    req.Metric,
		Tags:      req.Tags,
		Value:     req.Value,
		Timestamp: time.Unix(0, req.Timestamp),
	}

	// Call service
	resp, err := tsService.WritePoint(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.TSWritePointResponse{
		Success:   resp.Success,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// TSWriteBatch writes multiple data points atomically.
func (s *Server) TSWriteBatch(ctx context.Context, req *pb.TSWriteBatchRequest) (*pb.TSWriteBatchResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if len(req.Points) == 0 {
		return nil, status.Error(codes.InvalidArgument, "no points provided")
	}

	// Check if service supports timeseries
	tsService, ok := s.service.(timeseriesServiceInterface)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "timeseries service not available")
	}

	// Convert to service request
	points := make([]*service.WritePointRequest, len(req.Points))
	for i, p := range req.Points {
		points[i] = &service.WritePointRequest{
			Metric:    p.Metric,
			Tags:      p.Tags,
			Value:     p.Value,
			Timestamp: time.Unix(0, p.Timestamp),
		}
	}

	serviceReq := &service.WriteBatchRequest{
		Points: points,
	}

	// Call service
	resp, err := tsService.WriteBatch(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	pointsWritten, err := conv.SafeIntToInt32(resp.PointsWritten)
	if err != nil {
		return nil, responseRangeError("points_written", err)
	}

	return &pb.TSWriteBatchResponse{
		Success:       resp.Success,
		PointsWritten: pointsWritten,
		Timestamp:     time.Now().UnixNano(),
	}, nil
}

// TSQuery performs time range queries with optional aggregation.
func (s *Server) TSQuery(ctx context.Context, req *pb.TSQueryRequest) (*pb.TSQueryResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Metric == "" {
		return nil, status.Error(codes.InvalidArgument, "metric is required")
	}

	if req.StartTime == 0 {
		return nil, status.Error(codes.InvalidArgument, "start_time is required")
	}

	if req.EndTime == 0 {
		return nil, status.Error(codes.InvalidArgument, "end_time is required")
	}

	// Check if service supports timeseries
	tsService, ok := s.service.(timeseriesServiceInterface)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "timeseries service not available")
	}

	// Convert to service request
	serviceReq := &service.TSQueryRequest{
		Metric:      req.Metric,
		Tags:        req.Tags,
		StartTime:   time.Unix(0, req.StartTime),
		EndTime:     time.Unix(0, req.EndTime),
		Aggregation: req.Aggregation,
		IntervalSec: req.IntervalSeconds,
		GroupBy:     req.GroupBy,
		Limit:       int(req.Limit),
		Percentile:  req.Percentile,
	}

	// Call service
	resp, err := tsService.TSQuery(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	// Convert response
	points := make([]*pb.TSDataPoint, 0, len(resp.Points))
	for _, p := range resp.Points {
		points = append(points, &pb.TSDataPoint{
			Metric:    p.Metric,
			Tags:      p.Tags,
			Value:     p.Value,
			Timestamp: p.Timestamp.UnixNano(),
		})
	}

	// Convert grouped results
	var groupedResults []*pb.TSGroupedResult
	for _, gr := range resp.GroupedResults {
		grPoints := make([]*pb.TSDataPoint, 0, len(gr.Points))
		for _, p := range gr.Points {
			grPoints = append(grPoints, &pb.TSDataPoint{
				Metric:    p.Metric,
				Tags:      p.Tags,
				Value:     p.Value,
				Timestamp: p.Timestamp.UnixNano(),
			})
		}
		pbGR := &pb.TSGroupedResult{
			Tags:   gr.Tags,
			Points: grPoints,
		}
		if gr.AggregatedValue != nil {
			pbGR.AggregatedValue = *gr.AggregatedValue
			pbGR.HasAggregatedValue = true
		}
		groupedResults = append(groupedResults, pbGR)
	}

	pbResp := &pb.TSQueryResponse{
		Metric:         resp.Metric,
		Tags:           resp.Tags,
		Points:         points,
		GroupedResults: groupedResults,
		Timestamp:      time.Now().UnixNano(),
	}

	if resp.AggregatedValue != nil {
		pbResp.AggregatedValue = *resp.AggregatedValue
		pbResp.HasAggregatedValue = true
	}

	return pbResp, nil
}

// TSDeleteSeries deletes a time series or specific data points.
func (s *Server) TSDeleteSeries(ctx context.Context, req *pb.TSDeleteSeriesRequest) (*pb.TSDeleteSeriesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Metric == "" {
		return nil, status.Error(codes.InvalidArgument, "metric is required")
	}

	// Check if service supports timeseries
	tsService, ok := s.service.(timeseriesServiceInterface)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "timeseries service not available")
	}

	// Convert to service request
	serviceReq := &service.TSDeleteRequest{
		Metric: req.Metric,
		Tags:   req.Tags,
	}

	if req.HasTimeRange {
		startTime := time.Unix(0, req.StartTime)
		endTime := time.Unix(0, req.EndTime)
		serviceReq.StartTime = &startTime
		serviceReq.EndTime = &endTime
	}

	// Call service
	resp, err := tsService.DeleteSeries(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.TSDeleteSeriesResponse{
		Success:   resp.Success,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// TSGetRetentionPolicy retrieves the retention policy for a metric.
func (s *Server) TSGetRetentionPolicy(ctx context.Context, req *pb.TSGetRetentionPolicyRequest) (*pb.TSGetRetentionPolicyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Metric == "" {
		return nil, status.Error(codes.InvalidArgument, "metric is required")
	}

	// Check if service supports timeseries
	tsService, ok := s.service.(timeseriesServiceInterface)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "timeseries service not available")
	}

	// Convert to service request
	serviceReq := &service.TSGetRetentionPolicyRequest{
		Metric: req.Metric,
	}

	// Call service
	resp, err := tsService.GetRetentionPolicy(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	pbResp := &pb.TSGetRetentionPolicyResponse{
		Metric:    resp.Metric,
		Timestamp: time.Now().UnixNano(),
	}

	if resp.Policy != nil {
		pbResp.Policy = &pb.TSRetentionPolicy{
			DurationSeconds:   int64(resp.Policy.Duration.Seconds()),
			ResolutionSeconds: int64(resp.Policy.Resolution.Seconds()),
			Aggregation:       resp.Policy.Aggregation,
		}
	}

	return pbResp, nil
}

// TSSetRetentionPolicy sets the retention policy for a metric.
func (s *Server) TSSetRetentionPolicy(ctx context.Context, req *pb.TSSetRetentionPolicyRequest) (*pb.TSSetRetentionPolicyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Metric == "" {
		return nil, status.Error(codes.InvalidArgument, "metric is required")
	}

	if req.Policy == nil {
		return nil, status.Error(codes.InvalidArgument, "policy is required")
	}

	// Check if service supports timeseries
	tsService, ok := s.service.(timeseriesServiceInterface)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "timeseries service not available")
	}

	// Convert to service request
	serviceReq := &service.TSSetRetentionPolicyRequest{
		Metric: req.Metric,
		Policy: &service.TSRetentionPolicy{
			Duration:    time.Duration(req.Policy.DurationSeconds) * time.Second,
			Resolution:  time.Duration(req.Policy.ResolutionSeconds) * time.Second,
			Aggregation: req.Policy.Aggregation,
		},
	}

	// Call service
	resp, err := tsService.SetRetentionPolicy(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.TSSetRetentionPolicyResponse{
		Success:   resp.Success,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// TSListMetrics lists all available metrics.
func (s *Server) TSListMetrics(ctx context.Context, req *pb.TSListMetricsRequest) (*pb.TSListMetricsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Check if service supports timeseries
	tsService, ok := s.service.(timeseriesServiceInterface)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "timeseries service not available")
	}

	// Convert to service request
	serviceReq := &service.TSListMetricsRequest{
		Prefix: req.Prefix,
		Tags:   req.Tags,
		Limit:  int(req.Limit),
		Cursor: req.Cursor,
	}

	// Call service
	resp, err := tsService.ListMetrics(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.TSListMetricsResponse{
		Metrics:    resp.Metrics,
		NextCursor: resp.NextCursor,
		Timestamp:  time.Now().UnixNano(),
	}, nil
}
