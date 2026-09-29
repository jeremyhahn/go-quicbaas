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

	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// =============================================================================
// Operator verbs on a bound project's forge: filing issues, listing the
// accounts that may rule
// =============================================================================
//
// Both act through the BINDING'S forge adapter and token, so the Software
// Factory is run through QuicBaaS itself rather than by an operator reaching
// the forge around it. Filing is an audited write gated sdlc:issue_create on
// every transport; the collaborator listing is a read gated sdlc:read, and is
// what the binding editor offers as the ruler picker.

// FileIssue files an issue on the project's bound repository and returns it
// as the forge stored it. Every attempt that reaches the forge, and every
// refusal of a well-addressed request, is audited with its outcome.
func (s *SDLCService) FileIssue(ctx context.Context, tenantID, projectID string, filing sdlc.IssueFiling) (*sdlc.ForgeIssue, error) {
	if tenantID == "" {
		return nil, &sdlc.InvalidInputError{Field: "tenant_id", Message: "issues are filed on a tenant's bound project"}
	}
	if projectID == "" {
		return nil, &sdlc.InvalidInputError{Field: "project_id", Message: "must name the bound project to file on"}
	}
	if s.auditor == nil {
		return nil, &sdlc.AuditUnavailableError{Operation: "file issue"}
	}
	binding, forge, err := s.boundForge(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	issue, err := sdlc.FileIssue(ctx, forge, binding.Repo(), filing)
	event := sdlc.IssueFiledEvent{
		TenantID: tenantID, ProjectID: projectID, Actor: callerName(ctx),
		Repo: binding.Repo(), Filing: filing, Err: err,
	}
	if issue != nil {
		event.Number = issue.Number
	}
	s.auditor.RecordIssueFiled(ctx, event)
	if err != nil {
		return nil, err
	}
	return issue, nil
}

// Collaborators returns the accounts that may rule on the project's bound
// repository: its owner first, then every collaborator. It changes nothing.
func (s *SDLCService) Collaborators(ctx context.Context, tenantID, projectID string) ([]sdlc.ForgeUser, error) {
	if tenantID == "" {
		return nil, &sdlc.InvalidInputError{Field: "tenant_id", Message: "collaborators are read from a tenant's bound project"}
	}
	if projectID == "" {
		return nil, &sdlc.InvalidInputError{Field: "project_id", Message: "must name the bound project"}
	}
	binding, forge, err := s.boundForge(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	return sdlc.Collaborators(ctx, forge, binding.Repo())
}

// boundForge reads the project's binding and builds its forge adapter.
func (s *SDLCService) boundForge(ctx context.Context, tenantID, projectID string) (*sdlc.ProjectBinding, sdlc.ForgeBackend, error) {
	binding, err := s.store.GetBinding(ctx, tenantID, projectID)
	if err != nil {
		return nil, nil, err
	}
	forge, err := s.ForgeFor(ctx, tenantID, binding.Forge)
	if err != nil {
		return nil, nil, err
	}
	return binding, forge, nil
}

// checkRulers refuses a binding naming a ruler the bound repository grants
// no access to. A binding with no rulers of its own reads nothing from the
// forge, so every binding written before rulers existed binds exactly as
// before.
func (s *SDLCService) checkRulers(ctx context.Context, tenantID string, binding *sdlc.ProjectBinding) error {
	if len(binding.Rulers) == 0 {
		return nil
	}
	forge, err := s.ForgeFor(ctx, tenantID, binding.Forge)
	if err != nil {
		return err
	}
	users, err := sdlc.Collaborators(ctx, forge, binding.Repo())
	if err != nil {
		return err
	}
	return sdlc.CheckRulers(binding.Rulers, users)
}
