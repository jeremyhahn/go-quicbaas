//go:build mcp

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

package mcp

import (
	"context"
	stderrors "errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// errTenantBarrierMCPBoom drives the generic (non-sentinel) error branch.
var errTenantBarrierMCPBoom = stderrors.New("tenant barrier mcp boom")

// fakeTenantBarrierMCPService is a hand-written test double for
// service.TenantBarrierServicer scoped to the MCP tool tests.
type fakeTenantBarrierMCPService struct {
	initFn        func(ctx context.Context, tenantID, passphrase string, threshold, shares int, strategy string) (*qrdbtransport.TenantBarrierInitResponse, error)
	unsealFn      func(ctx context.Context, tenantID string, key []byte) error
	unsealShareFn func(ctx context.Context, tenantID, share string) (*qrdbtransport.TenantBarrierUnsealProgress, error)
	sealFn        func(ctx context.Context, tenantID string) error
	statusFn      func(ctx context.Context, tenantID string) (*qrdbtransport.TenantBarrierStatusResponse, error)
}

func (f *fakeTenantBarrierMCPService) TenantBarrierInit(ctx context.Context, tenantID, passphrase string, threshold, shares int, strategy string) (*qrdbtransport.TenantBarrierInitResponse, error) {
	if f.initFn != nil {
		return f.initFn(ctx, tenantID, passphrase, threshold, shares, strategy)
	}
	return &qrdbtransport.TenantBarrierInitResponse{TenantID: tenantID}, nil
}

func (f *fakeTenantBarrierMCPService) TenantBarrierUnseal(ctx context.Context, tenantID string, key []byte) error {
	if f.unsealFn != nil {
		return f.unsealFn(ctx, tenantID, key)
	}
	return nil
}

func (f *fakeTenantBarrierMCPService) TenantBarrierUnsealShare(ctx context.Context, tenantID, share string) (*qrdbtransport.TenantBarrierUnsealProgress, error) {
	if f.unsealShareFn != nil {
		return f.unsealShareFn(ctx, tenantID, share)
	}
	return &qrdbtransport.TenantBarrierUnsealProgress{TenantID: tenantID}, nil
}

func (f *fakeTenantBarrierMCPService) TenantBarrierSeal(ctx context.Context, tenantID string) error {
	if f.sealFn != nil {
		return f.sealFn(ctx, tenantID)
	}
	return nil
}

func (f *fakeTenantBarrierMCPService) TenantBarrierStatus(ctx context.Context, tenantID string) (*qrdbtransport.TenantBarrierStatusResponse, error) {
	if f.statusFn != nil {
		return f.statusFn(ctx, tenantID)
	}
	return &qrdbtransport.TenantBarrierStatusResponse{TenantID: tenantID}, nil
}

// newTenantBarrierTestRegistry builds a ToolRegistry wired to a fake
// per-tenant barrier facade.
func newTenantBarrierTestRegistry(t *testing.T, fake service.TenantBarrierServicer) *ToolRegistry {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test-tenant-barrier",
	})
	require.NoError(t, err)
	r := NewToolRegistry(&MockQuicBaaS{}, log)
	r.SetTenantBarrierService(fake)
	return r
}

// =============================================================================
// mapTenantBarrierToolError: qrdb canon — tenant.ErrBarrierNotEnabled is
// classified as a typed QBError with ErrPreconditionFailed so the JSON-RPC
// layer emits the PreconditionFailed code. Everything else passes through.
// =============================================================================

func TestMapTenantBarrierToolError_BarrierNotEnabled(t *testing.T) {
	mapped := mapTenantBarrierToolError(qrdbsdk.ErrTenantBarrierNotEnabled)

	var qbErr *qberrors.QBError
	require.True(t, stderrors.As(mapped, &qbErr), "expected a typed QBError, got %T", mapped)
	assert.Equal(t, qberrors.ErrPreconditionFailed, qbErr.Code)
	// The canonical qrdb message must be preserved as the QBError cause.
	assert.Contains(t, mapped.Error(), "barrier not enabled (seal subsystem disabled)")
	// The sentinel identity must survive for errors.Is callers downstream.
	assert.True(t, stderrors.Is(mapped, qrdbsdk.ErrTenantBarrierNotEnabled))
}

