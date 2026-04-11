// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package timeseries

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// mockTransport is a mock transport for testing.
type mockTransport struct {
	requestFn       func(ctx context.Context, service, method string, req interface{}) (interface{}, error)
	requestStreamFn func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error)
}

func (m *mockTransport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	if m.requestFn != nil {
		return m.requestFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	if m.requestStreamFn != nil {
		return m.requestStreamFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) Conn() interface{} {
	return nil
}

func (m *mockTransport) Close() error {
	return nil
}

func (m *mockTransport) Healthy() bool {
	return true
}

func TestNewClient(t *testing.T) {
	t.Run("with valid transport", func(t *testing.T) {
		client := NewClient(&mockTransport{})
		if client == nil {
			t.Error("expected non-nil client")
		}
	})

	t.Run("with nil transport", func(t *testing.T) {
		client := NewClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})
}

func TestClient_Write_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *WriteRequest
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
			errMsg:  "request is required",
		},
		{
			name: "empty metric",
			req: &WriteRequest{
				Timestamp: time.Now(),
				Value:     1.0,
			},
			wantErr: true,
			errMsg:  "metric name is required",
		},
		{
			name: "zero timestamp",
			req: &WriteRequest{
				Metric: "cpu.usage",
				Value:  1.0,
			},
			wantErr: true,
			errMsg:  "timestamp is required",
		},
		{
			name: "valid request without tags",
			req: &WriteRequest{
				Metric:    "cpu.usage",
				Timestamp: time.Now(),
				Value:     75.5,
			},
			wantErr: false,
		},
		{
			name: "valid request with tags",
			req: &WriteRequest{
				Metric:    "cpu.usage",
				Tags:      map[string]string{"host": "server1", "region": "us-east"},
				Timestamp: time.Now(),
				Value:     75.5,
			},
			wantErr: false,
		},
		{
			name: "valid request with zero value",
			req: &WriteRequest{
				Metric:    "counter.reset",
				Timestamp: time.Now(),
				Value:     0.0,
			},
			wantErr: false,
		},
		{
			name: "valid request with negative value",
			req: &WriteRequest{
				Metric:    "temperature",
				Timestamp: time.Now(),
				Value:     -10.5,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.Write(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Write() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_Write_TransportError(t *testing.T) {
	transportErr := errors.NewUnavailable("transport", "connection failed")
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, transportErr
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	err := client.Write(ctx, &WriteRequest{
		Metric:    "cpu.usage",
		Timestamp: time.Now(),
		Value:     50.0,
	})

	if err == nil {
		t.Error("expected error from transport")
	}
	if !errors.IsUnavailable(err) {
		t.Errorf("expected Unavailable error, got %v", err)
	}
}

func TestClient_Write_VerifyServiceAndMethod(t *testing.T) {
	var capturedService, capturedMethod string
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			capturedService = service
			capturedMethod = method
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	err := client.Write(ctx, &WriteRequest{
		Metric:    "test.metric",
		Timestamp: time.Now(),
		Value:     1.0,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedService != "quicbaas.TimeSeries" {
		t.Errorf("expected service 'quicbaas.TimeSeries', got %q", capturedService)
	}
	if capturedMethod != "Write" {
		t.Errorf("expected method 'Write', got %q", capturedMethod)
	}
}

func TestClient_Query_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &QueryResponse{Series: []Point{}}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)

	tests := []struct {
		name    string
		req     *QueryRequest
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
			errMsg:  "request is required",
		},
		{
			name: "empty metric",
			req: &QueryRequest{
				Start: oneHourAgo,
				End:   now,
			},
			wantErr: true,
			errMsg:  "metric name is required",
		},
		{
			name: "zero start time",
			req: &QueryRequest{
				Metric: "cpu.usage",
				End:    now,
			},
			wantErr: true,
			errMsg:  "start time is required",
		},
		{
			name: "zero end time",
			req: &QueryRequest{
				Metric: "cpu.usage",
				Start:  oneHourAgo,
			},
			wantErr: true,
			errMsg:  "end time is required",
		},
		{
			name: "end before start",
			req: &QueryRequest{
				Metric: "cpu.usage",
				Start:  now,
				End:    oneHourAgo,
			},
			wantErr: true,
			errMsg:  "end time must be after start time",
		},
		{
			name: "invalid aggregate function",
			req: &QueryRequest{
				Metric:    "cpu.usage",
				Start:     oneHourAgo,
				End:       now,
				Aggregate: AggregateFunction(99),
			},
			wantErr: true,
			errMsg:  "invalid aggregate function",
		},
		{
			name: "aggregation without interval",
			req: &QueryRequest{
				Metric:    "cpu.usage",
				Start:     oneHourAgo,
				End:       now,
				Aggregate: AggregateAvg,
				Interval:  0,
			},
			wantErr: true,
			errMsg:  "interval is required when using aggregation",
		},
		{
			name: "aggregation with negative interval",
			req: &QueryRequest{
				Metric:    "cpu.usage",
				Start:     oneHourAgo,
				End:       now,
				Aggregate: AggregateSum,
				Interval:  -5 * time.Minute,
			},
			wantErr: true,
			errMsg:  "interval is required when using aggregation",
		},
		{
			name: "valid request without aggregation",
			req: &QueryRequest{
				Metric:    "cpu.usage",
				Start:     oneHourAgo,
				End:       now,
				Aggregate: AggregateNone,
			},
			wantErr: false,
		},
		{
			name: "valid request with tags",
			req: &QueryRequest{
				Metric: "cpu.usage",
				Tags:   map[string]string{"host": "server1"},
				Start:  oneHourAgo,
				End:    now,
			},
			wantErr: false,
		},
		{
			name: "valid request with aggregation",
			req: &QueryRequest{
				Metric:    "cpu.usage",
				Start:     oneHourAgo,
				End:       now,
				Aggregate: AggregateAvg,
				Interval:  5 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "valid request with all aggregate functions - Sum",
			req: &QueryRequest{
				Metric:    "requests.count",
				Start:     oneHourAgo,
				End:       now,
				Aggregate: AggregateSum,
				Interval:  1 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "valid request with all aggregate functions - Min",
			req: &QueryRequest{
				Metric:    "response.time",
				Start:     oneHourAgo,
				End:       now,
				Aggregate: AggregateMin,
				Interval:  1 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "valid request with all aggregate functions - Max",
			req: &QueryRequest{
				Metric:    "response.time",
				Start:     oneHourAgo,
				End:       now,
				Aggregate: AggregateMax,
				Interval:  1 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "valid request with all aggregate functions - Count",
			req: &QueryRequest{
				Metric:    "events",
				Start:     oneHourAgo,
				End:       now,
				Aggregate: AggregateCount,
				Interval:  1 * time.Minute,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Query(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Query() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_Query_TransportError(t *testing.T) {
	transportErr := errors.NewUnavailable("transport", "connection failed")
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, transportErr
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	now := time.Now()
	_, err := client.Query(ctx, &QueryRequest{
		Metric: "cpu.usage",
		Start:  now.Add(-1 * time.Hour),
		End:    now,
	})

	if err == nil {
		t.Error("expected error from transport")
	}
	if !errors.IsUnavailable(err) {
		t.Errorf("expected Unavailable error, got %v", err)
	}
}

func TestClient_Query_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	now := time.Now()
	resp, err := client.Query(ctx, &QueryRequest{
		Metric: "cpu.usage",
		Start:  now.Add(-1 * time.Hour),
		End:    now,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Series == nil {
		t.Error("expected non-nil Series")
	}
	if len(resp.Series) != 0 {
		t.Errorf("expected empty Series, got %d points", len(resp.Series))
	}
}

func TestClient_Query_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "invalid response type", nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	now := time.Now()
	_, err := client.Query(ctx, &QueryRequest{
		Metric: "cpu.usage",
		Start:  now.Add(-1 * time.Hour),
		End:    now,
	})

	if err == nil {
		t.Error("expected error for invalid response type")
	}
	var qbErr *errors.QBError
	if !errors.IsInvalidInput(err) {
		qbErr, ok := err.(*errors.QBError)
		if !ok || qbErr.Code != errors.CodeInternal {
			t.Errorf("expected Internal error, got %v", err)
		}
	}
	_ = qbErr // silence unused variable warning in some paths
}

func TestClient_Query_ValidResponse(t *testing.T) {
	expectedPoints := []Point{
		{Timestamp: time.Now().Add(-30 * time.Minute), Value: 50.0},
		{Timestamp: time.Now().Add(-15 * time.Minute), Value: 75.0},
		{Timestamp: time.Now(), Value: 60.0},
	}
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &QueryResponse{Series: expectedPoints}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	now := time.Now()
	resp, err := client.Query(ctx, &QueryRequest{
		Metric: "cpu.usage",
		Start:  now.Add(-1 * time.Hour),
		End:    now,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Series) != len(expectedPoints) {
		t.Errorf("expected %d points, got %d", len(expectedPoints), len(resp.Series))
	}
	for i, point := range resp.Series {
		if point.Value != expectedPoints[i].Value {
			t.Errorf("point %d: expected value %f, got %f", i, expectedPoints[i].Value, point.Value)
		}
	}
}

func TestClient_Query_VerifyServiceAndMethod(t *testing.T) {
	var capturedService, capturedMethod string
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			capturedService = service
			capturedMethod = method
			return &QueryResponse{Series: []Point{}}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	now := time.Now()
	_, err := client.Query(ctx, &QueryRequest{
		Metric: "test.metric",
		Start:  now.Add(-1 * time.Hour),
		End:    now,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedService != "quicbaas.TimeSeries" {
		t.Errorf("expected service 'quicbaas.TimeSeries', got %q", capturedService)
	}
	if capturedMethod != "Query" {
		t.Errorf("expected method 'Query', got %q", capturedMethod)
	}
}

func TestAggregateFunction_String(t *testing.T) {
	tests := []struct {
		fn       AggregateFunction
		expected string
	}{
		{AggregateNone, "NONE"},
		{AggregateSum, "SUM"},
		{AggregateAvg, "AVG"},
		{AggregateMin, "MIN"},
		{AggregateMax, "MAX"},
		{AggregateCount, "COUNT"},
		{AggregateFunction(99), "UNKNOWN"},
		{AggregateFunction(-1), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.fn.String(); got != tt.expected {
				t.Errorf("String() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestAggregateFunction_Valid(t *testing.T) {
	tests := []struct {
		fn    AggregateFunction
		valid bool
	}{
		{AggregateNone, true},
		{AggregateSum, true},
		{AggregateAvg, true},
		{AggregateMin, true},
		{AggregateMax, true},
		{AggregateCount, true},
		{AggregateFunction(99), false},
		{AggregateFunction(-1), false},
	}

	for _, tt := range tests {
		t.Run(tt.fn.String(), func(t *testing.T) {
			if got := tt.fn.Valid(); got != tt.valid {
				t.Errorf("Valid() = %v, expected %v", got, tt.valid)
			}
		})
	}
}

func TestValidateWriteRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *WriteRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name: "empty metric",
			req: &WriteRequest{
				Timestamp: time.Now(),
			},
			wantErr: true,
		},
		{
			name: "zero timestamp",
			req: &WriteRequest{
				Metric: "test",
			},
			wantErr: true,
		},
		{
			name: "valid request",
			req: &WriteRequest{
				Metric:    "test",
				Timestamp: time.Now(),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWriteRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateWriteRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateQueryRequest(t *testing.T) {
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)

	tests := []struct {
		name    string
		req     *QueryRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name: "empty metric",
			req: &QueryRequest{
				Start: oneHourAgo,
				End:   now,
			},
			wantErr: true,
		},
		{
			name: "zero start",
			req: &QueryRequest{
				Metric: "test",
				End:    now,
			},
			wantErr: true,
		},
		{
			name: "zero end",
			req: &QueryRequest{
				Metric: "test",
				Start:  oneHourAgo,
			},
			wantErr: true,
		},
		{
			name: "end before start",
			req: &QueryRequest{
				Metric: "test",
				Start:  now,
				End:    oneHourAgo,
			},
			wantErr: true,
		},
		{
			name: "invalid aggregate",
			req: &QueryRequest{
				Metric:    "test",
				Start:     oneHourAgo,
				End:       now,
				Aggregate: AggregateFunction(100),
			},
			wantErr: true,
		},
		{
			name: "aggregation without interval",
			req: &QueryRequest{
				Metric:    "test",
				Start:     oneHourAgo,
				End:       now,
				Aggregate: AggregateSum,
			},
			wantErr: true,
		},
		{
			name: "valid no aggregation",
			req: &QueryRequest{
				Metric:    "test",
				Start:     oneHourAgo,
				End:       now,
				Aggregate: AggregateNone,
			},
			wantErr: false,
		},
		{
			name: "valid with aggregation",
			req: &QueryRequest{
				Metric:    "test",
				Start:     oneHourAgo,
				End:       now,
				Aggregate: AggregateAvg,
				Interval:  5 * time.Minute,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateQueryRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateQueryRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_Write_ContextCancellation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, ctx.Err()
		},
	}
	client := NewClient(mockT)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := client.Write(ctx, &WriteRequest{
		Metric:    "cpu.usage",
		Timestamp: time.Now(),
		Value:     50.0,
	})

	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestClient_Query_ContextCancellation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, ctx.Err()
		},
	}
	client := NewClient(mockT)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	now := time.Now()
	_, err := client.Query(ctx, &QueryRequest{
		Metric: "cpu.usage",
		Start:  now.Add(-1 * time.Hour),
		End:    now,
	})

	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestClient_Write_RequestPassthrough(t *testing.T) {
	var capturedReq interface{}
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			capturedReq = req
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	originalReq := &WriteRequest{
		Metric:    "cpu.usage",
		Tags:      map[string]string{"host": "server1"},
		Timestamp: time.Now(),
		Value:     75.5,
	}

	err := client.Write(ctx, originalReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedReq != originalReq {
		t.Error("request was not passed through to transport")
	}
}

func TestClient_Query_RequestPassthrough(t *testing.T) {
	var capturedReq interface{}
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			capturedReq = req
			return &QueryResponse{Series: []Point{}}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	now := time.Now()
	originalReq := &QueryRequest{
		Metric:    "cpu.usage",
		Tags:      map[string]string{"host": "server1"},
		Start:     now.Add(-1 * time.Hour),
		End:       now,
		Aggregate: AggregateAvg,
		Interval:  5 * time.Minute,
	}

	_, err := client.Query(ctx, originalReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedReq != originalReq {
		t.Error("request was not passed through to transport")
	}
}

func TestPoint_Fields(t *testing.T) {
	ts := time.Now()
	point := Point{
		Timestamp: ts,
		Value:     123.456,
	}

	if point.Timestamp != ts {
		t.Errorf("expected timestamp %v, got %v", ts, point.Timestamp)
	}
	if point.Value != 123.456 {
		t.Errorf("expected value 123.456, got %f", point.Value)
	}
}

func TestWriteRequest_Fields(t *testing.T) {
	ts := time.Now()
	tags := map[string]string{"host": "server1", "region": "us-east"}
	req := WriteRequest{
		Metric:    "cpu.usage",
		Tags:      tags,
		Timestamp: ts,
		Value:     75.5,
	}

	if req.Metric != "cpu.usage" {
		t.Errorf("expected metric 'cpu.usage', got %q", req.Metric)
	}
	if len(req.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(req.Tags))
	}
	if req.Tags["host"] != "server1" {
		t.Errorf("expected tag host=server1, got %q", req.Tags["host"])
	}
	if req.Timestamp != ts {
		t.Errorf("expected timestamp %v, got %v", ts, req.Timestamp)
	}
	if req.Value != 75.5 {
		t.Errorf("expected value 75.5, got %f", req.Value)
	}
}

func TestQueryRequest_Fields(t *testing.T) {
	now := time.Now()
	start := now.Add(-1 * time.Hour)
	tags := map[string]string{"host": "server1"}
	req := QueryRequest{
		Metric:    "cpu.usage",
		Tags:      tags,
		Start:     start,
		End:       now,
		Aggregate: AggregateAvg,
		Interval:  5 * time.Minute,
	}

	if req.Metric != "cpu.usage" {
		t.Errorf("expected metric 'cpu.usage', got %q", req.Metric)
	}
	if len(req.Tags) != 1 {
		t.Errorf("expected 1 tag, got %d", len(req.Tags))
	}
	if req.Start != start {
		t.Errorf("expected start %v, got %v", start, req.Start)
	}
	if req.End != now {
		t.Errorf("expected end %v, got %v", now, req.End)
	}
	if req.Aggregate != AggregateAvg {
		t.Errorf("expected aggregate AggregateAvg, got %v", req.Aggregate)
	}
	if req.Interval != 5*time.Minute {
		t.Errorf("expected interval 5m, got %v", req.Interval)
	}
}

func TestQueryResponse_Fields(t *testing.T) {
	points := []Point{
		{Timestamp: time.Now(), Value: 1.0},
		{Timestamp: time.Now(), Value: 2.0},
	}
	resp := QueryResponse{
		Series: points,
	}

	if len(resp.Series) != 2 {
		t.Errorf("expected 2 points, got %d", len(resp.Series))
	}
}
