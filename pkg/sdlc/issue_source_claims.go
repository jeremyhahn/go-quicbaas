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
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Pull-request claims on issues, across sweeps
// =============================================================================
//
// One sweep's open pull requests say which issues are already being closed,
// and ONLY AN OPEN PULL REQUEST HOLDS AN ISSUE. When the pull request leaves
// the open listing, the issue is offered again on the next sweep, whichever
// way it left:
//
//   - CLOSED WITHOUT MERGING. Nobody is delivering the issue any more. This is
//     the re-evaluation go-quicbaas#1054 asks for.
//   - MERGED. A merged change is history, not a claim. An issue that is still
//     open and still carries the trigger label after its change merged is one
//     a person reopened or re-triggered because the fix was incomplete or
//     reverted, and holding it for the old pull request would mean it is never
//     adopted again. The duplicate the incident produced came from two changes
//     under review at once, which only an open pull request can cause.
//
// So the answer needs no reads beyond the listing, and nothing is carried
// between sweeps except which skips were already audited (claimMemory).

// AdoptionSkip is one issue the source declined to offer because a pull
// request already closes it.
type AdoptionSkip struct {
	TenantID  string
	ProjectID string

	// Issue is the issue's provenance reference, owner/repo#number.
	Issue string

	// PullRequest is the pull request that closes it.
	PullRequest int
}

// AdoptionAuditor records the issues the source declines to adopt.
//
// A port, so the source writes to the platform's audit trail without importing
// it. The server binds it to the same audit logger the orchestration stores use.
type AdoptionAuditor interface {
	// RecordAdoptionSkip appends one skip to the audit trail. It must not
	// block the sweep.
	RecordAdoptionSkip(ctx context.Context, skip AdoptionSkip)
}

// claimMemory is the per-project record of which issues were skipped for which
// pull request on the last sweep. It decides only what is audited afresh; it
// never holds an issue. PER PROCESS: after a restart each standing skip is
// audited once more.
type claimMemory struct {
	mu       sync.Mutex
	projects map[string]map[int]ClosingClaim
}

func projectKey(project *orchestration.Project) string {
	return project.TenantID + "/" + project.ID
}

func (m *claimMemory) held(project *orchestration.Project) map[int]ClosingClaim {
	m.mu.Lock()
	defer m.mu.Unlock()
	held := make(map[int]ClosingClaim, len(m.projects[projectKey(project)]))
	for issue, claim := range m.projects[projectKey(project)] {
		held[issue] = claim
	}
	return held
}

func (m *claimMemory) replace(project *orchestration.Project, claims map[int]ClosingClaim) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.projects == nil {
		m.projects = map[string]map[int]ClosingClaim{}
	}
	if len(claims) == 0 {
		delete(m.projects, projectKey(project))
		return
	}
	m.projects[projectKey(project)] = claims
}

// SetAdoptionAuditor supplies the audit sink skips are recorded on. A nil
// argument is ignored, for the reason SetLiveWorkReader gives.
func (s *IssueSource) SetAdoptionAuditor(auditor AdoptionAuditor) {
	if auditor == nil {
		return
	}
	s.auditor = auditor
}

// closingClaims reads which issues an open pull request already closes, from
// its closing references. It is one listing per sweep.
func closingClaims(ctx context.Context, forge ForgeBackend, repo RepoRef, repoURL string) (map[int]ClosingClaim, error) {
	pulls, err := forge.ListOpenPullRequests(ctx, repo)
	if err != nil {
		return nil, err
	}
	claims := map[int]ClosingClaim{}
	for issue, number := range closingPullRequests(repo, repoURL, pulls) {
		claims[issue] = ClosingClaim{PullRequest: number}
	}
	return claims, nil
}

// settleClaims records this sweep's skips. A skip that is new, or now names a
// different pull request, is audited, so the trail carries one record per
// claim rather than one per sweep. Only claims on issues this sweep skipped
// are remembered: a claim that lapsed (its pull request left the open listing)
// or whose issue closed or lost its label is forgotten, and a later claim on
// the same issue is audited afresh.
func (s *IssueSource) settleClaims(ctx context.Context, project *orchestration.Project, repo RepoRef, skipped map[int]ClosingClaim) {
	prior := s.claims.held(project)
	for issue, claim := range skipped {
		if held, ok := prior[issue]; ok && held == claim {
			continue
		}
		if s.auditor == nil {
			continue
		}
		s.auditor.RecordAdoptionSkip(ctx, AdoptionSkip{
			TenantID: project.TenantID, ProjectID: project.ID,
			Issue: IssueRefString(repo, issue), PullRequest: claim.PullRequest,
		})
	}
	s.claims.replace(project, skipped)
}
