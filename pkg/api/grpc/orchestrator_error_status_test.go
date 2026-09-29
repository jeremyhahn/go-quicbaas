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
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func TestConvertOrchestratorError_OversizedPromptAppendIsInvalidArgument(t *testing.T) {
	err := convertOrchestratorError(&orchestration.PromptAppendTooLargeError{Field: "prompt_append", Size: 70000, Limit: orchestration.MaxPromptAppendBytes})
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("code = %s (%v), want InvalidArgument", got, err)
	}
}

func TestConvertSDLCError_OversizedSlotAppendIsInvalidArgument(t *testing.T) {
	err := convertSDLCError(&orchestration.PromptAppendTooLargeError{Field: "worker_prompt_append", Size: 70000, Limit: orchestration.MaxPromptAppendBytes})
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("code = %s (%v), want InvalidArgument", got, err)
	}
}

func TestConvertOrchestratorError_AnAgentOverBudgetIsResourceExhausted(t *testing.T) {
	err := convertOrchestratorError(&orchestration.AgentSpendExhaustedError{AgentID: "a", Window: "daily", Axis: "tokens", Used: 2, Limit: 1})
	if got := status.Code(err); got != codes.ResourceExhausted {
		t.Fatalf("code = %s (%v), want ResourceExhausted", got, err)
	}
	if got := status.Code(convertOrchestratorError(&orchestration.AgentSpendUndecidableError{AgentID: "a"})); got != codes.Unavailable {
		t.Fatalf("an unchecked limit = %s, want Unavailable", got)
	}
}
