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

package workflowcrud

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

func toServer(t *testing.T, def *orchestrator.WorkflowDefinition) *orchestration.WorkflowDefinition {
	t.Helper()
	body, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out orchestration.WorkflowDefinition
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return &out
}

// The round trips only prove anything if the agent service accepts the
// fixture and refuses its invalid twin.
func TestDefinition_PassesServerValidation(t *testing.T) {
	if err := toServer(t, Definition(t)).Validate(); err != nil {
		t.Fatalf("the fixture definition is invalid: %v", err)
	}
}

func TestInvalid_FailsServerValidation(t *testing.T) {
	if err := toServer(t, Invalid()).Validate(); !errors.Is(err, orchestration.ErrWorkflowValidation) {
		t.Fatalf("Invalid() validated with %v, want a workflow validation error", err)
	}
}
