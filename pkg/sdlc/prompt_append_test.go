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
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// Each SDLC slot carries its own run-time prompt material: the binding's
// worker and triage appends, and each review seat's. The charter stays the
// system prompt; the append travels beside it to SpawnSession, which composes
// the two.

func TestBinding_BoundsEverySlotsPromptAppend(t *testing.T) {
	oversized := strings.Repeat("a", orchestration.MaxPromptAppendBytes+1)
	cases := map[string]struct {
		edit  func(*ProjectBinding)
		field string
	}{
		"worker": {func(b *ProjectBinding) { b.WorkerPromptAppend = oversized }, "worker_prompt_append"},
		"triage": {func(b *ProjectBinding) { b.TriagePromptAppend = oversized }, "triage_prompt_append"},
		"default reviewer": {func(b *ProjectBinding) {
			b.Reviewer = ReviewerConfig{Agent: testReviewerAgent, Login: "rev", TokenSecretRef: "tok", PromptAppend: oversized}
		}, "reviewer.prompt_append"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			binding := testBinding()
			tc.edit(binding)
			var tooLarge *orchestration.PromptAppendTooLargeError
			if err := binding.Validate(); !errors.As(err, &tooLarge) || tooLarge.Field != tc.field {
				t.Fatalf("Validate = %v, want PromptAppendTooLargeError on %s", err, tc.field)
			}
		})
	}
}

func TestBinding_AcceptsSlotAppendsWithinTheBound(t *testing.T) {
	binding := testBinding()
	binding.WorkerPromptAppend = "run make lint before you finish"
	binding.TriagePromptAppend = strings.Repeat("t", orchestration.MaxPromptAppendBytes)
	if err := binding.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestRunner_TheWorkerCarriesTheBindingsAppend(t *testing.T) {
	rig := newRunnerRig(t)
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) {
		b.Verification = []string{"true"}
		b.WorkerPromptAppend = "this project uses tabs"
	}); err != nil {
		t.Fatal(err)
	}
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	spawn := rig.agents.spawns[0]
	if spawn.SystemPrompt != WorkerCharter(sdlcPrompts) || spawn.PromptAppend != "this project uses tabs" {
		t.Fatalf("worker spawned with {SystemPrompt charter:%v PromptAppend:%q}", spawn.SystemPrompt == WorkerCharter(sdlcPrompts), spawn.PromptAppend)
	}
}

func TestReviewRunner_TheSeatCarriesItsOwnAppend(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.rebind(func(b *ProjectBinding) { b.Reviewer.PromptAppend = "check the migration is idempotent" })
	rig.reviewerSays(reviewSaying("fine", ReviewCodeApprove))
	rig.mustRun(41)
	spawn := rig.agents.spawns[0]
	if spawn.SystemPrompt != ReviewerCharter(sdlcPrompts) || spawn.PromptAppend != "check the migration is idempotent" {
		t.Fatalf("reviewer spawned with {charter:%v PromptAppend:%q}", spawn.SystemPrompt == ReviewerCharter(sdlcPrompts), spawn.PromptAppend)
	}
}

func TestTriageRunner_CarriesTheBindingsAppend(t *testing.T) {
	rig := newTriageRig(t)
	rig.binding.TriagePromptAppend = "issues labelled infra are never ready"
	if err := rig.store.PutBinding(context.Background(), testTenant, rig.binding); err != nil {
		t.Fatal(err)
	}
	rig.forge.putIssue(801, "an issue to triage")
	rig.answers("read it\n\nSDLC-TRIAGE: needs-info")
	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	rig.agents.mu.Lock()
	defer rig.agents.mu.Unlock()
	if len(rig.agents.spawns) != 1 || rig.agents.spawns[0].PromptAppend != "issues labelled infra are never ready" {
		t.Fatalf("triage spawns = %+v, want the binding's append", rig.agents.spawns)
	}
}
