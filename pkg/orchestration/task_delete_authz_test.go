// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package orchestration

import (
	"context"
	"errors"
	"testing"
)

// TestDeleteTask_ActingAgentGatedByProjectMatrix proves a DeleteTask done for
// a fleet node asks the per-project role matrix for the lead role (#640).
func TestDeleteTask_ActingAgentGatedByProjectMatrix(t *testing.T) {
	t.Parallel()

	roles := map[string]ProjectRole{
		"lead-agent":     ProjectRoleLead,
		"worker-agent":   ProjectRoleWorker,
		"observer-agent": ProjectRoleObserver,
		"denied-agent":   ProjectRoleDenied,
	}

	cases := []struct {
		agent   string
		allowed bool
	}{
		{"lead-agent", true},
		{"worker-agent", false},
		{"observer-agent", false},
		{"denied-agent", false},
		{"unlisted-agent", false},
	}

	for _, tc := range cases {
		t.Run(tc.agent, func(t *testing.T) {
			t.Parallel()
			svc, store, tenantID, _ := claimAuthzSetup(t, roles)

			ctx := WithActingAgent(context.Background(), tc.agent)
			err := svc.DeleteTask(ctx, tenantID, "task-1")

			store.mu.Lock()
			deletes := len(store.deleteCalls)
			store.mu.Unlock()

			if tc.allowed {
				if err != nil {
					t.Fatalf("lead delete refused: %v", err)
				}
				if deletes != 1 {
					t.Fatalf("expected 1 store delete, got %d", deletes)
				}
				return
			}
			if !errors.Is(err, ErrProjectAccessDenied) {
				t.Fatalf("expected ErrProjectAccessDenied, got %v", err)
			}
			if deletes != 0 {
				t.Fatalf("refused delete reached the store (%d calls)", deletes)
			}
		})
	}
}

// TestDeleteTask_NoActingAgent_TenantRBACAlone keeps an operator's delete,
// which names no fleet node, ungated by the node matrix.
func TestDeleteTask_NoActingAgent_TenantRBACAlone(t *testing.T) {
	t.Parallel()

	svc, _, tenantID, _ := claimAuthzSetup(t, map[string]ProjectRole{"other": ProjectRoleLead})
	if err := svc.DeleteTask(context.Background(), tenantID, "task-1"); err != nil {
		t.Fatalf("operator delete refused: %v", err)
	}
}

func TestWithActingAgent_EmptyLeavesContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	if WithActingAgent(ctx, "") != ctx {
		t.Fatal("empty agent must leave ctx unchanged")
	}
	if got := ActingAgentFromContext(WithActingAgent(ctx, "n1")); got != "n1" {
		t.Fatalf("got %q", got)
	}
}
