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
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	sdkmcp "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// barrier_enable_encryption_roundtrip_test.go drives the SDK MCP client's
// BarrierEnableEncryption against this server's own barrier tool handlers,
// so the client is held to the arguments and tool names the server reads.

const (
	systemEnableEncryptionTool = "qrdb_barrier_enable_encryption"
	tenantEnableEncryptionTool = "qrdb_tenant_barrier_enable_encryption"
)

// enableEncryptionRoundTrip serves both enable-encryption tools over MCP
// and returns a client transport plus a function that lists the tools the
// server has received so far.
func enableEncryptionRoundTrip(t *testing.T, fake *fakeBarrierService) (*sdkmcp.Transport, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var reached []string
	record := func(tool string) {
		mu.Lock()
		defer mu.Unlock()
		reached = append(reached, tool)
	}
	tr := registryMCPTransport(t, newBarrierTestRegistry(t, fake), record,
		systemEnableEncryptionTool, tenantEnableEncryptionTool)
	return tr, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), reached...)
	}
}

func TestBarrierEnableEncryptionRoundTrip_SystemRequestReachesSystemTool(t *testing.T) {
	fake := &fakeBarrierService{}
	tr, reached := enableEncryptionRoundTrip(t, fake)

	resp, err := tr.BarrierEnableEncryption(context.Background(), &qrdbtransport.BarrierEnableEncryptionRequest{
		Strategy:        "passphrase",
		Mechanism:       "copy",
		Availability:    string(qrdbsdk.AvailabilityOnline),
		Passphrase:      []byte("correct horse battery staple"),
		ShamirThreshold: 2,
		ShamirTotal:     3,
	})
	require.NoError(t, err)

	assert.Equal(t, "encryption enabled", resp.Status)
	assert.Equal(t, []string{systemEnableEncryptionTool}, reached())
	assert.Empty(t, fake.gotEnableEncTenantID, "the system tool must migrate the system barrier")
	assert.Equal(t, qrdbsdk.StrategyID("passphrase"), fake.gotEnableEncOpts.TargetStrategy)
	assert.Equal(t, qrdbsdk.MigrationMechanism("copy"), fake.gotEnableEncOpts.Mechanism)
	assert.Equal(t, qrdbsdk.AvailabilityOnline, fake.gotEnableEncOpts.Availability)
	assert.Equal(t, 2, fake.gotEnableEncOpts.ShamirThreshold)
	assert.Equal(t, 3, fake.gotEnableEncOpts.ShamirTotal)
	assert.Equal(t, []byte("correct horse battery staple"), fake.gotEnableEncSecret)
}

func TestBarrierEnableEncryptionRoundTrip_TenantRequestReachesTenantToolOnly(t *testing.T) {
	fake := &fakeBarrierService{}
	tr, reached := enableEncryptionRoundTrip(t, fake)

	resp, err := tr.BarrierEnableEncryption(context.Background(), &qrdbtransport.BarrierEnableEncryptionRequest{
		TenantID:        "t1",
		Strategy:        "shamir",
		ShamirThreshold: 2,
		ShamirTotal:     3,
	})
	require.NoError(t, err)

	assert.Equal(t, "encryption enabled", resp.Status)
	assert.Equal(t, []string{tenantEnableEncryptionTool}, reached(),
		"a tenant request must never reach the system tool")
	assert.Equal(t, "t1", fake.gotEnableEncTenantID)
	assert.Equal(t, qrdbsdk.StrategyID("shamir"), fake.gotEnableEncOpts.TargetStrategy)
	assert.Equal(t, qrdbsdk.MechanismSwap, fake.gotEnableEncOpts.Mechanism)
	assert.Equal(t, qrdbsdk.AvailabilityOnline, fake.gotEnableEncOpts.Availability)
	assert.Equal(t, 2, fake.gotEnableEncOpts.ShamirThreshold)
	assert.Equal(t, 3, fake.gotEnableEncOpts.ShamirTotal)
	assert.Nil(t, fake.gotEnableEncSecret)
}

func TestBarrierEnableEncryptionRoundTrip_NilRequestIsInvalidInput(t *testing.T) {
	tr, reached := enableEncryptionRoundTrip(t, &fakeBarrierService{})

	_, err := tr.BarrierEnableEncryption(context.Background(), nil)

	assert.True(t, sdkerrors.IsInvalidInput(err), "got %v", err)
	assert.Empty(t, reached())
}
