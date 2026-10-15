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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

func TestSDLCRepoOnboardingTransform_DecodesTheReport(t *testing.T) {
	if got, ok := sdlcToolName("OnboardRepo"); !ok || got != "sdlc_project_onboard" {
		t.Fatalf("OnboardRepo → %q %v", got, ok)
	}
	decoded, err := transformSDLCResponse("OnboardRepo", map[string]any{
		"project_id": "p2", "repo": "acme/web",
		"steps": []map[string]any{
			{"step": "project", "outcome": "created"},
			{"step": "protection", "outcome": "finding", "detail": "missing", "remedy": "require it"},
		},
		"conformance": map[string]any{"project_id": "p2", "conformant": true},
	})
	if err != nil {
		t.Fatalf("OnboardRepo transform: %v", err)
	}
	report, ok := decoded.(*sdlc.RepoOnboardingReport)
	if !ok || report.Repo != "acme/web" || len(report.Steps) != 2 {
		t.Fatalf("OnboardRepo decoded to %T = %+v", decoded, decoded)
	}
	if report.Steps[1].Outcome != sdlc.OnboardFinding || report.Steps[1].Remedy != "require it" {
		t.Errorf("finding step = %+v", report.Steps[1])
	}
	if report.Conformance == nil || !report.Conformance.Conformant {
		t.Errorf("the conformance half was dropped: %+v", report.Conformance)
	}
}

func TestSDLCRepoOnboardingTransform_RefusesNonsense(t *testing.T) {
	if _, err := transformSDLCResponse("OnboardRepo", nil); err == nil {
		t.Error("an empty tool result decoded to a report")
	}
	if _, err := transformSDLCResponse("OnboardRepo", "not an object"); err == nil {
		t.Error("a scalar tool result decoded to a report")
	}
}

func TestSDLCRepoOnboardingArguments_CarryPreflight(t *testing.T) {
	tr := &Transport{}
	args := tr.convertToArguments(&sdlc.RepoOnboardRequest{Repo: "web", Like: "p1", Paused: true, Preflight: true})
	if args["preflight"] != true || args["paused"] != true || args["like"] != "p1" {
		t.Fatalf("tool arguments = %+v, want preflight, paused and like", args)
	}
	if _, sent := tr.convertToArguments(&sdlc.RepoOnboardRequest{Repo: "web", Like: "p1"})["preflight"]; sent {
		t.Fatal("an unset preflight was sent; the tool must see it absent")
	}
}
