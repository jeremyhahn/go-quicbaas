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

package quic

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	sdktransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// =============================================================================
// RotateSessionAccount on the QUIC wire
// =============================================================================

// TestOrchestratorRotateSubOpIsAllocated pins the sub-op byte the SDK prepends.
// The server-side parity assertion lives in pkg/api/quic, which can import both
// tables; this half guards the SDK against a local edit.
func TestOrchestratorRotateSubOpIsAllocated(t *testing.T) {
	assert.Equal(t, byte(0x20), SubOpOrchestratorRotateAccount,
		"the rotate sub-op byte is part of the v1 wire format and must not move")

	subOp, ok := OrchestratorMethodToSubOp("RotateSessionAccount")
	require.True(t, ok, "an unmapped method makes the QUIC transport reject every rotation")
	assert.Equal(t, SubOpOrchestratorRotateAccount, subOp)
}

// TestOrchestratorRotateSubOpIsDisjoint keeps 0x20 out of every other
// vocabulary that rides the OpOrchestrator (0x8F) dispatch byte.
func TestOrchestratorRotateSubOpIsDisjoint(t *testing.T) {
	subOp := SubOpOrchestratorRotateAccount

	assert.NotContains(t, []byte{
		SubOpOrchestratorSpawn, SubOpOrchestratorPrompt, SubOpOrchestratorSessionStatus,
		SubOpOrchestratorCloseSession, SubOpOrchestratorListSessions,
		SubOpOrchestratorCreateTask, SubOpOrchestratorGetTask, SubOpOrchestratorListTasks,
		SubOpOrchestratorClaimTask, SubOpOrchestratorCompleteTask,
		SubOpOrchestratorListBackends, SubOpOrchestratorHealth, SubOpOrchestratorInference,
	}, subOp)

	assert.Less(t, subOp, SubOpAccountCreate,
		"0x20 must stay below the pooled-account range so the two vocabularies cannot overlap")

	// Every method in the table must map to a distinct sub-op, or two verbs
	// would land on one server handler.
	seen := map[byte]string{}
	for method, code := range orchestratorMethodToSubOp {
		if other, dup := seen[code]; dup {
			t.Fatalf("sub-op 0x%02X is claimed by both %q and %q", code, other, method)
		}
		seen[code] = method
	}
}

// TestConvertRotateSessionAccountResponse decodes the server's wire result,
// which is orchestration.RotateSessionAccountResult marshalled with the same
// JSON tags the SDK response type declares.
func TestConvertRotateSessionAccountResponse(t *testing.T) {
	raw := []byte(`{"session_id":"s1","previous_account_id":"acct-a","account_id":"acct-b","strategy":"least_usage","mode":"resume"}`)

	got, err := decodeOrchestratorResponse("RotateSessionAccount", raw)
	require.NoError(t, err)

	resp, ok := got.(*orchestrator.RotateSessionAccountResponse)
	require.Truef(t, ok, "decoded %T, want *orchestrator.RotateSessionAccountResponse", got)
	assert.Equal(t, "s1", resp.SessionID)
	assert.Equal(t, "acct-a", resp.PreviousAccountID)
	assert.Equal(t, "acct-b", resp.AccountID)
	assert.Equal(t, "least_usage", resp.Strategy)
	assert.Equal(t, "resume", resp.Mode)
}

func TestConvertRotateSessionAccountResponse_InvalidJSON(t *testing.T) {
	_, err := decodeOrchestratorResponse("RotateSessionAccount", []byte(`{"session_id":`))
	require.Error(t, err)
}

// TestRotateSessionAccountWirePayloadCarriesTheTenant proves the rotation body
// goes through the same tenant injection every other orchestrator verb uses;
// the server's dispatcher peeks tenant_id out of the payload to scope the call.
func TestRotateSessionAccountWirePayloadCarriesTheTenant(t *testing.T) {
	payload, err := EncodePayload(&orchestrator.RotateSessionAccountRequest{SessionID: "s1"})
	require.NoError(t, err)

	ctx := sdktransport.ContextWithTenant(context.Background(), "tenant-a")
	injected := injectTenantID(ctx, payload)
	assert.Contains(t, string(injected), `"tenant_id":"tenant-a"`)
	assert.Contains(t, string(injected), `"session_id":"s1"`)
}

// TestSpawnSessionWirePayloadCarriesAccountAllow is the QUIC half of gap 2: the
// server decodes the payload straight into orchestration.SpawnSessionRequest,
// so the field only arrives if the SDK type declares the json tag.
func TestSpawnSessionWirePayloadCarriesAccountAllow(t *testing.T) {
	payload, err := EncodePayload(&orchestrator.SpawnSessionRequest{
		Backend:      "claude",
		AccountID:    "acct-a",
		AccountAllow: []string{"acct-a", "acct-b"},
	})
	require.NoError(t, err)
	assert.Contains(t, string(payload), `"account_allow":["acct-a","acct-b"]`)
}
