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
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func createSealServiceForCoverage(mgr *mockBadSealManager) *SealService {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	return NewSealService(mgr, log)
}

// =============================================================================
// SealService Type Assertion Failure Paths
// =============================================================================

func TestSealServiceCoverage_Bootstrap_BadType(t *testing.T) {
	svc := createSealServiceForCoverage(&mockBadSealManager{returnBadType: true})
	resp, err := svc.Bootstrap(context.Background(), &BootstrapRequest{Threshold: 2, TotalShares: 3})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "unexpected")
}

func TestSealServiceCoverage_Unseal_BadType(t *testing.T) {
	svc := createSealServiceForCoverage(&mockBadSealManager{returnBadType: true})
	resp, err := svc.Unseal(context.Background(), &UnsealRequestDTO{Share: []byte("s")})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "unexpected")
}

func TestSealServiceCoverage_Status_BadType(t *testing.T) {
	svc := createSealServiceForCoverage(&mockBadSealManager{returnBadType: true})
	resp, err := svc.Status(context.Background())
	// Status returns error in DTO, not as Go error
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "unexpected")
}

func TestSealServiceCoverage_Status_ManagerError(t *testing.T) {
	svc := createSealServiceForCoverage(&mockBadSealManager{statusErr: assert.AnError})
	resp, err := svc.Status(context.Background())
	require.NoError(t, err)
	assert.Contains(t, resp.Error, assert.AnError.Error())
}

func TestSealServiceCoverage_Rekey_BadType(t *testing.T) {
	svc := createSealServiceForCoverage(&mockBadSealManager{returnBadType: true})
	resp, err := svc.Rekey(context.Background(), &RekeyRequestDTO{NewThreshold: 3, NewTotal: 5})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "unexpected")
}

// =============================================================================
// ServerWithSeal Full Lifecycle with Real Manager
// =============================================================================

func TestServerWithSealCoverage_FullLifecycle(t *testing.T) {
	mgr := newDefaultSealManager()
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	srv := &Server{logger: log}
	sws := NewServerWithSeal(srv, mgr)

	// Init
	initResp, err := sws.SealInit(context.Background(), &pb.SealInitRequest{Threshold: 2, TotalShares: 3})
	require.NoError(t, err)
	assert.Empty(t, initResp.Error)
	assert.NotEmpty(t, initResp.Shares)

	// Status
	statusResp, err := sws.SealStatus(context.Background(), &pb.SealStatusRequest{})
	require.NoError(t, err)
	assert.True(t, statusResp.Sealed)
	assert.True(t, statusResp.Initialized)

	// Unseal
	unsealResp, err := sws.Unseal(context.Background(), &pb.UnsealRequest{Share: []byte("s1")})
	require.NoError(t, err)
	assert.Empty(t, unsealResp.Error)

	unsealResp, err = sws.Unseal(context.Background(), &pb.UnsealRequest{Share: []byte("s2")})
	require.NoError(t, err)
	assert.False(t, unsealResp.Sealed)

	// Seal
	sealResp, err := sws.Seal(context.Background(), &pb.SealRequest{})
	require.NoError(t, err)
	assert.True(t, sealResp.Sealed)

	// Unseal again for Rekey
	_, _ = sws.Unseal(context.Background(), &pb.UnsealRequest{Share: []byte("s1")})
	_, _ = sws.Unseal(context.Background(), &pb.UnsealRequest{Share: []byte("s2")})

	// Rekey
	rekeyResp, err := sws.SealRekey(context.Background(), &pb.SealRekeyRequest{NewThreshold: 3, NewTotal: 5})
	require.NoError(t, err)
	assert.Empty(t, rekeyResp.Error)
	assert.NotEmpty(t, rekeyResp.Shares)
}
