// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package cluster

import (
	"context"
	"testing"

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

func TestClient_Status(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful status",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if service != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, service)
					}
					if method != "Status" {
						t.Errorf("expected method %q, got %q", "Status", method)
					}
					return &ClusterStatus{
						ClusterID: "cluster-1",
						LeaderID:  "node-1",
						Healthy:   true,
						Members: []ClusterMember{
							{ID: "node-1", Name: "node-1", Address: "localhost:50051", IsLeader: true, Healthy: true},
							{ID: "node-2", Name: "node-2", Address: "localhost:50052", IsLeader: false, Healthy: true},
						},
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			status, err := client.Status(tt.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("Status() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && status == nil {
				t.Error("expected non-nil status")
			}
		})
	}
}

func TestClient_Nodes(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
		nodeCount int
	}{
		{
			name: "successful nodes list",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "Nodes" {
						t.Errorf("expected method %q, got %q", "Nodes", method)
					}
					return &NodesResponse{
						Nodes: []NodeInfo{
							{ID: "node-1", Name: "node-1", Address: "localhost:50051", State: NodeStateHealthy},
							{ID: "node-2", Name: "node-2", Address: "localhost:50052", State: NodeStateHealthy},
							{ID: "node-3", Name: "node-3", Address: "localhost:50053", State: NodeStateUnhealthy},
						},
					}, nil
				},
			},
			wantErr:   false,
			nodeCount: 3,
		},
		{
			name: "empty nodes list with nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr:   false,
			nodeCount: 0,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return 123, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			nodes, err := client.Nodes(tt.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("Nodes() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && len(nodes.Nodes) != tt.nodeCount {
				t.Errorf("expected %d nodes, got %d", tt.nodeCount, len(nodes.Nodes))
			}
		})
	}
}

func TestClient_AddNode_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &addNodeResponse{}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *AddNodeRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "missing shard",
			req:     &AddNodeRequest{NodeID: 4, Address: "localhost:50054"},
			wantErr: true,
		},
		{
			name:    "missing node",
			req:     &AddNodeRequest{ShardID: 1, Address: "localhost:50054"},
			wantErr: true,
		},
		{
			name:    "empty address",
			req:     &AddNodeRequest{ShardID: 1, NodeID: 4},
			wantErr: true,
		},
		{
			name:    "valid request with identity",
			req:     &AddNodeRequest{ShardID: 1, NodeID: 4, Address: "localhost:50054", Identity: "spki:ab"},
			wantErr: false,
		},
		{
			name:    "valid request using the committed identity",
			req:     &AddNodeRequest{ShardID: 1, NodeID: 4, Address: "localhost:50054"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.AddNode(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddNode() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_AddNode(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful add node",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "AddNode" {
						t.Errorf("expected method %q, got %q", "AddNode", method)
					}
					addReq, ok := req.(*AddNodeRequest)
					if !ok {
						t.Errorf("expected AddNodeRequest, got %T", req)
					}
					if addReq.Address != "localhost:50054" {
						t.Errorf("expected address localhost:50054, got %s", addReq.Address)
					}
					return &addNodeResponse{}, nil
				},
			},
			wantErr: false,
		},
		{
			name: "successful add node with nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			err := client.AddNode(tt.ctx, &AddNodeRequest{ShardID: 1, NodeID: 4, Address: "localhost:50054"})
			if (err != nil) != tt.wantErr {
				t.Errorf("AddNode() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && tt.errCode != errors.CodeUnknown {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
		})
	}
}

func TestClient_RemoveNode_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &removeNodeResponse{}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *RemoveNodeRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty node ID",
			req:     &RemoveNodeRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &RemoveNodeRequest{NodeID: "node-3"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.RemoveNode(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("RemoveNode() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_RemoveNode(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful remove node",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "RemoveNode" {
						t.Errorf("expected method %q, got %q", "RemoveNode", method)
					}
					return &removeNodeResponse{}, nil
				},
			},
			wantErr: false,
		},
		{
			name: "successful remove node with nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "node not found",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewNotFound("test", "node not found")
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return 123, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			err := client.RemoveNode(tt.ctx, &RemoveNodeRequest{NodeID: "node-3"})
			if (err != nil) != tt.wantErr {
				t.Errorf("RemoveNode() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && tt.errCode != errors.CodeUnknown {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
		})
	}
}

