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

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Project.Autonomous and Project.ContextRepos
// =============================================================================

// newAutonomyProjectService builds a project service over an in-memory store.
// Neither field consults the workflow registry, so none is wired.
func newAutonomyProjectService() ProjectService {
	return NewProjectService(orchestration.NewInMemoryProjectStore())
}

// handbookRepos is the shape a real project carries: the company handbook
// beside the checkout.
func handbookRepos() []orchestration.ContextRepo {
	return []orchestration.ContextRepo{
		{Name: "handbook", RepoURL: "https://forge.example/org/handbook", Ref: "main"},
	}
}

func TestProjectService_CreateProject_CarriesAutonomyAndContextRepos(t *testing.T) {
	svc := newAutonomyProjectService()
	resp, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID:     "tenant-1",
		Name:         "api",
		Autonomous:   true,
		ContextRepos: handbookRepos(),
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if !resp.Project.Autonomous {
		t.Error("Autonomous = false, want true")
	}
	if len(resp.Project.ContextRepos) != 1 {
		t.Fatalf("ContextRepos len = %d, want 1", len(resp.Project.ContextRepos))
	}
	repo := resp.Project.ContextRepos[0]
	if repo.Name != "handbook" || repo.RepoURL != "https://forge.example/org/handbook" || repo.Ref != "main" {
		t.Errorf("ContextRepos[0] = %+v, want the handbook at main", repo)
	}

	// The stored record, not just the response, carries both.
	got, err := svc.GetProject(context.Background(), &GetProjectRequest{
		TenantID: "tenant-1", ProjectID: resp.Project.ID,
	})
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if !got.Project.Autonomous || len(got.Project.ContextRepos) != 1 {
		t.Errorf("stored project = autonomous %v with %d context repos, want true with 1",
			got.Project.Autonomous, len(got.Project.ContextRepos))
	}
}

// TestProjectService_CreateProject_DefaultsToOperatorDriven pins the zero
// value: a project is not autonomous and clones nothing unless asked.
func TestProjectService_CreateProject_DefaultsToOperatorDriven(t *testing.T) {
	svc := newAutonomyProjectService()
	resp, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID: "tenant-1", Name: "api",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if resp.Project.Autonomous {
		t.Error("Autonomous = true, want false by default")
	}
	if resp.Project.ContextRepos != nil {
		t.Errorf("ContextRepos = %v, want nil by default", resp.Project.ContextRepos)
	}
}

// TestProjectService_CreateProject_RefusesInvalidContextRepos covers each way
// a context repository can be unusable to the worker.
func TestProjectService_CreateProject_RefusesInvalidContextRepos(t *testing.T) {
	cases := []struct {
		name  string
		repos []orchestration.ContextRepo
	}{
		{
			name: "duplicate directory",
			repos: []orchestration.ContextRepo{
				{Name: "handbook", RepoURL: "https://forge.example/a", Ref: "main"},
				{Name: "handbook", RepoURL: "https://forge.example/b", Ref: "main"},
			},
		},
		{
			name:  "relative path escapes the workspace",
			repos: []orchestration.ContextRepo{{Name: "..", RepoURL: "https://forge.example/a", Ref: "main"}},
		},
		{
			name:  "path separator in the name",
			repos: []orchestration.ContextRepo{{Name: "a/b", RepoURL: "https://forge.example/a", Ref: "main"}},
		},
		{
			name:  "empty repo url",
			repos: []orchestration.ContextRepo{{Name: "handbook", RepoURL: "", Ref: "main"}},
		},
		{
			name:  "empty ref",
			repos: []orchestration.ContextRepo{{Name: "handbook", RepoURL: "https://forge.example/a", Ref: ""}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newAutonomyProjectService()
			_, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
				TenantID: "tenant-1", Name: "api", ContextRepos: tc.repos,
			})
			requireProjectInvalidField(t, err, "context_repos")
			if !errors.Is(err, orchestration.ErrContextRepoInvalid) {
				t.Errorf("error does not carry ErrContextRepoInvalid: %v", err)
			}
			var domain *orchestration.ContextRepoInvalidError
			if !errors.As(err, &domain) {
				t.Errorf("error does not carry *ContextRepoInvalidError: %v", err)
			}
		})
	}
}

