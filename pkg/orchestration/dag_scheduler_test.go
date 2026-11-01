// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"fmt"
	"testing"
)

// =============================================================================
// Helper Functions
// =============================================================================

// workItem creates a WorkItem with the given ID and dependencies.
func workItem(id string, deps ...string) *WorkItem {
	return &WorkItem{
		ID:           id,
		Title:        "task-" + id,
		Description:  "Work item " + id,
		Dependencies: deps,
	}
}

// waveIDs extracts the IDs from a wave of work items as a set.
func waveIDs(wave []*WorkItem) map[string]bool {
	ids := make(map[string]bool, len(wave))
	for _, item := range wave {
		ids[item.ID] = true
	}
	return ids
}

// allWaveIDs extracts all IDs across all waves.
func allWaveIDs(waves [][]*WorkItem) map[string]bool {
	ids := make(map[string]bool)
	for _, wave := range waves {
		for _, item := range wave {
			ids[item.ID] = true
		}
	}
	return ids
}

// assertWaveContains asserts that a wave contains exactly the expected IDs.
func assertWaveContains(t *testing.T, wave []*WorkItem, waveIndex int, expectedIDs ...string) {
	t.Helper()
	ids := waveIDs(wave)
	if len(ids) != len(expectedIDs) {
		t.Errorf("wave %d: expected %d items, got %d (items: %v)",
			waveIndex, len(expectedIDs), len(ids), ids)
		return
	}
	for _, id := range expectedIDs {
		if !ids[id] {
			t.Errorf("wave %d: expected item %q not found (items: %v)",
				waveIndex, id, ids)
		}
	}
}

// =============================================================================
// BuildDAG Tests
// =============================================================================

