//go:build rest

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

package server

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// TestRegisteredForwardersCarryTheirRunnersLeasePolicy is the regression guard
// for go-quicbaas#938.
//
// The engine reads a lease policy off the runner it REGISTERED, never the live
// module runner behind it. sdlcReviewRunnerForwarder did not implement
// JobLeasePolicy at all, so every review lease counted against the project's
// MaxConcurrency even though ReviewRunner has always declared OffProjectBudget.
// An sdlc job parked awaiting a verdict holds a slot, so the review that would
// release it was refused "project concurrency exceeded" and neither side moved.
//
// A forwarder whose runner declares a policy and which does not carry it is a
// silent scheduling defect, so every forwarder is checked rather than only the
// one that was wrong.
func TestRegisteredForwardersCarryTheirRunnersLeasePolicy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		forwarder orchestration.JobRunner
		runner    orchestration.JobLeasePolicy
	}{
		{"review", sdlcReviewRunnerForwarder{}, &sdlc.ReviewRunner{}},
		{"triage", sdlcTriageForwarder{}, &sdlc.TriageRunner{}},
		{"preflight", sdlcPreflightForwarder{}, &sdlc.PreflightRunner{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			policy, ok := tc.forwarder.(orchestration.JobLeasePolicy)
			if !ok {
				t.Fatalf("%s forwarder does not implement JobLeasePolicy; its lease policy is invisible to the engine", tc.name)
			}
			if got, want := policy.OffProjectBudget(), tc.runner.OffProjectBudget(); got != want {
				t.Errorf("%s OffProjectBudget: forwarder=%v runner=%v; they must agree", tc.name, got, want)
			}
		})
	}
}

// TestReviewForwarderLeaseSubjectSerializesOnThePullRequest: two reviews of the
// same pull request must serialize, and reviews of different pull requests must
// not block each other.
func TestReviewForwarderLeaseSubjectSerializesOnThePullRequest(t *testing.T) {
	t.Parallel()
	fwd := sdlcReviewRunnerForwarder{}

	cfgFor := func(project string, pr string) *orchestration.JobConfig {
		return &orchestration.JobConfig{
			ProjectID: project,
			Params:    map[string]string{sdlc.ParamPullRequest: pr},
		}
	}

	a := fwd.LeaseSubject(cfgFor("proj-1", "65"))
	if a == "" {
		t.Fatal("LeaseSubject is empty for a well-formed review config")
	}
	if again := fwd.LeaseSubject(cfgFor("proj-1", "65")); again != a {
		t.Errorf("same pull request produced %q then %q; two reviews of one head must serialize", a, again)
	}
	if other := fwd.LeaseSubject(cfgFor("proj-1", "64")); other == a {
		t.Errorf("pull requests 65 and 64 share the subject %q; different heads must not block each other", other)
	}
	if cross := fwd.LeaseSubject(cfgFor("proj-2", "65")); cross == a {
		t.Errorf("projects proj-1 and proj-2 share the subject %q; a lease must not span projects", cross)
	}
	// A config the module cannot read names no subject rather than an
	// accidental one every malformed job would then share.
	if empty := fwd.LeaseSubject(cfgFor("", "65")); empty != "" {
		t.Errorf("LeaseSubject with no project = %q, want empty", empty)
	}
	if nilCfg := fwd.LeaseSubject(nil); nilCfg != "" {
		t.Errorf("LeaseSubject(nil) = %q, want empty", nilCfg)
	}
}
