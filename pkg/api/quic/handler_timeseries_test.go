//go:build quic

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

package quic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock TimeSeries Service for Testing
// =============================================================================

type mockTimeSeriesService struct {
	writePointFn         func(ctx context.Context, req *service.WritePointRequest) (*service.WritePointResponse, error)
	writeBatchFn         func(ctx context.Context, req *service.WriteBatchRequest) (*service.WriteBatchResponse, error)
	tsQueryFn            func(ctx context.Context, req *service.TSQueryRequest) (*service.TSQueryResponse, error)
	deleteSeriesFn       func(ctx context.Context, req *service.TSDeleteRequest) (*service.TSDeleteResponse, error)
	getRetentionPolicyFn func(ctx context.Context, req *service.TSGetRetentionPolicyRequest) (*service.TSGetRetentionPolicyResponse, error)
	setRetentionPolicyFn func(ctx context.Context, req *service.TSSetRetentionPolicyRequest) (*service.TSSetRetentionPolicyResponse, error)
	listMetricsFn        func(ctx context.Context, req *service.TSListMetricsRequest) (*service.TSListMetricsResponse, error)
}

func (m *mockTimeSeriesService) WritePoint(ctx context.Context, req *service.WritePointRequest) (*service.WritePointResponse, error) {
	if m.writePointFn != nil {
		return m.writePointFn(ctx, req)
	}
	return &service.WritePointResponse{
		Success:   true,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockTimeSeriesService) WriteBatch(ctx context.Context, req *service.WriteBatchRequest) (*service.WriteBatchResponse, error) {
	if m.writeBatchFn != nil {
		return m.writeBatchFn(ctx, req)
	}
	return &service.WriteBatchResponse{
		Success:       true,
		PointsWritten: len(req.Points),
		Timestamp:     time.Now(),
	}, nil
}

func (m *mockTimeSeriesService) TSQuery(ctx context.Context, req *service.TSQueryRequest) (*service.TSQueryResponse, error) {
	if m.tsQueryFn != nil {
		return m.tsQueryFn(ctx, req)
	}
	return &service.TSQueryResponse{
		Metric:    req.Metric,
		Points:    []*service.TSDataPoint{},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockTimeSeriesService) DeleteSeries(ctx context.Context, req *service.TSDeleteRequest) (*service.TSDeleteResponse, error) {
	if m.deleteSeriesFn != nil {
		return m.deleteSeriesFn(ctx, req)
	}
	return &service.TSDeleteResponse{
		Success:   true,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockTimeSeriesService) GetRetentionPolicy(ctx context.Context, req *service.TSGetRetentionPolicyRequest) (*service.TSGetRetentionPolicyResponse, error) {
	if m.getRetentionPolicyFn != nil {
		return m.getRetentionPolicyFn(ctx, req)
	}
	return &service.TSGetRetentionPolicyResponse{
		Metric: req.Metric,
		Policy: &service.TSRetentionPolicy{
			Duration: 30 * 24 * time.Hour,
		},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockTimeSeriesService) SetRetentionPolicy(ctx context.Context, req *service.TSSetRetentionPolicyRequest) (*service.TSSetRetentionPolicyResponse, error) {
	if m.setRetentionPolicyFn != nil {
		return m.setRetentionPolicyFn(ctx, req)
	}
	return &service.TSSetRetentionPolicyResponse{
		Success:   true,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockTimeSeriesService) ListMetrics(ctx context.Context, req *service.TSListMetricsRequest) (*service.TSListMetricsResponse, error) {
	if m.listMetricsFn != nil {
		return m.listMetricsFn(ctx, req)
	}
	return &service.TSListMetricsResponse{
		Metrics:   []string{"cpu.usage", "memory.free", "disk.io"},
		Timestamp: time.Now(),
	}, nil
}

// =============================================================================
// Mock Service Wrapper with TimeSeries Support
// =============================================================================

type mockTSEnabledService struct {
	mockService
	tsSvc *mockTimeSeriesService
}

// Implement timeseriesServiceInterface by forwarding to the mock.
func (m *mockTSEnabledService) WritePoint(ctx context.Context, req *service.WritePointRequest) (*service.WritePointResponse, error) {
	if m.tsSvc != nil {
		return m.tsSvc.WritePoint(ctx, req)
	}
	return nil, errors.New("timeseries service not available")
}

func (m *mockTSEnabledService) WriteBatch(ctx context.Context, req *service.WriteBatchRequest) (*service.WriteBatchResponse, error) {
	if m.tsSvc != nil {
		return m.tsSvc.WriteBatch(ctx, req)
	}
	return nil, errors.New("timeseries service not available")
}

func (m *mockTSEnabledService) TSQuery(ctx context.Context, req *service.TSQueryRequest) (*service.TSQueryResponse, error) {
	if m.tsSvc != nil {
		return m.tsSvc.TSQuery(ctx, req)
	}
	return nil, errors.New("timeseries service not available")
}

func (m *mockTSEnabledService) DeleteSeries(ctx context.Context, req *service.TSDeleteRequest) (*service.TSDeleteResponse, error) {
	if m.tsSvc != nil {
		return m.tsSvc.DeleteSeries(ctx, req)
	}
	return nil, errors.New("timeseries service not available")
}

func (m *mockTSEnabledService) GetRetentionPolicy(ctx context.Context, req *service.TSGetRetentionPolicyRequest) (*service.TSGetRetentionPolicyResponse, error) {
	if m.tsSvc != nil {
		return m.tsSvc.GetRetentionPolicy(ctx, req)
	}
	return nil, errors.New("timeseries service not available")
}

func (m *mockTSEnabledService) SetRetentionPolicy(ctx context.Context, req *service.TSSetRetentionPolicyRequest) (*service.TSSetRetentionPolicyResponse, error) {
	if m.tsSvc != nil {
		return m.tsSvc.SetRetentionPolicy(ctx, req)
	}
	return nil, errors.New("timeseries service not available")
}

func (m *mockTSEnabledService) ListMetrics(ctx context.Context, req *service.TSListMetricsRequest) (*service.TSListMetricsResponse, error) {
	if m.tsSvc != nil {
		return m.tsSvc.ListMetrics(ctx, req)
	}
	return nil, errors.New("timeseries service not available")
}

// =============================================================================
// Test Helpers
// =============================================================================

func createTSTestLogger() *logger.Logger {
	cfg := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "quic-ts-test",
	}
	log, err := logger.New(cfg)
	if err != nil {
		panic(err)
	}
	return log
}

func createTSEnabledHandler(tsSvc *mockTimeSeriesService) *Handler {
	mockSvc := &mockTSEnabledService{
		tsSvc: tsSvc,
	}
	return NewHandler(mockSvc, nil, nil, nil, createTSTestLogger())
}

func createTSErrorHandler() *Handler {
	// Create a handler with a service that does NOT implement timeseriesServiceInterface
	mockSvc := &mockService{}
	return NewHandler(mockSvc, nil, nil, nil, createTSTestLogger())
}

// =============================================================================
// handleTSWritePoint Tests
// =============================================================================

func TestHandleTSWritePoint_Success(t *testing.T) {
	tsSvc := &mockTimeSeriesService{
		writePointFn: func(ctx context.Context, req *service.WritePointRequest) (*service.WritePointResponse, error) {
			return &service.WritePointResponse{
				Success:   true,
				Timestamp: time.Now(),
			}, nil
		},
	}
	handler := createTSEnabledHandler(tsSvc)

	payload, err := EncodePayload(&service.WritePointRequest{
		Metric:    "cpu.usage",
		Value:     85.5,
		Timestamp: time.Now(),
		Tags:      map[string]string{"host": "server1"},
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpTSWritePoint,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleTSWritePoint_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTSTestLogger())

	payload, _ := EncodePayload(&service.WritePointRequest{})
	req := &Request{
		Opcode:  OpTSWritePoint,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleTSWritePoint_DecodeError(t *testing.T) {
	tsSvc := &mockTimeSeriesService{}
	handler := createTSEnabledHandler(tsSvc)

	req := &Request{
		Opcode:  OpTSWritePoint,
		Payload: []byte("invalid json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleTSWritePoint_ServiceNotAvailable(t *testing.T) {
	handler := createTSErrorHandler()

	payload, _ := EncodePayload(&service.WritePointRequest{
		Metric: "cpu.usage",
		Value:  85.5,
	})
	req := &Request{
		Opcode:  OpTSWritePoint,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error when timeseries service not available")
	}
}

func TestHandleTSWritePoint_ServiceError(t *testing.T) {
	tsSvc := &mockTimeSeriesService{
		writePointFn: func(ctx context.Context, req *service.WritePointRequest) (*service.WritePointResponse, error) {
			return nil, errors.New("write point failed")
		},
	}
	handler := createTSEnabledHandler(tsSvc)

	payload, _ := EncodePayload(&service.WritePointRequest{
		Metric: "cpu.usage",
		Value:  85.5,
	})
	req := &Request{
		Opcode:  OpTSWritePoint,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleTSWriteBatch Tests
// =============================================================================

func TestHandleTSWriteBatch_Success(t *testing.T) {
	tsSvc := &mockTimeSeriesService{
		writeBatchFn: func(ctx context.Context, req *service.WriteBatchRequest) (*service.WriteBatchResponse, error) {
			return &service.WriteBatchResponse{
				Success:       true,
				PointsWritten: len(req.Points),
				Timestamp:     time.Now(),
			}, nil
		},
	}
	handler := createTSEnabledHandler(tsSvc)

	payload, err := EncodePayload(&service.WriteBatchRequest{
		Points: []*service.WritePointRequest{
			{Metric: "cpu.usage", Value: 85.5, Timestamp: time.Now()},
			{Metric: "memory.free", Value: 4096, Timestamp: time.Now()},
		},
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpTSWriteBatch,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleTSWriteBatch_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTSTestLogger())

	payload, _ := EncodePayload(&service.WriteBatchRequest{})
	req := &Request{
		Opcode:  OpTSWriteBatch,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleTSWriteBatch_DecodeError(t *testing.T) {
	tsSvc := &mockTimeSeriesService{}
	handler := createTSEnabledHandler(tsSvc)

	req := &Request{
		Opcode:  OpTSWriteBatch,
		Payload: []byte("invalid json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleTSWriteBatch_ServiceNotAvailable(t *testing.T) {
	handler := createTSErrorHandler()

	payload, _ := EncodePayload(&service.WriteBatchRequest{})
	req := &Request{
		Opcode:  OpTSWriteBatch,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error when timeseries service not available")
	}
}

func TestHandleTSWriteBatch_ServiceError(t *testing.T) {
	tsSvc := &mockTimeSeriesService{
		writeBatchFn: func(ctx context.Context, req *service.WriteBatchRequest) (*service.WriteBatchResponse, error) {
			return nil, errors.New("write batch failed")
		},
	}
	handler := createTSEnabledHandler(tsSvc)

	payload, _ := EncodePayload(&service.WriteBatchRequest{})
	req := &Request{
		Opcode:  OpTSWriteBatch,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleTSQuery Tests
// =============================================================================

func TestHandleTSQuery_Success(t *testing.T) {
	tsSvc := &mockTimeSeriesService{
		tsQueryFn: func(ctx context.Context, req *service.TSQueryRequest) (*service.TSQueryResponse, error) {
			return &service.TSQueryResponse{
				Metric:    "cpu.usage",
				Points:    []*service.TSDataPoint{},
				Timestamp: time.Now(),
			}, nil
		},
	}
	handler := createTSEnabledHandler(tsSvc)

	payload, err := EncodePayload(&service.TSQueryRequest{
		Metric:    "cpu.usage",
		StartTime: time.Now().Add(-time.Hour),
		EndTime:   time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpTSQuery,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleTSQuery_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTSTestLogger())

	payload, _ := EncodePayload(&service.TSQueryRequest{})
	req := &Request{
		Opcode:  OpTSQuery,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleTSQuery_DecodeError(t *testing.T) {
	tsSvc := &mockTimeSeriesService{}
	handler := createTSEnabledHandler(tsSvc)

	req := &Request{
		Opcode:  OpTSQuery,
		Payload: []byte("invalid json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleTSQuery_ServiceNotAvailable(t *testing.T) {
	handler := createTSErrorHandler()

	payload, _ := EncodePayload(&service.TSQueryRequest{Metric: "cpu.usage"})
	req := &Request{
		Opcode:  OpTSQuery,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error when timeseries service not available")
	}
}

func TestHandleTSQuery_ServiceError(t *testing.T) {
	tsSvc := &mockTimeSeriesService{
		tsQueryFn: func(ctx context.Context, req *service.TSQueryRequest) (*service.TSQueryResponse, error) {
			return nil, errors.New("query failed")
		},
	}
	handler := createTSEnabledHandler(tsSvc)

	payload, _ := EncodePayload(&service.TSQueryRequest{Metric: "cpu.usage"})
	req := &Request{
		Opcode:  OpTSQuery,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleTSDeleteSeries Tests
// =============================================================================

func TestHandleTSDeleteSeries_Success(t *testing.T) {
	tsSvc := &mockTimeSeriesService{
		deleteSeriesFn: func(ctx context.Context, req *service.TSDeleteRequest) (*service.TSDeleteResponse, error) {
			return &service.TSDeleteResponse{
				Success:   true,
				Timestamp: time.Now(),
			}, nil
		},
	}
	handler := createTSEnabledHandler(tsSvc)

	payload, err := EncodePayload(&service.TSDeleteRequest{
		Metric: "cpu.usage",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpTSDeleteSeries,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleTSDeleteSeries_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTSTestLogger())

	payload, _ := EncodePayload(&service.TSDeleteRequest{})
	req := &Request{
		Opcode:  OpTSDeleteSeries,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleTSDeleteSeries_DecodeError(t *testing.T) {
	tsSvc := &mockTimeSeriesService{}
	handler := createTSEnabledHandler(tsSvc)

	req := &Request{
		Opcode:  OpTSDeleteSeries,
		Payload: []byte("invalid json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleTSDeleteSeries_ServiceNotAvailable(t *testing.T) {
	handler := createTSErrorHandler()

	payload, _ := EncodePayload(&service.TSDeleteRequest{Metric: "cpu.usage"})
	req := &Request{
		Opcode:  OpTSDeleteSeries,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error when timeseries service not available")
	}
}

func TestHandleTSDeleteSeries_ServiceError(t *testing.T) {
	tsSvc := &mockTimeSeriesService{
		deleteSeriesFn: func(ctx context.Context, req *service.TSDeleteRequest) (*service.TSDeleteResponse, error) {
			return nil, errors.New("delete failed")
		},
	}
	handler := createTSEnabledHandler(tsSvc)

	payload, _ := EncodePayload(&service.TSDeleteRequest{Metric: "cpu.usage"})
	req := &Request{
		Opcode:  OpTSDeleteSeries,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleTSGetRetentionPolicy Tests
// =============================================================================

func TestHandleTSGetRetentionPolicy_Success(t *testing.T) {
	tsSvc := &mockTimeSeriesService{
		getRetentionPolicyFn: func(ctx context.Context, req *service.TSGetRetentionPolicyRequest) (*service.TSGetRetentionPolicyResponse, error) {
			return &service.TSGetRetentionPolicyResponse{
				Metric: req.Metric,
				Policy: &service.TSRetentionPolicy{
					Duration: 30 * 24 * time.Hour,
				},
				Timestamp: time.Now(),
			}, nil
		},
	}
	handler := createTSEnabledHandler(tsSvc)

	payload, err := EncodePayload(&service.TSGetRetentionPolicyRequest{
		Metric: "cpu.usage",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpTSGetRetentionPolicy,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleTSGetRetentionPolicy_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTSTestLogger())

	payload, _ := EncodePayload(&service.TSGetRetentionPolicyRequest{})
	req := &Request{
		Opcode:  OpTSGetRetentionPolicy,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleTSGetRetentionPolicy_DecodeError(t *testing.T) {
	tsSvc := &mockTimeSeriesService{}
	handler := createTSEnabledHandler(tsSvc)

	req := &Request{
		Opcode:  OpTSGetRetentionPolicy,
		Payload: []byte("invalid json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleTSGetRetentionPolicy_ServiceNotAvailable(t *testing.T) {
	handler := createTSErrorHandler()

	payload, _ := EncodePayload(&service.TSGetRetentionPolicyRequest{Metric: "cpu.usage"})
	req := &Request{
		Opcode:  OpTSGetRetentionPolicy,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error when timeseries service not available")
	}
}

func TestHandleTSGetRetentionPolicy_ServiceError(t *testing.T) {
	tsSvc := &mockTimeSeriesService{
		getRetentionPolicyFn: func(ctx context.Context, req *service.TSGetRetentionPolicyRequest) (*service.TSGetRetentionPolicyResponse, error) {
			return nil, errors.New("get retention policy failed")
		},
	}
	handler := createTSEnabledHandler(tsSvc)

	payload, _ := EncodePayload(&service.TSGetRetentionPolicyRequest{Metric: "cpu.usage"})
	req := &Request{
		Opcode:  OpTSGetRetentionPolicy,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleTSSetRetentionPolicy Tests
// =============================================================================

func TestHandleTSSetRetentionPolicy_Success(t *testing.T) {
	tsSvc := &mockTimeSeriesService{
		setRetentionPolicyFn: func(ctx context.Context, req *service.TSSetRetentionPolicyRequest) (*service.TSSetRetentionPolicyResponse, error) {
			return &service.TSSetRetentionPolicyResponse{
				Success:   true,
				Timestamp: time.Now(),
			}, nil
		},
	}
	handler := createTSEnabledHandler(tsSvc)

	payload, err := EncodePayload(&service.TSSetRetentionPolicyRequest{
		Metric: "cpu.usage",
		Policy: &service.TSRetentionPolicy{
			Duration: 7 * 24 * time.Hour,
		},
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpTSSetRetentionPolicy,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleTSSetRetentionPolicy_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTSTestLogger())

	payload, _ := EncodePayload(&service.TSSetRetentionPolicyRequest{})
	req := &Request{
		Opcode:  OpTSSetRetentionPolicy,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleTSSetRetentionPolicy_DecodeError(t *testing.T) {
	tsSvc := &mockTimeSeriesService{}
	handler := createTSEnabledHandler(tsSvc)

	req := &Request{
		Opcode:  OpTSSetRetentionPolicy,
		Payload: []byte("invalid json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleTSSetRetentionPolicy_ServiceNotAvailable(t *testing.T) {
	handler := createTSErrorHandler()

	payload, _ := EncodePayload(&service.TSSetRetentionPolicyRequest{Metric: "cpu.usage"})
	req := &Request{
		Opcode:  OpTSSetRetentionPolicy,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error when timeseries service not available")
	}
}

func TestHandleTSSetRetentionPolicy_ServiceError(t *testing.T) {
	tsSvc := &mockTimeSeriesService{
		setRetentionPolicyFn: func(ctx context.Context, req *service.TSSetRetentionPolicyRequest) (*service.TSSetRetentionPolicyResponse, error) {
			return nil, errors.New("set retention policy failed")
		},
	}
	handler := createTSEnabledHandler(tsSvc)

	payload, _ := EncodePayload(&service.TSSetRetentionPolicyRequest{Metric: "cpu.usage"})
	req := &Request{
		Opcode:  OpTSSetRetentionPolicy,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleTSListMetrics Tests
// =============================================================================

func TestHandleTSListMetrics_Success(t *testing.T) {
	tsSvc := &mockTimeSeriesService{
		listMetricsFn: func(ctx context.Context, req *service.TSListMetricsRequest) (*service.TSListMetricsResponse, error) {
			return &service.TSListMetricsResponse{
				Metrics:   []string{"cpu.usage", "memory.free", "disk.io"},
				Timestamp: time.Now(),
			}, nil
		},
	}
	handler := createTSEnabledHandler(tsSvc)

	payload, err := EncodePayload(&service.TSListMetricsRequest{
		Prefix: "cpu.",
		Limit:  100,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpTSListMetrics,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleTSListMetrics_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTSTestLogger())

	payload, _ := EncodePayload(&service.TSListMetricsRequest{})
	req := &Request{
		Opcode:  OpTSListMetrics,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleTSListMetrics_DecodeError(t *testing.T) {
	tsSvc := &mockTimeSeriesService{}
	handler := createTSEnabledHandler(tsSvc)

	req := &Request{
		Opcode:  OpTSListMetrics,
		Payload: []byte("invalid json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleTSListMetrics_ServiceNotAvailable(t *testing.T) {
	handler := createTSErrorHandler()

	payload, _ := EncodePayload(&service.TSListMetricsRequest{})
	req := &Request{
		Opcode:  OpTSListMetrics,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error when timeseries service not available")
	}
}

func TestHandleTSListMetrics_ServiceError(t *testing.T) {
	tsSvc := &mockTimeSeriesService{
		listMetricsFn: func(ctx context.Context, req *service.TSListMetricsRequest) (*service.TSListMetricsResponse, error) {
			return nil, errors.New("list metrics failed")
		},
	}
	handler := createTSEnabledHandler(tsSvc)

	payload, _ := EncodePayload(&service.TSListMetricsRequest{})
	req := &Request{
		Opcode:  OpTSListMetrics,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}
