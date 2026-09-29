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

package grpc

import (
	"slices"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// go-quicbaas#653 — the agent grant must cover the gRPC methods a daemon calls,
// and nothing more.
//
// The grant is a literal list in pkg/iam; the requirement is a literal table
// here. These tests are the join, so a rename on either side surfaces as a
// failing test instead of a fleet that enrolls and is then refused.
// =============================================================================

// agentDaemonMethods are the methods an enrolled fleet node daemon calls on the
// central: the heartbeat that keeps it dispatchable, the bidi stream it
// forwards session events on, and the three pooled-account verbs it resolves a
// provider credential with on the host that runs the agent (go-quicbaas#734).
//
// The account verbs are here rather than in the negative list below because a
// daemon that cannot reach the pool refuses every pooled spawn — the
// `no_account_resolver` failure #734 recorded. They are still narrow: two
// metadata reads and one release, all tenant-scoped, none of them a write to
// the pool. The session reservation pair (go-quicbaas#1047) rides the
// release's permission: a daemon claims the slot its release will be used
// in, and gives it back when the spawn fails. Neither changes the pool.
var agentDaemonMethods = []string{
	"/quicbaas.orchestrator.v1.OrchestratorService/FleetNodeHeartbeat",
	"/quicbaas.orchestrator.v1.OrchestratorService/PushEvents",
	"/quicbaas.account.v1.AccountService/GetAccount",
	"/quicbaas.account.v1.AccountService/ListAccounts",
	"/quicbaas.account.v1.AccountService/GetAccountUsage",
	"/quicbaas.account.v1.AccountService/MaterializeAccountCredential",
	"/quicbaas.account.v1.AccountService/ReserveAccountSession",
	"/quicbaas.account.v1.AccountService/ReleaseAccountSession",
}

// TestAgentRuntimeGrant_CoversEveryMethodADaemonCalls pins the grant against
// the enforcement table.
func TestAgentRuntimeGrant_CoversEveryMethodADaemonCalls(t *testing.T) {
	grant := iam.FleetRuntimeGrant()
	for _, method := range agentDaemonMethods {
		perm, ok := grpcMethodPermissions[method]
		if !ok {
			t.Fatalf("%s has no permission entry", method)
		}
		required := perm.resource + ":" + perm.action
		if !slices.Contains(grant, required) {
			t.Errorf("an enrolled agent may not call %s: the grant %v omits %q",
				method, grant, required)
		}
	}
}

// TestAgentRuntimeGrant_CoversNothingElseOnTheOrchestrator is the negative
// half: every OTHER method must remain closed to an agent runtime. An agent
// must not be able to administer the platform that runs it — nor reshape the
// account pool it draws from — so a widened grant fails here rather than
// shipping.
func TestAgentRuntimeGrant_CoversNothingElseOnTheOrchestrator(t *testing.T) {
	grant := iam.FleetRuntimeGrant()
	for method, perm := range grpcMethodPermissions {
		if slices.Contains(agentDaemonMethods, method) {
			continue
		}
		required := perm.resource + ":" + perm.action
		if slices.Contains(grant, required) {
			t.Errorf("the agent runtime grant admits %s (%q); an agent runtime must not hold it",
				method, required)
		}
	}
}
