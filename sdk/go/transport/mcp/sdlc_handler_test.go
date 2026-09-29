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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

func TestSDLCHandler_ToolNamesAndTransforms(t *testing.T) {
	if !isMCPSDLCService("quicbaas.SDLC") || isMCPSDLCService("quicbaas.Project") {
		t.Fatal("service detection")
	}
	for method, want := range map[string]string{"Bind": "sdlc_bind", "Unbind": "sdlc_unbind", "Binding": "sdlc_binding", "Delivery": "sdlc_delivery"} {
		if got, ok := sdlcToolName(method); !ok || got != want {
			t.Fatalf("%s → %q %v", method, got, ok)
		}
	}
	if _, ok := sdlcToolName("Nope"); ok {
		t.Fatal("unknown method mapped")
	}
	if got := ToolName("quicbaas.SDLC", "Binding"); got != "sdlc_binding" {
		t.Fatalf("ToolName = %q", got)
	}

	binding, err := transformSDLCResponse("Binding", map[string]interface{}{"project_id": "p1", "forge": map[string]interface{}{"type": "gitea", "trigger_label": "task:ready"}})
	if err != nil || binding.(*sdlc.ProjectBinding).Forge.TriggerLabel != "task:ready" {
		t.Fatalf("binding transform: %+v %v", binding, err)
	}
	bound, err := transformSDLCResponse("Bind", map[string]interface{}{"project_id": "p1"})
	if err != nil || bound.(*sdlc.ProjectBinding).ProjectID != "p1" {
		t.Fatalf("bind transform: %+v %v", bound, err)
	}
	unbound, err := transformSDLCResponse("Unbind", map[string]interface{}{"project_id": "p1", "unbound": true})
	if err != nil || !unbound.(*sdlc.UnbindResponse).Unbound {
		t.Fatalf("unbind transform: %+v %v", unbound, err)
	}
	delivery, err := transformSDLCResponse("Delivery", map[string]interface{}{"task_id": "t1", "attempt": 2, "pull_request": map[string]interface{}{"number": 5, "state": "open"}})
	if err != nil || delivery.(*sdlc.Delivery).PullRequest.Number != 5 || delivery.(*sdlc.Delivery).Attempt != 2 {
		t.Fatalf("delivery transform: %+v %v", delivery, err)
	}
	if _, err := transformSDLCResponse("Nope", map[string]interface{}{}); err == nil {
		t.Fatal("unknown method transformed")
	}
	if _, err := transformSDLCResponse("Binding", nil); err == nil {
		t.Fatal("empty result transformed")
	}
	if _, err := transformSDLCResponse("Binding", map[string]interface{}{"project_id": 12}); err == nil {
		t.Fatal("a mistyped field decoded")
	}
	if _, err := transformSDLCResponse("Delivery", map[string]interface{}{"attempt": "two"}); err == nil {
		t.Fatal("a mistyped delivery decoded")
	}
	if _, err := transformSDLCResponse("Unbind", map[string]interface{}{"unbound": "yes"}); err == nil {
		t.Fatal("a mistyped unbind decoded")
	}
	if !(&Transport{}).SupportsSDLC() {
		t.Fatal("SupportsSDLC")
	}
	if err := newSDLCUnsupportedError("X"); err == nil {
		t.Fatal("unsupported error")
	}
}

// TestSDLCOnboardingTransforms_DecodeTheReports proves the MCP side decodes
// a tool result into the SDK's own types rather than handing back a map.
func TestSDLCOnboardingTransforms_DecodeTheReports(t *testing.T) {
	onboarding, err := transformSDLCResponse("Onboard", map[string]any{
		"project_id": "p1", "webhook_ensured": true, "labels_created": []string{"task:ready"},
		"conformance": map[string]any{"project_id": "p1", "conformant": true},
	})
	if err != nil {
		t.Fatalf("Onboard transform: %v", err)
	}
	report, ok := onboarding.(*sdlc.OnboardingReport)
	if !ok || !report.WebhookEnsured || len(report.LabelsCreated) != 1 {
		t.Fatalf("Onboard decoded to %T = %+v", onboarding, onboarding)
	}
	if report.Conformance == nil || !report.Conformance.Conformant {
		t.Fatalf("the conformance half was dropped: %+v", report.Conformance)
	}

	conformance, err := transformSDLCResponse("Conformance", map[string]any{
		"project_id": "p1", "conformant": false,
		"findings": []map[string]any{{"class": "label_missing", "check": "labels", "subject": "x", "detail": "absent", "remedy": "onboard"}},
	})
	if err != nil {
		t.Fatalf("Conformance transform: %v", err)
	}
	decoded, ok := conformance.(*sdlc.ConformanceReport)
	if !ok || decoded.Conformant || len(decoded.Findings) != 1 {
		t.Fatalf("Conformance decoded to %T = %+v", conformance, conformance)
	}
	if decoded.Findings[0].Remedy == "" {
		t.Error("a finding arrived without its remedy; the reader is left to guess at the repair")
	}
}

// TestSDLCOnboardingTransforms_RefuseNonsense is the error half.
func TestSDLCOnboardingTransforms_RefuseNonsense(t *testing.T) {
	if _, err := transformSDLCResponse("Onboard", nil); err == nil {
		t.Error("an empty tool result decoded to a report")
	}
	if _, err := transformSDLCResponse("Conformance", "not an object"); err == nil {
		t.Error("a scalar tool result decoded to a report")
	}
	if _, err := transformSDLCResponse("NoSuchVerb", map[string]any{}); err == nil {
		t.Error("an unmapped verb was transformed rather than refused")
	}
}

// TestSDLCOpenWorkTransform decodes sdlc_open_work's result into the SDK's
// own type, and refuses a result that is not one.
func TestSDLCOpenWorkTransform(t *testing.T) {
	if got, ok := sdlcToolName("OpenWork"); !ok || got != "sdlc_open_work" {
		t.Fatalf("OpenWork → %q %v", got, ok)
	}
	decoded, err := transformSDLCResponse("OpenWork", map[string]any{
		"project_id": "p1", "repo": "acme/api", "agent": "agent-api",
		"pull_requests": []map[string]any{{"number": 7, "head_ref": "issue/7", "ci_state": "failure", "review_state": "changes_requested", "age_seconds": 60}},
		"issues":        []map[string]any{{"number": 3, "labels": []string{"task:blocked"}}},
	})
	if err != nil {
		t.Fatalf("OpenWork transform: %v", err)
	}
	work, ok := decoded.(*sdlc.OpenWork)
	if !ok || work.Agent != "agent-api" || len(work.PullRequests) != 1 || work.PullRequests[0].CIState != "failure" ||
		work.PullRequests[0].AgeSeconds != 60 || len(work.Issues) != 1 || work.Issues[0].Labels[0] != "task:blocked" {
		t.Fatalf("OpenWork decoded to %T = %+v", decoded, decoded)
	}
	if _, err := transformSDLCResponse("OpenWork", map[string]any{"pull_requests": "none"}); err == nil {
		t.Error("a mistyped open-work result decoded")
	}
}