// TestProjectService_UpdateProject_AutonomousIsTriState: nil leaves the switch
// alone, and a pointer to either value sets it.
func TestProjectService_UpdateProject_AutonomousIsTriState(t *testing.T) {
	svc := newAutonomyProjectService()
	created, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID: "tenant-1", Name: "api", Autonomous: true,
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// An unrelated update leaves it on.
	updated, err := svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID: "tenant-1", ProjectID: created.Project.ID, Description: "now with agents",
	})
	if err != nil {
		t.Fatalf("UpdateProject (unchanged): %v", err)
	}
	if !updated.Project.Autonomous {
		t.Error("Autonomous = false after an update that did not name it, want true")
	}

	// An explicit false turns it off.
	off := false
	updated, err = svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID: "tenant-1", ProjectID: created.Project.ID, Autonomous: &off,
	})
	if err != nil {
		t.Fatalf("UpdateProject (off): %v", err)
	}
	if updated.Project.Autonomous {
		t.Error("Autonomous = true after an explicit false, want false")
	}

	// And an explicit true turns it back on.
	on := true
	updated, err = svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID: "tenant-1", ProjectID: created.Project.ID, Autonomous: &on,
	})
	if err != nil {
		t.Fatalf("UpdateProject (on): %v", err)
	}
	if !updated.Project.Autonomous {
		t.Error("Autonomous = false after an explicit true, want true")
	}
}

// TestProjectService_UpdateProject_ContextReposReplaceAndClear: a non-nil list
// replaces the whole list; ClearContextRepos empties it.
func TestProjectService_UpdateProject_ContextReposReplaceAndClear(t *testing.T) {
	svc := newAutonomyProjectService()
	created, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID: "tenant-1", Name: "api", ContextRepos: handbookRepos(),
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// A nil list leaves the existing repositories alone.
	unchanged, err := svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID: "tenant-1", ProjectID: created.Project.ID, Description: "unrelated",
	})
	if err != nil {
		t.Fatalf("UpdateProject (unchanged): %v", err)
	}
	if len(unchanged.Project.ContextRepos) != 1 {
		t.Fatalf("ContextRepos len = %d after an unrelated update, want 1",
			len(unchanged.Project.ContextRepos))
	}

	// A non-nil list replaces, it does not append.
	replaced, err := svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID:  "tenant-1",
		ProjectID: created.Project.ID,
		ContextRepos: []orchestration.ContextRepo{
			{Name: "specs", RepoURL: "https://forge.example/org/specs", Ref: "v2"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateProject (replace): %v", err)
	}
	if len(replaced.Project.ContextRepos) != 1 || replaced.Project.ContextRepos[0].Name != "specs" {
		t.Fatalf("ContextRepos = %+v, want exactly the specs repo", replaced.Project.ContextRepos)
	}

	// Clearing empties the list.
	cleared, err := svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID: "tenant-1", ProjectID: created.Project.ID, ClearContextRepos: true,
	})
	if err != nil {
		t.Fatalf("UpdateProject (clear): %v", err)
	}
	if len(cleared.Project.ContextRepos) != 0 {
		t.Fatalf("ContextRepos = %+v after clear, want none", cleared.Project.ContextRepos)
	}

	// The clear survives a re-read.
	got, err := svc.GetProject(context.Background(), &GetProjectRequest{
		TenantID: "tenant-1", ProjectID: created.Project.ID,
	})
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if len(got.Project.ContextRepos) != 0 {
		t.Fatalf("stored ContextRepos = %+v after clear, want none", got.Project.ContextRepos)
	}
}

// TestProjectService_UpdateProject_ClearWinsOverAReplacement pins the
// precedence ClearDefaultWorkflow already sets: an explicit clear beats a list
// sent in the same request.
func TestProjectService_UpdateProject_ClearWinsOverAReplacement(t *testing.T) {
	svc := newAutonomyProjectService()
	created, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID: "tenant-1", Name: "api", ContextRepos: handbookRepos(),
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	updated, err := svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID:          "tenant-1",
		ProjectID:         created.Project.ID,
		ContextRepos:      handbookRepos(),
		ClearContextRepos: true,
	})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if len(updated.Project.ContextRepos) != 0 {
		t.Fatalf("ContextRepos = %+v, want none when clear is set", updated.Project.ContextRepos)
	}
}

// TestProjectService_UpdateProject_RefusesInvalidContextRepos: the update path
// validates too, so a bad list cannot reach the store through it.
func TestProjectService_UpdateProject_RefusesInvalidContextRepos(t *testing.T) {
	svc := newAutonomyProjectService()
	created, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID: "tenant-1", Name: "api", ContextRepos: handbookRepos(),
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	_, err = svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID:  "tenant-1",
		ProjectID: created.Project.ID,
		ContextRepos: []orchestration.ContextRepo{
			{Name: "specs", RepoURL: "https://forge.example/org/specs", Ref: ""},
		},
	})
	requireProjectInvalidField(t, err, "context_repos")
	if !errors.Is(err, orchestration.ErrContextRepoInvalid) {
		t.Errorf("error does not carry ErrContextRepoInvalid: %v", err)
	}

	// The refused update left the stored list untouched.
	got, err := svc.GetProject(context.Background(), &GetProjectRequest{
		TenantID: "tenant-1", ProjectID: created.Project.ID,
	})
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if len(got.Project.ContextRepos) != 1 || got.Project.ContextRepos[0].Name != "handbook" {
		t.Fatalf("stored ContextRepos = %+v, want the original handbook entry", got.Project.ContextRepos)
	}
}
