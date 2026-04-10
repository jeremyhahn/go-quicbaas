//go:build rest && timeseries

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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ========================================
// TimeSeries Service Handlers
// ========================================

// HandleWritePoint writes a single data point to time series storage.
func (s *Server) HandleWritePoint(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	// Parse request
	var req WritePointRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Validate request
	if req.Metric == "" {
		RespondBadRequest(w, r, "metric is required")
		return
	}
	if req.Timestamp.IsZero() {
		RespondBadRequest(w, r, "timestamp is required")
		return
	}

	// Convert to service request
	serviceReq := &service.WritePointRequest{
		Metric:    req.Metric,
		Tags:      req.Tags,
		Value:     req.Value,
		Timestamp: req.Timestamp,
	}

	// Get timeseries service capability
	tsService, ok := s.service.(interface {
		WritePoint(ctx context.Context, req *service.WritePointRequest) (*service.WritePointResponse, error)
	})
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrTimeseriesServiceUnavailable)
		return
	}

	// Call service
	resp, err := tsService.WritePoint(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Convert to REST response
	restResp := WritePointResponse{
		Success:   resp.Success,
		Timestamp: time.Now(),
	}

	RespondJSON(w, r, http.StatusOK, restResp)
}

// HandleWriteBatch writes multiple data points atomically.
func (s *Server) HandleWriteBatch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	// Parse request
	var req WriteBatchRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Validate request
	if len(req.Points) == 0 {
		RespondBadRequest(w, r, "no points provided")
		return
	}

	// Convert to service request
	serviceReq := &service.WriteBatchRequest{
		Points: make([]*service.WritePointRequest, len(req.Points)),
	}

	for i, p := range req.Points {
		if p.Metric == "" {
			RespondBadRequest(w, r, fmt.Sprintf("metric is required for point %d", i))
			return
		}
		if p.Timestamp.IsZero() {
			RespondBadRequest(w, r, fmt.Sprintf("timestamp is required for point %d", i))
			return
		}

		serviceReq.Points[i] = &service.WritePointRequest{
			Metric:    p.Metric,
			Tags:      p.Tags,
			Value:     p.Value,
			Timestamp: p.Timestamp,
		}
	}

	// Get timeseries service capability
	tsService, ok := s.service.(interface {
		WriteBatch(ctx context.Context, req *service.WriteBatchRequest) (*service.WriteBatchResponse, error)
	})
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrTimeseriesServiceUnavailable)
		return
	}

	// Call service
	resp, err := tsService.WriteBatch(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Convert to REST response
	restResp := WriteBatchResponse{
		Success:       resp.Success,
		PointsWritten: resp.PointsWritten,
		Timestamp:     time.Now(),
	}

	RespondJSON(w, r, http.StatusOK, restResp)
}

// ========================================
// TimeSeries Query Handler
// ========================================

// HandleTSQuery performs time range queries with optional aggregation.
func (s *Server) HandleTSQuery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	// Parse request
	var req TSQueryRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Validate request
	if req.Metric == "" {
		RespondBadRequest(w, r, "metric is required")
		return
	}
	if req.StartTime.IsZero() {
		RespondBadRequest(w, r, "start_time is required")
		return
	}
	if req.EndTime.IsZero() {
		RespondBadRequest(w, r, "end_time is required")
		return
	}
	if req.EndTime.Before(req.StartTime) {
		RespondBadRequest(w, r, "end_time must be after start_time")
		return
	}

	// Convert to service request
	serviceReq := &service.TSQueryRequest{
		Metric:      req.Metric,
		Tags:        req.Tags,
		StartTime:   req.StartTime,
		EndTime:     req.EndTime,
		Aggregation: req.Aggregation,
		IntervalSec: req.IntervalSec,
		GroupBy:     req.GroupBy,
		Limit:       req.Limit,
		Percentile:  req.Percentile,
	}

	// Get timeseries service capability
	tsService, ok := s.service.(interface {
		TSQuery(ctx context.Context, req *service.TSQueryRequest) (*service.TSQueryResponse, error)
	})
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrTimeseriesServiceUnavailable)
		return
	}

	// Call service
	resp, err := tsService.TSQuery(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Convert to REST response
	restResp := convertTSQueryResponse(resp)
	RespondJSON(w, r, http.StatusOK, restResp)
}

// ========================================
// TimeSeries Delete Handler
// ========================================

// HandleDeleteSeries deletes a time series or specific data points.
func (s *Server) HandleDeleteSeries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	// Parse request
	var req DeleteSeriesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Validate request
	if req.Metric == "" {
		RespondBadRequest(w, r, "metric is required")
		return
	}

	// Convert to service request
	serviceReq := &service.TSDeleteRequest{
		Metric:    req.Metric,
		Tags:      req.Tags,
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}

	// Get timeseries service capability
	tsService, ok := s.service.(interface {
		DeleteSeries(ctx context.Context, req *service.TSDeleteRequest) (*service.TSDeleteResponse, error)
	})
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrTimeseriesServiceUnavailable)
		return
	}

	// Call service
	resp, err := tsService.DeleteSeries(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Convert to REST response
	restResp := DeleteSeriesResponse{
		Success:   resp.Success,
		Timestamp: time.Now(),
	}

	RespondJSON(w, r, http.StatusOK, restResp)
}

// ========================================
// TimeSeries Retention Policy Handlers
// ========================================

