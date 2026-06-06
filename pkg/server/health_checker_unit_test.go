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

// health_checker_unit_test.go exercises the HealthChecker methods against
// a freshly-constructed Server where no transport listeners exist yet.
// These tests do NOT start any network listeners; they verify the
// uninitialized-component branches that report Unhealthy without touching
// the listener address. The "everything started" branches are exercised by
// integration tests; this file locks in the diagnostics path the BaaS
// operator sees when a subsystem is missing.

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthChecker_CheckGRPC_NotInitialized(t *testing.T) {
	srv := withTempServer(t)
	hc := srv.GetHealthChecker()
	require.NotNil(t, hc)

	got := hc.CheckGRPC()
	assert.Equal(t, HealthStatusUnhealthy, got.Status)
	assert.Equal(t, "grpc", got.Name)
	assert.Contains(t, got.Message, "not initialized")
	assert.False(t, got.LastCheck.IsZero())
}

func TestHealthChecker_CheckREST_NotInitialized(t *testing.T) {
	srv := withTempServer(t)
	hc := srv.GetHealthChecker()
	require.NotNil(t, hc)

	got := hc.CheckREST()
	assert.Equal(t, HealthStatusUnhealthy, got.Status)
	assert.Equal(t, "rest", got.Name)
	assert.Contains(t, got.Message, "not initialized")
}

func TestHealthChecker_GetOverallHealth_AllComponentsUnhealthy(t *testing.T) {
	srv := withTempServer(t)
	hc := srv.GetHealthChecker()
	overall := hc.GetOverallHealth()
	require.NotNil(t, overall)
	// With both gRPC and REST uninitialized, the overall status is unhealthy.
	assert.Equal(t, HealthStatusUnhealthy, overall.Status)
	assert.NotEmpty(t, overall.Components)
	// Each named subsystem should appear in the components map.
	assert.Contains(t, overall.Components, "grpc")
	assert.Contains(t, overall.Components, "rest")
}

func TestHealthChecker_IsHealthy_FalseWhenSubsystemMissing(t *testing.T) {
	srv := withTempServer(t)
	assert.False(t, srv.GetHealthChecker().IsHealthy())
}

func TestHealthChecker_IsReady_FalseWhenSubsystemMissing(t *testing.T) {
	srv := withTempServer(t)
	assert.False(t, srv.GetHealthChecker().IsReady())
}

func TestServer_GetHealthChecker_ReturnsAttachedChecker(t *testing.T) {
	srv := withTempServer(t)
	got := srv.GetHealthChecker()
	require.NotNil(t, got)
	// Calling twice must yield the same instance (no per-call allocation).
	assert.Same(t, got, srv.GetHealthChecker())
}
