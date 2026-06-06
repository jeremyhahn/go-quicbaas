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
	"math"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMethodToOpcode_KnownAndUnknown exercises both branches of the core
// service/method -> opcode lookup that doRequest depends on.
func TestMethodToOpcode_KnownAndUnknown(t *testing.T) {
	t.Run("known new-format pair", func(t *testing.T) {
		op, ok := MethodToOpcode("quicbaas.KV", "Put")
		require.True(t, ok)
		assert.Equal(t, OpPut, op)
	})

	t.Run("known legacy-format pair", func(t *testing.T) {
		op, ok := MethodToOpcode("QuicBaaS", "Get")
		require.True(t, ok)
		assert.Equal(t, OpGet, op)
	})

	t.Run("unknown pair", func(t *testing.T) {
		_, ok := MethodToOpcode("Nope", "Nope")
		assert.False(t, ok)
	})
}

// TestOpcodeToMethod_KnownAndUnknown covers the reverse lookup table including
// the RAG opcodes registered via the package init().
func TestOpcodeToMethod_KnownAndUnknown(t *testing.T) {
	t.Run("kv opcode", func(t *testing.T) {
		m, ok := OpcodeToMethod(OpPut)
		require.True(t, ok)
		assert.NotEmpty(t, m)
	})

	t.Run("rag opcode registered in init", func(t *testing.T) {
		m, ok := OpcodeToMethod(OpRAGQuery)
		require.True(t, ok)
		assert.Equal(t, "quicbaas.RAG/Query", m)
	})

	t.Run("unknown opcode", func(t *testing.T) {
		_, ok := OpcodeToMethod(0xCF)
		// 0xCF is unassigned in opcodeToMethod.
		assert.False(t, ok)
	})
}

// TestEncodePayload_NilAndError covers the nil short-circuit and the marshal
// error path (an infinity float cannot be JSON encoded).
func TestEncodePayload_NilAndError(t *testing.T) {
	t.Run("nil entity returns nil", func(t *testing.T) {
		out, err := EncodePayload(nil)
		require.NoError(t, err)
		assert.Nil(t, out)
	})

	t.Run("unmarshalable entity errors", func(t *testing.T) {
		_, err := EncodePayload(map[string]interface{}{"bad": math.Inf(1)})
		require.Error(t, err)
		assert.Equal(t, errors.CodeInternal, errors.Code(err))
	})
}

// TestDecodePayload_NilEntityAndEmpty covers the two early-return guards.
func TestDecodePayload_NilEntityAndEmpty(t *testing.T) {
	t.Run("empty data is a no-op", func(t *testing.T) {
		require.NoError(t, DecodePayload(nil, &map[string]interface{}{}))
	})

	t.Run("nil entity with data errors", func(t *testing.T) {
		err := DecodePayload([]byte(`{"a":1}`), nil)
		require.Error(t, err)
		assert.Equal(t, errors.CodeInvalidInput, errors.Code(err))
	})
}

