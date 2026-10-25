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
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// errTenantBarrierMCPBoom drives the generic (non-sentinel) error branch.
var errTenantBarrierMCPBoom = stderrors.New("tenant barrier mcp boom")

// fakeTenantBarrierMCPService is a hand-written test double for
// service.TenantBarrierServicer scoped to the MCP tool tests.
type fakeTenantBarrierMCPService struct {
	// The officer, claim, quorum and rekey verbs are pass-through; their
	// MCP tools are exercised in tools_barrier_holders_test.go.
	service.TenantBarrierServicer

	initFn func(ctx context.Context, tenantID string, passphrase []byte, threshold, shares int, strategy string) (*qrdbtransport.TenantBarrierInitResponse, error)
	// initReq records the request the last TenantBarrierInit call received.
	initReq  *qrdbtransport.TenantBarrierInitRequest
	unsealFn func(ctx context.Context, tenantID string, key []byte) error
	sealFn   func(ctx context.Context, tenantID string) error
	statusFn func(ctx context.Context, tenantID string) (*qrdbtransport.TenantBarrierStatusResponse, error)
}

func (f *fakeTenantBarrierMCPService) TenantBarrierInit(ctx context.Context, req *qrdbtransport.TenantBarrierInitRequest) (*qrdbtransport.TenantBarrierInitResponse, error) {
	f.initReq = req
	if f.initFn != nil {
		return f.initFn(ctx, req.TenantID, req.Passphrase, req.Threshold, req.Shares, req.Strategy)
	}
	return &qrdbtransport.TenantBarrierInitResponse{TenantID: req.TenantID}, nil
}

func (f *fakeTenantBarrierMCPService) TenantBarrierUnseal(ctx context.Context, tenantID string, key []byte) error {
	if f.unsealFn != nil {
		return f.unsealFn(ctx, tenantID, key)
	}
	return nil
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
	assert.Same(t, errTenantBarrierMCPBoom, mapped)
}

// =============================================================================
// Tool-level contract: every qrdb_tenant_barrier_* tool surfaces the typed
// precondition-failed QBError when the facade fails with the sentinel.
// =============================================================================

