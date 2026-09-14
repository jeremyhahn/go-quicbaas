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

package orchestration

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// decodeUsage parses raw the way an agent CLI's output is parsed, so a test
// asserts on what the decode boundary actually produces rather than on a
// hand-built struct that never went through it.
func decodeUsage(t *testing.T, raw string) *TokenUsage {
	t.Helper()
	var usage TokenUsage
	if err := json.Unmarshal([]byte(raw), &usage); err != nil {
		t.Fatalf("Unmarshal(%s) error = %v", raw, err)
	}
	return &usage
}

// capturingLogger records Warnf lines so a test can assert that a repaired
// turn SAYS so — the half of #242 an operator can act on.
type capturingLogger struct {
	agentTestLogger
	mu    sync.Mutex
	warns []string
}

func (l *capturingLogger) Warnf(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warns = append(l.warns, format)
	_ = args
}

func (l *capturingLogger) warnings() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.warns...)
}

var _ interfaces.Logger = (*capturingLogger)(nil)

// TestTokenUsage_NegativeCountsAreRepairedAtTheDecodeBoundary is the regression
// test for #242.
//
// The filed defect is arithmetic: {"input_tokens":1,"output_tokens":-1} sums to
// zero, reads as "the CLI reported nothing", and the turn is billed as nothing.
// The defect underneath it is that the result is INDISTINGUISHABLE from a turn
// that genuinely cost nothing — so the assertions below come in pairs, and the
// contrast half matters as much as the failing half.
func TestTokenUsage_NegativeCountsAreRepairedAtTheDecodeBoundary(t *testing.T) {
	t.Run("a cancelling pair no longer sums to nothing", func(t *testing.T) {
		usage := decodeUsage(t, `{"input_tokens":1,"output_tokens":-1}`)

		if usage.OutputTokens != 0 {
			t.Errorf("OutputTokens = %d, want 0 — a negative dimension must be clamped, not carried", usage.OutputTokens)
		}
		if usage.InputTokens != 1 {
			t.Errorf("InputTokens = %d, want 1 — clamping the bad dimension must not disturb the good one", usage.InputTokens)
		}
		if usage.Total() != 1 {
			t.Errorf("Total() = %d, want 1 — the turn cost something and must not sum to zero", usage.Total())
		}
		if usage.Empty() {
			t.Error("Empty() = true; a turn that consumed a token must not read as unmeasured")
		}
		if !usage.Malformed() {
			t.Error("Malformed() = false; a repaired record must say it was repaired")
		}
	})

	t.Run("an all-negative record is empty but not silent", func(t *testing.T) {
		usage := decodeUsage(t, `{"input_tokens":-5}`)

		if usage.Total() != 0 {
			t.Errorf("Total() = %d, want 0 — a negative total must never reach the ledger", usage.Total())
		}
		// This is the case the whole issue turns on: it clamps to empty and is
		// dropped, so Malformed is the ONLY thing separating it from a turn that
		// genuinely cost nothing.
		if !usage.Malformed() {
			t.Error("Malformed() = false; a record repaired down to empty is indistinguishable from a free turn without this")
		}
	})

	t.Run("a genuinely empty record is not marked", func(t *testing.T) {
		usage := decodeUsage(t, `{"input_tokens":0,"output_tokens":0}`)

		if !usage.Empty() {
			t.Error("Empty() = false for an all-zero record")
		}
		// The contrast half. Without it, marking everything would pass every
		// assertion above and mean nothing.
		if usage.Malformed() {
			t.Error("Malformed() = true for a clean zero record; the mark must distinguish, not decorate")
		}
	})

	t.Run("an absent record is not marked", func(t *testing.T) {
		var usage *TokenUsage
		if usage.Malformed() {
			t.Error("Malformed() = true on a nil receiver; a CLI that reported nothing reported nothing wrong")
		}
	})

	t.Run("well-formed counts are untouched", func(t *testing.T) {
		usage := decodeUsage(t, `{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":3,"cache_read_input_tokens":2}`)

		if usage.Total() != 20 {
			t.Errorf("Total() = %d, want 20", usage.Total())
		}
		if usage.Malformed() {
			t.Error("Malformed() = true for a well-formed record")
		}
	})

	t.Run("Add carries the mark into the accumulated total", func(t *testing.T) {
		// The accumulated per-message total is what gets billed when the
		// terminal result carries no aggregate, so a total that forgot one of
		// its parts was repaired is the same silence with an extra step.
		accumulated := &TokenUsage{}
		accumulated.Add(decodeUsage(t, `{"input_tokens":10}`))
		accumulated.Add(decodeUsage(t, `{"output_tokens":-3}`))

		if accumulated.Total() != 10 {
			t.Errorf("Total() = %d, want 10", accumulated.Total())
		}
		if !accumulated.Malformed() {
			t.Error("Malformed() = false; one repaired message must mark the turn's total")
		}
	})

	t.Run("collectStreamJSONUsage no longer discards a cancelling pair", func(t *testing.T) {
		result := decodeUsage(t, `{"input_tokens":1,"output_tokens":-1}`)

		got := collectStreamJSONUsage(result, &TokenUsage{})

		if got == nil {
			t.Fatal("collectStreamJSONUsage() = nil; a measured turn was discarded as unmeasured")
		}
		if got.Total() != 1 {
			t.Errorf("Total() = %d, want 1", got.Total())
		}
	})

	t.Run("the daemon wire is a decode boundary too", func(t *testing.T) {
		// Not JSON, so UnmarshalJSON never runs — the counts come from a remote
		// daemon's protobuf, which got them from a CLI on that host.
		evidence, ok := turnFailureEvidenceFromProto(&proto.TurnFailureEvidence{
			UsageReported: true,
			InputTokens:   1,
			OutputTokens:  -1,
		})
		if !ok {
			t.Fatal("turnFailureEvidenceFromProto() reported no evidence")
		}
		if evidence.Usage.OutputTokens != 0 {
			t.Errorf("OutputTokens = %d, want 0 — the wire boundary must clamp like the JSON one", evidence.Usage.OutputTokens)
		}
		if !evidence.Usage.Malformed() {
			t.Error("Malformed() = false; a repair on the wire path must mark the record too")
		}
	})

	// #235 decided that malformed accounting is not evidence of anything, and
	// proved it by testing each dimension: -5 is not 0. Clamping at the decode
	// boundary takes that proof away, because the record now arrives as four
	// zeroes. If usageProvesNoTurn does not ask whether the record was
	// repaired, this fix starts refunding attempts on exactly the records #235
	// refused to trust.
	t.Run("a repaired record is still not evidence that no turn ran", func(t *testing.T) {
		evidence := TurnFailureEvidence{Usage: decodeUsage(t, `{"input_tokens":-5}`)}

		if evidence.usageProvesNoTurn() {
			t.Error("usageProvesNoTurn() = true for a repaired record; #242's clamp must not manufacture #235's evidence")
		}
	})

	t.Run("a genuine zero is still evidence that no turn ran", func(t *testing.T) {
		evidence := TurnFailureEvidence{Usage: decodeUsage(t, `{"input_tokens":0}`)}

		// The contrast half: #235's behaviour must survive intact for records
		// that were never repaired.
		if !evidence.usageProvesNoTurn() {
			t.Error("usageProvesNoTurn() = false for a clean all-zero record; #235's reading must survive #242")
		}
	})
}

