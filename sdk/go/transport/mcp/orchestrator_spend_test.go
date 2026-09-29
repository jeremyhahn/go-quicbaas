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

package mcp

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestGetSpendReportToolName pins the SDK method to the tool the server
// registers. The generic service_method derivation would ask for
// quicbaas_orchestrator_get_spend_report, which does not exist.
func TestGetSpendReportToolName(t *testing.T) {
	tr := &Transport{}
	got := tr.mapToToolName("quicbaas.Orchestrator", "GetSpendReport")
	if got != "quicbaas_orchestrator_cost" {
		t.Fatalf("mapToToolName = %q, want quicbaas_orchestrator_cost", got)
	}
}

// TestDecodeMCPSpendReport decodes the report the server's executeAgentCost
// marshals, including the two fields a renderer must not lose:
// priced_entries and an unattributed child row.
func TestDecodeMCPSpendReport(t *testing.T) {
	raw := []byte(`{"level":"project","tenant_id":"t1","project_id":"p1",
		"entries":3,"input_tokens":120,"output_tokens":80,"total_tokens":200,
		"equivalent_micros":0,"priced_entries":0,"successes":2,"failures":1,
		"child_level":"task",
		"children":[{"id":"task-1","level":"task","total_tokens":150},
		            {"level":"task","unattributed":true,"total_tokens":50}],
		"accounts":[{"account_id":"a1","total_tokens":200,"policy_status":"none"}],
		"models":[{"engine":"acp","model":"claude-opus-5","total_tokens":200}]}`)

	got, err := decodeMCPSpendReport(raw)
	if err != nil {
		t.Fatalf("decodeMCPSpendReport: %v", err)
	}
	report, ok := got.(*orchestrator.GetSpendReportResponse)
	if !ok {
		t.Fatalf("decoded %T, want *orchestrator.GetSpendReportResponse", got)
	}
	if report.Level != orchestrator.SpendLevelProject || report.TotalTokens != 200 {
		t.Fatalf("report = %+v, want project/200", report)
	}
	if report.PricedEntries != 0 {
		t.Fatal("an unpriced report must not decode as a measured cost")
	}
	if len(report.Children) != 2 || !report.Children[1].Unattributed {
		t.Fatalf("children = %+v, want the unattributed row kept", report.Children)
	}
	if len(report.Accounts) != 1 || report.Accounts[0].PolicyStatus != orchestrator.SpendPolicyNone {
		t.Fatalf("accounts = %+v, want acct a1 with no spend policy", report.Accounts)
	}
}

// TestDecodeMCPSpendReport_InvalidJSON asserts an undecodable result is a
// typed failure rather than an empty report, which would read as a tenant
// that has spent nothing.
func TestDecodeMCPSpendReport_InvalidJSON(t *testing.T) {
	if _, err := decodeMCPSpendReport([]byte(`{"accounts":`)); err == nil {
		t.Fatal("a malformed tool result was accepted")
	}
}

// TestGetSpendReportTransformIsRegistered proves the method goes through the
// typed decoder rather than being handed back as a raw map, which would fail
// the SDK client's response type assertion.
func TestGetSpendReportTransformIsRegistered(t *testing.T) {
	h := newOrchestratorMCPHandler()

	got, err := h.transformResponse("GetSpendReport",
		`{"level":"tenant","tenant_id":"t1","priced_entries":0}`)
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}
	if _, ok := got.(*orchestrator.GetSpendReportResponse); !ok {
		t.Fatalf("transformResponse returned %T, want *orchestrator.GetSpendReportResponse", got)
	}
}

// TestGetSpendReportArgumentsAreSnakeCase asserts the tool arguments the
// transport derives from the SDK request match the server's declared schema.
// A camelCase key would be silently ignored by the tool, widening the scope.
func TestGetSpendReportArgumentsAreSnakeCase(t *testing.T) {
	tr := &Transport{}
	args := tr.convertToArguments(&orchestrator.GetSpendReportRequest{
		TenantID:  "t1",
		ProjectID: "p1",
		TaskID:    "w1",
		JobID:     "j1",
		SessionID: "s1",
		Round:     2,
		RoundSet:  true,
		Host:      "gpu-host:9091",
	})

	for _, key := range []string{"tenant_id", "project_id", "task_id", "job_id", "session_id", "round", "host"} {
		if _, ok := args[key]; !ok {
			t.Fatalf("tool arguments %v are missing %q", args, key)
		}
	}
}

// TestTenantSpendPolicyToolNames pins the tools the two tenant spend-policy
// verbs dispatch to (go-quicbaas#1058).
func TestTenantSpendPolicyToolNames(t *testing.T) {
	for method, want := range map[string]string{
		"GetTenantSpendPolicy": "quicbaas_orchestrator_spend_policy_get",
		"SetTenantSpendPolicy": "quicbaas_orchestrator_spend_policy_set",
	} {
		if got := (&Transport{}).mapToToolName("quicbaas.Orchestrator", method); got != want {
			t.Fatalf("%s maps to %q, want %q", method, got, want)
		}
		if _, ok := orchestratorMCPTransforms[method]; !ok {
			t.Fatalf("%s has no result decoder", method)
		}
	}
}

// TestDecodeMCPTenantSpendPolicy decodes the report the server's tools marshal.
func TestDecodeMCPTenantSpendPolicy(t *testing.T) {
	raw := []byte(`{"tenant_id":"t1","limits":{"windows":[{"name":"monthly","size":2592000000000000,
		"max_equivalent_micros":2000000000}]},"updated_at":"2026-09-23T06:00:00Z",
		"windows":[{"name":"monthly","used_equivalent_micros":7,"exhausted":false}]}`)
	got, err := decodeMCPTenantSpendPolicy(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	policy, ok := got.(*orchestrator.TenantSpendPolicy)
	if !ok || policy.TenantID != "t1" || len(policy.Limits.Windows) != 1 || policy.UpdatedAt == nil || policy.Windows[0].UsedEquivalentMicros != 7 {
		t.Fatalf("policy = %+v", got)
	}
	if _, err := decodeMCPTenantSpendPolicy([]byte("{bad")); err == nil {
		t.Fatal("invalid JSON decoded")
	}
}
