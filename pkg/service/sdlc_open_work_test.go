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

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/giteafake"
)

// TestSDLCService_OpenWorkReadsTheProjectsForge reads a bound project's open
// work through the real Gitea adapter against a stateful Gitea: the agent's
// pull requests with their CI and review state, and the labelled issues.
func TestSDLCService_OpenWorkReadsTheProjectsForge(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	agent := &giteafake.User{Login: giteafake.DefaultLogin}
	rig.forge.PutPullRequestRaw(giteafake.PullRequest{Number: 7, Title: "fix", State: "open", User: agent,
		Head: giteafake.Commit{Ref: "issue/7-fix", SHA: "head7"}, Base: giteafake.Commit{Ref: "main"}})
	rig.forge.PutPullRequestRaw(giteafake.PullRequest{Number: 8, Title: "a person's", State: "open",
		User: &giteafake.User{Login: "a-person"}, Head: giteafake.Commit{Ref: "feature", SHA: "head8"}})
	rig.forge.PutStatus("head7", "ci", "success")
	rig.forge.PutReview(7, "codex", "APPROVED", "head7")
	rig.forge.PutIssue(3, "needs an answer", "task:ready")
	rig.forge.PutIssue(4, "unrelated", "bug")

	work, err := rig.svc.OpenWork(ctx, sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatalf("OpenWork: %v", err)
	}
	if work.Agent != giteafake.DefaultLogin || work.ProjectID != sdlcTestProject {
		t.Fatalf("work = %+v", work)
	}
	if len(work.PullRequests) != 1 {
		t.Fatalf("pull requests = %+v, want only the agent's #7", work.PullRequests)
	}
	pr := work.PullRequests[0]
	if pr.Number != 7 || pr.HeadRef != "issue/7-fix" || pr.CIState != sdlc.CISuccess || pr.ReviewState != sdlc.ReviewApproved {
		t.Fatalf("pull request = %+v", pr)
	}
	if len(work.Issues) != 1 || work.Issues[0].Number != 3 {
		t.Fatalf("issues = %+v, want only the triggered #3", work.Issues)
	}
}

// TestSDLCService_OpenWorkRefusesBadSubjects: no tenant, a project that does
// not exist and a project with no binding each have no forge to read.
func TestSDLCService_OpenWorkRefusesBadSubjects(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()

	var invalid *sdlc.InvalidInputError
	if _, err := rig.svc.OpenWork(ctx, "", sdlcTestProject); !errors.As(err, &invalid) {
		t.Fatalf("no tenant = %v, want InvalidInputError", err)
	}
	if _, err := rig.svc.OpenWork(ctx, sdlcTestTenant, "no-such-project"); err == nil {
		t.Fatal("a missing project answered with open work")
	}
	if _, err := rig.svc.OpenWork(ctx, sdlcTestTenant, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("an unbound project = %v, want ErrNotFound", err)
	}
}

// TestSDLCService_OpenWorkReportsAForgeFailure: a forge that cannot be read
// fails the read rather than answering an empty project.
func TestSDLCService_OpenWorkReportsAForgeFailure(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	rig.forge.ForceStatus("GET", "/issues", 403, "forbidden")

	if work, err := rig.svc.OpenWork(ctx, sdlcTestTenant, sdlcTestProject); err == nil {
		t.Fatalf("a forbidden issue listing answered %+v", work)
	}
}
