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

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Close Deep Path Tests
// =============================================================================

func TestQuicBaaS_Close_WithAllComponentsNoError(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockBackup := &mockBackupManager{}

	svc := createTestService(t,
		withMockRaftNode(mockRaft),
		withBackupManager(mockBackup),
	)

	db := svc.(*quicBaaS)
	err := db.Close()
	require.NoError(t, err)
	assert.True(t, db.closed)
}

func TestQuicBaaS_Close_AlreadyClosedIdempotent(t *testing.T) {
	svc := createTestService(t)

	db := svc.(*quicBaaS)

	err1 := db.Close()
	require.NoError(t, err1)
	assert.True(t, db.closed)

	// Second call should be idempotent
	err2 := db.Close()
	require.NoError(t, err2)
}

func TestQuicBaaS_Close_WithBackupManagerCleanup(t *testing.T) {
	mockBackup := &mockBackupManager{}

	svc := createTestService(t, withBackupManager(mockBackup))

	db := svc.(*quicBaaS)
	err := db.Close()
	require.NoError(t, err)
	assert.True(t, db.closed)
}

func TestQuicBaaS_Close_NilLoggerDoesNotPanic(t *testing.T) {
	svc := createTestService(t)
	db := svc.(*quicBaaS)
	db.logger = nil

	err := db.Close()
	require.NoError(t, err)
	assert.True(t, db.closed)
}

// =============================================================================
// GetUpgradeStatus Error Paths
// =============================================================================

func TestOperationsService_GetUpgradeStatus_NilRequestFails(t *testing.T) {
	svc := createTestService(t)
	resp, err := svc.GetUpgradeStatus(context.Background(), nil)
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestOperationsService_GetUpgradeStatus_EmptyJobIDFails(t *testing.T) {
	svc := createTestService(t)
	resp, err := svc.GetUpgradeStatus(context.Background(), &GetUpgradeStatusRequest{JobID: ""})
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestOperationsService_GetUpgradeStatus_JobNotFoundFails(t *testing.T) {
	svc := createTestService(t)
	resp, err := svc.GetUpgradeStatus(context.Background(), &GetUpgradeStatusRequest{JobID: "nonexistent"})
	assert.Nil(t, resp)
	require.Error(t, err)
}
