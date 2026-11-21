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

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/backup"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/health"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/query"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	svc "github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/test/mock"
)

// ============================================================================
// Vector Client Tests
// ============================================================================

func TestVectorClient_UpsertWithMetadata(t *testing.T) {
	mt := mock.NewTransport()
	client := vector.NewClient(mt)
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	expectedID := "vec-001"
	mt.SetResponse("quicbaas.Vector", "Upsert", &vector.UpsertResponse{ID: expectedID}, nil)

	ctx := context.Background()
	req := &vector.UpsertRequest{
		Vector: &vector.VectorEntity{
			Namespace: "documents",
			ID:        "vec-001",
			Vector:    []float32{0.1, 0.2, 0.3, 0.4, 0.5},
			Metadata: map[string]string{
				"title":    "Test Document",
				"category": "testing",
			},
		},
	}

	resp, err := client.Upsert(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.ID != expectedID {
		t.Errorf("expected ID %q, got %q", expectedID, resp.ID)
	}

	// Verify the request was captured correctly
	requests := mt.Requests()
	if len(requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(requests))
	}
	if requests[0].Service != "quicbaas.Vector" {
		t.Errorf("expected service %q, got %q", "quicbaas.Vector", requests[0].Service)
	}
	if requests[0].Method != "Upsert" {
		t.Errorf("expected method %q, got %q", "Upsert", requests[0].Method)
	}
}

func TestVectorClient_SearchSimilarWithTopK(t *testing.T) {
	mt := mock.NewTransport()
	client := vector.NewClient(mt)

	expectedResults := []*vector.SearchResult{
		{Vector: &vector.VectorEntity{ID: "vec-001", Vector: []float32{0.1, 0.2, 0.3}, Metadata: map[string]string{"category": "testing"}}, Score: 0.95},
		{Vector: &vector.VectorEntity{ID: "vec-002", Vector: []float32{0.2, 0.3, 0.4}, Metadata: map[string]string{"category": "testing"}}, Score: 0.87},
		{Vector: &vector.VectorEntity{ID: "vec-003", Vector: []float32{0.3, 0.4, 0.5}, Metadata: map[string]string{"category": "testing"}}, Score: 0.75},
	}
	mt.SetResponse("quicbaas.Vector", "SearchSimilar", &vector.SearchSimilarResponse{Results: expectedResults}, nil)

	ctx := context.Background()
	req := &vector.SearchSimilarRequest{
		Namespace:   "documents",
		QueryVector: []float32{0.15, 0.25, 0.35},
		K:           3,
	}

	resp, err := client.SearchSimilar(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Results) != 3 {
		t.Errorf("expected 3 results, got %d", len(resp.Results))
	}

	// Verify results are ordered by score
	for i := 0; i < len(resp.Results)-1; i++ {
		if resp.Results[i].Score < resp.Results[i+1].Score {
			t.Errorf("results not in descending score order at index %d", i)
		}
	}
}

func TestVectorClient_Delete(t *testing.T) {
	mt := mock.NewTransport()
	client := vector.NewClient(mt)

	mt.SetResponse("quicbaas.Vector", "Delete", &vector.DeleteResponse{Deleted: true}, nil)

	ctx := context.Background()
	req := &vector.DeleteRequest{
		Namespace: "documents",
		ID:        "vec-001",
	}

	resp, err := client.Delete(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Deleted {
		t.Error("expected Deleted to be true")
	}

	// Verify the request was captured
	requests := mt.Requests()
	if len(requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(requests))
	}
	payload, ok := requests[0].Payload.(*vector.DeleteRequest)
	if !ok {
		t.Fatal("expected DeleteRequest payload")
	}
	if payload.ID != "vec-001" {
		t.Errorf("expected ID %q, got %q", "vec-001", payload.ID)
	}
}

func TestVectorClient_SearchSimilarWithEmptyResults(t *testing.T) {
	mt := mock.NewTransport()
	client := vector.NewClient(mt)

	// Return nil to simulate empty results - SearchSimilar returns
	// an empty response when server returns nil.
	mt.SetResponse("quicbaas.Vector", "SearchSimilar", nil, nil)

	ctx := context.Background()
	req := &vector.SearchSimilarRequest{
		Namespace:   "documents",
		QueryVector: []float32{0.99, 0.99, 0.99},
		K:           10,
	}

	resp, err := client.SearchSimilar(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(resp.Results))
	}
}

func TestVectorClient_UpsertWithHighDimensionalVector(t *testing.T) {
	mt := mock.NewTransport()
	client := vector.NewClient(mt)

	expectedID := "high-dim-001"
	mt.SetResponse("quicbaas.Vector", "Upsert", &vector.UpsertResponse{ID: expectedID}, nil)

	// Create a high-dimensional vector (e.g., 1536 dimensions like OpenAI embeddings)
	highDimVector := make([]float32, 1536)
	for i := range highDimVector {
		highDimVector[i] = float32(i) / 1536.0
	}

	ctx := context.Background()
	req := &vector.UpsertRequest{
		Vector: &vector.VectorEntity{
			Namespace: "embeddings",
			ID:        expectedID,
			Vector:    highDimVector,
			Metadata: map[string]string{
				"model":      "text-embedding-ada-002",
				"dimensions": "1536",
			},
		},
	}

	resp, err := client.Upsert(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != expectedID {
		t.Errorf("expected ID %q, got %q", expectedID, resp.ID)
	}

	// Verify the vector was sent correctly
	requests := mt.Requests()
	if len(requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(requests))
	}
	payload, ok := requests[0].Payload.(*vector.UpsertRequest)
	if !ok {
		t.Fatal("expected UpsertRequest payload")
	}
	if len(payload.Vector.Vector) != 1536 {
		t.Errorf("expected 1536 dimensions, got %d", len(payload.Vector.Vector))
	}
}

func TestVectorClient_UpsertValidationErrors(t *testing.T) {
	mt := mock.NewTransport()
	client := vector.NewClient(mt)
	ctx := context.Background()

	testCases := []struct {
		name    string
		req     *vector.UpsertRequest
		wantErr string
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: "request is required",
		},
		{
			name:    "nil vector entity",
			req:     &vector.UpsertRequest{},
			wantErr: "vector is required",
		},
		{
			name:    "empty ID",
			req:     &vector.UpsertRequest{Vector: &vector.VectorEntity{Namespace: "test", Vector: []float32{0.1}}},
			wantErr: "vector ID is required",
		},
		{
			name:    "empty namespace",
			req:     &vector.UpsertRequest{Vector: &vector.VectorEntity{ID: "test", Vector: []float32{0.1}}},
			wantErr: "namespace is required",
		},
		{
			name:    "empty vector data",
			req:     &vector.UpsertRequest{Vector: &vector.VectorEntity{ID: "test", Namespace: "test"}},
			wantErr: "vector data is required",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.Upsert(ctx, tc.req)
			if err == nil {
				t.Fatal("expected error")
			}
			sdkErr, ok := err.(*errors.QBError)
			if !ok {
				t.Fatalf("expected *errors.QBError, got %T", err)
			}
			if sdkErr.Message != tc.wantErr {
				t.Errorf("expected message %q, got %q", tc.wantErr, sdkErr.Message)
			}
		})
	}
}

