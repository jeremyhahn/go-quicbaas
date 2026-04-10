// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package service

import (
	"testing"
)

// TestBatchPutObjectsRequestValidate tests BatchPutObjectsRequest validation.
func TestBatchPutObjectsRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		req     *BatchPutObjectsRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty objects list",
			req:     &BatchPutObjectsRequest{Objects: nil},
			wantErr: true,
		},
		{
			name: "valid request with one object",
			req: &BatchPutObjectsRequest{
				Objects: []*PutObjectRequest{
					{Key: "test-key", Value: []byte("test-value")},
				},
			},
			wantErr: false,
		},
		{
			name: "valid request with multiple objects",
			req: &BatchPutObjectsRequest{
				Objects: []*PutObjectRequest{
					{Key: "key1", Value: []byte("value1")},
					{Key: "key2", Value: []byte("value2")},
				},
				TenantID:        "tenant-1",
				ContinueOnError: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestBatchGetObjectsRequestValidate tests BatchGetObjectsRequest validation.
func TestBatchGetObjectsRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		req     *BatchGetObjectsRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty keys list",
			req:     &BatchGetObjectsRequest{Keys: nil},
			wantErr: true,
		},
		{
			name:    "empty keys slice",
			req:     &BatchGetObjectsRequest{Keys: []string{}},
			wantErr: true,
		},
		{
			name: "valid request with one key",
			req: &BatchGetObjectsRequest{
				Keys: []string{"test-key"},
			},
			wantErr: false,
		},
		{
			name: "valid request with multiple keys",
			req: &BatchGetObjectsRequest{
				Keys:            []string{"key1", "key2", "key3"},
				TenantID:        "tenant-1",
				IncludeMetadata: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestBatchDeleteObjectsRequestValidate tests BatchDeleteObjectsRequest validation.
func TestBatchDeleteObjectsRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		req     *BatchDeleteObjectsRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty keys list",
			req:     &BatchDeleteObjectsRequest{Keys: nil},
			wantErr: true,
		},
		{
			name:    "empty keys slice",
			req:     &BatchDeleteObjectsRequest{Keys: []string{}},
			wantErr: true,
		},
		{
			name: "valid request with one key",
			req: &BatchDeleteObjectsRequest{
				Keys: []string{"test-key"},
			},
			wantErr: false,
		},
		{
			name: "valid request with multiple keys",
			req: &BatchDeleteObjectsRequest{
				Keys:            []string{"key1", "key2"},
				TenantID:        "tenant-1",
				ContinueOnError: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestBatchPutResult tests BatchPutResult struct.
func TestBatchPutResult(t *testing.T) {
	t.Run("successful put result", func(t *testing.T) {
		result := &BatchPutResult{
			Key:      "test-key",
			Success:  true,
			ObjectID: 12345,
			Checksum: []byte("checksum-value"),
		}
		if !result.Success {
			t.Error("expected Success to be true")
		}
		if result.Key != "test-key" {
			t.Errorf("expected Key 'test-key', got '%s'", result.Key)
		}
		if result.ObjectID != 12345 {
			t.Errorf("expected ObjectID 12345, got %d", result.ObjectID)
		}
	})

	t.Run("failed put result", func(t *testing.T) {
		result := &BatchPutResult{
			Key:     "failed-key",
			Success: false,
			Error:   "put operation failed",
		}
		if result.Success {
			t.Error("expected Success to be false")
		}
		if result.Error != "put operation failed" {
			t.Errorf("expected Error message, got '%s'", result.Error)
		}
	})
}

// TestBatchGetResult tests BatchGetResult struct.
func TestBatchGetResult(t *testing.T) {
	t.Run("found object result", func(t *testing.T) {
		result := &BatchGetResult{
			Key:         "found-key",
			Found:       true,
			Value:       []byte("object-value"),
			ContentType: "application/json",
			Size:        12,
			Checksum:    []byte("checksum"),
			Version:     1,
			Tags:        map[string]string{"env": "test"},
		}
		if !result.Found {
			t.Error("expected Found to be true")
		}
		if string(result.Value) != "object-value" {
			t.Errorf("expected Value 'object-value', got '%s'", result.Value)
		}
		if result.Size != 12 {
			t.Errorf("expected Size 12, got %d", result.Size)
		}
	})

	t.Run("not found result", func(t *testing.T) {
		result := &BatchGetResult{
			Key:   "missing-key",
			Found: false,
			Error: "object not found",
		}
		if result.Found {
			t.Error("expected Found to be false")
		}
		if result.Error != "object not found" {
			t.Errorf("expected Error message, got '%s'", result.Error)
		}
	})
}

// TestBatchDeleteResult tests BatchDeleteResult struct.
func TestBatchDeleteResult(t *testing.T) {
	t.Run("successful delete result", func(t *testing.T) {
		result := &BatchDeleteResult{
			Key:     "deleted-key",
			Success: true,
			Size:    1024,
		}
		if !result.Success {
			t.Error("expected Success to be true")
		}
		if result.Size != 1024 {
			t.Errorf("expected Size 1024, got %d", result.Size)
		}
	})

	t.Run("failed delete result", func(t *testing.T) {
		result := &BatchDeleteResult{
			Key:     "failed-key",
			Success: false,
			Error:   "delete failed: permission denied",
		}
		if result.Success {
			t.Error("expected Success to be false")
		}
		if result.Error != "delete failed: permission denied" {
			t.Errorf("expected Error message, got '%s'", result.Error)
		}
	})
}

// TestBatchPutObjectsResponse tests BatchPutObjectsResponse struct.
func TestBatchPutObjectsResponse(t *testing.T) {
	resp := &BatchPutObjectsResponse{
		Results: []*BatchPutResult{
			{Key: "key1", Success: true, ObjectID: 1},
			{Key: "key2", Success: true, ObjectID: 2},
			{Key: "key3", Success: false, Error: "failed"},
		},
		SuccessCount: 2,
		FailureCount: 1,
		Timestamp:    1234567890,
	}

	if resp.SuccessCount != 2 {
		t.Errorf("expected SuccessCount 2, got %d", resp.SuccessCount)
	}
	if resp.FailureCount != 1 {
		t.Errorf("expected FailureCount 1, got %d", resp.FailureCount)
	}
	if len(resp.Results) != 3 {
		t.Errorf("expected 3 results, got %d", len(resp.Results))
	}
}

// TestBatchGetObjectsResponse tests BatchGetObjectsResponse struct.
func TestBatchGetObjectsResponse(t *testing.T) {
	resp := &BatchGetObjectsResponse{
		Results: []*BatchGetResult{
			{Key: "key1", Found: true, Value: []byte("value1")},
			{Key: "key2", Found: false, Error: "not found"},
		},
		FoundCount:    1,
		NotFoundCount: 1,
		Timestamp:     1234567890,
	}

	if resp.FoundCount != 1 {
		t.Errorf("expected FoundCount 1, got %d", resp.FoundCount)
	}
	if resp.NotFoundCount != 1 {
		t.Errorf("expected NotFoundCount 1, got %d", resp.NotFoundCount)
	}
}

// TestBatchDeleteObjectsResponse tests BatchDeleteObjectsResponse struct.
func TestBatchDeleteObjectsResponse(t *testing.T) {
	resp := &BatchDeleteObjectsResponse{
		Results: []*BatchDeleteResult{
			{Key: "key1", Success: true, Size: 100},
			{Key: "key2", Success: true, Size: 200},
		},
		DeletedCount: 2,
		FailureCount: 0,
		Timestamp:    1234567890,
	}

	if resp.DeletedCount != 2 {
		t.Errorf("expected DeletedCount 2, got %d", resp.DeletedCount)
	}
	if resp.FailureCount != 0 {
		t.Errorf("expected FailureCount 0, got %d", resp.FailureCount)
	}
}