func TestClient_RebalanceCluster_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &RebalanceClusterResponse{
				OperationID: "op-123",
				Started:     1000000000,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *RebalanceClusterRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "valid request with strategy",
			req:     &RebalanceClusterRequest{Strategy: "round_robin"},
			wantErr: false,
		},
		{
			name:    "valid request without strategy",
			req:     &RebalanceClusterRequest{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.RebalanceCluster(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("RebalanceCluster() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_RebalanceCluster(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful rebalance",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if service != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, service)
					}
					if method != "Rebalance" {
						t.Errorf("expected method %q, got %q", "Rebalance", method)
					}
					rebalanceReq, ok := req.(*RebalanceClusterRequest)
					if !ok {
						t.Errorf("expected *RebalanceClusterRequest, got %T", req)
					}
					if rebalanceReq.Strategy != "round_robin" {
						t.Errorf("expected strategy %q, got %q", "round_robin", rebalanceReq.Strategy)
					}
					return &RebalanceClusterResponse{
						OperationID: "op-456",
						Started:     1700000000,
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.RebalanceCluster(tt.ctx, &RebalanceClusterRequest{Strategy: "round_robin"})
			if (err != nil) != tt.wantErr {
				t.Errorf("RebalanceCluster() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && tt.errCode != errors.CodeUnknown {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr {
				if resp == nil {
					t.Fatal("expected non-nil response")
				}
				if resp.OperationID != "op-456" {
					t.Errorf("expected operation ID %q, got %q", "op-456", resp.OperationID)
				}
				if resp.Started != 1700000000 {
					t.Errorf("expected started %d, got %d", 1700000000, resp.Started)
				}
			}
		})
	}
}

func TestClient_GetRebalanceProgress_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetRebalanceProgressResponse{
				OperationID:     "op-123",
				Status:          "running",
				PercentComplete: 50.0,
				ShardsMoved:     5,
				TotalShards:     10,
				ElapsedTime:     30000000000,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GetRebalanceProgressRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty operation ID",
			req:     &GetRebalanceProgressRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &GetRebalanceProgressRequest{OperationID: "op-123"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.GetRebalanceProgress(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetRebalanceProgress() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_GetRebalanceProgress(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful get progress",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if service != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, service)
					}
					if method != "GetRebalanceStatus" {
						t.Errorf("expected method %q, got %q", "GetRebalanceStatus", method)
					}
					progressReq, ok := req.(*GetRebalanceProgressRequest)
					if !ok {
						t.Errorf("expected *GetRebalanceProgressRequest, got %T", req)
					}
					if progressReq.OperationID != "op-789" {
						t.Errorf("expected operation ID %q, got %q", "op-789", progressReq.OperationID)
					}
					return &GetRebalanceProgressResponse{
						OperationID:     "op-789",
						Status:          "completed",
						PercentComplete: 100.0,
						ShardsMoved:     10,
						TotalShards:     10,
						ElapsedTime:     60000000000,
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return 42, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "operation not found",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewNotFound("test", "operation not found")
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GetRebalanceProgress(tt.ctx, &GetRebalanceProgressRequest{OperationID: "op-789"})
			if (err != nil) != tt.wantErr {
				t.Errorf("GetRebalanceProgress() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && tt.errCode != errors.CodeUnknown {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr {
				if resp == nil {
					t.Fatal("expected non-nil response")
				}
				if resp.OperationID != "op-789" {
					t.Errorf("expected operation ID %q, got %q", "op-789", resp.OperationID)
				}
				if resp.Status != "completed" {
					t.Errorf("expected status %q, got %q", "completed", resp.Status)
				}
				if resp.PercentComplete != 100.0 {
					t.Errorf("expected percent complete %f, got %f", 100.0, resp.PercentComplete)
				}
				if resp.ShardsMoved != 10 {
					t.Errorf("expected shards moved %d, got %d", 10, resp.ShardsMoved)
				}
				if resp.TotalShards != 10 {
					t.Errorf("expected total shards %d, got %d", 10, resp.TotalShards)
				}
			}
		})
	}
}

func TestNodeState_String(t *testing.T) {
	tests := []struct {
		state    NodeState
		expected string
	}{
		{NodeStateUnknown, "UNKNOWN"},
		{NodeStateHealthy, "HEALTHY"},
		{NodeStateUnhealthy, "UNHEALTHY"},
		{NodeStateLeaving, "LEAVING"},
		{NodeState(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expected {
				t.Errorf("String() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestClusterStatus_Members(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &ClusterStatus{
				ClusterID: "test-cluster",
				LeaderID:  "node-1",
				Healthy:   true,
				Members: []ClusterMember{
					{ID: "node-1", Name: "primary", Address: "10.0.0.1:50051", IsLeader: true, Healthy: true},
					{ID: "node-2", Name: "secondary-1", Address: "10.0.0.2:50051", IsLeader: false, Healthy: true},
					{ID: "node-3", Name: "secondary-2", Address: "10.0.0.3:50051", IsLeader: false, Healthy: false},
				},
			}, nil
		},
	}

	client := NewClient(mockT)
	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.ClusterID != "test-cluster" {
		t.Errorf("expected cluster ID 'test-cluster', got %q", status.ClusterID)
	}

	if len(status.Members) != 3 {
		t.Fatalf("expected 3 members, got %d", len(status.Members))
	}

	leaderCount := 0
	healthyCount := 0
	for _, m := range status.Members {
		if m.IsLeader {
			leaderCount++
		}
		if m.Healthy {
			healthyCount++
		}
	}

	if leaderCount != 1 {
		t.Errorf("expected 1 leader, got %d", leaderCount)
	}
	if healthyCount != 2 {
		t.Errorf("expected 2 healthy members, got %d", healthyCount)
	}
}
