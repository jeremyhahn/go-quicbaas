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
	"slices"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// TestTrain_EjectsWithTheProjectsCustomizedCIFailurePrompt launches a train
// job the way the envelope does: it asks the runner which set it renders from
// and hands the job that set, resolved for the project. The tenant has edited
// ci_red, so the findings a culprit is ejected with must carry the edit, and
// the job must record the versions it ran with.
func TestTrain_EjectsWithTheProjectsCustomizedCIFailurePrompt(t *testing.T) {
	ctx := context.Background()
	var runner orchestration.JobRunner = &TrainRunner{}
	provider, ok := runner.(orchestration.PromptSetProvider)
	if !ok || provider.PromptSet() != SetSDLC {
		t.Fatal("the train runner names no prompt set, so its jobs launch without the project's prompts")
	}
	set, err := tenantEditedResolver(t, "ci_red", "TENANT-EDITED ").Resolve(ctx, testTenant, testProject, provider.PromptSet())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	rig := newTrainRig(t)
	rig.prompts = set
	rig.queue(101, "task-a")
	rig.pipeline(trainCommit(1, 101), "failure", genuineRed)
	job := rig.job(ctx, orchestration.JobStateInitializing)
	if err := rig.runner.Run(ctx, job); err != nil {
		t.Fatalf("Run: %v", err)
	}

	ejected := rig.entry("task-a")
	if ejected.Ejection == nil || !strings.Contains(ejected.Ejection.Findings, "TENANT-EDITED ") {
		t.Fatalf("the ejection ignores the tenant's ci_red edit: %+v", ejected.Ejection)
	}
	recorded := job.Status().Prompts
	if !slices.Equal(recorded, set.Refs()) {
		t.Fatalf("job recorded prompts %+v, want the resolved %+v", recorded, set.Refs())
	}
	edited := slices.IndexFunc(recorded, func(ref prompts.VersionRef) bool {
		return ref.Name == "ci_red" && ref.Scope == prompts.ScopeTenant && ref.Version == 1
	})
	if edited < 0 {
		t.Fatalf("job does not record the tenant's ci_red version: %+v", recorded)
	}
}
