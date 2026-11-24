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

// TestComputedForIssue_ReleaseLabelOutranksCriticalAtTheCeiling: a release
// label lifts an issue at the floor above a priority:critical issue at the
// ceiling, and release-candidate counts the same as release-blocker.
func TestComputedForIssue_ReleaseLabelOutranksCriticalAtTheCeiling(t *testing.T) {
	critical := PriorityInputs{BlastRadius: ComputedCeiling, ProjectDefault: ComputedCeiling}.
		ComputedForIssue(&ForgeIssue{Labels: []string{"priority:critical"}})
	floor := PriorityInputs{}
	cases := []struct {
		name   string
		labels []string
	}{
		{"release-blocker", []string{"release-blocker"}},
		{"release-candidate", []string{"release-candidate"}},
		{"padded and cased", []string{" Release-Blocker "}},
		{"with high", []string{"priority:high", "release-candidate"}},
	}
	blocker := floor.ComputedForIssue(&ForgeIssue{Labels: []string{"release-blocker"}})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := floor.ComputedForIssue(&ForgeIssue{Labels: tc.labels})
			if got <= critical {
				t.Fatalf("%v at the floor = %d, want above critical at the ceiling %d", tc.labels, got, critical)
			}
			if len(tc.labels) == 1 && got != blocker {
				t.Fatalf("%v = %d, want the same as release-blocker %d", tc.labels, got, blocker)
			}
		})
	}
}

func TestComputedForIssue_ReleaseAtLowOutranksEveryNonReleaseIssue(t *testing.T) {
	highest := PriorityInputs{BlastRadius: ComputedCeiling, ProjectDefault: ComputedCeiling}.
		ComputedForIssue(&ForgeIssue{Labels: []string{"priority:critical"}, Milestone: "v1"})
	labels := []string{"release-blocker", "priority:low"}
	for _, in := range []PriorityInputs{{}, {ProjectDefault: -(1 << 40)}} {
		lowest := in.ComputedForIssue(&ForgeIssue{Labels: labels})
		if lowest <= highest {
			t.Fatalf("%v with project default %d = %d, want above a milestoned critical at the ceiling %d",
				labels, in.ProjectDefault, lowest, highest)
		}
	}
}

// TestComputedForIssue_MilestoneOrdersInsideOneTier: a milestone lifts an
// issue above an unmilestoned one in its tier and never past the next tier.
func TestComputedForIssue_MilestoneOrdersInsideOneTier(t *testing.T) {
	cases := []struct {
		name  string
		in    PriorityInputs
		tier  []string
		above []string
	}{
		{"low below normal", PriorityInputs{BlastRadius: 99}, []string{"priority:low"}, nil},
		{"normal below high", PriorityInputs{BlastRadius: 50}, nil, []string{"priority:high"}},
		{"high below critical", PriorityInputs{BlastRadius: 20}, []string{"priority:high"}, []string{"priority:critical"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plain := tc.in.ComputedForIssue(&ForgeIssue{Labels: tc.tier})
			milestoned := tc.in.ComputedForIssue(&ForgeIssue{Labels: tc.tier, Milestone: "v1.0"})
			next := tc.in.ComputedForIssue(&ForgeIssue{Labels: tc.above})
			if milestoned <= plain {
				t.Fatalf("milestoned = %d, unmilestoned = %d; want milestoned higher", milestoned, plain)
			}
			if milestoned >= next {
				t.Fatalf("milestoned = %d, next tier = %d; want below the next tier", milestoned, next)
			}
		})
	}
	if got, want := (PriorityInputs{}).ComputedForIssue(&ForgeIssue{Milestone: "  "}), (PriorityInputs{}).Computed(); got != want {
		t.Fatalf("blank milestone = %d, want %d", got, want)
	}
}

// TestComputedForIssue_NeitherIsComputedFor: an issue with no release label
// and no milestone ranks exactly as ComputedFor its labels; nil is Computed.
func TestComputedForIssue_NeitherIsComputedFor(t *testing.T) {
	for _, in := range []PriorityInputs{{}, {BlastRadius: 3, ProjectDefault: 7}, {BlastRadius: 500, ProjectDefault: 900}} {
		for _, labels := range [][]string{nil, {"bug"}, {"priority:low"}, {"priority:high"}, {"priority:critical"}} {
			if got, want := in.ComputedForIssue(&ForgeIssue{Labels: labels}), in.ComputedFor(labels); got != want {
				t.Fatalf("ComputedForIssue(%v) with %+v = %d, want ComputedFor = %d", labels, in, got, want)
			}
		}
		if got, want := in.ComputedForIssue(nil), in.Computed(); got != want {
			t.Fatalf("ComputedForIssue(nil) with %+v = %d, want Computed = %d", in, got, want)
		}
	}
}

// TestComputedForIssue_StaysBounded: absurd inputs stay inside the band.
func TestComputedForIssue_StaysBounded(t *testing.T) {
	high := ComputedCeiling + PriorityShiftCritical + PriorityShiftReleaseBlocker + PriorityShiftMilestone
	top := PriorityInputs{BlastRadius: 1 << 40, ProjectDefault: 1 << 40}.ComputedForIssue(&ForgeIssue{
		Labels: []string{"priority:critical", "release-blocker", "release-candidate"}, Milestone: "v1",
	})
	if top != high {
		t.Fatalf("everything at the ceiling = %d, want %d", top, high)
	}
	bottom := PriorityInputs{BlastRadius: -(1 << 40), ProjectDefault: -(1 << 40)}.ComputedForIssue(&ForgeIssue{
		Labels: []string{"priority:low"},
	})
	if bottom != ComputedFloor+PriorityShiftLow {
		t.Fatalf("low at the floor = %d, want %d", bottom, ComputedFloor+PriorityShiftLow)
	}
	if got := (PriorityInputs{ProjectDefault: -(1 << 40)}).ComputedForIssue(&ForgeIssue{Labels: []string{"priority:low"}, Milestone: "m"}); got < ComputedFloor+PriorityShiftLow || got > high {
		t.Fatalf("milestoned low = %d, outside [%d, %d]", got, ComputedFloor+PriorityShiftLow, high)
	}
}
