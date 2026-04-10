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

package testutil

import (
	"context"

	"github.com/stretchr/testify/mock"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// MockClusterDEKService is a testify mock implementation of service.ClusterDEKService.
type MockClusterDEKService struct {
	mock.Mock
}

// Ensure MockClusterDEKService implements service.ClusterDEKService at compile time.
var _ service.ClusterDEKService = (*MockClusterDEKService)(nil)

// NewMockClusterDEKService creates a new MockClusterDEKService.
func NewMockClusterDEKService() *MockClusterDEKService {
	return &MockClusterDEKService{}
}

func (m *MockClusterDEKService) InitializeClusterDEK(ctx context.Context, req *service.InitClusterDEKRequest) (*service.InitClusterDEKResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.InitClusterDEKResponse)
	return resp, args.Error(1)
}

func (m *MockClusterDEKService) GetClusterDEKStatus(ctx context.Context) (*service.ClusterDEKStatusResponse, error) {
	args := m.Called(ctx)
	resp, _ := args.Get(0).(*service.ClusterDEKStatusResponse)
	return resp, args.Error(1)
}

func (m *MockClusterDEKService) RotateClusterDEK(ctx context.Context, req *service.RotateClusterDEKRequest) (*service.RotateClusterDEKResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RotateClusterDEKResponse)
	return resp, args.Error(1)
}

func (m *MockClusterDEKService) SyncNodeDEK(ctx context.Context, req *service.SyncNodeDEKRequest) (*service.SyncNodeDEKResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.SyncNodeDEKResponse)
	return resp, args.Error(1)
}

func (m *MockClusterDEKService) GetNodeDEKStatus(ctx context.Context, req *service.GetNodeDEKStatusRequest) (*service.GetNodeDEKStatusResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetNodeDEKStatusResponse)
	return resp, args.Error(1)
}

func (m *MockClusterDEKService) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.StartReencryptionResponse)
	return resp, args.Error(1)
}

func (m *MockClusterDEKService) GetReencryptionStatus(ctx context.Context) (*service.ReencryptionStatusResponse, error) {
	args := m.Called(ctx)
	resp, _ := args.Get(0).(*service.ReencryptionStatusResponse)
	return resp, args.Error(1)
}

func (m *MockClusterDEKService) CancelReencryption(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockClusterDEKService) RecoverDEK(ctx context.Context, req *service.RecoverDEKRequest) (*service.RecoverDEKResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RecoverDEKResponse)
	return resp, args.Error(1)
}
