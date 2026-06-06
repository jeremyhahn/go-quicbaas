//go:build quic

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

package quic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/jeremyhahn/go-qrdb/pkg/api/grpc/proto/qrdbv1"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// admin.Health — the qrdb client connect-time liveness probe.
//
// This is the regression test for the original bug: the qrdb SDK sub-client
// the quicbaas SDK builds runs admin.Health at connect time and tears the
// connection down with "health check failed" if it returns Unimplemented.
// admin.Health must therefore always be present in the dispatch table and
// must always succeed regardless of which facades are wired.
// =============================================================================

func TestQRDBAdminHealth_DispatchTableEntry(t *testing.T) {
	server := newQRDBCredentialServer(t, nil)
	handlers := server.buildQRDBHandlers()
	_, ok := handlers["admin.Health"]
	require.True(t, ok, "admin.Health must be present so the qrdb client connect-time probe succeeds")
}

func TestQRDBAdminHealth_AlwaysServingEvenWithNoFacades(t *testing.T) {
	// No credential, barrier, or ceremony facade wired — admin.Health must
	// still succeed so the qrdb client does not close the connection.
	server := newQRDBCredentialServer(t, nil)

	handler := server.buildQRDBHandlers()["admin.Health"]
	out, err := handler(context.Background(), nil)
	require.NoError(t, err)

	var resp pb.HealthResponse
	require.NoError(t, proto.Unmarshal(out, &resp))
	// The qrdb client's Healthy() check requires a non-empty Status.
	assert.NotEmpty(t, resp.GetStatus())
	assert.Equal(t, qrdbHealthStatusServing, resp.GetStatus())
	assert.True(t, resp.GetLeader())
}

// =============================================================================
// barrier.Status dispatch through buildQRDBHandlers.
// =============================================================================

func TestQRDBBarrierStatus_DispatchHappyPath(t *testing.T) {
	server := newQRDBCredentialServer(t, nil)
	initAt := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	server.SetBarrierService(&mockBarrierServicer{
		statusInfo: &service.BarrierStatusInfo{
			Sealed:         false,
			Strategy:       "shamir",
			HardwareBacked: true,
			InitializedAt:  initAt,
		},
	})

	handler := server.buildQRDBHandlers()["barrier.Status"]
	require.NotNil(t, handler)

	out, err := handler(context.Background(), nil)
	require.NoError(t, err)

	var resp pb.BarrierStatusResponse
	require.NoError(t, proto.Unmarshal(out, &resp))
	assert.False(t, resp.GetSealed())
	assert.Equal(t, "shamir", resp.GetStrategy())
	assert.True(t, resp.GetHardwareBacked())
	// Timestamp is encoded RFC3339Nano; the qrdb client parses RFC3339.
	parsed, perr := time.Parse(time.RFC3339, resp.GetInitializedAt())
	require.NoError(t, perr)
	assert.True(t, initAt.Equal(parsed))
}

func TestQRDBBarrierStatus_NotConfigured(t *testing.T) {
	// Barrier facade not wired — barrier.Status must return the typed
	// not-configured error, NOT panic and NOT block admin.Health.
	server := newQRDBCredentialServer(t, nil)

	handler := server.buildQRDBHandlers()["barrier.Status"]
	require.NotNil(t, handler)

	_, err := handler(context.Background(), nil)
	require.Error(t, err)
	var notCfg *QRDBBarrierNotConfiguredError
	assert.ErrorAs(t, err, &notCfg)
}

func TestQRDBBarrierStatus_FacadeError(t *testing.T) {
	server := newQRDBCredentialServer(t, nil)
	server.SetBarrierService(&mockBarrierServicer{err: errors.New("barrier sealed-store unavailable")})

	handler := server.buildQRDBHandlers()["barrier.Status"]
	_, err := handler(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "barrier sealed-store unavailable")
}

// =============================================================================
// ceremony.Status dispatch — verifies the ceremony surface is wired and
// gated independently of the barrier and credential facades.
// =============================================================================

func TestQRDBCeremonyStatus_NotConfigured(t *testing.T) {
	server := newQRDBCredentialServer(t, nil)

	handler := server.buildQRDBHandlers()["ceremony.Status"]
	require.NotNil(t, handler)

	_, err := handler(context.Background(), nil)
	require.Error(t, err)
	var notCfg *QRDBCeremonyNotConfiguredError
	assert.ErrorAs(t, err, &notCfg)
}

// =============================================================================
// Typed-error strings for the new not-configured errors.
// =============================================================================

func TestQRDBLifecycleTypedErrors(t *testing.T) {
	assert.Equal(t, "qrdb barrier service not configured", (&QRDBBarrierNotConfiguredError{}).Error())
	assert.Equal(t, "qrdb ceremony service not configured", (&QRDBCeremonyNotConfiguredError{}).Error())
}
