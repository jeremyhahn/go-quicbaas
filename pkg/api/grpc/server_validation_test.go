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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// These tests cover the package-level validation and conversion functions
// Note: Full Server RPC method tests would require refactoring Server to use interfaces

func TestValidateGetRequest(t *testing.T) {
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

func TestValidateDeleteRequest(t *testing.T) {
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

func TestValidateListRequest(t *testing.T) {
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

func TestValidateQueryRequest(t *testing.T) {
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

func TestValidateAggregateRequest(t *testing.T) {
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
			name: "invalid time range",
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
			name: "valid request",
			req: &pb.AggregateRequest{
				EntityType: "sensor",
				FieldName:  "value",
				Function:   pb.AggregateFunction_AGGREGATE_FUNCTION_AVG,
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

func TestValidatePutRequest2(t *testing.T) {
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
				Data:       []byte("test"),
			},
			expectError: true,
		},
		{
			name: "empty data",
			req: &pb.PutRequest{
				EntityType: "user",
				Data:       []byte{},
			},
			expectError: false, // Empty data is valid - key-value stores allow empty values
		},
		{
			name: "valid request",
			req: &pb.PutRequest{
				EntityType:  "user",
				Id:          123,
				Data:        []byte("test data"),
				Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_STRONG,
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