func TestVectorClient_SearchSimilarValidationErrors(t *testing.T) {
	mt := mock.NewTransport()
	client := vector.NewClient(mt)
	ctx := context.Background()

	testCases := []struct {
		name    string
		req     *vector.SearchSimilarRequest
		wantErr string
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: "request is required",
		},
		{
			name:    "empty namespace",
			req:     &vector.SearchSimilarRequest{QueryVector: []float32{0.1}, K: 10},
			wantErr: "namespace is required",
		},
		{
			name:    "empty query vector",
			req:     &vector.SearchSimilarRequest{Namespace: "test", K: 10},
			wantErr: "query vector is required",
		},
		{
			name:    "invalid K",
			req:     &vector.SearchSimilarRequest{Namespace: "test", QueryVector: []float32{0.1}, K: 0},
			wantErr: "K must be at least 1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.SearchSimilar(ctx, tc.req)
			if err == nil {
				t.Fatal("expected error")
			}
			sdkErr, ok := err.(*errors.QBError)
			if !ok {
				t.Fatalf("expected *errors.QBError, got %T", err)
			}
			if sdkErr.Message != tc.wantErr {
				t.Errorf("expected message %q, got %q", tc.wantErr, sdkErr.Message)
			}
		})
	}
}

// ============================================================================
// TimeSeries Client Tests
// ============================================================================

func TestTimeSeriesClient_WriteSingleDataPoint(t *testing.T) {
	mt := mock.NewTransport()
	client := timeseries.NewClient(mt)
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	mt.SetResponse("quicbaas.TimeSeries", "Write", nil, nil)

	ctx := context.Background()
	now := time.Now()
	req := &timeseries.WriteRequest{
		Metric:    "cpu.usage",
		Timestamp: now,
		Value:     75.5,
	}

	err := client.Write(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify request was captured
	requests := mt.Requests()
	if len(requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(requests))
	}
	payload, ok := requests[0].Payload.(*timeseries.WriteRequest)
	if !ok {
		t.Fatal("expected WriteRequest payload")
	}
	if payload.Metric != "cpu.usage" {
		t.Errorf("expected metric %q, got %q", "cpu.usage", payload.Metric)
	}
	if payload.Value != 75.5 {
		t.Errorf("expected value %f, got %f", 75.5, payload.Value)
	}
}