func TestBuildDAG(t *testing.T) {
	tests := []struct {
		name      string
		items     []*WorkItem
		wantItems int
		wantErr   error
		check     func(t *testing.T, err error)
	}{
		{name: "single item", items: []*WorkItem{workItem("A")}, wantItems: 1},
		{name: "empty slice", items: []*WorkItem{}, wantItems: 0},
		{name: "nil slice", items: nil, wantItems: 0},
		{name: "nil work item", items: []*WorkItem{workItem("A"), nil}, wantErr: ErrNilWorkItem},
		{
			name:    "duplicate ID",
			items:   []*WorkItem{workItem("A"), workItem("A")},
			wantErr: ErrDuplicateWorkItem,
			check: func(t *testing.T, err error) {
				var dupErr *DuplicateWorkItemError
				if !errors.As(err, &dupErr) {
					t.Fatalf("expected DuplicateWorkItemError, got: %T", err)
				}
				if dupErr.ItemID != "A" {
					t.Fatalf("expected item ID 'A', got %q", dupErr.ItemID)
				}
			},
		},
		{
			name:    "missing dependency",
			items:   []*WorkItem{workItem("A", "B")}, // B does not exist
			wantErr: ErrMissingDependency,
			check: func(t *testing.T, err error) {
				var missErr *MissingDependencyError
				if !errors.As(err, &missErr) {
					t.Fatalf("expected MissingDependencyError, got: %T", err)
				}
				if missErr.ItemID != "A" || missErr.DependencyID != "B" {
					t.Fatalf("expected A -> B, got %q -> %q", missErr.ItemID, missErr.DependencyID)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dag, err := BuildDAG(tt.items)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected %v, got: %v", tt.wantErr, err)
				}
				if tt.check != nil {
					tt.check(t, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(dag.items) != tt.wantItems {
				t.Fatalf("expected %d items, got %d", tt.wantItems, len(dag.items))
			}
			for _, item := range tt.items {
				if _, ok := dag.items[item.ID]; !ok {
					t.Fatalf("expected item %s in DAG", item.ID)
				}
			}
		})
	}
}

// =============================================================================
// DetectCycles Tests
// =============================================================================

func TestDAG_DetectCycles(t *testing.T) {
	tests := []struct {
		name      string
		items     []*WorkItem
		wantCycle bool
	}{
		{name: "linear chain has no cycle", items: []*WorkItem{workItem("A"), workItem("B", "A"), workItem("C", "B")}},
		{name: "empty graph has no cycle", items: []*WorkItem{}},
		{
			// A depends on C, B depends on A, C depends on B.
			name:      "three-node cycle",
			items:     []*WorkItem{workItem("A", "C"), workItem("B", "A"), workItem("C", "B")},
			wantCycle: true,
		},
		{name: "self cycle", items: []*WorkItem{workItem("A", "A")}, wantCycle: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dag, err := BuildDAG(tt.items)
			if err != nil {
				t.Fatalf("unexpected build error: %v", err)
			}
			err = dag.DetectCycles()
			if !tt.wantCycle {
				if err != nil {
					t.Fatalf("unexpected cycle detected: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrCyclicDependency) {
				t.Fatalf("expected ErrCyclicDependency, got: %v", err)
			}
			var cycleErr *CyclicDependencyError
			if !errors.As(err, &cycleErr) {
				t.Fatalf("expected CyclicDependencyError, got: %T", err)
			}
			// The path has at least 2 nodes and loops back to its start.
			if len(cycleErr.Cycle) < 2 {
				t.Fatalf("cycle path too short: %v", cycleErr.Cycle)
			}
			if cycleErr.Cycle[0] != cycleErr.Cycle[len(cycleErr.Cycle)-1] {
				t.Fatalf("cycle should loop: %v", cycleErr.Cycle)
			}
		})
	}
}

// =============================================================================
// TopologicalWaves Tests
// =============================================================================

func TestDAG_LinearChain(t *testing.T) {
	// A -> B -> C: 3 waves, one item each.
	// B depends on A, C depends on B.
	items := []*WorkItem{
		workItem("A"),
		workItem("B", "A"),
		workItem("C", "B"),
	}
	dag, err := BuildDAG(items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	waves := dag.TopologicalWaves()
	if len(waves) != 3 {
		t.Fatalf("expected 3 waves, got %d", len(waves))
	}
	assertWaveContains(t, waves[0], 0, "A")
	assertWaveContains(t, waves[1], 1, "B")
	assertWaveContains(t, waves[2], 2, "C")
}

func TestDAG_Diamond(t *testing.T) {
	// Diamond: A -> {B, C} -> D
	// B and C depend on A, D depends on B and C.
	items := []*WorkItem{
		workItem("A"),
		workItem("B", "A"),
		workItem("C", "A"),
		workItem("D", "B", "C"),
	}
	dag, err := BuildDAG(items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	waves := dag.TopologicalWaves()
	if len(waves) != 3 {
		t.Fatalf("expected 3 waves, got %d", len(waves))
	}
	assertWaveContains(t, waves[0], 0, "A")
	assertWaveContains(t, waves[1], 1, "B", "C")
	assertWaveContains(t, waves[2], 2, "D")
}

func TestDAG_FullyParallel(t *testing.T) {
	// No dependencies: all items in wave 0.
	items := []*WorkItem{
		workItem("A"),
		workItem("B"),
		workItem("C"),
		workItem("D"),
	}
	dag, err := BuildDAG(items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	waves := dag.TopologicalWaves()
	if len(waves) != 1 {
		t.Fatalf("expected 1 wave, got %d", len(waves))
	}
	assertWaveContains(t, waves[0], 0, "A", "B", "C", "D")
}

func TestDAG_EmptyWaves(t *testing.T) {
	dag, err := BuildDAG([]*WorkItem{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	waves := dag.TopologicalWaves()
	if waves != nil {
		t.Fatalf("expected nil waves for empty DAG, got %d waves", len(waves))
	}
}

func TestDAG_ComplexGraph(t *testing.T) {
	// Realistic 8-item graph:
	//
	//   A (no deps)
	//   B (no deps)
	//   C depends on A
	//   D depends on A
	//   E depends on B
	//   F depends on C, D
	//   G depends on E
	//   H depends on F, G
	//
	// Expected waves:
	//   Wave 0: A, B
	//   Wave 1: C, D, E
	//   Wave 2: F, G
	//   Wave 3: H
	items := []*WorkItem{
		workItem("A"),
		workItem("B"),
		workItem("C", "A"),
		workItem("D", "A"),
		workItem("E", "B"),
		workItem("F", "C", "D"),
		workItem("G", "E"),
		workItem("H", "F", "G"),
	}
	dag, err := BuildDAG(items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := dag.DetectCycles(); err != nil {
		t.Fatalf("unexpected cycle: %v", err)
	}
	waves := dag.TopologicalWaves()
	if len(waves) != 4 {
		t.Fatalf("expected 4 waves, got %d", len(waves))
	}
	assertWaveContains(t, waves[0], 0, "A", "B")
	assertWaveContains(t, waves[1], 1, "C", "D", "E")
	assertWaveContains(t, waves[2], 2, "F", "G")
	assertWaveContains(t, waves[3], 3, "H")

	// Verify all items are accounted for.
	allIDs := allWaveIDs(waves)
	if len(allIDs) != 8 {
		t.Fatalf("expected 8 unique items across all waves, got %d", len(allIDs))
	}
}

// =============================================================================
// EnforceParallelism Tests
// =============================================================================

func TestEnforceParallelism(t *testing.T) {
	tests := []struct {
		name           string
		waveSizes      []int // nil means a nil waves slice
		maxParallelism int
		wantSizes      []int // nil means a nil result
	}{
		{name: "split one wave", waveSizes: []int{5}, maxParallelism: 2, wantSizes: []int{2, 2, 1}},
		{name: "split only oversized waves", waveSizes: []int{3, 2}, maxParallelism: 2, wantSizes: []int{2, 1, 2}},
		{name: "exact fit is not split", waveSizes: []int{3}, maxParallelism: 3, wantSizes: []int{3}},
		{name: "zero parallelism leaves waves unchanged", waveSizes: []int{3}, maxParallelism: 0, wantSizes: []int{3}},
		{name: "negative parallelism leaves waves unchanged", waveSizes: []int{2}, maxParallelism: -1, wantSizes: []int{2}},
		{name: "nil waves", waveSizes: nil, maxParallelism: 2, wantSizes: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var waves [][]*WorkItem
			next := 0
			for _, size := range tt.waveSizes {
				wave := make([]*WorkItem, size)
				for i := range wave {
					wave[i] = workItem(fmt.Sprintf("item-%d", next))
					next++
				}
				waves = append(waves, wave)
			}

			result := EnforceParallelism(waves, tt.maxParallelism)
			if tt.wantSizes == nil {
				if result != nil {
					t.Fatalf("expected nil result, got %d waves", len(result))
				}
				return
			}
			if len(result) != len(tt.wantSizes) {
				t.Fatalf("expected %d waves, got %d", len(tt.wantSizes), len(result))
			}
			for i, want := range tt.wantSizes {
				if len(result[i]) != want {
					t.Fatalf("expected wave %d to have %d items, got %d", i, want, len(result[i]))
				}
			}
		})
	}
}

// =============================================================================
// ExecutionPlan Tests
// =============================================================================

func TestExecutionPlan_AdvanceWave(t *testing.T) {
	plan := &ExecutionPlan{
		Waves: [][]*WorkItem{
			{workItem("A")},
			{workItem("B")},
			{workItem("C")},
		},
	}
	if plan.CurrentWave() != 0 {
		t.Fatalf("expected initial wave 0, got %d", plan.CurrentWave())
	}
	next := plan.AdvanceWave()
	if next != 1 {
		t.Fatalf("expected next wave 1, got %d", next)
	}
	if plan.CurrentWave() != 1 {
		t.Fatalf("expected current wave 1, got %d", plan.CurrentWave())
	}
}

func TestExecutionPlan_IsComplete(t *testing.T) {
	plan := &ExecutionPlan{
		Waves: [][]*WorkItem{
			{workItem("A")},
		},
	}
	if plan.IsComplete() {
		t.Fatal("expected plan not complete at wave 0")
	}
	plan.AdvanceWave()
	if !plan.IsComplete() {
		t.Fatal("expected plan complete after advancing past last wave")
	}
}

func TestExecutionPlan_EmptyPlan(t *testing.T) {
	plan := &ExecutionPlan{}
	if !plan.IsComplete() {
		t.Fatal("expected empty plan to be complete")
	}
	if plan.TotalWaves() != 0 {
		t.Fatalf("expected 0 waves, got %d", plan.TotalWaves())
	}
}

// =============================================================================
// ScheduleDAG Integration Tests
// =============================================================================

func TestScheduleDAG_HappyPath(t *testing.T) {
	items := []*WorkItem{
		workItem("A"),
		workItem("B", "A"),
		workItem("C", "A"),
		workItem("D", "B", "C"),
	}
	plan, err := ScheduleDAG(items, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With maxParallelism=1, wave 1 (B,C) should be split into 2 sub-waves.
	// Total: A, B, C, D -> 4 waves with parallelism 1.
	if plan.TotalWaves() != 4 {
		t.Fatalf("expected 4 waves with parallelism 1, got %d", plan.TotalWaves())
	}
	if plan.MaxParallelism != 1 {
		t.Fatalf("expected max parallelism 1, got %d", plan.MaxParallelism)
	}
}

func TestScheduleDAG_UnlimitedParallelism(t *testing.T) {
	items := []*WorkItem{
		workItem("A"),
		workItem("B"),
		workItem("C"),
	}
	plan, err := ScheduleDAG(items, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No parallelism enforcement, all items in one wave.
	if plan.TotalWaves() != 1 {
		t.Fatalf("expected 1 wave, got %d", plan.TotalWaves())
	}
}

func TestScheduleDAG_Errors(t *testing.T) {
	tests := []struct {
		name    string
		items   []*WorkItem
		wantErr error
	}{
		{
			name:    "cycle from DetectCycles",
			items:   []*WorkItem{workItem("A", "C"), workItem("B", "A"), workItem("C", "B")},
			wantErr: ErrCyclicDependency,
		},
		{
			name:    "BuildDAG error is returned",
			items:   []*WorkItem{workItem("A", "Z")},
			wantErr: ErrMissingDependency,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := ScheduleDAG(tt.items, 2)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got: %v", tt.wantErr, err)
			}
			if plan != nil {
				t.Fatal("expected nil plan on error")
			}
		})
	}
}

// =============================================================================
// Error Message Tests
// =============================================================================

func TestDAGError_Message(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "cyclic dependency",
			err:  &CyclicDependencyError{Cycle: []string{"A", "B", "C", "A"}},
			want: "dag: cyclic dependency detected: A -> B -> C -> A",
		},
		{
			name: "missing dependency",
			err:  &MissingDependencyError{ItemID: "X", DependencyID: "Y"},
			want: "dag: missing dependency: item X depends on Y",
		},
		{
			name: "duplicate work item",
			err:  &DuplicateWorkItemError{ItemID: "A"},
			want: "dag: duplicate work item ID: A",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}