func TestTenantBarrierTools_BarrierNotEnabled_PreconditionFailed(t *testing.T) {
	sentinel := qrdbsdk.ErrTenantBarrierNotEnabled
	fake := &fakeTenantBarrierMCPService{
		initFn: func(context.Context, string, []byte, int, int, string) (*qrdbtransport.TenantBarrierInitResponse, error) {
			return nil, sentinel
		},
		unsealFn: func(context.Context, string, []byte) error { return sentinel },
		sealFn:   func(context.Context, string) error { return sentinel },
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

// The inline Shamir roster and its policy reach the service in go-qrdb's
// request type, and the holder certificates go-qrdb issues come back
// (Codex 6709 P1).
func TestTenantBarrierInitTool_ForwardsOfficersAndPolicy(t *testing.T) {
	fake := &fakeTenantBarrierMCPService{
		initFn: func(_ context.Context, tenantID string, _ []byte, threshold, shares int, _ string) (*qrdbtransport.TenantBarrierInitResponse, error) {
			return &qrdbtransport.TenantBarrierInitResponse{
				TenantID:    tenantID,
				Strategy:    "shamir",
				Threshold:   threshold,
				TotalShares: shares,
				Holders:     []qrdbtransport.TenantBarrierHolderCert{{Username: "officer-1", Role: "unseal", CertPEM: "cert-1"}},
			}, nil
		},
	}
	r := newTenantBarrierTestRegistry(t, fake)

	// Arguments arrive JSON-decoded: the roster is a list of objects.
	args := map[string]interface{}{
		"tenant_id": "t1",
		"strategy":  "shamir",
		"threshold": float64(1),
		"shares":    float64(1),
		"policy":    "any-enrolled",
		"officers": []interface{}{map[string]interface{}{
			"username":   "officer-1",
			"role":       "unseal",
			"csr_pem":    "csr-1",
			"factors":    []interface{}{map[string]interface{}{"name": "mtls"}},
			"designated": "mtls",
		}},
	}
	result, err := r.ExecuteTool(context.Background(), "qrdb_tenant_barrier_init", args)
	require.NoError(t, err)

	require.NotNil(t, fake.initReq, "service received no init request")
	assert.Equal(t, "any-enrolled", fake.initReq.Policy)
	assert.Equal(t, []qrdbtransport.OfficerRegisterRequest{{
		Username: "officer-1", Role: "unseal", CSRPEM: "csr-1",
		Factors:    []qrdbtransport.HolderFactorEnrolment{{Name: "mtls"}},
		Designated: "mtls",
	}}, fake.initReq.Officers)

	out, ok := result.(map[string]interface{})
	require.True(t, ok, "unexpected result type %T", result)
	assert.True(t, reflect.DeepEqual(out["holders"], []qrdbtransport.TenantBarrierHolderCert{{Username: "officer-1", Role: "unseal", CertPEM: "cert-1"}}),
		"holder certificates = %v", out["holders"])
}

// An officers argument that is not a roster is refused as invalid input
// before the service is called.
func TestTenantBarrierInitTool_MalformedOfficersRefused(t *testing.T) {
	fake := &fakeTenantBarrierMCPService{}
	r := newTenantBarrierTestRegistry(t, fake)

	_, err := r.ExecuteTool(context.Background(), "qrdb_tenant_barrier_init", map[string]interface{}{
		"tenant_id": "t1",
		"strategy":  "shamir",
		"officers":  "officer-1",
	})
	var argErr *TenantBarrierOfficersArgError
	require.True(t, stderrors.As(err, &argErr), "expected TenantBarrierOfficersArgError, got %T: %v", err, err)
	assert.Nil(t, fake.initReq, "a malformed roster must not reach the service")
}

// go-qrdb's typed refusal of an invalid roster reaches the caller unchanged.
func TestTenantBarrierInitTool_InvalidRosterErrorUnchanged(t *testing.T) {
	refusal := &qrdbtransport.FactorTypeUnsupportedError{Type: "bogus"}
	fake := &fakeTenantBarrierMCPService{
		initFn: func(context.Context, string, []byte, int, int, string) (*qrdbtransport.TenantBarrierInitResponse, error) {
			return nil, refusal
		},
	}
	r := newTenantBarrierTestRegistry(t, fake)

	_, err := r.ExecuteTool(context.Background(), "qrdb_tenant_barrier_init", map[string]interface{}{
		"tenant_id": "t1",
		"strategy":  "shamir",
		"threshold": float64(1),
		"shares":    float64(1),
		"officers":  []interface{}{map[string]interface{}{"username": "officer-1", "factors": []interface{}{map[string]interface{}{"name": "bogus"}}}},
	})
	var got *qrdbtransport.FactorTypeUnsupportedError
	require.True(t, stderrors.As(err, &got), "expected go-qrdb's FactorTypeUnsupportedError, got %T: %v", err, err)
	assert.Same(t, refusal, got)
}

// TestTenantBarrierInitTool_StatementUnsupportedAnswersUnimplemented pins
// Codex 6757 finding 1 on MCP: a raft-isolated tenant's init, which no MCP
// argument can carry a signed init statement for, is answered as
// unimplemented with the refusal's text, never as a server failure.
func TestTenantBarrierInitTool_StatementUnsupportedAnswersUnimplemented(t *testing.T) {
	refusal := &service.TenantBarrierInitStatementUnsupportedError{TenantID: "t1"}
	fake := &fakeTenantBarrierMCPService{
		initFn: func(context.Context, string, []byte, int, int, string) (*qrdbtransport.TenantBarrierInitResponse, error) {
			return nil, refusal
		},
	}
	r := newTenantBarrierTestRegistry(t, fake)

	_, err := r.ExecuteTool(context.Background(), "qrdb_tenant_barrier_init", map[string]interface{}{
		"tenant_id": "t1",
		"strategy":  "none",
	})
	require.Error(t, err)
	result := ToolErrorResult(nil, "qrdb_tenant_barrier_init", err)
	require.True(t, result.IsError)
	assert.Equal(t, mcptransport.ToolError{Code: mcptransport.MCPUnimplemented}, result.StructuredContent)
	assert.Equal(t, refusal.Error(), toolResultText(t, result))
}
