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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func TestRoleCharters_TheSDLCRolesStartWithTheirCharters(t *testing.T) {
	cases := map[orchestration.AgentRole]string{
		orchestration.AgentRoleCoding: WorkerCharter(sdlcPrompts),
		orchestration.AgentRoleReview: ReviewerCharter(sdlcPrompts),
		orchestration.AgentRoleTriage: TriageCharter(),
	}
	for role, want := range cases {
		got, ok := orchestration.RoleCharter(role)
		if !ok || got != want || got == "" {
			t.Fatalf("RoleCharter(%s) = %q, %v; want the role's non-empty charter", role, got, ok)
		}
	}
}

func TestRoleCharters_RolesTheSDLCDoesNotRunHaveNone(t *testing.T) {
	for _, role := range []orchestration.AgentRole{orchestration.AgentRoleLead, orchestration.AgentRoleGeneral, orchestration.AgentRoleProjectManager} {
		if got, ok := orchestration.RoleCharter(role); ok {
			t.Fatalf("RoleCharter(%s) = %q; the SDLC registers no charter for it", role, got)
		}
	}
}
