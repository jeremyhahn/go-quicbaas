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
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
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

// TestAgentRuntimeGrant_IsRegisteredVocabulary asserts every permission the
// grant names is one qrdb's registry admits after QuicBaaS contributes its own.
// A role cannot hold an unregistered permission — qrdb's role store rejects it
// at write time — so an unregistered member would make enrollment itself fail.
func TestAgentRuntimeGrant_IsRegisteredVocabulary(t *testing.T) {
	quicbaaspermission.Register()

	for _, granted := range iam.FleetRuntimeGrant() {
		// The service-surface scopes (agent:runtime:*) are the daemon's OWN
		// spawn-server vocabulary, checked in-process by GRPCSpawnServer and
		// never presented to qrdb's PDP, so they are not registry members.
		if strings.HasPrefix(granted, "agent:runtime:") {
			continue
		}
		if !qrdbsdk.IsPermissionRegistered(qrdbsdk.Permission(granted)) {
			t.Errorf("agent runtime grant names %q, which no registry defines; "+
				"a role cannot hold it and every route it guards is unreachable", granted)
		}
	}
}