func TestTimeSeriesClient_WriteWithTags(t *testing.T) {
	mt := mock.NewTransport()
	client := timeseries.NewClient(mt)

	mt.SetResponse("quicbaas.TimeSeries", "Write", nil, nil)

	ctx := context.Background()
	req := &timeseries.WriteRequest{
		Metric:    "http.requests",
		Timestamp: time.Now(),
		Value:     100,
		Tags: map[string]string{
			"host":   "server-01",
			"region": "us-east-1",
			"method": "GET",
			"path":   "/api/v1/health",
		},
	}

	err := client.Write(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify tags were captured
	requests := mt.Requests()
	payload, ok := requests[0].Payload.(*timeseries.WriteRequest)
	if !ok {
		t.Fatal("expected WriteRequest payload")
	}
	if len(payload.Tags) != 4 {
		t.Errorf("expected 4 tags, got %d", len(payload.Tags))
	}
	if payload.Tags["host"] != "server-01" {
		t.Errorf("expected host tag %q, got %q", "server-01", payload.Tags["host"])
	}
}

func TestTimeSeriesClient_QueryRawData(t *testing.T) {
	mt := mock.NewTransport()
	client := timeseries.NewClient(mt)

	now := time.Now()
	expectedPoints := []timeseries.Point{
		{Timestamp: now.Add(-5 * time.Minute), Value: 50.0},
		{Timestamp: now.Add(-4 * time.Minute), Value: 55.0},
		{Timestamp: now.Add(-3 * time.Minute), Value: 60.0},
		{Timestamp: now.Add(-2 * time.Minute), Value: 65.0},
		{Timestamp: now.Add(-1 * time.Minute), Value: 70.0},
	}
	mt.SetResponse("quicbaas.TimeSeries", "Query", &timeseries.QueryResponse{Series: expectedPoints}, nil)

	ctx := context.Background()
	req := &timeseries.QueryRequest{
		Metric:    "cpu.usage",
		Start:     now.Add(-5 * time.Minute),
		End:       now,
		Aggregate: timeseries.AggregateNone,
	}

	resp, err := client.Query(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Series) != 5 {
		t.Errorf("expected 5 data points, got %d", len(resp.Series))
	}
}

func TestTimeSeriesClient_QueryWithSUMAggregation(t *testing.T) {
	mt := mock.NewTransport()
	client := timeseries.NewClient(mt)

	now := time.Now()
	expectedPoints := []timeseries.Point{
		{Timestamp: now.Add(-5 * time.Minute), Value: 300.0}, // Sum of values in bucket
	}
	mt.SetResponse("quicbaas.TimeSeries", "Query", &timeseries.QueryResponse{Series: expectedPoints}, nil)

	ctx := context.Background()
	req := &timeseries.QueryRequest{
		Metric:    "http.requests",
		Start:     now.Add(-5 * time.Minute),
		End:       now,
		Aggregate: timeseries.AggregateSum,
		Interval:  5 * time.Minute,
	}

	resp, err := client.Query(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Series[0].Value != 300.0 {
		t.Errorf("expected sum value %f, got %f", 300.0, resp.Series[0].Value)
	}
}

func TestTimeSeriesClient_QueryWithAVGAggregation(t *testing.T) {
	mt := mock.NewTransport()
	client := timeseries.NewClient(mt)

	now := time.Now()
	expectedPoints := []timeseries.Point{
		{Timestamp: now.Add(-5 * time.Minute), Value: 60.0}, // Average of values in bucket
	}
	mt.SetResponse("quicbaas.TimeSeries", "Query", &timeseries.QueryResponse{Series: expectedPoints}, nil)

	ctx := context.Background()
	req := &timeseries.QueryRequest{
		Metric:    "cpu.usage",
		Start:     now.Add(-5 * time.Minute),
		End:       now,
		Aggregate: timeseries.AggregateAvg,
		Interval:  5 * time.Minute,
	}

	resp, err := client.Query(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Series[0].Value != 60.0 {
		t.Errorf("expected avg value %f, got %f", 60.0, resp.Series[0].Value)
	}
}

func TestTimeSeriesClient_QueryWithMINMAXAggregation(t *testing.T) {
	mt := mock.NewTransport()
	client := timeseries.NewClient(mt)

	now := time.Now()

	// Test MIN
	mt.SetResponse("quicbaas.TimeSeries", "Query", &timeseries.QueryResponse{
		Series: []timeseries.Point{{Timestamp: now.Add(-5 * time.Minute), Value: 25.0}},
	}, nil)

	ctx := context.Background()
	minReq := &timeseries.QueryRequest{
		Metric:    "temperature",
		Start:     now.Add(-5 * time.Minute),
		End:       now,
		Aggregate: timeseries.AggregateMin,
		Interval:  5 * time.Minute,
	}

	resp, err := client.Query(ctx, minReq)
	if err != nil {
		t.Fatalf("MIN query error: %v", err)
	}
	if resp.Series[0].Value != 25.0 {
		t.Errorf("expected min value %f, got %f", 25.0, resp.Series[0].Value)
	}

	// Reset transport and test MAX
	mt.Reset()
	mt.SetResponse("quicbaas.TimeSeries", "Query", &timeseries.QueryResponse{
		Series: []timeseries.Point{{Timestamp: now.Add(-5 * time.Minute), Value: 95.0}},
	}, nil)

	maxReq := &timeseries.QueryRequest{
		Metric:    "temperature",
		Start:     now.Add(-5 * time.Minute),
		End:       now,
		Aggregate: timeseries.AggregateMax,
		Interval:  5 * time.Minute,
	}

	resp, err = client.Query(ctx, maxReq)
	if err != nil {
		t.Fatalf("MAX query error: %v", err)
	}
	if resp.Series[0].Value != 95.0 {
		t.Errorf("expected max value %f, got %f", 95.0, resp.Series[0].Value)
	}
}

func TestTimeSeriesClient_QueryWithCOUNTAggregation(t *testing.T) {
	mt := mock.NewTransport()
	client := timeseries.NewClient(mt)

	now := time.Now()
	expectedPoints := []timeseries.Point{
		{Timestamp: now.Add(-5 * time.Minute), Value: 150.0}, // Count of values in bucket
	}
	mt.SetResponse("quicbaas.TimeSeries", "Query", &timeseries.QueryResponse{Series: expectedPoints}, nil)

	ctx := context.Background()
	req := &timeseries.QueryRequest{
		Metric:    "http.requests",
		Start:     now.Add(-5 * time.Minute),
		End:       now,
		Aggregate: timeseries.AggregateCount,
		Interval:  5 * time.Minute,
	}

	resp, err := client.Query(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Series[0].Value != 150.0 {
		t.Errorf("expected count value %f, got %f", 150.0, resp.Series[0].Value)
	}
}

func TestTimeSeriesClient_QueryWithIntervalBucketing(t *testing.T) {
	mt := mock.NewTransport()
	client := timeseries.NewClient(mt)

	now := time.Now()
	// 6 buckets of 5 minutes each for 30 minutes of data
	expectedPoints := []timeseries.Point{
		{Timestamp: now.Add(-30 * time.Minute), Value: 100.0},
		{Timestamp: now.Add(-25 * time.Minute), Value: 110.0},
		{Timestamp: now.Add(-20 * time.Minute), Value: 120.0},
		{Timestamp: now.Add(-15 * time.Minute), Value: 130.0},
		{Timestamp: now.Add(-10 * time.Minute), Value: 140.0},
		{Timestamp: now.Add(-5 * time.Minute), Value: 150.0},
	}
	mt.SetResponse("quicbaas.TimeSeries", "Query", &timeseries.QueryResponse{Series: expectedPoints}, nil)

	ctx := context.Background()
	req := &timeseries.QueryRequest{
		Metric:    "http.requests",
		Start:     now.Add(-30 * time.Minute),
		End:       now,
		Aggregate: timeseries.AggregateSum,
		Interval:  5 * time.Minute,
	}

	resp, err := client.Query(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Series) != 6 {
		t.Errorf("expected 6 buckets, got %d", len(resp.Series))
	}

	// Verify bucket timestamps are properly spaced
	for i := 0; i < len(resp.Series)-1; i++ {
		diff := resp.Series[i+1].Timestamp.Sub(resp.Series[i].Timestamp)
		if diff != 5*time.Minute {
			t.Errorf("expected 5 minute interval at bucket %d, got %v", i, diff)
		}
	}
}

func TestTimeSeriesClient_WriteValidationErrors(t *testing.T) {
	mt := mock.NewTransport()
	client := timeseries.NewClient(mt)
	ctx := context.Background()

	testCases := []struct {
		name    string
		req     *timeseries.WriteRequest
		wantErr string
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: "request is required",
		},
		{
			name:    "empty metric",
			req:     &timeseries.WriteRequest{Timestamp: time.Now(), Value: 50.0},
			wantErr: "metric name is required",
		},
		{
			name:    "zero timestamp",
			req:     &timeseries.WriteRequest{Metric: "cpu", Value: 50.0},
			wantErr: "timestamp is required",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := client.Write(ctx, tc.req)
			if err == nil {
				t.Fatal("expected error")
			}
			sdkErr, ok := err.(*errors.QBError)
			if !ok {
				t.Fatalf("expected *errors.QBError, got %T", err)
			}
			if sdkErr.Message != tc.wantErr {
				t.Errorf("expected message %q, got %q", tc.wantErr, sdkErr.Message)
			}
		})
	}
}

func TestTimeSeriesClient_QueryValidationErrors(t *testing.T) {
	mt := mock.NewTransport()
	client := timeseries.NewClient(mt)
	ctx := context.Background()
	now := time.Now()

	testCases := []struct {
		name    string
		req     *timeseries.QueryRequest
		wantErr string
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: "request is required",
		},
		{
			name:    "empty metric",
			req:     &timeseries.QueryRequest{Start: now.Add(-1 * time.Hour), End: now},
			wantErr: "metric name is required",
		},
		{
			name:    "zero start time",
			req:     &timeseries.QueryRequest{Metric: "cpu", End: now},
			wantErr: "start time is required",
		},
		{
			name:    "zero end time",
			req:     &timeseries.QueryRequest{Metric: "cpu", Start: now.Add(-1 * time.Hour)},
			wantErr: "end time is required",
		},
		{
			name:    "end before start",
			req:     &timeseries.QueryRequest{Metric: "cpu", Start: now, End: now.Add(-1 * time.Hour)},
			wantErr: "end time must be after start time",
		},
		{
			name: "aggregation without interval",
			req: &timeseries.QueryRequest{
				Metric:    "cpu",
				Start:     now.Add(-1 * time.Hour),
				End:       now,
				Aggregate: timeseries.AggregateSum,
			},
			wantErr: "interval is required when using aggregation",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.Query(ctx, tc.req)
			if err == nil {
				t.Fatal("expected error")
			}
			sdkErr, ok := err.(*errors.QBError)
			if !ok {
				t.Fatalf("expected *errors.QBError, got %T", err)
			}
			if sdkErr.Message != tc.wantErr {
				t.Errorf("expected message %q, got %q", tc.wantErr, sdkErr.Message)
			}
		})
	}
}

