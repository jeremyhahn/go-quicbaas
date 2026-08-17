//go:build mcp

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

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// MockQuicBaaS is a mock implementation of service.QuicBaaS that embeds
// mockService for default method implementations and mock.Mock for explicit
// mock expectations.
type MockQuicBaaS struct {
	mock.Mock
	mockService
}

// Override methods that need mock.Mock behavior for testability.
func (m *MockQuicBaaS) Put(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.PutResponse), args.Error(1)
}

func (m *MockQuicBaaS) Get(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetResponse), args.Error(1)
}

func (m *MockQuicBaaS) Delete(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.DeleteResponse), args.Error(1)
}

func (m *MockQuicBaaS) List(ctx context.Context, req *service.ListRequest, stream chan<- *service.ListResponse) error {
	args := m.Called(ctx, req, stream)
	return args.Error(0)
}

func (m *MockQuicBaaS) Health(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.HealthResponse), args.Error(1)
}

func (m *MockQuicBaaS) Liveness(ctx context.Context, req *service.LivenessRequest) (*service.LivenessResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.LivenessResponse), args.Error(1)
}

func (m *MockQuicBaaS) Readiness(ctx context.Context, req *service.ReadinessRequest) (*service.ReadinessResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ReadinessResponse), args.Error(1)
}

func (m *MockQuicBaaS) ClusterStatus(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ClusterStatusResponse), args.Error(1)
}

func (m *MockQuicBaaS) Login(ctx context.Context, req *service.LoginRequest) (*service.LoginResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.LoginResponse), args.Error(1)
}

func (m *MockQuicBaaS) ValidateToken(ctx context.Context, req *service.ValidateTokenRequest) (*service.ValidateTokenResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ValidateTokenResponse), args.Error(1)
}

func (m *MockQuicBaaS) Transaction(ctx context.Context, req *service.TransactionRequest) (*service.TransactionResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.TransactionResponse), args.Error(1)
}

func (m *MockQuicBaaS) SetSignalService(svc interface{})      {}
func (m *MockQuicBaaS) Signal() interface{}                   { return nil }
func (m *MockQuicBaaS) AgentAdmin() service.AgentAdminService { return nil }

// Compile-time assertion that MockQuicBaaS satisfies service.QuicBaaS.
var _ service.QuicBaaS = (*MockQuicBaaS)(nil)

// newTestServer builds an MCP tool-registry server over a fresh
// MockQuicBaaS for tests that need direct control over service expectations.
func newTestServer(t *testing.T) (*Server, *MockQuicBaaS) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout", Component: "mcp-test"})
	require.NoError(t, err)
	mockSvc := &MockQuicBaaS{}
	server := NewServer(mockSvc, nil, log)
	return server, mockSvc
}
