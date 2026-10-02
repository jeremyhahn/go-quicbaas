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

func TestServer_AddNode(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.AddNodeRequest
		mockSetup   func(m *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.AddNodeResponse)
	}{
		{
			name: "successful_add_node",
			request: &pb.AddNodeRequest{
				ShardId: 1,
				NodeId:  2,
				Address: "10.0.0.2:5001",
			},
			mockSetup: func(m *mock.Mock) {
				m.On("AddNode", mock.Anything, mock.MatchedBy(func(req *service.AddNodeRequest) bool {
					return req.ShardID == 1 && req.NodeID == 2 && req.Address == "10.0.0.2:5001"
				})).Return(&service.AddNodeResponse{
					Success: true,
					Message: "node added",
				}, nil).Once()
			},
			validate: func(t *testing.T, resp *pb.AddNodeResponse) {
				assert.True(t, resp.Success)
				assert.Equal(t, "node added", resp.Message)
				assert.Equal(t, uint64(1), resp.ShardId)
				assert.Equal(t, uint64(2), resp.NodeId)
			},
		},
		{
			name: "identity_reaches_the_service",
			request: &pb.AddNodeRequest{
				ShardId:  1,
				NodeId:   3,
				Address:  "10.0.0.3:5001",
				Identity: "spki:0123abcd",
			},
			mockSetup: func(m *mock.Mock) {
				m.On("AddNode", mock.Anything, mock.MatchedBy(func(req *service.AddNodeRequest) bool {
					return req.NodeID == 3 && req.Identity == "spki:0123abcd"
				})).Return(&service.AddNodeResponse{Success: true}, nil).Once()
			},
			validate: func(t *testing.T, resp *pb.AddNodeResponse) {
				assert.True(t, resp.Success)
			},
		},
		{
			name:        "nil_request",
			request:     nil,
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_shard_id",
			request: &pb.AddNodeRequest{
				ShardId: 0,
				NodeId:  2,
				Address: "10.0.0.2:5001",
			},
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_node_id",
			request: &pb.AddNodeRequest{
				ShardId: 1,
				NodeId:  0,
				Address: "10.0.0.2:5001",
			},
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_address",
			request: &pb.AddNodeRequest{
				ShardId: 1,
				NodeId:  2,
				Address: "",
			},
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_error",
			request: &pb.AddNodeRequest{
				ShardId: 1,
				NodeId:  2,
				Address: "10.0.0.2:5001",
			},
			mockSetup: func(m *mock.Mock) {
				m.On("AddNode", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.AddNode(context.Background(), tt.request)
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

func TestServer_RemoveNode(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.RemoveNodeRequest
		mockSetup   func(m *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.RemoveNodeResponse)
	}{
		{
			name: "successful_remove_node",
			request: &pb.RemoveNodeRequest{
				ShardId: 1,
				NodeId:  2,
			},
			mockSetup: func(m *mock.Mock) {
				m.On("RemoveNode", mock.Anything, mock.MatchedBy(func(req *service.RemoveNodeRequest) bool {
					return req.ShardID == 1 && req.NodeID == 2
				})).Return(&service.RemoveNodeResponse{
					Success: true,
					Message: "node removed",
				}, nil).Once()
			},
			validate: func(t *testing.T, resp *pb.RemoveNodeResponse) {
				assert.True(t, resp.Success)
				assert.Equal(t, "node removed", resp.Message)
				assert.Equal(t, uint64(1), resp.ShardId)
				assert.Equal(t, uint64(2), resp.NodeId)
			},
		},
		{
			name:        "nil_request",
			request:     nil,
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_shard_id",
			request: &pb.RemoveNodeRequest{
				ShardId: 0,
				NodeId:  2,
			},
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_node_id",
			request: &pb.RemoveNodeRequest{
				ShardId: 1,
				NodeId:  0,
			},
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_error",
			request: &pb.RemoveNodeRequest{
				ShardId: 1,
				NodeId:  2,
			},
			mockSetup: func(m *mock.Mock) {
				m.On("RemoveNode", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.RemoveNode(context.Background(), tt.request)
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

func TestServer_CreateShard(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.CreateShardRequest
		mockSetup   func(m *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.CreateShardResponse)
	}{
		{
			name: "successful_create_shard",
			request: &pb.CreateShardRequest{
				ShardId: 5,
				InitialMembers: map[uint64]string{
					1: "10.0.0.1:5001",
					2: "10.0.0.2:5001",
					3: "10.0.0.3:5001",
				},
			},
			mockSetup: func(m *mock.Mock) {
				m.On("CreateShard", mock.Anything, mock.MatchedBy(func(req *service.CreateShardRequest) bool {
					return req.ShardID == 5 && len(req.InitialMembers) == 3
				})).Return(&service.CreateShardResponse{
					ShardID:  5,
					LeaderID: 1,
					Status:   "created",
				}, nil).Once()
			},
			validate: func(t *testing.T, resp *pb.CreateShardResponse) {
				assert.True(t, resp.Success)
				assert.Equal(t, "created", resp.Message)
				assert.Equal(t, uint64(5), resp.ShardId)
				assert.Equal(t, uint64(1), resp.LeaderId)
			},
		},
		{
			name:        "nil_request",
			request:     nil,
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_shard_id",
			request: &pb.CreateShardRequest{
				ShardId:        0,
				InitialMembers: map[uint64]string{1: "10.0.0.1:5001"},
			},
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_initial_members",
			request: &pb.CreateShardRequest{
				ShardId:        5,
				InitialMembers: map[uint64]string{},
			},
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_error",
			request: &pb.CreateShardRequest{
				ShardId:        5,
				InitialMembers: map[uint64]string{1: "10.0.0.1:5001"},
			},
			mockSetup: func(m *mock.Mock) {
				m.On("CreateShard", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.CreateShard(context.Background(), tt.request)
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

func TestServer_RebalanceCluster(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.RebalanceClusterRequest
		mockSetup   func(m *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.RebalanceClusterResponse)
	}{
		{
			name: "successful_rebalance",
			request: &pb.RebalanceClusterRequest{
				Strategy: "balanced",
			},
			mockSetup: func(m *mock.Mock) {
				m.On("RebalanceCluster", mock.Anything, mock.MatchedBy(func(req *service.RebalanceClusterRequest) bool {
					return req.Strategy == "balanced"
				})).Return(&service.RebalanceClusterResponse{
					JobID:               "job-123",
					EstimatedCompletion: 1706616000,
				}, nil).Once()
			},
			validate: func(t *testing.T, resp *pb.RebalanceClusterResponse) {
				assert.Equal(t, "job-123", resp.OperationId)
				assert.Equal(t, int64(1706616000), resp.Started)
			},
		},
		{
			name:        "nil_request",
			request:     nil,
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_error",
			request: &pb.RebalanceClusterRequest{
				Strategy: "balanced",
			},
			mockSetup: func(m *mock.Mock) {
				m.On("RebalanceCluster", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.RebalanceCluster(context.Background(), tt.request)
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