// ============================================================================
// Backup Client Tests
// ============================================================================

func TestBackupClient_CreateCompressedEncrypted(t *testing.T) {
	mt := mock.NewTransport()
	client := backup.NewClient(mt)
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	expectedResp := &backup.BackupResponse{
		BackupID:  "backup-20251227-001",
		Size:      1024 * 1024 * 100, // 100MB
		Duration:  5 * time.Second,
		CreatedAt: time.Now(),
	}
	mt.SetResponse("quicbaas.Backup", "Create", expectedResp, nil)

	ctx := context.Background()
	req := &backup.BackupRequest{
		Destination: "s3://my-bucket/backups/",
		Compress:    true,
		Encrypt:     true,
	}

	resp, err := client.Create(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.BackupID != expectedResp.BackupID {
		t.Errorf("expected backup ID %q, got %q", expectedResp.BackupID, resp.BackupID)
	}
	if resp.Size != expectedResp.Size {
		t.Errorf("expected size %d, got %d", expectedResp.Size, resp.Size)
	}

	// Verify request was captured with correct flags
	requests := mt.Requests()
	payload, ok := requests[0].Payload.(*backup.BackupRequest)
	if !ok {
		t.Fatal("expected BackupRequest payload")
	}
	if !payload.Compress {
		t.Error("expected Compress to be true")
	}
	if !payload.Encrypt {
		t.Error("expected Encrypt to be true")
	}
}

func TestBackupClient_CreateUncompressed(t *testing.T) {
	mt := mock.NewTransport()
	client := backup.NewClient(mt)

	expectedResp := &backup.BackupResponse{
		BackupID:  "backup-20251227-002",
		Size:      1024 * 1024 * 500, // 500MB (larger because uncompressed)
		Duration:  3 * time.Second,
		CreatedAt: time.Now(),
	}
	mt.SetResponse("quicbaas.Backup", "Create", expectedResp, nil)

	ctx := context.Background()
	req := &backup.BackupRequest{
		Destination: "/mnt/backups/",
		Compress:    false,
		Encrypt:     false,
	}

	resp, err := client.Create(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.BackupID != expectedResp.BackupID {
		t.Errorf("expected backup ID %q, got %q", expectedResp.BackupID, resp.BackupID)
	}
}

func TestBackupClient_RestoreFromBackupID(t *testing.T) {
	mt := mock.NewTransport()
	client := backup.NewClient(mt)

	serviceResp := &svc.RestoreBackupResponse{
		JobID:  "restore-backup-20251227-001",
		Status: "completed",
	}
	mt.SetResponse("quicbaas.Backup", "Restore", serviceResp, nil)

	ctx := context.Background()
	req := &backup.RestoreRequest{
		BackupID: "backup-20251227-001",
	}

	resp, err := client.Restore(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if !resp.Restored {
		t.Error("expected Restored to be true")
	}
	if resp.JobID != serviceResp.JobID {
		t.Errorf("expected job ID %q, got %q", serviceResp.JobID, resp.JobID)
	}

	payload, ok := mt.Requests()[0].Payload.(*svc.RestoreBackupRequest)
	if !ok {
		t.Fatal("expected RestoreBackupRequest payload")
	}
	if payload.BackupID != req.BackupID {
		t.Errorf("expected backup ID %q, got %q", req.BackupID, payload.BackupID)
	}
}

func TestBackupClient_RestoreFromExternalSource(t *testing.T) {
	mt := mock.NewTransport()
	client := backup.NewClient(mt)

	ctx := context.Background()
	req := &backup.RestoreRequest{
		Source: "s3://external-bucket/backups/snapshot-2025-12-27.tar.gz",
	}

	resp, err := client.Restore(ctx, req)
	if !errors.IsInvalidInput(err) {
		t.Fatalf("expected invalid input error, got %v", err)
	}
	if resp != nil {
		t.Errorf("expected nil response, got %+v", resp)
	}
	if n := len(mt.Requests()); n != 0 {
		t.Errorf("expected no transport request, got %d", n)
	}
}

func TestBackupClient_ListWithResults(t *testing.T) {
	mt := mock.NewTransport()
	client := backup.NewClient(mt)

	now := time.Now()
	expectedResp := &backup.ListBackupsResponse{
		Backups: []backup.BackupInfo{
			{ID: "backup-001", Size: 100 * 1024 * 1024, CreatedAt: now.Add(-24 * time.Hour), Encrypted: true},
			{ID: "backup-002", Size: 95 * 1024 * 1024, CreatedAt: now.Add(-48 * time.Hour), Encrypted: true},
			{ID: "backup-003", Size: 110 * 1024 * 1024, CreatedAt: now.Add(-72 * time.Hour), Encrypted: false},
		},
	}
	mt.SetResponse("quicbaas.Backup", "List", expectedResp, nil)

	ctx := context.Background()
	resp, err := client.List(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Backups) != 3 {
		t.Errorf("expected 3 backups, got %d", len(resp.Backups))
	}

	// Verify backup info
	if resp.Backups[0].ID != "backup-001" {
		t.Errorf("expected first backup ID %q, got %q", "backup-001", resp.Backups[0].ID)
	}
	if !resp.Backups[0].Encrypted {
		t.Error("expected first backup to be encrypted")
	}
}

func TestBackupClient_ListEmpty(t *testing.T) {
	mt := mock.NewTransport()
	client := backup.NewClient(mt)

	// Return nil to simulate empty list
	mt.SetResponse("quicbaas.Backup", "List", nil, nil)

	ctx := context.Background()
	resp, err := client.List(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Backups) != 0 {
		t.Errorf("expected 0 backups, got %d", len(resp.Backups))
	}
}

func TestBackupClient_CreateValidationErrors(t *testing.T) {
	mt := mock.NewTransport()
	client := backup.NewClient(mt)
	ctx := context.Background()

	testCases := []struct {
		name    string
		req     *backup.BackupRequest
		wantErr string
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: "request is required",
		},
		{
			name:    "empty destination",
			req:     &backup.BackupRequest{Compress: true},
			wantErr: "destination is required",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.Create(ctx, tc.req)
			if err == nil {
				t.Fatal("expected error")
			}
			sdkErr, ok := err.(*errors.QBError)
			if !ok {
				t.Fatalf("expected *errors.QBError, got %T", err)
			}
			if sdkErr.Message != tc.wantErr {
				t.Errorf("expected message %q, got %q", tc.wantErr, sdkErr.Message)
			}
		})
	}
}

func TestBackupClient_RestoreValidationErrors(t *testing.T) {
	mt := mock.NewTransport()
	client := backup.NewClient(mt)
	ctx := context.Background()

	testCases := []struct {
		name    string
		req     *backup.RestoreRequest
		wantErr string
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: "request is required",
		},
		{
			name:    "empty backup ID and source",
			req:     &backup.RestoreRequest{},
			wantErr: "backup ID is required",
		},
		{
			name:    "source only",
			req:     &backup.RestoreRequest{Source: "nightly"},
			wantErr: "restore from a source is not supported; set backup ID",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.Restore(ctx, tc.req)
			if err == nil {
				t.Fatal("expected error")
			}
			sdkErr, ok := err.(*errors.QBError)
			if !ok {
				t.Fatalf("expected *errors.QBError, got %T", err)
			}
			if sdkErr.Message != tc.wantErr {
				t.Errorf("expected message %q, got %q", tc.wantErr, sdkErr.Message)
			}
		})
	}
}

