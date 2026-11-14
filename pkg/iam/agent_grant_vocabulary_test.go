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

package iam_test

import (
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	quicbaaspermission "github.com/jeremyhahn/go-quicbaas/pkg/permission"
)

// =============================================================================
// The agent runtime grant is spelled in pkg/iam and DEFINED in pkg/permission.
//
// pkg/iam deliberately does not import the permission package (see
// fleet_registrar.go), so the two spellings are restated literals that could
// drift. A drift is silent and total: the daemon would be enrolled holding a
// permission no route resolves and no registry admits, and every call it
// guards would be refused with no signal naming the typo.
//
// This file lives in package iam_test so it may import pkg/permission without
// putting that dependency on the production package.
// =============================================================================

// TestAgentRuntimeGrant_MatchesTheContributedVocabulary pins the credential-plane
// literal against the permission it names.
func TestAgentRuntimeGrant_MatchesTheContributedVocabulary(t *testing.T) {
	if iam.FleetScopeAccountMaterialize != string(quicbaaspermission.AgentAccountMaterialize) {
		t.Fatalf("iam.FleetScopeAccountMaterialize = %q, permission.AgentAccountMaterialize = %q; the two spellings must agree",
			iam.FleetScopeAccountMaterialize, quicbaaspermission.AgentAccountMaterialize)
	}
}

// TestAgentRuntimeScopes_MatchTheContributedVocabulary pins the agent
// runtime verbs the spawn server checks to the ones pkg/permission
// registers (SPEC Authorization clean-up D6).
func TestAgentRuntimeScopes_MatchTheContributedVocabulary(t *testing.T) {
	pairs := map[string]qrdbsdk.Permission{
		orchestration.AgentScopeRuntimeSpawn:  quicbaaspermission.AgentRuntimeSpawn,
		orchestration.AgentScopeRuntimePrompt: quicbaaspermission.AgentRuntimePrompt,
		orchestration.AgentScopeRuntimeStatus: quicbaaspermission.AgentRuntimeStatus,
		orchestration.AgentScopeRuntimeInfer:  quicbaaspermission.AgentRuntimeInfer,
	}
	for scope, perm := range pairs {
		if scope != string(perm) {
			t.Errorf("orchestration spells %q, permission spells %q; the two must agree", scope, perm)
		}
	}
}

// TestAgentRuntimeGrant_IsRegisteredVocabulary asserts every permission the
// grant names is one qrdb's registry admits after QuicBaaS contributes its own.
// A role cannot hold an unregistered permission — qrdb's role store rejects it
// at write time — so an unregistered member would make enrollment itself fail.
func TestAgentRuntimeGrant_IsRegisteredVocabulary(t *testing.T) {
	quicbaaspermission.Register()

	// The agent:runtime:* verbs are registry members too (SPEC
	// Authorization clean-up D6), so the whole grant is checked, plus the
	// infer verb the spawn server also demands.
	for _, granted := range append(iam.FleetRuntimeGrant(), orchestration.FleetRuntimeScopes()...) {
		if !qrdbsdk.IsPermissionRegistered(qrdbsdk.Permission(granted)) {
			t.Errorf("agent runtime grant names %q, which no registry defines; "+
				"a role cannot hold it and every route it guards is unreachable", granted)
		}
	}
}