// TestRecordTurnUsage_RepairedTurnIsDistinguishableFromAFreeTurn is the
// operator-visible half. A turn repaired down to empty is dropped from the
// ledger exactly as a free turn is, so if nothing says so the two are the same
// event to everyone downstream.
func TestRecordTurnUsage_RepairedTurnIsDistinguishableFromAFreeTurn(t *testing.T) {
	newRecorder := func(t *testing.T) (*agentServiceImpl, *capturingLogger) {
		t.Helper()
		log := &capturingLogger{}
		kv := kvstore.NewMemory()
		svc, err := NewAgentService(NewAgentStore(kv), map[string]AgentBackend{}, log)
		if err != nil {
			t.Fatalf("NewAgentService: %v", err)
		}
		impl, ok := svc.(*agentServiceImpl)
		if !ok {
			t.Fatalf("NewAgentService returned %T, want *agentServiceImpl", svc)
		}
		impl.SetCostTracker(NewCostTracker(kv, log))
		return impl, log
	}

	repairWarnings := func(log *capturingLogger) int {
		count := 0
		for _, w := range log.warnings() {
			if strings.Contains(w, "negative token count") {
				count++
			}
		}
		return count
	}

	ctx := ContextWithTenant(context.Background(), "tenant-a")

	t.Run("a turn repaired down to empty warns", func(t *testing.T) {
		svc, log := newRecorder(t)

		svc.recordTurnUsage(ctx, completedTurn{turnID: "turn-sess-1", sessionID: "sess-1", projectID: "proj-1"}, decodeUsage(t, `{"input_tokens":-5}`))

		if got := repairWarnings(log); got != 1 {
			t.Errorf("repair warnings = %d, want 1 — a repaired turn dropped from the ledger must not be silent", got)
		}
	})

	t.Run("a turn that genuinely cost nothing does not warn", func(t *testing.T) {
		svc, log := newRecorder(t)

		svc.recordTurnUsage(ctx, completedTurn{turnID: "turn-sess-2", sessionID: "sess-2", projectID: "proj-1"}, decodeUsage(t, `{"input_tokens":0}`))

		// The contrast half, and the whole point: the two turns leave the
		// ledger in the same state, so the warning is the only thing that
		// tells them apart.
		if got := repairWarnings(log); got != 0 {
			t.Errorf("repair warnings = %d, want 0 — a free turn must stay quiet or the signal means nothing", got)
		}
	})

	t.Run("a repaired turn that still cost something warns and is recorded", func(t *testing.T) {
		svc, log := newRecorder(t)

		svc.recordTurnUsage(ctx, completedTurn{turnID: "turn-sess-3", sessionID: "sess-3", projectID: "proj-1"}, decodeUsage(t, `{"input_tokens":1,"output_tokens":-1}`))

		if got := repairWarnings(log); got != 1 {
			t.Errorf("repair warnings = %d, want 1", got)
		}
		usage, err := svc.costTracker.GetProjectUsage(ctx, "tenant-a", "proj-1")
		if err != nil {
			t.Fatalf("GetProjectUsage: %v", err)
		}
		if usage.TotalTokens != 1 {
			t.Errorf("project usage = %d, want 1 — the turn consumed a token and must be billed for it", usage.TotalTokens)
		}
	})

	t.Run("a well-formed turn is recorded and quiet", func(t *testing.T) {
		svc, log := newRecorder(t)

		svc.recordTurnUsage(ctx, completedTurn{turnID: "turn-sess-4", sessionID: "sess-4", projectID: "proj-1"}, decodeUsage(t, `{"input_tokens":10,"output_tokens":5}`))

		if got := repairWarnings(log); got != 0 {
			t.Errorf("repair warnings = %d, want 0", got)
		}
		usage, err := svc.costTracker.GetProjectUsage(ctx, "tenant-a", "proj-1")
		if err != nil {
			t.Fatalf("GetProjectUsage: %v", err)
		}
		if usage.TotalTokens != 15 {
			t.Errorf("project usage = %d, want 15", usage.TotalTokens)
		}
	})
}