// ============================================================================
// Cluster Client Tests
// ============================================================================

func TestClusterClient_Status(t *testing.T) {
	mt := mock.NewTransport()
	client := cluster.NewClient(mt)
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	expectedStatus := &cluster.ClusterStatus{
		ClusterID: "cluster-001",
		LeaderID:  "node-1",
		Healthy:   true,
		Members: []cluster.ClusterMember{
			{ID: "node-1", Name: "quicbaas-1", Address: "10.0.0.1:5001", IsLeader: true, Healthy: true},
			{ID: "node-2", Name: "quicbaas-2", Address: "10.0.0.2:5001", IsLeader: false, Healthy: true},
			{ID: "node-3", Name: "quicbaas-3", Address: "10.0.0.3:5001", IsLeader: false, Healthy: true},
		},
	}
	mt.SetResponse("quicbaas.Cluster", "Status", expectedStatus, nil)

	ctx := context.Background()
	status, err := client.Status(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.ClusterID != expectedStatus.ClusterID {
		t.Errorf("expected cluster ID %q, got %q", expectedStatus.ClusterID, status.ClusterID)
	}
	if status.LeaderID != expectedStatus.LeaderID {
		t.Errorf("expected leader ID %q, got %q", expectedStatus.LeaderID, status.LeaderID)
	}
	if !status.Healthy {
		t.Error("expected cluster to be healthy")
	}
	if len(status.Members) != 3 {
		t.Errorf("expected 3 members, got %d", len(status.Members))
	}
}

func TestClusterClient_Nodes(t *testing.T) {
	mt := mock.NewTransport()
	client := cluster.NewClient(mt)

	expectedResp := &cluster.NodesResponse{
		Nodes: []cluster.NodeInfo{
			{ID: "node-1", Name: "quicbaas-1", Address: "10.0.0.1:5001", State: cluster.NodeStateHealthy},
			{ID: "node-2", Name: "quicbaas-2", Address: "10.0.0.2:5001", State: cluster.NodeStateHealthy},
			{ID: "node-3", Name: "quicbaas-3", Address: "10.0.0.3:5001", State: cluster.NodeStateUnhealthy},
		},
	}
	mt.SetResponse("quicbaas.Cluster", "Nodes", expectedResp, nil)

	ctx := context.Background()
	resp, err := client.Nodes(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(resp.Nodes))
	}

	// Verify node states
	if resp.Nodes[0].State != cluster.NodeStateHealthy {
		t.Errorf("expected node-1 state HEALTHY, got %s", resp.Nodes[0].State.String())
	}
	if resp.Nodes[2].State != cluster.NodeStateUnhealthy {
		t.Errorf("expected node-3 state UNHEALTHY, got %s", resp.Nodes[2].State.String())
	}
}

func TestClusterClient_AddNode(t *testing.T) {
	mt := mock.NewTransport()
	client := cluster.NewClient(mt)

	mt.SetResponse("quicbaas.Cluster", "AddNode", nil, nil)

	ctx := context.Background()
	req := &cluster.AddNodeRequest{
		ShardID: 1,
		NodeID:  4,
		Address: "10.0.0.4:5001",
	}

	err := client.AddNode(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify request was captured
	requests := mt.Requests()
	if len(requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(requests))
	}
	payload, ok := requests[0].Payload.(*cluster.AddNodeRequest)
	if !ok {
		t.Fatal("expected AddNodeRequest payload")
	}
	if payload.Address != req.Address {
		t.Errorf("expected address %q, got %q", req.Address, payload.Address)
	}
	if payload.ShardID != req.ShardID || payload.NodeID != req.NodeID {
		t.Errorf("expected shard %d node %d, got shard %d node %d", req.ShardID, req.NodeID, payload.ShardID, payload.NodeID)
	}
}

func TestClusterClient_RemoveNode(t *testing.T) {
	mt := mock.NewTransport()
	client := cluster.NewClient(mt)

	mt.SetResponse("quicbaas.Cluster", "RemoveNode", nil, nil)

	ctx := context.Background()
	req := &cluster.RemoveNodeRequest{
		NodeID: "node-3",
	}

	err := client.RemoveNode(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify request was captured
	requests := mt.Requests()
	if len(requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(requests))
	}
	payload, ok := requests[0].Payload.(*cluster.RemoveNodeRequest)
	if !ok {
		t.Fatal("expected RemoveNodeRequest payload")
	}
	if payload.NodeID != req.NodeID {
		t.Errorf("expected node ID %q, got %q", req.NodeID, payload.NodeID)
	}
}

func TestClusterClient_AddNodeValidationErrors(t *testing.T) {
	mt := mock.NewTransport()
	client := cluster.NewClient(mt)
	ctx := context.Background()

	testCases := []struct {
		name    string
		req     *cluster.AddNodeRequest
		wantErr string
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: "request is required",
		},
		{
			name:    "empty address",
			req:     &cluster.AddNodeRequest{ShardID: 1, NodeID: 4},
			wantErr: "address is required",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := client.AddNode(ctx, tc.req)
			if err == nil {
				t.Fatal("expected error")
			}
			sdkErr, ok := err.(*errors.QBError)
			if !ok {
				t.Fatalf("expected *errors.QBError, got %T", err)
			}
			if sdkErr.Message != tc.wantErr {
				t.Errorf("expected message %q, got %q", tc.wantErr, sdkErr.Message)
			}
		})
	}
}

