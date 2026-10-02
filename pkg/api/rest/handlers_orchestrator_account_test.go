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
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// spawnWithBody posts a raw spawn body and returns the SpawnSessionRequest the
// handler built. Raw JSON (not a marshalled struct) so a field the handler
// fails to read is genuinely absent from the Go-side request, exactly as a
// real client would send it.
func spawnWithBody(t *testing.T, rawBody string) *orchestration.SpawnSessionRequest {
	t.Helper()

	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	var captured *orchestration.SpawnSessionRequest
	agentSvc.spawnSessionFunc = func(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
		captured = req
		return &orchestration.SpawnSessionResponse{SessionID: "s1", Backend: req.Backend}, nil
	}

	req := systemRequest(http.MethodPost, "/api/v1/orchestrator/sessions", bytes.NewReader([]byte(rawBody)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.HandleOrchestratorSpawn(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, "spawn failed: %s", rec.Body.String())
	require.NotNil(t, captured)
	return captured
}

// TestOrchestratorSpawn_PinsAccountID asserts an explicit account_id reaches
// the service layer. Dropping it would silently spawn against the daemon
// host's shared identity instead of the pinned pooled account.
func TestOrchestratorSpawn_PinsAccountID(t *testing.T) {
	captured := spawnWithBody(t, `{"backend":"claude","account_id":"acct-pinned"}`)
	assert.Equal(t, "acct-pinned", captured.AccountID, "REST spawn dropped account_id")
	assert.False(t, captured.AccountAuto, "REST spawn invented account_auto")
}

// TestOrchestratorSpawn_CarriesAccountAuto asserts account_auto and the named
// strategy reach the service layer. Without account_auto the dynamic
// least-usage strategy is unreachable from outside the process.
func TestOrchestratorSpawn_CarriesAccountAuto(t *testing.T) {
	captured := spawnWithBody(t, `{"backend":"claude","account_auto":true,"account_strategy":"least_usage"}`)
	assert.True(t, captured.AccountAuto, "REST spawn dropped account_auto")
	assert.Equal(t, "least_usage", captured.AccountStrategy, "REST spawn dropped account_strategy")
	assert.Empty(t, captured.AccountID, "REST spawn invented an account_id")
}

// TestOrchestratorSpawn_NoAccountFieldsStaysUnset is the negative case: a
// spawn that names no account must leave every account field zero so the
// pre-account behavior is preserved exactly.
func TestOrchestratorSpawn_NoAccountFieldsStaysUnset(t *testing.T) {
	captured := spawnWithBody(t, `{"backend":"claude"}`)
	assert.Empty(t, captured.AccountID)
	assert.False(t, captured.AccountAuto)
	assert.Empty(t, captured.AccountStrategy)
}