// HandleGetRetentionPolicy retrieves the retention policy for a metric.
func (s *Server) HandleGetRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	metric := chi.URLParam(r, "metric")

	if metric == "" {
		RespondBadRequest(w, r, "metric is required")
		return
	}

	// Convert to service request
	serviceReq := &service.TSGetRetentionPolicyRequest{
		Metric: metric,
	}

	// Get timeseries service capability
	tsService, ok := s.service.(interface {
		GetRetentionPolicy(ctx context.Context, req *service.TSGetRetentionPolicyRequest) (*service.TSGetRetentionPolicyResponse, error)
	})
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrTimeseriesServiceUnavailable)
		return
	}

	// Call service
	resp, err := tsService.GetRetentionPolicy(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Convert to REST response
	var policy *TSRetentionPolicy
	if resp.Policy != nil {
		policy = &TSRetentionPolicy{
			DurationSec:   int64(resp.Policy.Duration.Seconds()),
			ResolutionSec: int64(resp.Policy.Resolution.Seconds()),
			Aggregation:   resp.Policy.Aggregation,
		}
	}

	restResp := GetRetentionPolicyResponse{
		Metric:    metric,
		Policy:    policy,
		Timestamp: time.Now(),
	}

	RespondJSON(w, r, http.StatusOK, restResp)
}

// HandleSetRetentionPolicy sets the retention policy for a metric.
func (s *Server) HandleSetRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	metric := chi.URLParam(r, "metric")

	if metric == "" {
		RespondBadRequest(w, r, "metric is required")
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	// Parse request
	var req SetRetentionPolicyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Validate request
	if req.Policy == nil {
		RespondBadRequest(w, r, "policy is required")
		return
	}

	// Convert to service request
	serviceReq := &service.TSSetRetentionPolicyRequest{
		Metric: metric,
		Policy: &service.TSRetentionPolicy{
			Duration:    time.Duration(req.Policy.DurationSec) * time.Second,
			Resolution:  time.Duration(req.Policy.ResolutionSec) * time.Second,
			Aggregation: req.Policy.Aggregation,
		},
	}

	// Get timeseries service capability
	tsService, ok := s.service.(interface {
		SetRetentionPolicy(ctx context.Context, req *service.TSSetRetentionPolicyRequest) (*service.TSSetRetentionPolicyResponse, error)
	})
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrTimeseriesServiceUnavailable)
		return
	}

	// Call service
	resp, err := tsService.SetRetentionPolicy(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Convert to REST response
	restResp := SetRetentionPolicyResponse{
		Success:   resp.Success,
		Timestamp: time.Now(),
	}

	RespondJSON(w, r, http.StatusOK, restResp)
}

// ========================================
// TimeSeries Metrics Handler
// ========================================

// HandleListMetrics lists all available metrics.
func (s *Server) HandleListMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters
	prefix := r.URL.Query().Get("prefix")

	// Parse tags from query string if provided
	tags := make(map[string]string)
	if tagsParam := r.URL.Query().Get("tags"); tagsParam != "" {
		if err := json.Unmarshal([]byte(tagsParam), &tags); err != nil {
			// Ignore invalid tags parameter
			tags = nil
		}
	}

	// Convert to service request
	serviceReq := &service.TSListMetricsRequest{
		Prefix: prefix,
		Tags:   tags,
		Limit:  100, // Default limit
		Cursor: "",
	}

	// Get timeseries service capability
	tsService, ok := s.service.(interface {
		ListMetrics(ctx context.Context, req *service.TSListMetricsRequest) (*service.TSListMetricsResponse, error)
	})
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrTimeseriesServiceUnavailable)
		return
	}

	// Call service
	resp, err := tsService.ListMetrics(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Convert to REST response
	restResp := ListMetricsResponse{
		Metrics:    resp.Metrics,
		NextCursor: resp.NextCursor,
		Timestamp:  time.Now(),
	}

	RespondJSON(w, r, http.StatusOK, restResp)
}

// ========================================
// Helper Functions
// ========================================

// convertTSQueryResponse converts service TSQueryResponse to REST response.
func convertTSQueryResponse(resp *service.TSQueryResponse) *TSQueryResponse {
	restResp := &TSQueryResponse{
		Metric:          resp.Metric,
		Tags:            resp.Tags,
		AggregatedValue: resp.AggregatedValue,
		Timestamp:       time.Now(),
	}

	// Convert points
	if len(resp.Points) > 0 {
		restResp.Points = make([]TSDataPoint, len(resp.Points))
		for i, p := range resp.Points {
			restResp.Points[i] = TSDataPoint{
				Metric:    p.Metric,
				Tags:      p.Tags,
				Value:     p.Value,
				Timestamp: p.Timestamp,
			}
		}
	}

	// Convert grouped results
	if len(resp.GroupedResults) > 0 {
		restResp.GroupedResults = make([]TSGroupedResult, len(resp.GroupedResults))
		for i, gr := range resp.GroupedResults {
			restGr := TSGroupedResult{
				Tags:            gr.Tags,
				AggregatedValue: gr.AggregatedValue,
			}
			if len(gr.Points) > 0 {
				restGr.Points = make([]TSDataPoint, len(gr.Points))
				for j, p := range gr.Points {
					restGr.Points[j] = TSDataPoint{
						Metric:    p.Metric,
						Tags:      p.Tags,
						Value:     p.Value,
						Timestamp: p.Timestamp,
					}
				}
			}
			restResp.GroupedResults[i] = restGr
		}
	}

	return restResp
}
