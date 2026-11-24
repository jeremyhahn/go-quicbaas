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

package sdlc

import (
	"errors"
	"testing"
)

func TestRepoOnboardRequest_ValidateAcceptsARepoAndTemplate(t *testing.T) {
	req := RepoOnboardRequest{Repo: "web", Like: "proj-1", MaxConcurrency: 2, Paused: true}
	if err := req.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestRepoOnboardRequest_ValidateRefusesBadRequests(t *testing.T) {
	cases := map[string]struct {
		req   RepoOnboardRequest
		field string
	}{
		"no repo":              {RepoOnboardRequest{Like: "p"}, "repo"},
		"padded repo":          {RepoOnboardRequest{Repo: " web", Like: "p"}, "repo"},
		"owner/repo":           {RepoOnboardRequest{Repo: "acme/web", Like: "p"}, "repo"},
		"dot dot":              {RepoOnboardRequest{Repo: "..", Like: "p"}, "repo"},
		"no template":          {RepoOnboardRequest{Repo: "web", Like: " "}, "like"},
		"negative concurrency": {RepoOnboardRequest{Repo: "web", Like: "p", MaxConcurrency: -1}, "max_concurrency"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var invalid *InvalidInputError
			if err := tc.req.Validate(); !errors.As(err, &invalid) || invalid.Field != tc.field {
				t.Fatalf("Validate = %v, want InvalidInputError on %s", err, tc.field)
			}
		})
	}
}

func TestRepoOnboardRefs(t *testing.T) {
	if got := ForgeTokenRef("web"); got != "sdlc/forge/web/token" {
		t.Fatalf("ForgeTokenRef = %q", got)
	}
	if got := WebhookSecretRef("web"); got != "sdlc/webhook/web" {
		t.Fatalf("WebhookSecretRef = %q", got)
	}
	if got := SigningKeyName("web"); got != "agent-web-signing" {
		t.Fatalf("SigningKeyName = %q", got)
	}
}

func TestRepoOnboardingReport_FindingsAndStep(t *testing.T) {
	report := &RepoOnboardingReport{Steps: []RepoOnboardStep{
		{Step: OnboardStepProject, Outcome: OnboardCreated},
		{Step: OnboardStepProtection, Outcome: OnboardFinding, Detail: "missing"},
	}}

	findings := report.Findings()
	if len(findings) != 1 || findings[0].Step != OnboardStepProtection {
		t.Fatalf("Findings = %+v, want the protection finding only", findings)
	}
	if step, ok := report.Step(OnboardStepProject); !ok || step.Outcome != OnboardCreated {
		t.Fatalf("Step(project) = %+v, %v", step, ok)
	}
	if _, ok := report.Step(OnboardStepWorkflows); ok {
		t.Fatal("Step reported a step that did not run")
	}
}
