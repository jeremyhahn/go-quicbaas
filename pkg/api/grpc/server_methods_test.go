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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Server Address Methods Tests
// =============================================================================

func TestServer_SetAddress(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)

	t.Run("set_address", func(t *testing.T) {
		srv.SetAddress("localhost:5000")
		assert.Equal(t, "localhost:5000", srv.Address())
	})

	t.Run("update_address", func(t *testing.T) {
		srv.SetAddress("0.0.0.0:6000")
		assert.Equal(t, "0.0.0.0:6000", srv.Address())
	})
}

func TestServer_Address(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)

	t.Run("empty_address_initially", func(t *testing.T) {
		assert.Equal(t, "", srv.Address())
	})
}

// =============================================================================
// RecordQuery and QPS Tests
// =============================================================================

func TestServer_RecordQuery(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	srv.initMetrics()

	t.Run("record_query_with_tracker", func(t *testing.T) {
		// Should not panic
		srv.RecordQuery()
	})

	t.Run("record_query_nil_tracker", func(t *testing.T) {
		srv.qpsTracker = nil
		// Should not panic when tracker is nil
		srv.RecordQuery()
	})
}

func TestServer_QPS(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	srv.initMetrics()

	t.Run("qps_with_tracker", func(t *testing.T) {
		qps := srv.QPS()
		assert.GreaterOrEqual(t, qps, float64(0))
	})

	t.Run("qps_nil_tracker", func(t *testing.T) {
		srv.qpsTracker = nil
		qps := srv.QPS()
		assert.Equal(t, float64(0), qps)
	})
}

// =============================================================================
// ListEntityTypes Tests
// =============================================================================