func TestClusterClient_RemoveNodeValidationErrors(t *testing.T) {
	mt := mock.NewTransport()
	client := cluster.NewClient(mt)
	ctx := context.Background()

	testCases := []struct {
		name    string
		req     *cluster.RemoveNodeRequest
		wantErr string
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: "request is required",
		},
		{
			name:    "empty node ID",
			req:     &cluster.RemoveNodeRequest{},
			wantErr: "node ID is required",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := client.RemoveNode(ctx, tc.req)
			if err == nil {
				t.Fatal("expected error")
			}
			sdkErr, ok := err.(*errors.QBError)
			if !ok {
				t.Fatalf("expected *errors.QBError, got %T", err)
			}
			if sdkErr.Message != tc.wantErr {
				t.Errorf("expected message %q, got %q", tc.wantErr, sdkErr.Message)
			}
		})
	}
}

// ============================================================================
// Health Client Tests
// ============================================================================

func TestHealthClient_Ping(t *testing.T) {
	mt := mock.NewTransport()
	client := health.NewClient(mt)
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	mt.SetResponse("quicbaas.Health", "Ping", nil, nil)

	ctx := context.Background()
	err := client.Ping(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify request was made
	requests := mt.Requests()
	if len(requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(requests))
	}
	if requests[0].Service != "quicbaas.Health" {
		t.Errorf("expected service %q, got %q", "quicbaas.Health", requests[0].Service)
	}
	if requests[0].Method != "Ping" {
		t.Errorf("expected method %q, got %q", "Ping", requests[0].Method)
	}
}

func TestHealthClient_Liveness(t *testing.T) {
	mt := mock.NewTransport()
	client := health.NewClient(mt)

	expectedResp := &health.HealthResponse{
		Healthy: true,
		Status:  "SERVING",
		Details: map[string]interface{}{
			"uptime":  "24h",
			"version": "1.0.0",
		},
	}
	mt.SetResponse("quicbaas.Health", "Liveness", expectedResp, nil)

	ctx := context.Background()
	resp, err := client.Liveness(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if !resp.Healthy {
		t.Error("expected Healthy to be true")
	}
	if resp.Status != "SERVING" {
		t.Errorf("expected status %q, got %q", "SERVING", resp.Status)
	}
	if resp.Details["version"] != "1.0.0" {
		t.Errorf("expected version %q, got %v", "1.0.0", resp.Details["version"])
	}
}

func TestHealthClient_Readiness(t *testing.T) {
	mt := mock.NewTransport()
	client := health.NewClient(mt)

	expectedResp := &health.HealthResponse{
		Healthy: true,
		Status:  "READY",
		Details: map[string]interface{}{
			"raft_state":   "Leader",
			"storage":      "healthy",
			"cluster_size": 3,
		},
	}
	mt.SetResponse("quicbaas.Health", "Readiness", expectedResp, nil)

	ctx := context.Background()
	resp, err := client.Readiness(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if !resp.Healthy {
		t.Error("expected Healthy to be true")
	}
	if resp.Status != "READY" {
		t.Errorf("expected status %q, got %q", "READY", resp.Status)
	}
	if resp.Details["raft_state"] != "Leader" {
		t.Errorf("expected raft_state %q, got %v", "Leader", resp.Details["raft_state"])
	}
}

func TestHealthClient_LivenessUnhealthy(t *testing.T) {
	mt := mock.NewTransport()
	client := health.NewClient(mt)

	expectedResp := &health.HealthResponse{
		Healthy: false,
		Status:  "NOT_SERVING",
		Details: map[string]interface{}{
			"error": "storage backend unavailable",
		},
	}
	mt.SetResponse("quicbaas.Health", "Liveness", expectedResp, nil)

	ctx := context.Background()
	resp, err := client.Liveness(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Healthy {
		t.Error("expected Healthy to be false")
	}
	if resp.Status != "NOT_SERVING" {
		t.Errorf("expected status %q, got %q", "NOT_SERVING", resp.Status)
	}
}

func TestHealthClient_ReadinessNotReady(t *testing.T) {
	mt := mock.NewTransport()
	client := health.NewClient(mt)

	expectedResp := &health.HealthResponse{
		Healthy: false,
		Status:  "NOT_READY",
		Details: map[string]interface{}{
			"reason": "cluster not formed",
		},
	}
	mt.SetResponse("quicbaas.Health", "Readiness", expectedResp, nil)

	ctx := context.Background()
	resp, err := client.Readiness(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Healthy {
		t.Error("expected Healthy to be false")
	}
	if resp.Status != "NOT_READY" {
		t.Errorf("expected status %q, got %q", "NOT_READY", resp.Status)
	}
}

func TestHealthClient_NilContext(t *testing.T) {
	mt := mock.NewTransport()
	client := health.NewClient(mt)

	testCases := []struct {
		name string
		fn   func() error
	}{
		{
			name: "Ping",
			fn:   func() error { return client.Ping(nil) },
		},
		{
			name: "Liveness",
			fn: func() error {
				_, err := client.Liveness(nil)
				return err
			},
		},
		{
			name: "Readiness",
			fn: func() error {
				_, err := client.Readiness(nil)
				return err
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			if err == nil {
				t.Fatal("expected error")
			}
			sdkErr, ok := err.(*errors.QBError)
			if !ok {
				t.Fatalf("expected *errors.QBError, got %T", err)
			}
			if sdkErr.Message != "context is required" {
				t.Errorf("expected message %q, got %q", "context is required", sdkErr.Message)
			}
		})
	}
}

// ============================================================================
// Query Client Tests
// ============================================================================

func TestQueryClient_ExecuteEqual(t *testing.T) {
	mt := mock.NewTransport()
	client := query.NewClient(mt)
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	expectedResp := &query.QueryResponse{
		Results: []query.QueryResult{
			{Key: "user:1", Value: []byte(`{"name": "Alice", "email": "alice@example.com"}`)},
			{Key: "user:2", Value: []byte(`{"name": "Alice", "email": "alice2@example.com"}`)},
		},
		HasMore:    false,
		NextCursor: "",
	}
	mt.SetResponse("quicbaas.Query", "Execute", expectedResp, nil)

	ctx := context.Background()
	req := &query.QueryRequest{
		EntityType: "user",
		Index:      "name",
		Value:      "Alice",
		Operator:   query.OpEqual,
		Limit:      100,
	}

	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(resp.Results))
	}
	if resp.HasMore {
		t.Error("expected HasMore to be false")
	}
}

func TestQueryClient_ExecuteNotEqual(t *testing.T) {
	mt := mock.NewTransport()
	client := query.NewClient(mt)

	expectedResp := &query.QueryResponse{
		Results: []query.QueryResult{
			{Key: "user:3", Value: []byte(`{"name": "Bob"}`)},
			{Key: "user:4", Value: []byte(`{"name": "Charlie"}`)},
		},
	}
	mt.SetResponse("quicbaas.Query", "Execute", expectedResp, nil)

	ctx := context.Background()
	req := &query.QueryRequest{
		EntityType: "user",
		Index:      "name",
		Value:      "Alice",
		Operator:   query.OpNotEqual,
		Limit:      100,
	}

	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(resp.Results))
	}
}

