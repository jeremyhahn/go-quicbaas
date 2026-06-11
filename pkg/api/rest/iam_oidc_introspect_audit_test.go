//go:build rest

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

package rest

import (
	"context"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// oidcAuditErrService returns a configurable (logger, err) from GetAuditLogger.
type oidcAuditErrService struct {
	*testutil.MockQuicBaaS
	logger iam.AuditLogger
	err    error
}

func (s *oidcAuditErrService) GetAuditLogger(_ context.Context) (iam.AuditLogger, error) {
	return s.logger, s.err
}

func newOIDCAuditHandler(t *testing.T, svc service.QuicBaaS) *IAMHandler {
	t.Helper()
	log, lerr := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, lerr)
	cfg := DefaultConfig()
	cfg.Address = ":0"
	srv, serr := NewServer(svc, cfg, log)
	require.NoError(t, serr)
	return &IAMHandler{server: srv}
}

// TestIAMHandler_auditLogger_NilPaths covers the three nil-returning branches of
// auditLogger: a nil server, a service without the GetAuditLogger capability,
// and a service whose GetAuditLogger fails.
func TestIAMHandler_auditLogger_NilPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("nil server", func(t *testing.T) {
		h := &IAMHandler{server: nil}
		assert.Nil(t, h.auditLogger(ctx))
	})

	t.Run("service without capability", func(t *testing.T) {
		// MockQuicBaaS does not implement GetAuditLogger.
		h := newOIDCAuditHandler(t, testutil.NewMockQuicBaaS())
		assert.Nil(t, h.auditLogger(ctx))
	})

	t.Run("getter returns error", func(t *testing.T) {
		svc := &oidcAuditErrService{
			MockQuicBaaS: testutil.NewMockQuicBaaS(),
			err:          stderrors.New("audit backend down"),
		}
		h := newOIDCAuditHandler(t, svc)
		assert.Nil(t, h.auditLogger(ctx))
	})

	t.Run("getter returns logger", func(t *testing.T) {
		capture := &captureAuditLogger{}
		svc := &oidcAuditErrService{MockQuicBaaS: testutil.NewMockQuicBaaS(), logger: capture}
		h := newOIDCAuditHandler(t, svc)
		assert.Same(t, capture, h.auditLogger(ctx))
	})
}

// TestIAMHandler_auditAuthentication_Branches exercises the no-logger
// short-circuit and the LogDualIdentity-error warn branch (triggered by an
// empty tenant id, which LogDualIdentity rejects) plus the happy path.
func TestIAMHandler_auditAuthentication_Branches(t *testing.T) {
	ctx := context.Background()

	t.Run("no logger short-circuits", func(t *testing.T) {
		h := newOIDCAuditHandler(t, testutil.NewMockQuicBaaS())
		assert.NotPanics(t, func() {
			h.auditAuthentication(ctx, iam.DualIdentity{TenantID: "t1"}, "oauth2.token", iam.AuditOutcomeSuccess, nil)
		})
	})

	t.Run("dual-identity log error is warned, not fatal", func(t *testing.T) {
		capture := &captureAuditLogger{}
		svc := &oidcAuditErrService{MockQuicBaaS: testutil.NewMockQuicBaaS(), logger: capture}
		h := newOIDCAuditHandler(t, svc)
		// Empty TenantID with a bare context forces LogDualIdentity to error.
		assert.NotPanics(t, func() {
			h.auditAuthentication(ctx, iam.DualIdentity{}, "oauth2.token", iam.AuditOutcomeSuccess, nil)
		})
		assert.Empty(t, capture.events, "no event recorded when the tenant id is missing")
	})

	t.Run("happy path records event", func(t *testing.T) {
		capture := &captureAuditLogger{}
		svc := &oidcAuditErrService{MockQuicBaaS: testutil.NewMockQuicBaaS(), logger: capture}
		h := newOIDCAuditHandler(t, svc)
		h.auditAuthentication(ctx, iam.DualIdentity{TenantID: "t1", Subject: "u1"},
			"oauth2.token", iam.AuditOutcomeSuccess, map[string]interface{}{"k": "v"})
		require.Len(t, capture.events, 1)
		assert.Equal(t, "oauth2.token", capture.events[0].Action)
	})
}

// TestIAMHandler_auditIntrospection_Branches covers the no-logger short-circuit
// and the LogDualIdentity-error warn branch of auditIntrospection plus the
// happy path that records a dual-identity authorization event.
func TestIAMHandler_auditIntrospection_Branches(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/introspect", nil)
	req.RemoteAddr = "192.0.2.10:5000"
	req.Header.Set("User-Agent", "introspect-agent")
	ctx := context.Background()

	t.Run("no logger short-circuits", func(t *testing.T) {
		h := newOIDCAuditHandler(t, testutil.NewMockQuicBaaS())
		assert.NotPanics(t, func() {
			h.auditIntrospection(ctx, req, nil, "caller", &iam.IntrospectionResponse{TenantID: "t1"})
		})
	})

	t.Run("dual-identity log error is warned, not fatal", func(t *testing.T) {
		capture := &captureAuditLogger{}
		svc := &oidcAuditErrService{MockQuicBaaS: testutil.NewMockQuicBaaS(), logger: capture}
		h := newOIDCAuditHandler(t, svc)
		// Missing TenantID forces LogDualIdentity to error.
		assert.NotPanics(t, func() {
			h.auditIntrospection(ctx, req, nil, "caller", &iam.IntrospectionResponse{})
		})
		assert.Empty(t, capture.events)
	})

	t.Run("happy path records event with IP and UA", func(t *testing.T) {
		capture := &captureAuditLogger{}
		svc := &oidcAuditErrService{MockQuicBaaS: testutil.NewMockQuicBaaS(), logger: capture}
		h := newOIDCAuditHandler(t, svc)
		h.auditIntrospection(ctx, req, nil, "caller-client", &iam.IntrospectionResponse{
			TenantID: "t1",
			Subject:  "u1",
			ClientID: "client-1",
		})
		require.Len(t, capture.events, 1)
		ev := capture.events[0]
		assert.Equal(t, "oauth2.introspect", ev.Action)
		assert.Equal(t, "192.0.2.10", ev.IPAddress)
		assert.Equal(t, "introspect-agent", ev.UserAgent)
	})
}
