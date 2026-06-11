//go:build !integration

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
// IAM provider accessors (quicbaas_iam.go) — return nil / no error when no
// IAM provider is wired.
// =============================================================================

func TestQuicBaaS_GetAppStorage_NilWhenNoProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl, ok := svc.(*quicBaaS)
	require.True(t, ok)

	store, err := impl.GetAppStorage(context.Background())
	require.NoError(t, err)
	assert.Nil(t, store)
}

func TestQuicBaaS_GetFederationManager_NilWhenNoProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl, ok := svc.(*quicBaaS)
	require.True(t, ok)

	fm, err := impl.GetFederationManager(context.Background())
	require.NoError(t, err)
	assert.Nil(t, fm)
}
