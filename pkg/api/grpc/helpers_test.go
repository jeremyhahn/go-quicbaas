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
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// Test toProtoError with various error types.
func TestToProtoError_Detailed(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		expectedCode codes.Code
	}{
		{
			name:         "nil error",
			err:          nil,
			expectedCode: codes.OK,
		},
		{
			name:         "grpc status error passthrough",
			err:          status.Error(codes.NotFound, "not found"),
			expectedCode: codes.NotFound,
		},
		{
			name: "qb error not found",
			err: &errors.QBError{
				Code: errors.ErrNotFound,
				Op:   "test",
				Err:  assert.AnError,
			},
			expectedCode: codes.NotFound,
		},
		{
			name: "qb error invalid input",
			err: &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "test",
				Err:  assert.AnError,
			},
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "unknown error type",
			err:          assert.AnError,
			expectedCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toProtoError(tt.err)

			if tt.err == nil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				st, ok := status.FromError(result)
				assert.True(t, ok)
				assert.Equal(t, tt.expectedCode, st.Code())
			}
		})
	}
}

// Test validatePutRequest with comprehensive edge cases.
func TestValidatePutRequest_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		req         *pb.PutRequest
		expectError bool
	}{
		{
			name:        "nil request",
			req:         nil,
			expectError: true,
		},
		{
			name: "empty entity type",
			req: &pb.PutRequest{
				EntityType: "",
				Id:         123,
				Data:       []byte(`{"name":"test"}`),
			},
			expectError: true,
		},
		{
			name: "valid request with data",
			req: &pb.PutRequest{
				EntityType:  "user",
				Id:          123,
				Data:        []byte(`{"name":"test"}`),
				Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
			},
			expectError: false,
		},
		{
			name: "valid request with empty data - allowed",
			req: &pb.PutRequest{
				EntityType:  "user",
				Id:          123,
				Data:        []byte{},
				Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_STRONG,
			},
			expectError: false,
		},
		{
			name: "valid request with nil data - allowed",
			req: &pb.PutRequest{
				EntityType:  "user",
				Id:          123,
				Data:        nil,
				Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_EVENTUAL,
			},
			expectError: false,
		},
		{
			name: "valid request with unspecified consistency",
			req: &pb.PutRequest{
				EntityType:  "user",
				Id:          123,
				Data:        []byte(`test`),
				Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_UNSPECIFIED,
			},
			expectError: false,
		},
		{
			name: "invalid consistency level",
			req: &pb.PutRequest{
				EntityType:  "user",
				Id:          123,
				Data:        []byte(`test`),
				Consistency: pb.ConsistencyLevel(999),
			},
			expectError: true,
		},
		{
			name: "valid request with zero id - allowed",
			req: &pb.PutRequest{
				EntityType: "user",
				Id:         0,
				Data:       []byte(`test`),
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePutRequest(tt.req)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Test validation functions with edge cases.
func TestValidateGetRequest_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		req         *pb.GetRequest
		expectError bool
	}{
		{
			name:        "nil request",
			req:         nil,
			expectError: true,
		},
		{
			name: "empty entity type",
			req: &pb.GetRequest{
				EntityType: "",
				Id:         123,
			},
			expectError: true,
		},
		{
			name: "zero id",
			req: &pb.GetRequest{
				EntityType: "user",
				Id:         0,
			},
			expectError: true,
		},
		{
			name: "valid request",
			req: &pb.GetRequest{
				EntityType:  "user",
				Id:          123,
				Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGetRequest(tt.req)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateDeleteRequest_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		req         *pb.DeleteRequest
		expectError bool
	}{
		{
			name:        "nil request",
			req:         nil,
			expectError: true,
		},
		{
			name: "empty entity type",
			req: &pb.DeleteRequest{
				EntityType: "",
				Id:         123,
			},
			expectError: true,
		},
		{
			name: "zero id",
			req: &pb.DeleteRequest{
				EntityType: "user",
				Id:         0,
			},
			expectError: true,
		},
		{
			name: "valid request",
			req: &pb.DeleteRequest{
				EntityType:  "user",
				Id:          123,
				Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_STRONG,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDeleteRequest(tt.req)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateListRequest_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		req         *pb.ListRequest
		expectError bool
	}{
		{
			name:        "nil request",
			req:         nil,
			expectError: true,
		},
		{
			name: "empty entity type",
			req: &pb.ListRequest{
				EntityType: "",
			},
			expectError: true,
		},
		{
			name: "page size too large",
			req: &pb.ListRequest{
				EntityType: "user",
				PageSize:   20000,
			},
			expectError: true,
		},
		{
			name: "valid request",
			req: &pb.ListRequest{
				EntityType: "user",
				PageSize:   100,
			},
			expectError: false,
		},
		{
			name: "valid request with zero page size",
			req: &pb.ListRequest{
				EntityType: "user",
				PageSize:   0,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateListRequest(tt.req)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateQueryRequest_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		req         *pb.QueryRequest
		expectError bool
	}{
		{
			name:        "nil request",
			req:         nil,
			expectError: true,
		},
		{
			name: "empty entity type",
			req: &pb.QueryRequest{
				EntityType: "",
				IndexName:  "email",
				Value:      []byte("test"),
			},
			expectError: true,
		},
		{
			name: "empty index name",
			req: &pb.QueryRequest{
				EntityType: "user",
				IndexName:  "",
				Value:      []byte("test"),
			},
			expectError: true,
		},
		{
			name: "no value or range",
			req: &pb.QueryRequest{
				EntityType: "user",
				IndexName:  "email",
			},
			expectError: true,
		},
		{
			name: "valid exact match query",
			req: &pb.QueryRequest{
				EntityType: "user",
				IndexName:  "email",
				Value:      []byte("test@example.com"),
			},
			expectError: false,
		},
		{
			name: "valid range query",
			req: &pb.QueryRequest{
				EntityType: "user",
				IndexName:  "age",
				RangeStart: []byte{18},
				RangeEnd:   []byte{65},
			},
			expectError: false,
		},
		{
			name: "page size too large",
			req: &pb.QueryRequest{
				EntityType: "user",
				IndexName:  "email",
				Value:      []byte("test"),
				PageSize:   20000,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateQueryRequest(tt.req)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateAggregateRequest_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		req         *pb.AggregateRequest
		expectError bool
	}{
		{
			name:        "nil request",
			req:         nil,
			expectError: true,
		},
		{
			name: "empty entity type",
			req: &pb.AggregateRequest{
				EntityType: "",
				FieldName:  "value",
				Function:   pb.AggregateFunction_AGGREGATE_FUNCTION_AVG,
				StartTime:  1000,
				EndTime:    2000,
			},
			expectError: true,
		},
		{
			name: "empty field name",
			req: &pb.AggregateRequest{
				EntityType: "sensor",
				FieldName:  "",
				Function:   pb.AggregateFunction_AGGREGATE_FUNCTION_AVG,
				StartTime:  1000,
				EndTime:    2000,
			},
			expectError: true,
		},
		{
			name: "unspecified function",
			req: &pb.AggregateRequest{
				EntityType: "sensor",
				FieldName:  "value",
				Function:   pb.AggregateFunction_AGGREGATE_FUNCTION_UNSPECIFIED,
				StartTime:  1000,
				EndTime:    2000,
			},
			expectError: true,
		},
		{
			name: "invalid time range - equal",
			req: &pb.AggregateRequest{
				EntityType: "sensor",
				FieldName:  "value",
				Function:   pb.AggregateFunction_AGGREGATE_FUNCTION_AVG,
				StartTime:  2000,
				EndTime:    2000,
			},
			expectError: true,
		},
		{
			name: "invalid time range - reversed",
			req: &pb.AggregateRequest{
				EntityType: "sensor",
				FieldName:  "value",
				Function:   pb.AggregateFunction_AGGREGATE_FUNCTION_AVG,
				StartTime:  2000,
				EndTime:    1000,
			},
			expectError: true,
		},
		{
			name: "valid SUM request",
			req: &pb.AggregateRequest{
				EntityType: "sensor",
				FieldName:  "value",
				Function:   pb.AggregateFunction_AGGREGATE_FUNCTION_SUM,
				StartTime:  1000,
				EndTime:    2000,
			},
			expectError: false,
		},
		{
			name: "valid AVG request",
			req: &pb.AggregateRequest{
				EntityType: "sensor",
				FieldName:  "value",
				Function:   pb.AggregateFunction_AGGREGATE_FUNCTION_AVG,
				StartTime:  1000,
				EndTime:    2000,
			},
			expectError: false,
		},
		{
			name: "valid MIN request",
			req: &pb.AggregateRequest{
				EntityType: "sensor",
				FieldName:  "value",
				Function:   pb.AggregateFunction_AGGREGATE_FUNCTION_MIN,
				StartTime:  1000,
				EndTime:    2000,
			},
			expectError: false,
		},
		{
			name: "valid MAX request",
			req: &pb.AggregateRequest{
				EntityType: "sensor",
				FieldName:  "value",
				Function:   pb.AggregateFunction_AGGREGATE_FUNCTION_MAX,
				StartTime:  1000,
				EndTime:    2000,
			},
			expectError: false,
		},
		{
			name: "valid COUNT request",
			req: &pb.AggregateRequest{
				EntityType: "sensor",
				FieldName:  "value",
				Function:   pb.AggregateFunction_AGGREGATE_FUNCTION_COUNT,
				StartTime:  1000,
				EndTime:    2000,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAggregateRequest(tt.req)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Benchmarks for validation functions.
func BenchmarkValidateGetRequest(b *testing.B) {
	req := &pb.GetRequest{
		EntityType:  "user",
		Id:          123,
		Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateGetRequest(req)
	}
}

func BenchmarkValidateDeleteRequest(b *testing.B) {
	req := &pb.DeleteRequest{
		EntityType:  "user",
		Id:          123,
		Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_STRONG,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateDeleteRequest(req)
	}
}

func BenchmarkValidateQueryRequest(b *testing.B) {
	req := &pb.QueryRequest{
		EntityType: "user",
		IndexName:  "email",
		Value:      []byte("test@example.com"),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateQueryRequest(req)
	}
}

func BenchmarkConsistencyLevelConversion(b *testing.B) {
	level := pb.ConsistencyLevel_CONSISTENCY_LEVEL_STRONG

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := protoToConsistencyLevel(level)
		_ = consistencyLevelToProto(result)
	}
}

// --- Merged from helpers_additional_test.go ---

func TestNewServerValidation_OtherParams(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout", Component: "grpc-test"})

	// Nil service
	_, err := NewServer(nil, log, 1, "v")
	if err == nil {
		t.Fatalf("expected error for nil service")
	}

	// Nil logger
	mockSvc := testutil.NewMockQuicBaaS()
	var nilLog *logger.Logger
	_, err = NewServer(mockSvc, nilLog, 1, "v")
	if err == nil {
		t.Fatalf("expected error for nil logger")
	}
}
