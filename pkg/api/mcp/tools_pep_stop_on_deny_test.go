//go:build mcp

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

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// stopOnDenyDeniedError is the denial the counting authorizer returns.
type stopOnDenyDeniedError struct{ resource string }

func (e *stopOnDenyDeniedError) Error() string { return "denied: " + e.resource }

// countingAuthorizer denies the "deny" resource, allows every other one, and
// counts how many decisions it was asked for.
type countingAuthorizer struct{ calls int }

func (a *countingAuthorizer) Authorize(_ context.Context, _ *iam.Subject, resource, _ string) error {
	a.calls++
	if resource == "deny" {
		return &stopOnDenyDeniedError{resource: resource}
	}
	return nil
}

func TestExecutePEPCheckBatch_StopOnDeny(t *testing.T) {
	tests := []struct {
		name       string
		wantCount  int
		wantCalls  int
		wantLast   bool
		stopOnDeny interface{}
	}{
		{name: "flag on stops after the first deny", stopOnDeny: true, wantCount: 2, wantCalls: 2, wantLast: false},
		{name: "flag off evaluates every check", stopOnDeny: false, wantCount: 3, wantCalls: 3, wantLast: true},
		{name: "flag absent evaluates every check", stopOnDeny: nil, wantCount: 3, wantCalls: 3, wantLast: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			authz := &countingAuthorizer{}
			engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{
				Storage:        iam.NewMemoryStorage(),
				RBACAuthorizer: authz,
			})
			reg := &ToolRegistry{service: &pepMockService{engine: engine}, logger: newTestLogger(t)}

			checks := make([]interface{}, 0, 3)
			for _, resource := range []string{"allow-1", "deny", "allow-2"} {
				checks = append(checks, map[string]interface{}{
					"tenant_id": "t1", "principal": "user-1",
					"action": "read", "resource": resource,
				})
			}
			args := map[string]interface{}{"checks": checks}
			if tc.stopOnDeny != nil {
				args["stop_on_deny"] = tc.stopOnDeny
			}

			result, err := reg.executePEPCheckBatch(context.Background(), args)
			require.NoError(t, err)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Equal(t, float64(tc.wantCount), resultMap["count"])
			assert.Equal(t, tc.wantCalls, authz.calls)

			results, ok := resultMap["results"].([]interface{})
			require.True(t, ok)
			require.Len(t, results, tc.wantCount)
			last, ok := results[len(results)-1].(map[string]interface{})
			require.True(t, ok)
			assert.Equal(t, tc.wantLast, last["allowed"])
		})
	}
}

func TestPEPCheckBatchTool_SchemaDocumentsStopOnDeny(t *testing.T) {
	reg := &ToolRegistry{logger: newTestLogger(t)}
	reg.registerPEPTools()

	var tool *ToolDefinition
	for i := range reg.tools {
		if reg.tools[i].Name == "quicbaas_pep_check_batch" {
			tool = &reg.tools[i]
		}
	}
	require.NotNil(t, tool)
	props, ok := tool.InputSchema["properties"].(map[string]interface{})
	require.True(t, ok)
	stop, ok := props["stop_on_deny"].(map[string]interface{})
	require.True(t, ok, "schema must document stop_on_deny")
	assert.Equal(t, "boolean", stop["type"])
}
