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

package orchestration

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// TestSplitCSVFilter_DropsBlanksAndAnEmptyListNarrowsNothing proves the two
// answers a comma-separated filter has to get right: a stray comma asks the
// same question as none, and a wholly blank value narrows NOTHING rather than
// narrowing to nothing.
func TestSplitCSVFilter_DropsBlanksAndAnEmptyListNarrowsNothing(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw  string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{",,", nil},
		{"executing", []string{"executing"}},
		{"executing,,failed", []string{"executing", "failed"}},
		{" executing , failed ", []string{"executing", "failed"}},
	}
	for _, tc := range cases {
		if got := SplitCSVFilter(tc.raw); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("SplitCSVFilter(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

// TestParseTaskStatuses_AcceptsTheEnum proves every status the engine has is
// filterable, and that an absent parameter narrows nothing.
func TestParseTaskStatuses_AcceptsTheEnum(t *testing.T) {
	t.Parallel()

	statuses, err := ParseTaskStatuses("statuses", "pending,in_progress,blocked,completed,failed")
	if err != nil {
		t.Fatalf("ParseTaskStatuses() error = %v, want nil", err)
	}
	want := []TaskStatus{TaskStatusPending, TaskStatusInProgress, TaskStatusBlocked, TaskStatusCompleted, TaskStatusFailed}
	if !reflect.DeepEqual(statuses, want) {
		t.Errorf("statuses = %v, want %v", statuses, want)
	}

	empty, err := ParseTaskStatuses("statuses", "")
	if err != nil || empty != nil {
		t.Errorf("ParseTaskStatuses(\"\") = %v, %v; want nil, nil", empty, err)
	}

	// Every declared status must survive the round trip, or the enum and the
	// filter have drifted.
	for _, status := range TaskStatuses() {
		if !status.Valid() {
			t.Errorf("%q is declared but not valid", status)
		}
	}
}

// TestParseTaskStatuses_RefusesAValueOutsideTheEnum proves the one filter that
// refuses rather than matching nothing, and that the refusal names both the
// parameter and what the engine does have.
func TestParseTaskStatuses_RefusesAValueOutsideTheEnum(t *testing.T) {
	t.Parallel()

	statuses, err := ParseTaskStatuses("statuses", "pending,complete")
	if statuses != nil {
		t.Errorf("statuses = %v, want none for a refused filter", statuses)
	}
	var invalid *InvalidFilterParamError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v, want *InvalidFilterParamError", err)
	}
	if invalid.Param != "statuses" {
		t.Errorf("Param = %q, want statuses", invalid.Param)
	}
	if !strings.Contains(invalid.Value, "complete") || !strings.Contains(invalid.Value, "completed") {
		t.Errorf("refusal %q names neither the offending value nor the ones that exist", invalid.Value)
	}
	if !errors.Is(err, ErrFilterParamInvalid) {
		t.Error("the refusal does not unwrap to ErrFilterParamInvalid, so no transport maps it to 400")
	}
}

// TestJobStatesFrom_AcceptsEveryEngineState proves the jobs listing can be
// asked about each state the engine drives a job through.
func TestJobStatesFrom_AcceptsEveryEngineState(t *testing.T) {
	t.Parallel()

	for _, state := range engineJobStates {
		states, err := JobStatesFrom("statuses", []string{string(state)})
		if err != nil {
			t.Errorf("JobStatesFrom(%q) error = %v, want nil", state, err)
			continue
		}
		if len(states) != 1 || states[0] != state {
			t.Errorf("JobStatesFrom(%q) = %v, want the state back", state, states)
		}
	}
	if states, err := JobStatesFrom("statuses", nil); states != nil || err != nil {
		t.Errorf("JobStatesFrom(nil) = %v, %v; want nil, nil", states, err)
	}
}

// TestJobStatesFrom_RefusesAValueOutsideTheEnum proves a misspelled state is
// a refusal rather than an empty job list.
func TestJobStatesFrom_RefusesAValueOutsideTheEnum(t *testing.T) {
	t.Parallel()

	states, err := JobStatesFrom("statuses", []string{"executing", "executng"})
	if states != nil {
		t.Errorf("states = %v, want none for a refused filter", states)
	}
	var invalid *InvalidFilterParamError
	if !errors.As(err, &invalid) || invalid.Param != "statuses" {
		t.Fatalf("error = %v, want *InvalidFilterParamError naming statuses", err)
	}
	if !strings.Contains(invalid.Value, "executng") {
		t.Errorf("refusal %q does not name the offending value", invalid.Value)
	}
}

// TestJobStateValid_IncludesTheRunnersOwnStates proves the union: a state a
// registered runner declares is as filterable as one the engine drives,
// because most of what the factory does happens in a runner's own states.
func TestJobStateValid_IncludesTheRunnersOwnStates(t *testing.T) {
	t.Parallel()

	// The engine's own workflow runner is always registered, so the union is
	// never just the constant list — which is what makes this an assertion
	// about the union rather than about an empty registry.
	declared := KnownJobStates()
	if len(declared) == 0 {
		t.Fatal("no runner declares any state; the union under test is empty")
	}
	for _, spec := range declared {
		if !spec.State.Valid() {
			t.Errorf("%q is declared by a registered runner but refused by the filter", spec.State)
		}
	}
	names := JobStateNames()
	for _, spec := range declared {
		if !containsString(names, string(spec.State)) {
			t.Errorf("JobStateNames() omits the registered runner state %q", spec.State)
		}
	}
	if JobState("not-a-state").Valid() {
		t.Error("an invented state reported itself valid")
	}
}

// containsString reports whether values holds want.
func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
