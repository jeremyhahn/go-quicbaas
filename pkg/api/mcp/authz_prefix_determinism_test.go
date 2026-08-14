//go:build mcp

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

package mcp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Determinism guards for the legacy broker-vocabulary prefix map consulted by
// getToolResourceAction (the RequireAuthz=false AuthzService path).
//
// toolPrefixMapping deliberately contains overlapping pairs whose actions
// differ, so a first-match-wins loop over Go's randomised map range resolved
// them differently from run to run. For "qrdb_credential_delete" that
// intermittently downgraded a Delete decision to a Write.
// =============================================================================

// overlappingPrefixPairs are the pairs where one prefix contains another and the
// actions differ. The specific prefix must always win.
var overlappingPrefixPairs = []struct {
	tool     string
	resource string
	action   string
}{
	{"qrdb_barrier_status", interfaces.ResourceBarrier, interfaces.ActionRead},
	{"qrdb_barrier_recovery_status", interfaces.ResourceBarrier, interfaces.ActionRead},
	{"qrdb_barrier_root_token", interfaces.ResourceBarrier, interfaces.ActionAdmin},
	{"qrdb_barrier_rekey", interfaces.ResourceBarrier, interfaces.ActionAdmin},
	{"qrdb_ceremony_status", interfaces.ResourceCeremony, interfaces.ActionRead},
	{"qrdb_ceremony_initialize", interfaces.ResourceCeremony, interfaces.ActionAdmin},
	{"qrdb_ceremony_sign_csr", interfaces.ResourceCeremony, interfaces.ActionAdmin},
	{"qrdb_credential_delete", interfaces.ResourceSecrets, interfaces.ActionDelete},
	{"qrdb_credential_list", interfaces.ResourceSecrets, interfaces.ActionRead},
	{"qrdb_credential_retrieve", interfaces.ResourceSecrets, interfaces.ActionRead},
	{"qrdb_credential_strategy", interfaces.ResourceSecrets, interfaces.ActionRead},
	{"qrdb_credential_submit", interfaces.ResourceSecrets, interfaces.ActionWrite},
	{"qrdb_vector_batch_delete", interfaces.ResourceVector, interfaces.ActionDelete},
}

// TestGetToolResourceAction_LongestPrefixWins pins the specific prefix as the
// winner for every overlapping pair. Under first-match-wins these assertions
// pass only by luck of the map's randomised range order.
func TestGetToolResourceAction_LongestPrefixWins(t *testing.T) {
	for _, tc := range overlappingPrefixPairs {
		t.Run(tc.tool, func(t *testing.T) {
			resource, action := getToolResourceAction(tc.tool)
			assert.Equal(t, tc.resource, resource, "tool %q resource", tc.tool)
			assert.Equal(t, tc.action, action, "tool %q action", tc.tool)
		})
	}
}

// TestGetToolResourceAction_StableAcrossIterations calls the resolver repeatedly
// for each overlapping tool and asserts the answer never changes. Go randomises
// map iteration order per range statement, so a non-deterministic resolver shows
// up here as two different decisions for the same tool within one test run.
func TestGetToolResourceAction_StableAcrossIterations(t *testing.T) {
	const iterations = 500

	for _, tc := range overlappingPrefixPairs {
		firstResource, firstAction := getToolResourceAction(tc.tool)
		for i := 0; i < iterations; i++ {
			resource, action := getToolResourceAction(tc.tool)
			require.Equalf(t, firstResource+":"+firstAction, resource+":"+action,
				"tool %q resolved to two different permissions across iterations "+
					"(iteration %d) — the prefix resolution is order-dependent", tc.tool, i)
		}
	}
}

// TestToolPrefixMappingOverlapsAreCovered is the reverse guard: it derives every
// overlapping prefix pair whose actions differ straight from the live map and
// requires each to be pinned by overlappingPrefixPairs. A new overlapping entry
// therefore cannot ship without a determinism assertion.
func TestToolPrefixMappingOverlapsAreCovered(t *testing.T) {
	pinned := make(map[string]struct{}, len(overlappingPrefixPairs))
	for _, tc := range overlappingPrefixPairs {
		pinned[tc.tool] = struct{}{}
	}

	for long, longMapping := range toolPrefixMapping {
		for short, shortMapping := range toolPrefixMapping {
			if long == short || !strings.HasPrefix(long, short) {
				continue
			}
			if longMapping == shortMapping {
				continue
			}
			if _, ok := pinned[long]; !ok {
				t.Errorf("prefix %q is contained in %q with a DIFFERENT permission "+
					"(%s:%s vs %s:%s) but is not pinned in overlappingPrefixPairs; add it "+
					"so the resolution order stays asserted",
					long, short, longMapping.resource, longMapping.action,
					shortMapping.resource, shortMapping.action)
			}
		}
	}
}
