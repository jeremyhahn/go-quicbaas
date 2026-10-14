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
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// recordingIssueAuditor records every filing the service audits.
type recordingIssueAuditor struct {
	mu     sync.Mutex
	events []sdlc.IssueFiledEvent
}

func (a *recordingIssueAuditor) RecordIssueFiled(_ context.Context, event sdlc.IssueFiledEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, event)
}

func (a *recordingIssueAuditor) recorded() []sdlc.IssueFiledEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]sdlc.IssueFiledEvent(nil), a.events...)
}

// boundRig returns a rig whose fixture project is bound.
func boundRig(t *testing.T) *sdlcServiceRig {
	t.Helper()
	rig := newSDLCServiceRig(t)
	if _, err := rig.svc.Bind(context.Background(), sdlcTestTenant, rig.binding(), BindOptions{}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	return rig
}

func operatorContext() context.Context {
	return auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: "u-1", Username: "lead", Authenticated: true})
}

func TestSDLCService_FileIssueFilesAndAudits(t *testing.T) {
	rig := boundRig(t)
	rig.forge.PutIssue(12, "the parent")

	issue, err := rig.svc.FileIssue(operatorContext(), sdlcTestTenant, sdlcTestProject, sdlc.IssueFiling{
		Title: "Follow-up", Body: "The rest.", Labels: []string{"task:ready"}, Parent: 12,
	})
	if err != nil {
		t.Fatalf("FileIssue: %v", err)
	}
	stored, ok := rig.forge.Issue(issue.Number)
	if !ok || stored.Body != "The rest.\n\nParent: #12" || len(stored.Labels) != 1 || stored.Labels[0].Name != "task:ready" {
		t.Fatalf("stored = %+v, %v; want the follow-up linked to #12 and labelled", stored, ok)
	}
	events := rig.audit.recorded()
	if len(events) != 1 || events[0].Actor != "lead" || events[0].Number != issue.Number || events[0].Err != nil ||
		events[0].Repo.String() != "acme/api" {
		t.Fatalf("audit = %+v, want one successful filing by lead", events)
	}
}

func TestSDLCService_FileIssueAuditsARefusal(t *testing.T) {
	rig := boundRig(t)
	_, err := rig.svc.FileIssue(operatorContext(), sdlcTestTenant, sdlcTestProject, sdlc.IssueFiling{Title: "t", Parent: 404})
	if !errors.Is(err, sdlc.ErrInvalidInput) {
		t.Fatalf("FileIssue = %v, want ErrInvalidInput for a missing parent", err)
	}
	if events := rig.audit.recorded(); len(events) != 1 || events[0].Err == nil || events[0].Number != 0 {
		t.Fatalf("audit = %+v, want the refusal recorded", events)
	}
	if rig.forge.IssueCount() != 0 {
		t.Fatal("an issue was filed for a refused request")
	}
}

func TestSDLCService_FileIssueRefusesBadAddresses(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := operatorContext()
	cases := map[string]struct {
		tenant, project string
		want            error
	}{
		"no tenant":           {"", sdlcTestProject, sdlc.ErrInvalidInput},
		"no project":          {sdlcTestTenant, "", sdlc.ErrInvalidInput},
		"an unbound project":  {sdlcTestTenant, sdlcTestProject, sdlc.ErrNotFound},
		"another tenant's id": {"tenant-b", sdlcTestProject, sdlc.ErrNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := rig.svc.FileIssue(ctx, tc.tenant, tc.project, sdlc.IssueFiling{Title: "t"}); !errors.Is(err, tc.want) {
				t.Fatalf("FileIssue = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSDLCService_FileIssueRefusesUnaudited(t *testing.T) {
	rig := boundRig(t)
	unaudited, err := NewSDLCService(SDLCServiceConfig{
		Store: rig.store, Routes: rig.svc.routes, Projects: rig.projects, ProjectWriter: rig.projects,
		Secrets: rig.secrets, WebhookPublicURL: sdlcTestPublicURL, Tasks: rig.agents, Agents: rig.agentCatalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	var refused *sdlc.AuditUnavailableError
	if _, err := unaudited.FileIssue(operatorContext(), sdlcTestTenant, sdlcTestProject, sdlc.IssueFiling{Title: "t"}); !errors.As(err, &refused) {
		t.Fatalf("FileIssue = %v, want AuditUnavailableError", err)
	}
	if rig.forge.IssueCount() != 0 {
		t.Fatal("an unaudited filing reached the forge")
	}
}

func TestSDLCService_CollaboratorsListsTheOwnerAndCollaborators(t *testing.T) {
	rig := boundRig(t)
	rig.forge.PutCollaborator("lead-agent")
	users, err := rig.svc.Collaborators(context.Background(), sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatalf("Collaborators: %v", err)
	}
	if len(users) != 2 || users[0].Login != "acme" || users[1].Login != "lead-agent" {
		t.Fatalf("users = %+v, want [acme lead-agent]", users)
	}
}

func TestSDLCService_CollaboratorsRefusesBadInput(t *testing.T) {
	rig := newSDLCServiceRig(t)
	if _, err := rig.svc.Collaborators(context.Background(), "", sdlcTestProject); !errors.Is(err, sdlc.ErrInvalidInput) {
		t.Fatalf("Collaborators(no tenant) = %v, want ErrInvalidInput", err)
	}
	if _, err := rig.svc.Collaborators(context.Background(), sdlcTestTenant, ""); !errors.Is(err, sdlc.ErrInvalidInput) {
		t.Fatalf("Collaborators(no project) = %v, want ErrInvalidInput", err)
	}
	if _, err := rig.svc.Collaborators(context.Background(), sdlcTestTenant, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("Collaborators(unbound) = %v, want ErrNotFound", err)
	}
}

func TestSDLCService_BindAdmitsACollaboratorAsRuler(t *testing.T) {
	rig := newSDLCServiceRig(t)
	rig.forge.PutCollaborator("lead-agent")
	binding := rig.binding()
	binding.Rulers = []string{"lead-agent", "acme"}
	bound, err := rig.svc.Bind(context.Background(), sdlcTestTenant, binding, BindOptions{})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if len(bound.Rulers) != 2 {
		t.Fatalf("rulers = %v, want both kept", bound.Rulers)
	}
}

func TestSDLCService_BindRefusesARulerWithoutAccess(t *testing.T) {
	rig := newSDLCServiceRig(t)
	binding := rig.binding()
	binding.Rulers = []string{"stranger"}
	var refused *sdlc.RulerNotCollaboratorError
	if _, err := rig.svc.Bind(context.Background(), sdlcTestTenant, binding, BindOptions{}); !errors.As(err, &refused) || refused.Login != "stranger" {
		t.Fatalf("Bind = %v, want RulerNotCollaboratorError naming stranger", err)
	}
	if _, err := rig.svc.Binding(context.Background(), sdlcTestTenant, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("a binding with a refused ruler was stored: %v", err)
	}
}

func TestSDLCService_BindRefusesRulersWhenTheForgeCannotSay(t *testing.T) {
	rig := newSDLCServiceRig(t)
	rig.forge.ForceStatus("GET", "/collaborators", 403, "forbidden")
	binding := rig.binding()
	binding.Rulers = []string{"lead-agent"}
	if _, err := rig.svc.Bind(context.Background(), sdlcTestTenant, binding, BindOptions{}); err == nil {
		t.Fatal("Bind admitted a ruler the forge could not confirm")
	}
}
