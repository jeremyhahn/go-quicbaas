//go:build rest && iam

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
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// webauthnAuditErrService returns a configurable (logger, err) pair from
// GetAuditLogger so the WebAuthn audit helpers' fail-soft branches can be
// exercised: a nil logger or a lookup error must short-circuit silently rather
// than panic on a nil-logger LogAsync.
type webauthnAuditErrService struct {
	*testutil.MockQuicBaaS
	logger iam.AuditLogger
	err    error
}

func (s *webauthnAuditErrService) GetAuditLogger(_ context.Context) (iam.AuditLogger, error) {
	return s.logger, s.err
}

func newWebAuthnAuditErrServer(t *testing.T, lg iam.AuditLogger, err error) *Server {
	t.Helper()
	log, lerr := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, lerr)
	mock := &webauthnAuditErrService{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		logger:       lg,
		err:          err,
	}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	srv, serr := NewServer(mock, cfg, log)
	require.NoError(t, serr)
	return srv
}

// newWebAuthnPlainServer builds a Server whose service does NOT implement the
// GetAuditLogger capability, exercising the type-assertion-fails branch of the
// audit helpers.
func newWebAuthnPlainServer(t *testing.T) *Server {
	t.Helper()
	log, lerr := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, lerr)
	cfg := DefaultConfig()
	cfg.Address = ":0"
	srv, serr := NewServer(testutil.NewMockQuicBaaS(), cfg, log)
	require.NoError(t, serr)
	return srv
}

// TestWebAuthnAudit_NoAuditLoggerCapability covers the "type assertion fails"
// branch of all three WebAuthn audit helpers: when the service does not expose
// GetAuditLogger, every helper must return without panicking.
func TestWebAuthnAudit_NoAuditLoggerCapability(t *testing.T) {
	h := &IAMHandler{server: newWebAuthnPlainServer(t)}
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	ctx := context.Background()

	assert.NotPanics(t, func() {
		h.logWebAuthnBeginLoginAudit(ctx, "t", "e@x", "rid", req)
		h.logWebAuthnLoginAudit(ctx, "t", "u", "e@x", "aa", "rid", req)
		h.logWebAuthnLoginFailureAudit(ctx, "t", "e@x", "code", "msg", "rid", req)
	})
}

// TestWebAuthnAudit_LoggerLookupError covers the GetAuditLogger-returns-error
// branch: the helpers must swallow the error and emit nothing.
func TestWebAuthnAudit_LoggerLookupError(t *testing.T) {
	h := &IAMHandler{server: newWebAuthnAuditErrServer(t, nil, stderrors.New("audit backend down"))}
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	ctx := context.Background()

	assert.NotPanics(t, func() {
		h.logWebAuthnBeginLoginAudit(ctx, "t", "e@x", "rid", req)
		h.logWebAuthnLoginAudit(ctx, "t", "u", "e@x", "aa", "rid", req)
		h.logWebAuthnLoginFailureAudit(ctx, "t", "e@x", "code", "msg", "rid", req)
	})
}

// TestWebAuthnAudit_NilLogger covers the logger == nil branch: GetAuditLogger
// succeeds but yields no logger, so the helpers must not attempt LogAsync.
func TestWebAuthnAudit_NilLogger(t *testing.T) {
	h := &IAMHandler{server: newWebAuthnAuditErrServer(t, nil, nil)}
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	ctx := context.Background()

	assert.NotPanics(t, func() {
		h.logWebAuthnBeginLoginAudit(ctx, "t", "e@x", "rid", req)
		h.logWebAuthnLoginAudit(ctx, "t", "u", "e@x", "aa", "rid", req)
		h.logWebAuthnLoginFailureAudit(ctx, "t", "e@x", "code", "msg", "rid", req)
	})
}

// TestWebAuthnAudit_CapturesRemoteIPAndUserAgent asserts the recently added
// IP/User-Agent enrichment: the audit event must carry the request's RemoteAddr
// and User-Agent header so security telemetry can attribute login attempts.
func TestWebAuthnAudit_CapturesRemoteIPAndUserAgent(t *testing.T) {
	capture := &captureAuditLogger{}
	h := &IAMHandler{server: newWebAuthnAuditErrServer(t, capture, nil)}

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.RemoteAddr = "203.0.113.7:55555"
	req.Header.Set("User-Agent", "Mozilla/5.0 (test-agent)")

	h.logWebAuthnLoginAudit(context.Background(), "t", "u", "e@x", "aa", "rid", req)

	require.Len(t, capture.events, 1)
	ev := capture.events[0]
	assert.Equal(t, "203.0.113.7:55555", ev.IPAddress)
	assert.Equal(t, "Mozilla/5.0 (test-agent)", ev.UserAgent)
}
