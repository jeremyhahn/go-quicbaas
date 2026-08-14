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

//go:build integration

package helpers

import "testing"

// TestIsDominantLeaderStable_GapHandling pins the property that decides whether
// VerifyLeaderStability is a useful signal or a flake generator: a check in
// which no leader could be resolved is recorded as noLeaderObserved rather than
// aborting the run, and it must count AGAINST stability without ever being
// mistaken for a leader.
//
// The two directions matter equally. Absorbing a one-check election gap is what
// stops a rebalance-induced leader handover from failing the suite; still
// failing on a sustained or trailing run of gaps is what keeps a genuine loss of
// leader availability detectable.
func TestIsDominantLeaderStable_GapHandling(t *testing.T) {
	const (
		nodeA = uint64(1)
		nodeB = uint64(2)
		gap   = noLeaderObserved
	)

	cases := []struct {
		name    string
		history []uint64
		want    bool
		leader  uint64
	}{
		{
			name:    "steady leader",
			history: []uint64{nodeA, nodeA, nodeA, nodeA, nodeA, nodeA},
			want:    true,
			leader:  nodeA,
		},
		{
			name:    "single election gap is absorbed",
			history: []uint64{nodeA, nodeA, gap, nodeA, nodeA, nodeA, nodeA, nodeA},
			want:    true,
			leader:  nodeA,
		},
		{
			name:    "handover through a gap is absorbed once B settles",
			history: []uint64{nodeA, gap, nodeB, nodeB, nodeB, nodeB, nodeB, nodeB, nodeB, nodeB},
			want:    true,
			leader:  nodeB,
		},
		{
			name:    "sustained loss of leadership fails",
			history: []uint64{nodeA, gap, gap, gap, gap, gap, gap},
			want:    false,
		},
		{
			name:    "trailing gaps fail even with a historically dominant leader",
			history: []uint64{nodeA, nodeA, nodeA, nodeA, nodeA, nodeA, gap, gap, gap, gap, gap, gap},
			want:    false,
		},
		{
			name:    "a gap never wins the dominance vote",
			history: []uint64{gap, gap, gap, gap, nodeA, nodeA, nodeA, nodeA, nodeA, nodeA, nodeA, nodeA, nodeA},
			want:    true,
			leader:  nodeA,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			leader, stable := isDominantLeaderStable(tc.history, minStableChecks)
			if stable != tc.want {
				t.Fatalf("isDominantLeaderStable(%v) stable = %v, want %v", tc.history, stable, tc.want)
			}
			if tc.want && leader != tc.leader {
				t.Fatalf("isDominantLeaderStable(%v) leader = %d, want %d", tc.history, leader, tc.leader)
			}
			if !tc.want && leader != 0 {
				t.Fatalf("unstable history must report no leader, got %d", leader)
			}
		})
	}
}

// TestIsDominantLeaderStable_TooFewChecks keeps the minimum-observation guard:
// a window shorter than minStableChecks can never be declared stable, so a
// verifier that barely ran cannot report success.
func TestIsDominantLeaderStable_TooFewChecks(t *testing.T) {
	if _, stable := isDominantLeaderStable([]uint64{1, 1}, minStableChecks); stable {
		t.Fatal("a history shorter than minStableChecks must not be stable")
	}
}
