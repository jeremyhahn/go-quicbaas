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

package grpc

import (
	"encoding/json"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// accountingTasks are tasks resting in each state whose accounting a reader
// must be able to tell apart without reading Result.Output (go-quicbaas#947).
func accountingTasks() map[string]*orchestration.Task {
	failureClass := func(class string) *orchestration.TaskResult {
		return &orchestration.TaskResult{Output: "prose", Metadata: map[string]string{"failure_class": class}}
	}
	return map[string]*orchestration.Task{
		"completed": {
			ID: "t-done", Status: orchestration.TaskStatusCompleted, Attempts: 1,
			ConclusionReason: orchestration.TaskConcludedCompleted,
		},
		"failed on the work": {
			ID: "t-work", Status: orchestration.TaskStatusFailed, Attempts: 3,
			ConclusionReason: orchestration.TaskConcludedBudgetExhausted,
			Result:           failureClass(orchestration.JobFailureClassStalled),
		},
		"awaiting operator repair": {
			ID: "t-env", Status: orchestration.TaskStatusBlocked, Attempts: 2, EnvironmentFaults: 1, AttemptBudget: 5,
			BlockKind: orchestration.TaskBlockKindBlocked, BlockedReason: "no checkout",
			BlockFaultClass:  orchestration.FaultWorkspaceUnfit,
			ConclusionReason: orchestration.TaskConcludedRunnerParked,
		},
		"awaiting an owner ruling": {
			ID: "t-ruling", Status: orchestration.TaskStatusBlocked, Attempts: 1, QuestionAttempts: 1,
			BlockKind: orchestration.TaskBlockKindNeedsInfo, BlockedReason: "architecture call",
			ConclusionReason: orchestration.TaskConcludedParkedNeedsInfo,
		},
		"abandoned": {
			ID: "t-lost", Status: orchestration.TaskStatusBlocked, Attempts: 3,
			BlockKind:        orchestration.TaskBlockKindEscalated,
			ConclusionReason: orchestration.TaskConcludedAbandoned,
			Result:           failureClass(orchestration.JobFailureClassAbandoned),
		},
	}
}

// TestServiceTaskToProto_AccountingMatchesREST is the gRPC half of the
// go-quicbaas#947 parity: every fault and budget field the REST
// representation carries reaches the proto with the same value, because a
// task whose accounting differs by transport is one of them lying.
func TestServiceTaskToProto_AccountingMatchesREST(t *testing.T) {
	for name, task := range accountingTasks() {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(task)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var rest struct {
				Attempts          int64  `json:"attempts"`
				ChargedAttempts   int64  `json:"charged_attempts"`
				EffectiveBudget   int64  `json:"effective_budget"`
				EnvironmentFaults int64  `json:"environment_faults"`
				QuestionAttempts  int64  `json:"question_attempts"`
				ConclusionReason  string `json:"conclusion_reason"`
				FaultClass        string `json:"fault_class"`
				FaultOrigin       string `json:"fault_origin"`
				BlockKind         string `json:"block_kind"`
				BlockedReason     string `json:"blocked_reason"`
				BlockFaultClass   string `json:"block_fault_class"`
			}
			if err := json.Unmarshal(data, &rest); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			got := serviceTaskToProto(task)
			checks := []struct {
				field     string
				grpc, rst any
			}{
				{"attempts", got.GetAttempts(), rest.Attempts},
				{"charged_attempts", got.GetChargedAttempts(), rest.ChargedAttempts},
				{"effective_budget", got.GetEffectiveBudget(), rest.EffectiveBudget},
				{"environment_faults", got.GetEnvironmentFaults(), rest.EnvironmentFaults},
				{"question_attempts", got.GetQuestionAttempts(), rest.QuestionAttempts},
				{"conclusion_reason", got.GetConclusionReason(), rest.ConclusionReason},
				{"fault_class", got.GetFaultClass(), rest.FaultClass},
				{"fault_origin", got.GetFaultOrigin(), rest.FaultOrigin},
				{"block_kind", got.GetBlockKind(), rest.BlockKind},
				{"blocked_reason", got.GetBlockedReason(), rest.BlockedReason},
				{"block_fault_class", got.GetBlockFaultClass(), rest.BlockFaultClass},
			}
			for _, c := range checks {
				if c.grpc != c.rst {
					t.Errorf("%s: gRPC %v, REST %v", c.field, c.grpc, c.rst)
				}
			}
			if got.GetEffectiveBudget() == 0 {
				t.Error("effective_budget is zero; the engine default never is")
			}
		})
	}
}

// TestServiceTaskToProto_OriginsAreDistinct: the three stops #947 is about
// must not collapse into one value on the wire.
func TestServiceTaskToProto_OriginsAreDistinct(t *testing.T) {
	tasks := accountingTasks()
	want := map[string]string{
		"completed":                "",
		"failed on the work":       string(orchestration.TaskFaultOriginWork),
		"awaiting operator repair": string(orchestration.TaskFaultOriginEnvironment),
		"awaiting an owner ruling": "",
		"abandoned":                string(orchestration.TaskFaultOriginUndiagnosed),
	}
	for name, origin := range want {
		if got := serviceTaskToProto(tasks[name]).GetFaultOrigin(); got != origin {
			t.Errorf("%s: fault_origin = %q, want %q", name, got, origin)
		}
	}
	if got := serviceTaskToProto(tasks["awaiting an owner ruling"]).GetBlockKind(); got != string(orchestration.TaskBlockKindNeedsInfo) {
		t.Errorf("an owner ruling's block_kind = %q, want needs_info", got)
	}
}