func TestMapTenantBarrierToolError_WrappedSentinel(t *testing.T) {
	wrapped := fmt.Errorf("tenant barrier seal: %w", qrdbsdk.ErrTenantBarrierNotEnabled)
	mapped := mapTenantBarrierToolError(wrapped)

	var qbErr *qberrors.QBError
	require.True(t, stderrors.As(mapped, &qbErr), "expected a typed QBError, got %T", mapped)
	assert.Equal(t, qberrors.ErrPreconditionFailed, qbErr.Code)
	assert.Contains(t, mapped.Error(), wrapped.Error())
}

func TestMapTenantBarrierToolError_GenericPassthrough(t *testing.T) {
	// Scoped classification: an unrelated error must pass through unchanged.
	mapped := mapTenantBarrierToolError(errTenantBarrierMCPBoom)
	assert.Same(t, errTenantBarrierMCPBoom, mapped) //nolint:testifylint // identity is the contract
}

// =============================================================================
// Tool-level contract: every qrdb_tenant_barrier_* tool surfaces the typed
// precondition-failed QBError when the facade fails with the sentinel.
// =============================================================================

func TestTenantBarrierTools_BarrierNotEnabled_PreconditionFailed(t *testing.T) {
	sentinel := qrdbsdk.ErrTenantBarrierNotEnabled
	fake := &fakeTenantBarrierMCPService{
		initFn: func(context.Context, string, string, int, int, string) (*qrdbtransport.TenantBarrierInitResponse, error) {
			return nil, sentinel
		},
		unsealFn: func(context.Context, string, []byte) error { return sentinel },
		unsealShareFn: func(context.Context, string, string) (*qrdbtransport.TenantBarrierUnsealProgress, error) {
			return nil, sentinel
		},
		sealFn: func(context.Context, string) error { return sentinel },
		statusFn: func(context.Context, string) (*qrdbtransport.TenantBarrierStatusResponse, error) {
			return nil, sentinel
		},
	}
	r := newTenantBarrierTestRegistry(t, fake)
	ctx := context.Background()

	cases := []struct {
		tool string
		args map[string]interface{}
	}{
		{"qrdb_tenant_barrier_init", map[string]interface{}{"tenant_id": "t1", "strategy": "passphrase", "passphrase": "p"}},
		{"qrdb_tenant_barrier_unseal", map[string]interface{}{"tenant_id": "t1", "key": "a2V5"}},
		{"qrdb_tenant_barrier_unseal_share", map[string]interface{}{"tenant_id": "t1", "share": "s"}},
		{"qrdb_tenant_barrier_seal", map[string]interface{}{"tenant_id": "t1"}},
		{"qrdb_tenant_barrier_status", map[string]interface{}{"tenant_id": "t1"}},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			_, err := r.ExecuteTool(ctx, c.tool, c.args)
			require.Error(t, err)
			var qbErr *qberrors.QBError
			require.True(t, stderrors.As(err, &qbErr), "expected a typed QBError, got %T: %v", err, err)
			assert.Equal(t, qberrors.ErrPreconditionFailed, qbErr.Code)
			assert.Contains(t, err.Error(), "barrier not enabled (seal subsystem disabled)")
		})
	}
}

// TestTenantBarrierTools_GenericErrorPassthrough pins the negative: a generic
// facade failure must NOT be reclassified as precondition-failed.
func TestTenantBarrierTools_GenericErrorPassthrough(t *testing.T) {
	fake := &fakeTenantBarrierMCPService{
		sealFn: func(context.Context, string) error { return errTenantBarrierMCPBoom },
	}
	r := newTenantBarrierTestRegistry(t, fake)
	_, err := r.ExecuteTool(context.Background(), "qrdb_tenant_barrier_seal",
		map[string]interface{}{"tenant_id": "t1"})
	require.Error(t, err)
	var qbErr *qberrors.QBError
	assert.False(t, stderrors.As(err, &qbErr), "generic error must not be reclassified: %v", err)
}