func TestQueryClient_ExecuteLessThan(t *testing.T) {
	mt := mock.NewTransport()
	client := query.NewClient(mt)

	expectedResp := &query.QueryResponse{
		Results: []query.QueryResult{
			{Key: "product:1", Value: []byte(`{"price": 10}`)},
			{Key: "product:2", Value: []byte(`{"price": 15}`)},
		},
	}
	mt.SetResponse("quicbaas.Query", "Execute", expectedResp, nil)

	ctx := context.Background()
	req := &query.QueryRequest{
		EntityType: "product",
		Index:      "price",
		Value:      20,
		Operator:   query.OpLessThan,
		Limit:      50,
	}

	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(resp.Results))
	}
}

func TestQueryClient_ExecuteGreaterThanOrEqual(t *testing.T) {
	mt := mock.NewTransport()
	client := query.NewClient(mt)

	expectedResp := &query.QueryResponse{
		Results: []query.QueryResult{
			{Key: "product:3", Value: []byte(`{"price": 100}`)},
			{Key: "product:4", Value: []byte(`{"price": 150}`)},
			{Key: "product:5", Value: []byte(`{"price": 200}`)},
		},
	}
	mt.SetResponse("quicbaas.Query", "Execute", expectedResp, nil)

	ctx := context.Background()
	req := &query.QueryRequest{
		EntityType: "product",
		Index:      "price",
		Value:      100,
		Operator:   query.OpGreaterThanOrEqual,
		Limit:      50,
	}

	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Results) != 3 {
		t.Errorf("expected 3 results, got %d", len(resp.Results))
	}
}

func TestQueryClient_ExecutePrefix(t *testing.T) {
	mt := mock.NewTransport()
	client := query.NewClient(mt)

	expectedResp := &query.QueryResponse{
		Results: []query.QueryResult{
			{Key: "user:1", Value: []byte(`{"email": "john@example.com"}`)},
			{Key: "user:2", Value: []byte(`{"email": "john.doe@example.com"}`)},
			{Key: "user:3", Value: []byte(`{"email": "johnny@example.com"}`)},
		},
	}
	mt.SetResponse("quicbaas.Query", "Execute", expectedResp, nil)

	ctx := context.Background()
	req := &query.QueryRequest{
		EntityType: "user",
		Index:      "email",
		Value:      "john",
		Operator:   query.OpPrefix,
		Limit:      100,
	}

	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Results) != 3 {
		t.Errorf("expected 3 results, got %d", len(resp.Results))
	}
}

func TestQueryClient_ExecuteRange(t *testing.T) {
	mt := mock.NewTransport()
	client := query.NewClient(mt)

	expectedResp := &query.QueryResponse{
		Results: []query.QueryResult{
			{Key: "product:10", Value: []byte(`{"price": 50}`)},
			{Key: "product:11", Value: []byte(`{"price": 75}`)},
			{Key: "product:12", Value: []byte(`{"price": 100}`)},
		},
	}
	mt.SetResponse("quicbaas.Query", "Execute", expectedResp, nil)

	ctx := context.Background()
	req := &query.QueryRequest{
		EntityType: "product",
		Index:      "price",
		Value:      []interface{}{50, 100}, // Range from 50 to 100
		Operator:   query.OpRange,
		Limit:      100,
	}

	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Results) != 3 {
		t.Errorf("expected 3 results, got %d", len(resp.Results))
	}
}

func TestQueryClient_ExecuteWithPagination(t *testing.T) {
	mt := mock.NewTransport()
	client := query.NewClient(mt)

	// First page
	firstPageResp := &query.QueryResponse{
		Results: []query.QueryResult{
			{Key: "user:1", Value: []byte(`{}`)},
			{Key: "user:2", Value: []byte(`{}`)},
		},
		HasMore:    true,
		NextCursor: "cursor-page-2",
	}
	mt.SetResponse("quicbaas.Query", "Execute", firstPageResp, nil)

	ctx := context.Background()
	req := &query.QueryRequest{
		EntityType: "user",
		Index:      "created_at",
		Value:      nil,
		Operator:   query.OpGreaterThan,
		Limit:      2,
	}

	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(resp.Results))
	}
	if !resp.HasMore {
		t.Error("expected HasMore to be true")
	}
	if resp.NextCursor != "cursor-page-2" {
		t.Errorf("expected NextCursor %q, got %q", "cursor-page-2", resp.NextCursor)
	}

	// Second page
	mt.Reset()
	secondPageResp := &query.QueryResponse{
		Results: []query.QueryResult{
			{Key: "user:3", Value: []byte(`{}`)},
			{Key: "user:4", Value: []byte(`{}`)},
		},
		HasMore:    true,
		NextCursor: "cursor-page-3",
	}
	mt.SetResponse("quicbaas.Query", "Execute", secondPageResp, nil)

	req.Cursor = resp.NextCursor
	resp, err = client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(resp.Results))
	}
	if resp.NextCursor != "cursor-page-3" {
		t.Errorf("expected NextCursor %q, got %q", "cursor-page-3", resp.NextCursor)
	}

	// Third (last) page
	mt.Reset()
	lastPageResp := &query.QueryResponse{
		Results: []query.QueryResult{
			{Key: "user:5", Value: []byte(`{}`)},
		},
		HasMore:    false,
		NextCursor: "",
	}
	mt.SetResponse("quicbaas.Query", "Execute", lastPageResp, nil)

	req.Cursor = resp.NextCursor
	resp, err = client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Errorf("expected 1 result, got %d", len(resp.Results))
	}
	if resp.HasMore {
		t.Error("expected HasMore to be false")
	}
	if resp.NextCursor != "" {
		t.Errorf("expected empty NextCursor, got %q", resp.NextCursor)
	}
}