func TestServer_ListEntityTypes(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.ListEntityTypesRequest
		mockSetup   func(m *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.ListEntityTypesResponse)
	}{
		{
			name:    "successful_list_entity_types",
			request: &pb.ListEntityTypesRequest{},
			mockSetup: func(m *mock.Mock) {
				m.On("ListEntityTypes", mock.Anything, mock.Anything).
					Return(&service.ListEntityTypesResponse{
						EntityTypes: []string{"users", "products", "orders"},
						TotalCount:  3,
					}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.ListEntityTypesResponse) {
				assert.Len(t, resp.EntityTypes, 3)
				assert.Contains(t, resp.EntityTypes, "users")
				assert.Contains(t, resp.EntityTypes, "products")
				assert.Contains(t, resp.EntityTypes, "orders")
				assert.Equal(t, uint64(3), resp.TotalCount)
			},
		},
		{
			name:    "nil_request_uses_defaults",
			request: nil,
			mockSetup: func(m *mock.Mock) {
				// Server creates default request when nil
				m.On("ListEntityTypes", mock.Anything, mock.Anything).
					Return(&service.ListEntityTypesResponse{
						EntityTypes: []string{"test"},
						TotalCount:  1,
					}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.ListEntityTypesResponse) {
				assert.Len(t, resp.EntityTypes, 1)
			},
		},
		{
			name:    "empty_result",
			request: &pb.ListEntityTypesRequest{},
			mockSetup: func(m *mock.Mock) {
				m.On("ListEntityTypes", mock.Anything, mock.Anything).
					Return(&service.ListEntityTypesResponse{
						EntityTypes: []string{},
						TotalCount:  0,
					}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.ListEntityTypesResponse) {
				assert.Empty(t, resp.EntityTypes)
				assert.Equal(t, uint64(0), resp.TotalCount)
			},
		},
		{
			name:    "service_error",
			request: &pb.ListEntityTypesRequest{},
			mockSetup: func(m *mock.Mock) {
				m.On("ListEntityTypes", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
		{
			name:    "with_pagination",
			request: &pb.ListEntityTypesRequest{PageSize: 10, PageToken: "token123"},
			mockSetup: func(m *mock.Mock) {
				m.On("ListEntityTypes", mock.Anything, mock.MatchedBy(func(req *service.ListEntityTypesRequest) bool {
					return req.PageSize == 10 && req.PageToken == "token123"
				})).Return(&service.ListEntityTypesResponse{
					EntityTypes:   []string{"a", "b"},
					TotalCount:    2,
					NextPageToken: "next",
				}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.ListEntityTypesResponse) {
				assert.Len(t, resp.EntityTypes, 2)
				assert.Equal(t, "next", resp.NextPageToken)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.ListEntityTypes(context.Background(), tt.request)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorCode != 0 {
					assert.Equal(t, tt.errorCode, status.Code(err))
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tt.validate != nil {
					tt.validate(t, resp)
				}
			}
			mockSvc.AssertExpectations(t)
		})
	}
}

// =============================================================================
// GetLeader Tests
// =============================================================================

func TestServer_GetLeader(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.GetLeaderRequest
		mockSetup   func(m *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.GetLeaderResponse)
	}{
		{
			name: "successful_get_leader",
			request: &pb.GetLeaderRequest{
				ShardId: 1,
			},
			mockSetup: func(m *mock.Mock) {
				m.On("GetLeader", mock.Anything, mock.MatchedBy(func(req *service.GetLeaderRequest) bool {
					return req.ShardID == 1
				})).Return(&service.GetLeaderResponse{
					LeaderID: 3,
					Address:  "10.0.0.3:5000",
				}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.GetLeaderResponse) {
				assert.Equal(t, uint64(3), resp.LeaderId)
			},
		},
		{
			name: "service_error_no_leader",
			request: &pb.GetLeaderRequest{
				ShardId: 1,
			},
			mockSetup: func(m *mock.Mock) {
				m.On("GetLeader", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
		{
			name: "get_leader_shard_2",
			request: &pb.GetLeaderRequest{
				ShardId: 2,
			},
			mockSetup: func(m *mock.Mock) {
				m.On("GetLeader", mock.Anything, mock.MatchedBy(func(req *service.GetLeaderRequest) bool {
					return req.ShardID == 2
				})).Return(&service.GetLeaderResponse{
					ShardID:  2,
					LeaderID: 5,
					Address:  "10.0.0.5:5000",
				}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.GetLeaderResponse) {
				assert.Equal(t, uint64(2), resp.ShardId)
				assert.Equal(t, uint64(5), resp.LeaderId)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.GetLeader(context.Background(), tt.request)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorCode != 0 {
					assert.Equal(t, tt.errorCode, status.Code(err))
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tt.validate != nil {
					tt.validate(t, resp)
				}
			}
			mockSvc.AssertExpectations(t)
		})
	}
}

// =============================================================================
// BatchGet Tests
// =============================================================================

func TestServer_BatchGet_Additional(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.BatchGetRequest
		mockSetup   func(m *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.BatchGetResponse)
	}{
		{
			name: "successful_batch_get",
			request: &pb.BatchGetRequest{
				Requests: []*pb.GetRequest{
					{EntityType: "users", Id: 1},
					{EntityType: "users", Id: 2},
					{EntityType: "users", Id: 3},
				},
			},
			mockSetup: func(m *mock.Mock) {
				m.On("BatchGet", mock.Anything, mock.MatchedBy(func(req *service.BatchGetRequest) bool {
					return len(req.Requests) == 3
				})).Return(&service.BatchGetResponse{
					Responses: []*service.GetResponse{
						{ID: 1, Version: 1, Data: []byte(`{"name":"user1"}`)},
						{ID: 2, Version: 1, Data: []byte(`{"name":"user2"}`)},
						{ID: 3, Version: 1, Data: []byte(`{"name":"user3"}`)},
					},
				}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.BatchGetResponse) {
				assert.Len(t, resp.Responses, 3)
				assert.Equal(t, uint64(1), resp.Responses[0].Id)
				assert.Equal(t, uint64(2), resp.Responses[1].Id)
				assert.Equal(t, uint64(3), resp.Responses[2].Id)
			},
		},
		{
			name: "empty_requests",
			request: &pb.BatchGetRequest{
				Requests: []*pb.GetRequest{},
			},
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_error",
			request: &pb.BatchGetRequest{
				Requests: []*pb.GetRequest{
					{EntityType: "users", Id: 1},
				},
			},
			mockSetup: func(m *mock.Mock) {
				m.On("BatchGet", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
		{
			name: "single_item_batch",
			request: &pb.BatchGetRequest{
				Requests: []*pb.GetRequest{
					{EntityType: "products", Id: 100},
				},
			},
			mockSetup: func(m *mock.Mock) {
				m.On("BatchGet", mock.Anything, mock.Anything).
					Return(&service.BatchGetResponse{
						Responses: []*service.GetResponse{
							{ID: 100, Version: 2, Data: []byte(`{"name":"product100"}`)},
						},
					}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.BatchGetResponse) {
				assert.Len(t, resp.Responses, 1)
				assert.Equal(t, uint64(100), resp.Responses[0].Id)
				assert.Equal(t, uint64(2), resp.Responses[0].Version)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.BatchGet(context.Background(), tt.request)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorCode != 0 {
					assert.Equal(t, tt.errorCode, status.Code(err))
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tt.validate != nil {
					tt.validate(t, resp)
				}
			}
			mockSvc.AssertExpectations(t)
		})
	}
}

// =============================================================================
// BatchDelete Tests
// =============================================================================

func TestServer_BatchDelete_Additional(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.BatchDeleteRequest
		mockSetup   func(m *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.BatchDeleteResponse)
	}{
		{
			name: "successful_batch_delete",
			request: &pb.BatchDeleteRequest{
				Requests: []*pb.DeleteRequest{
					{EntityType: "users", Id: 1},
					{EntityType: "users", Id: 2},
				},
			},
			mockSetup: func(m *mock.Mock) {
				m.On("BatchDelete", mock.Anything, mock.MatchedBy(func(req *service.BatchDeleteRequest) bool {
					return len(req.Requests) == 2
				})).Return(&service.BatchDeleteResponse{
					Responses: []*service.DeleteResponse{
						{Deleted: true, Timestamp: 1000},
						{Deleted: true, Timestamp: 1001},
					},
					Timestamp: 1001,
				}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.BatchDeleteResponse) {
				assert.Len(t, resp.Responses, 2)
				assert.True(t, resp.Responses[0].Deleted)
				assert.True(t, resp.Responses[1].Deleted)
			},
		},
		{
			name: "empty_requests",
			request: &pb.BatchDeleteRequest{
				Requests: []*pb.DeleteRequest{},
			},
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_error",
			request: &pb.BatchDeleteRequest{
				Requests: []*pb.DeleteRequest{
					{EntityType: "users", Id: 1},
				},
			},
			mockSetup: func(m *mock.Mock) {
				m.On("BatchDelete", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
		{
			name: "partial_delete_success",
			request: &pb.BatchDeleteRequest{
				Requests: []*pb.DeleteRequest{
					{EntityType: "items", Id: 10},
					{EntityType: "items", Id: 20},
				},
			},
			mockSetup: func(m *mock.Mock) {
				m.On("BatchDelete", mock.Anything, mock.Anything).
					Return(&service.BatchDeleteResponse{
						Responses: []*service.DeleteResponse{
							{Deleted: true, Timestamp: 2000},
							{Deleted: false, Timestamp: 2000}, // not found
						},
						Timestamp: 2000,
					}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.BatchDeleteResponse) {
				assert.Len(t, resp.Responses, 2)
				assert.True(t, resp.Responses[0].Deleted)
				assert.False(t, resp.Responses[1].Deleted)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.BatchDelete(context.Background(), tt.request)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorCode != 0 {
					assert.Equal(t, tt.errorCode, status.Code(err))
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tt.validate != nil {
					tt.validate(t, resp)
				}
			}
			mockSvc.AssertExpectations(t)
		})
	}
}

// =============================================================================
// Transaction Tests
// =============================================================================

func TestServer_Transaction_Additional(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.TransactionRequest
		mockSetup   func(m *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.TransactionResponse)
	}{
		{
			name: "successful_transaction",
			request: &pb.TransactionRequest{
				Success: []*pb.TransactionOp{
					{
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
						EntityType: "users",
						Id:         1,
						Data:       []byte(`{"name":"test"}`),
					},
				},
			},
			mockSetup: func(m *mock.Mock) {
				m.On("Transaction", mock.Anything, mock.Anything).
					Return(&service.TransactionResponse{
						Succeeded: true,
						Responses: []*service.TransactionOpResponse{
							{Success: true, ID: 1, Version: 1},
						},
					}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.TransactionResponse) {
				assert.True(t, resp.Succeeded)
				assert.Len(t, resp.Responses, 1)
			},
		},
		{
			name: "empty_operations",
			request: &pb.TransactionRequest{
				Success: []*pb.TransactionOp{},
			},
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "transaction_with_compare",
			request: &pb.TransactionRequest{
				Compare: []*pb.CompareCondition{
					{
						EntityType: "users",
						Id:         1,
						Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
						Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
						Version:    1,
					},
				},
				Success: []*pb.TransactionOp{
					{
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
						EntityType: "users",
						Id:         1,
						Data:       []byte(`{"name":"updated"}`),
					},
				},
			},
			mockSetup: func(m *mock.Mock) {
				m.On("Transaction", mock.Anything, mock.Anything).
					Return(&service.TransactionResponse{
						Succeeded: true,
						Responses: []*service.TransactionOpResponse{
							{Success: true, ID: 1, Version: 2},
						},
					}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.TransactionResponse) {
				assert.True(t, resp.Succeeded)
			},
		},
		{
			name: "transaction_service_error",
			request: &pb.TransactionRequest{
				Success: []*pb.TransactionOp{
					{
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
						EntityType: "users",
						Id:         1,
						Data:       []byte(`{"name":"test"}`),
					},
				},
			},
			mockSetup: func(m *mock.Mock) {
				m.On("Transaction", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
		{
			name: "transaction_with_failure_ops",
			request: &pb.TransactionRequest{
				Compare: []*pb.CompareCondition{
					{
						EntityType: "users",
						Id:         1,
						Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
						Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
						Version:    5, // This will fail the compare
					},
				},
				Success: []*pb.TransactionOp{
					{
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
						EntityType: "users",
						Id:         1,
						Data:       []byte(`{"name":"success"}`),
					},
				},
				Failure: []*pb.TransactionOp{
					{
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_GET,
						EntityType: "users",
						Id:         1,
					},
				},
			},
			mockSetup: func(m *mock.Mock) {
				m.On("Transaction", mock.Anything, mock.Anything).
					Return(&service.TransactionResponse{
						Succeeded: false, // Compare failed
						Responses: []*service.TransactionOpResponse{
							{Success: true, ID: 1, Data: []byte(`{"name":"current"}`)},
						},
					}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.TransactionResponse) {
				assert.False(t, resp.Succeeded) // Compare failed, failure ops executed
				assert.Len(t, resp.Responses, 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.Transaction(context.Background(), tt.request)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorCode != 0 {
					assert.Equal(t, tt.errorCode, status.Code(err))
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tt.validate != nil {
					tt.validate(t, resp)
				}
			}
			mockSvc.AssertExpectations(t)
		})
	}
}