// TestDecodeBarrierResponse covers the empty, fallback and error branches of
// the barrier decoder (all per-method converters are currently the generic
// map path).
func TestDecodeBarrierResponse(t *testing.T) {
	t.Run("empty payload", func(t *testing.T) {
		got, err := decodeBarrierResponse("BarrierStatus", nil)
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("generic map fallback", func(t *testing.T) {
		got, err := decodeBarrierResponse("BarrierStatus", []byte(`{"sealed":true}`))
		require.NoError(t, err)
		m, ok := got.(map[string]interface{})
		require.True(t, ok, "got %T", got)
		assert.Equal(t, true, m["sealed"])
	})

	t.Run("invalid json", func(t *testing.T) {
		_, err := decodeBarrierResponse("BarrierStatus", []byte(`{bad`))
		require.Error(t, err)
	})
}

// TestDecodeCeremonyResponse mirrors the barrier decoder coverage.
func TestDecodeCeremonyResponse(t *testing.T) {
	t.Run("empty payload", func(t *testing.T) {
		got, err := decodeCeremonyResponse("CeremonyStatus", nil)
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("generic map fallback", func(t *testing.T) {
		got, err := decodeCeremonyResponse("CeremonyStatus", []byte(`{"phase":"init"}`))
		require.NoError(t, err)
		m, ok := got.(map[string]interface{})
		require.True(t, ok, "got %T", got)
		assert.Equal(t, "init", m["phase"])
	})

	t.Run("invalid json", func(t *testing.T) {
		_, err := decodeCeremonyResponse("CeremonyStatus", []byte(`{bad`))
		require.Error(t, err)
	})
}

// TestSubOpLookups exercises the Barrier/Ceremony/MFA/Orchestrator sub-op maps
// directly, covering both the hit and the miss branches.
func TestSubOpLookups(t *testing.T) {
	t.Run("barrier known and unknown", func(t *testing.T) {
		op, ok := BarrierMethodToSubOp("BarrierStatus")
		require.True(t, ok)
		assert.Equal(t, SubOpBarrierStatus, op)
		_, ok = BarrierMethodToSubOp("Nope")
		assert.False(t, ok)
	})

	t.Run("ceremony known and unknown", func(t *testing.T) {
		op, ok := CeremonyMethodToSubOp("CeremonyInitialize")
		require.True(t, ok)
		assert.Equal(t, SubOpCeremonyInitialize, op)
		_, ok = CeremonyMethodToSubOp("Nope")
		assert.False(t, ok)
	})

	t.Run("mfa known and unknown", func(t *testing.T) {
		op, ok := MFAMethodToSubOp("GetMFAStatus")
		require.True(t, ok)
		assert.Equal(t, SubOpMFAGetStatus, op)
		_, ok = MFAMethodToSubOp("Nope")
		assert.False(t, ok)
	})

	t.Run("orchestrator known and unknown", func(t *testing.T) {
		op, ok := OrchestratorMethodToSubOp("SpawnSession")
		require.True(t, ok)
		assert.Equal(t, SubOpOrchestratorSpawn, op)
		_, ok = OrchestratorMethodToSubOp("Nope")
		assert.False(t, ok)
	})
}

// TestSchedulerCryptoOpcodeNoCollision is the regression guard for the
// Scheduler/Crypto wire fix. The SDK previously mapped Scheduler methods to the
// stale per-method opcodes 0xF0-0xFA, which collided with the Crypto/transit
// opcodes (0xF0-0xF9) and never matched the server, which dispatches Scheduler
// on OpScheduler=0x00 with a sub-op byte. This test pins the corrected mapping:
// Crypto keeps 0xF0; Scheduler routes to 0x00; and 0xF0 reverse-resolves to
// Crypto, not Scheduler.
func TestSchedulerCryptoOpcodeNoCollision(t *testing.T) {
	cryptoOp, ok := MethodToOpcode("quicbaas.Crypto", "Encrypt")
	require.True(t, ok)
	schedulerOp, ok := MethodToOpcode("quicbaas.Scheduler", "CreateJob")
	require.True(t, ok)

	// Crypto/Encrypt keeps its canonical 0xF0 opcode.
	assert.Equal(t, byte(0xF0), cryptoOp)

	// Scheduler dispatches on OpScheduler (0x00); the per-method distinction is
	// carried by the sub-op byte prepended to the payload, not the opcode.
	assert.Equal(t, OpScheduler, schedulerOp)
	assert.Equal(t, byte(0x00), schedulerOp)

	// The two no longer collide.
	assert.NotEqual(t, cryptoOp, schedulerOp,
		"Crypto/Encrypt and Scheduler/CreateJob must not share a wire opcode")

	// Every Scheduler method maps to OpScheduler and resolves to a distinct sub-op.
	methods := []string{
		"CreateJob", "GetJob", "UpdateJob", "DeleteJob", "ListJobs", "TriggerJob",
		"PauseJob", "ResumeJob", "CancelJob", "GetJobHistory", "GetStatus",
	}
	seen := make(map[byte]string, len(methods))
	for _, m := range methods {
		op, ok := MethodToOpcode("quicbaas.Scheduler", m)
		require.True(t, ok, "no opcode for quicbaas.Scheduler/%s", m)
		assert.Equal(t, OpScheduler, op, "Scheduler/%s must map to OpScheduler", m)

		subOp, ok := SchedulerMethodToSubOp(m)
		require.True(t, ok, "no sub-op for Scheduler/%s", m)
		if prev, dup := seen[subOp]; dup {
			t.Fatalf("Scheduler sub-op 0x%02X reused by %s and %s", subOp, prev, m)
		}
		seen[subOp] = m
	}

	// In the reverse map, 0xF0 now resolves to Crypto (the stale Scheduler entry
	// that previously shadowed it has been removed). 0x00 resolves to Scheduler.
	method, ok := OpcodeToMethod(0xF0)
	require.True(t, ok)
	assert.Equal(t, "quicbaas.Crypto/Encrypt", method)

	schedMethod, ok := OpcodeToMethod(OpScheduler)
	require.True(t, ok)
	assert.Equal(t, "quicbaas.Scheduler", schedMethod)
}
