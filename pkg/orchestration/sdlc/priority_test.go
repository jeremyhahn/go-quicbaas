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

import "testing"

// TestComputedFor_CriticalOutranksBlastRadius is #1129's acceptance: an
// owner-set priority:critical in a project nothing depends on comes before an
// unlabelled issue in a heavily depended-on project.
func TestComputedFor_CriticalOutranksBlastRadius(t *testing.T) {
	critical := PriorityInputs{BlastRadius: 0}.ComputedFor([]string{"priority:critical"})
	unlabelled := PriorityInputs{BlastRadius: 40}.ComputedFor(nil)
	if critical <= unlabelled {
		t.Fatalf("critical in a leaf project = %d, unlabelled in a 40-dependent project = %d; want critical first", critical, unlabelled)
	}
}

// TestComputedFor_LowSortsBelowNormalEvenAtTheFloor: priority:low must rank
// below an unlabelled issue even when that issue is already at the floor.
func TestComputedFor_LowSortsBelowNormalEvenAtTheFloor(t *testing.T) {
	in := PriorityInputs{}
	if low, normal := in.ComputedFor([]string{"priority:low"}), in.ComputedFor(nil); low >= normal {
		t.Fatalf("low = %d, normal = %d; want low below normal", low, normal)
	}
}

// TestComputedFor_NoLabelIsExactlyComputed: an issue without a priority label
// is ranked exactly as before #1129, whatever else it is labelled.
func TestComputedFor_NoLabelIsExactlyComputed(t *testing.T) {
	for _, in := range []PriorityInputs{{}, {BlastRadius: 3, ProjectDefault: 7}, {BlastRadius: 500, ProjectDefault: 900}} {
		for _, labels := range [][]string{nil, {"bug", "sdlc:ready"}, {"priority:normal"}} {
			if got, want := in.ComputedFor(labels), in.Computed(); got != want {
				t.Fatalf("ComputedFor(%v) with %+v = %d, want Computed = %d", labels, in, got, want)
			}
		}
	}
}

// TestComputedFor_TheMostUrgentTierWins: an issue carrying two tiers is ranked
// by the more urgent, and matching ignores case and padding.
func TestComputedFor_TheMostUrgentTierWins(t *testing.T) {
	in := PriorityInputs{BlastRadius: 2}
	both := in.ComputedFor([]string{"priority:low", " Priority:High "})
	high := in.ComputedFor([]string{"priority:high"})
	if both != high {
		t.Fatalf("low+high = %d, high alone = %d; want the high tier", both, high)
	}
	if critical := in.ComputedFor([]string{"priority:critical", "priority:high"}); critical <= high {
		t.Fatalf("critical+high = %d, want above high = %d", critical, high)
	}
}

// TestComputedFor_StaysBounded: a label shift never overflows the band a
// labelled priority may occupy.
func TestComputedFor_StaysBounded(t *testing.T) {
	top := PriorityInputs{BlastRadius: ComputedCeiling, ProjectDefault: ComputedCeiling}.ComputedFor([]string{"priority:critical"})
	if top != ComputedCeiling+PriorityShiftCritical {
		t.Fatalf("critical at the ceiling = %d, want %d", top, ComputedCeiling+PriorityShiftCritical)
	}
	bottom := PriorityInputs{ProjectDefault: -ComputedCeiling}.ComputedFor([]string{"priority:low"})
	if bottom != ComputedFloor+PriorityShiftLow {
		t.Fatalf("low at the floor = %d, want %d", bottom, ComputedFloor+PriorityShiftLow)
	}
}
