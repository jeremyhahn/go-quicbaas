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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func TestNewMockClusterDEKService(t *testing.T) {
	t.Run("returns_non_nil_mock", func(t *testing.T) {
		m := NewMockClusterDEKService()
		require.NotNil(t, m)
	})
}

// ---------------------------------------------------------------------------
// InitializeClusterDEK
// ---------------------------------------------------------------------------

func TestMockClusterDEKService_InitializeClusterDEK_Success(t *testing.T) {
	m := NewMockClusterDEKService()
	ctx := context.Background()
	req := &service.InitClusterDEKRequest{}
	expected := &service.InitClusterDEKResponse{}
	m.On("InitializeClusterDEK", ctx, req).Return(expected, nil)

	resp, err := m.InitializeClusterDEK(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockClusterDEKService_InitializeClusterDEK_Error(t *testing.T) {
	m := NewMockClusterDEKService()
	ctx := context.Background()
	req := &service.InitClusterDEKRequest{}
	m.On("InitializeClusterDEK", ctx, req).Return((*service.InitClusterDEKResponse)(nil), errTest)

	resp, err := m.InitializeClusterDEK(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// GetClusterDEKStatus
// ---------------------------------------------------------------------------

func TestMockClusterDEKService_GetClusterDEKStatus_Success(t *testing.T) {
	m := NewMockClusterDEKService()
	ctx := context.Background()
	expected := &service.ClusterDEKStatusResponse{}
	m.On("GetClusterDEKStatus", ctx).Return(expected, nil)

	resp, err := m.GetClusterDEKStatus(ctx)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockClusterDEKService_GetClusterDEKStatus_Error(t *testing.T) {
	m := NewMockClusterDEKService()
	ctx := context.Background()
	m.On("GetClusterDEKStatus", ctx).Return((*service.ClusterDEKStatusResponse)(nil), errTest)

	resp, err := m.GetClusterDEKStatus(ctx)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// RotateClusterDEK
// ---------------------------------------------------------------------------

func TestMockClusterDEKService_RotateClusterDEK_Success(t *testing.T) {
	m := NewMockClusterDEKService()
	ctx := context.Background()
	req := &service.RotateClusterDEKRequest{}
	expected := &service.RotateClusterDEKResponse{}
	m.On("RotateClusterDEK", ctx, req).Return(expected, nil)

	resp, err := m.RotateClusterDEK(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockClusterDEKService_RotateClusterDEK_Error(t *testing.T) {
	m := NewMockClusterDEKService()
	ctx := context.Background()
	req := &service.RotateClusterDEKRequest{}
	m.On("RotateClusterDEK", ctx, req).Return((*service.RotateClusterDEKResponse)(nil), errTest)

	resp, err := m.RotateClusterDEK(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// SyncNodeDEK
// ---------------------------------------------------------------------------

func TestMockClusterDEKService_SyncNodeDEK_Success(t *testing.T) {
	m := NewMockClusterDEKService()
	ctx := context.Background()
	req := &service.SyncNodeDEKRequest{}
	expected := &service.SyncNodeDEKResponse{}
	m.On("SyncNodeDEK", ctx, req).Return(expected, nil)

	resp, err := m.SyncNodeDEK(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockClusterDEKService_SyncNodeDEK_Error(t *testing.T) {
	m := NewMockClusterDEKService()
	ctx := context.Background()
	req := &service.SyncNodeDEKRequest{}
	m.On("SyncNodeDEK", ctx, req).Return((*service.SyncNodeDEKResponse)(nil), errTest)

	resp, err := m.SyncNodeDEK(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// GetNodeDEKStatus
// ---------------------------------------------------------------------------

func TestMockClusterDEKService_GetNodeDEKStatus_Success(t *testing.T) {
	m := NewMockClusterDEKService()
	ctx := context.Background()
	req := &service.GetNodeDEKStatusRequest{}
	expected := &service.GetNodeDEKStatusResponse{}
	m.On("GetNodeDEKStatus", ctx, req).Return(expected, nil)

	resp, err := m.GetNodeDEKStatus(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockClusterDEKService_GetNodeDEKStatus_Error(t *testing.T) {
	m := NewMockClusterDEKService()
	ctx := context.Background()
	req := &service.GetNodeDEKStatusRequest{}
	m.On("GetNodeDEKStatus", ctx, req).Return((*service.GetNodeDEKStatusResponse)(nil), errTest)

	resp, err := m.GetNodeDEKStatus(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// StartReencryption
// ---------------------------------------------------------------------------

func TestMockClusterDEKService_StartReencryption_Success(t *testing.T) {
	m := NewMockClusterDEKService()
	ctx := context.Background()
	req := &service.StartReencryptionRequest{}
	expected := &service.StartReencryptionResponse{}
	m.On("StartReencryption", ctx, req).Return(expected, nil)

	resp, err := m.StartReencryption(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockClusterDEKService_StartReencryption_Error(t *testing.T) {
	m := NewMockClusterDEKService()
	ctx := context.Background()
	req := &service.StartReencryptionRequest{}
	m.On("StartReencryption", ctx, req).Return((*service.StartReencryptionResponse)(nil), errTest)

	resp, err := m.StartReencryption(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// GetReencryptionStatus
// ---------------------------------------------------------------------------

func TestMockClusterDEKService_GetReencryptionStatus_Success(t *testing.T) {
	m := NewMockClusterDEKService()
	ctx := context.Background()
	expected := &service.ReencryptionStatusResponse{}
	m.On("GetReencryptionStatus", ctx).Return(expected, nil)

	resp, err := m.GetReencryptionStatus(ctx)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockClusterDEKService_GetReencryptionStatus_Error(t *testing.T) {
	m := NewMockClusterDEKService()
	ctx := context.Background()
	m.On("GetReencryptionStatus", ctx).Return((*service.ReencryptionStatusResponse)(nil), errTest)

	resp, err := m.GetReencryptionStatus(ctx)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// CancelReencryption
// ---------------------------------------------------------------------------

func TestMockClusterDEKService_CancelReencryption_Success(t *testing.T) {
	m := NewMockClusterDEKService()
	ctx := context.Background()
	m.On("CancelReencryption", ctx).Return(nil)

	err := m.CancelReencryption(ctx)
	assert.NoError(t, err)
	m.AssertExpectations(t)
}

func TestMockClusterDEKService_CancelReencryption_Error(t *testing.T) {
	m := NewMockClusterDEKService()
	ctx := context.Background()
	m.On("CancelReencryption", ctx).Return(errTest)

	err := m.CancelReencryption(ctx)
	assert.ErrorIs(t, err, errTest)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// RecoverDEK
// ---------------------------------------------------------------------------

func TestMockClusterDEKService_RecoverDEK_Success(t *testing.T) {
	m := NewMockClusterDEKService()
	ctx := context.Background()
	req := &service.RecoverDEKRequest{}
	expected := &service.RecoverDEKResponse{}
	m.On("RecoverDEK", ctx, req).Return(expected, nil)

	resp, err := m.RecoverDEK(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockClusterDEKService_RecoverDEK_Error(t *testing.T) {
	m := NewMockClusterDEKService()
	ctx := context.Background()
	req := &service.RecoverDEKRequest{}
	m.On("RecoverDEK", ctx, req).Return((*service.RecoverDEKResponse)(nil), errTest)

	resp, err := m.RecoverDEK(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}