func TestQueryClient_ExecuteEmptyResults(t *testing.T) {
	mt := mock.NewTransport()
	client := query.NewClient(mt)

	// Return nil to simulate empty results
	mt.SetResponse("quicbaas.Query", "Execute", nil, nil)

	ctx := context.Background()
	req := &query.QueryRequest{
		EntityType: "user",
		Index:      "name",
		Value:      "NonExistent",
		Operator:   query.OpEqual,
		Limit:      100,
	}

	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(resp.Results))
	}
}

func TestQueryClient_ExecuteValidationErrors(t *testing.T) {
	mt := mock.NewTransport()
	client := query.NewClient(mt)
	ctx := context.Background()

	testCases := []struct {
		name    string
		req     *query.QueryRequest
		wantErr string
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: "request is required",
		},
		{
			name:    "empty entity type",
			req:     &query.QueryRequest{Index: "name", Operator: query.OpEqual},
			wantErr: "entity type is required",
		},
		{
			name:    "empty index",
			req:     &query.QueryRequest{EntityType: "user", Operator: query.OpEqual},
			wantErr: "index is required",
		},
		{
			name:    "negative limit",
			req:     &query.QueryRequest{EntityType: "user", Index: "name", Operator: query.OpEqual, Limit: -1},
			wantErr: "limit cannot be negative",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.Execute(ctx, tc.req)
			if err == nil {
				t.Fatal("expected error")
			}
			sdkErr, ok := err.(*errors.QBError)
			if !ok {
				t.Fatalf("expected *errors.QBError, got %T", err)
			}
			if sdkErr.Message != tc.wantErr {
				t.Errorf("expected message %q, got %q", tc.wantErr, sdkErr.Message)
			}
		})
	}
}

// ============================================================================
// Transport Error Tests
// ============================================================================

func TestSpecializedClients_TransportErrors(t *testing.T) {
	mt := mock.NewTransport()
	transportErr := errors.NewConnection("test", "connection refused")

	ctx := context.Background()

	// Test Vector client transport error
	t.Run("Vector/Upsert", func(t *testing.T) {
		mt.Reset()
		mt.SetResponse("quicbaas.Vector", "Upsert", nil, transportErr)
		client := vector.NewClient(mt)
		_, err := client.Upsert(ctx, &vector.UpsertRequest{
			Vector: &vector.VectorEntity{
				Namespace: "test",
				ID:        "1",
				Vector:    []float32{0.1},
			},
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	// Test TimeSeries client transport error
	t.Run("TimeSeries/Write", func(t *testing.T) {
		mt.Reset()
		mt.SetResponse("quicbaas.TimeSeries", "Write", nil, transportErr)
		client := timeseries.NewClient(mt)
		err := client.Write(ctx, &timeseries.WriteRequest{
			Metric:    "test",
			Timestamp: time.Now(),
			Value:     1.0,
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	// Test Backup client transport error
	t.Run("Backup/Create", func(t *testing.T) {
		mt.Reset()
		mt.SetResponse("quicbaas.Backup", "Create", nil, transportErr)
		client := backup.NewClient(mt)
		_, err := client.Create(ctx, &backup.BackupRequest{
			Destination: "/tmp/backup",
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	// Test Cluster client transport error
	t.Run("Cluster/Status", func(t *testing.T) {
		mt.Reset()
		mt.SetResponse("quicbaas.Cluster", "Status", nil, transportErr)
		client := cluster.NewClient(mt)
		_, err := client.Status(ctx)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	// Test Health client transport error
	t.Run("Health/Ping", func(t *testing.T) {
		mt.Reset()
		mt.SetResponse("quicbaas.Health", "Ping", nil, transportErr)
		client := health.NewClient(mt)
		err := client.Ping(ctx)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	// Test Query client transport error
	t.Run("Query/Execute", func(t *testing.T) {
		mt.Reset()
		mt.SetResponse("quicbaas.Query", "Execute", nil, transportErr)
		client := query.NewClient(mt)
		_, err := client.Execute(ctx, &query.QueryRequest{
			EntityType: "test",
			Index:      "name",
			Operator:   query.OpEqual,
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

// ============================================================================
// Nil Transport Tests
// ============================================================================

func TestSpecializedClients_NilTransport(t *testing.T) {
	t.Run("Vector", func(t *testing.T) {
		client := vector.NewClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})

	t.Run("TimeSeries", func(t *testing.T) {
		client := timeseries.NewClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})

	t.Run("Backup", func(t *testing.T) {
		client := backup.NewClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})

	t.Run("Cluster", func(t *testing.T) {
		client := cluster.NewClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})

	t.Run("Health", func(t *testing.T) {
		client := health.NewClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})

	t.Run("Query", func(t *testing.T) {
		client := query.NewClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})
}

// ============================================================================
// Closed Transport Tests
// ============================================================================

func TestSpecializedClients_ClosedTransport(t *testing.T) {
	mt := mock.NewTransport()
	if err := mt.Close(); err != nil {
		t.Fatalf("failed to close transport: %v", err)
	}

	ctx := context.Background()

	t.Run("Vector/Upsert", func(t *testing.T) {
		client := vector.NewClient(mt)
		_, err := client.Upsert(ctx, &vector.UpsertRequest{
			Vector: &vector.VectorEntity{
				Namespace: "test",
				ID:        "1",
				Vector:    []float32{0.1},
			},
		})
		if err == nil {
			t.Fatal("expected error for closed transport")
		}
	})

	t.Run("TimeSeries/Write", func(t *testing.T) {
		client := timeseries.NewClient(mt)
		err := client.Write(ctx, &timeseries.WriteRequest{
			Metric:    "test",
			Timestamp: time.Now(),
			Value:     1.0,
		})
		if err == nil {
			t.Fatal("expected error for closed transport")
		}
	})
}

// ============================================================================
// Context Cancellation Tests
// ============================================================================

func TestSpecializedClients_ContextCancellation(t *testing.T) {
	mt := mock.NewTransport()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	t.Run("Vector/Upsert", func(t *testing.T) {
		client := vector.NewClient(mt)
		_, err := client.Upsert(ctx, &vector.UpsertRequest{
			Vector: &vector.VectorEntity{
				Namespace: "test",
				ID:        "1",
				Vector:    []float32{0.1},
			},
		})
		if err == nil {
			t.Fatal("expected error for canceled context")
		}
	})

	t.Run("TimeSeries/Query", func(t *testing.T) {
		client := timeseries.NewClient(mt)
		now := time.Now()
		_, err := client.Query(ctx, &timeseries.QueryRequest{
			Metric:    "test",
			Start:     now.Add(-1 * time.Hour),
			End:       now,
			Aggregate: timeseries.AggregateNone,
		})
		if err == nil {
			t.Fatal("expected error for canceled context")
		}
	})
}
