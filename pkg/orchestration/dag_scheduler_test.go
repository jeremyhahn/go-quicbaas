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

func TestBuildDAG_SingleItem(t *testing.T) {
	items := []*WorkItem{workItem("A")}
	dag, err := BuildDAG(items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dag.items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(dag.items))
	}
	if _, ok := dag.items["A"]; !ok {
		t.Fatal("expected item A in DAG")
	}
}

func TestBuildDAG_NilWorkItem(t *testing.T) {
	items := []*WorkItem{workItem("A"), nil}
	_, err := BuildDAG(items)
	if err == nil {
		t.Fatal("expected error for nil work item")
	}
	if !errors.Is(err, ErrNilWorkItem) {
		t.Fatalf("expected ErrNilWorkItem, got: %v", err)
	}
}

func TestBuildDAG_DuplicateID(t *testing.T) {
	items := []*WorkItem{workItem("A"), workItem("A")}
	_, err := BuildDAG(items)
	if err == nil {
		t.Fatal("expected error for duplicate ID")
	}
	var dupErr *DuplicateWorkItemError
	if !errors.As(err, &dupErr) {
		t.Fatalf("expected DuplicateWorkItemError, got: %T", err)
	}
	if dupErr.ItemID != "A" {
		t.Fatalf("expected item ID 'A', got %q", dupErr.ItemID)
	}
	if !errors.Is(err, ErrDuplicateWorkItem) {
		t.Fatal("expected error to unwrap to ErrDuplicateWorkItem")
	}
}

func TestBuildDAG_MissingDependency(t *testing.T) {
	items := []*WorkItem{workItem("A", "B")} // B does not exist
	_, err := BuildDAG(items)
	if err == nil {
		t.Fatal("expected error for missing dependency")
	}
	var missErr *MissingDependencyError
	if !errors.As(err, &missErr) {
		t.Fatalf("expected MissingDependencyError, got: %T", err)
	}
	if missErr.ItemID != "A" {
		t.Fatalf("expected item ID 'A', got %q", missErr.ItemID)
	}
	if missErr.DependencyID != "B" {
		t.Fatalf("expected dependency ID 'B', got %q", missErr.DependencyID)
	}
	if !errors.Is(err, ErrMissingDependency) {
		t.Fatal("expected error to unwrap to ErrMissingDependency")
	}
}

func TestBuildDAG_EmptyItems(t *testing.T) {
	dag, err := BuildDAG([]*WorkItem{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dag.items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(dag.items))
	}
}

func TestBuildDAG_NilSlice(t *testing.T) {
	dag, err := BuildDAG(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dag.items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(dag.items))
	}
}

// =============================================================================
// DetectCycles Tests
// =============================================================================

func TestDAG_CycleDetection_NoCycle(t *testing.T) {
	items := []*WorkItem{
		workItem("A"),
		workItem("B", "A"),
		workItem("C", "B"),
	}
	dag, err := BuildDAG(items)
	if err != nil {
		t.Fatalf("unexpected build error: %v", err)
	}
	if err := dag.DetectCycles(); err != nil {
		t.Fatalf("unexpected cycle detected: %v", err)
	}
}

func TestDAG_CycleDetection_SimpleCycle(t *testing.T) {
	// A -> B -> C -> A (cycle)
	// In our model, edges are "depends on", children are "depended on by".
	// A depends on C, B depends on A, C depends on B => cycle.
	items := []*WorkItem{
		workItem("A", "C"),
		workItem("B", "A"),
		workItem("C", "B"),
	}
	dag, err := BuildDAG(items)
	if err != nil {
		t.Fatalf("unexpected build error: %v", err)
	}
	err = dag.DetectCycles()
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !errors.Is(err, ErrCyclicDependency) {
		t.Fatalf("expected ErrCyclicDependency, got: %v", err)
	}
	var cycleErr *CyclicDependencyError
	if !errors.As(err, &cycleErr) {
		t.Fatalf("expected CyclicDependencyError, got: %T", err)
	}
	// The cycle should contain at least 2 nodes and loop back.
	if len(cycleErr.Cycle) < 2 {
		t.Fatalf("cycle path too short: %v", cycleErr.Cycle)
	}
	// First and last element should be the same (cycle loop).
	if cycleErr.Cycle[0] != cycleErr.Cycle[len(cycleErr.Cycle)-1] {
		t.Fatalf("cycle should loop: first=%q last=%q", cycleErr.Cycle[0], cycleErr.Cycle[len(cycleErr.Cycle)-1])
	}
}

func TestDAG_SelfCycle(t *testing.T) {
	items := []*WorkItem{workItem("A", "A")}
	dag, err := BuildDAG(items)
	if err != nil {
		t.Fatalf("unexpected build error: %v", err)
	}
	err = dag.DetectCycles()
	if err == nil {
		t.Fatal("expected cycle error for self-referencing item")
	}
	if !errors.Is(err, ErrCyclicDependency) {
		t.Fatalf("expected ErrCyclicDependency, got: %v", err)
	}
	var cycleErr *CyclicDependencyError
	if !errors.As(err, &cycleErr) {
		t.Fatalf("expected CyclicDependencyError, got: %T", err)
	}
	// Self-cycle: path should be [A, A].
	if len(cycleErr.Cycle) < 2 {
		t.Fatalf("expected self-cycle path, got: %v", cycleErr.Cycle)
	}
}

func TestDAG_CycleDetection_EmptyGraph(t *testing.T) {
	dag, err := BuildDAG([]*WorkItem{})
	if err != nil {
		t.Fatalf("unexpected build error: %v", err)
	}
	if err := dag.DetectCycles(); err != nil {
		t.Fatalf("unexpected error on empty graph: %v", err)
	}
}

// =============================================================================
// TopologicalWaves Tests
// =============================================================================

func TestDAG_SingleItem_OneWave(t *testing.T) {
	items := []*WorkItem{workItem("A")}
	dag, err := BuildDAG(items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	waves := dag.TopologicalWaves()
	if len(waves) != 1 {
		t.Fatalf("expected 1 wave, got %d", len(waves))
	}
	assertWaveContains(t, waves[0], 0, "A")
}

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

func TestEnforceParallelism_SplitWave(t *testing.T) {
	// 5 items in one wave, max parallelism 2 -> split into [2, 2, 1].
	wave := []*WorkItem{
		workItem("A"),
		workItem("B"),
		workItem("C"),
		workItem("D"),
		workItem("E"),
	}
	result := EnforceParallelism([][]*WorkItem{wave}, 2)
	if len(result) != 3 {
		t.Fatalf("expected 3 sub-waves, got %d", len(result))
	}
	if len(result[0]) != 2 {
		t.Fatalf("expected first sub-wave to have 2 items, got %d", len(result[0]))
	}
	if len(result[1]) != 2 {
		t.Fatalf("expected second sub-wave to have 2 items, got %d", len(result[1]))
	}
	if len(result[2]) != 1 {
		t.Fatalf("expected third sub-wave to have 1 item, got %d", len(result[2]))
	}
}

func TestEnforceParallelism_NoSplitNeeded(t *testing.T) {
	wave := []*WorkItem{workItem("A"), workItem("B")}
	result := EnforceParallelism([][]*WorkItem{wave}, 5)
	if len(result) != 1 {
		t.Fatalf("expected 1 wave, got %d", len(result))
	}
	if len(result[0]) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result[0]))
	}
}

func TestEnforceParallelism_ZeroParallelism(t *testing.T) {
	wave := []*WorkItem{workItem("A"), workItem("B"), workItem("C")}
	result := EnforceParallelism([][]*WorkItem{wave}, 0)
	if len(result) != 1 {
		t.Fatalf("expected unchanged waves, got %d", len(result))
	}
}

func TestEnforceParallelism_NegativeParallelism(t *testing.T) {
	wave := []*WorkItem{workItem("A"), workItem("B")}
	result := EnforceParallelism([][]*WorkItem{wave}, -1)
	if len(result) != 1 {
		t.Fatalf("expected unchanged waves, got %d", len(result))
	}
}

func TestEnforceParallelism_EmptyWaves(t *testing.T) {
	result := EnforceParallelism(nil, 2)
	if result != nil {
		t.Fatalf("expected nil for nil input, got %d waves", len(result))
	}
}

func TestEnforceParallelism_MultipleWaves(t *testing.T) {
	// Two waves: [3 items, 2 items] with max 2 -> [2, 1, 2].
	waves := [][]*WorkItem{
		{workItem("A"), workItem("B"), workItem("C")},
		{workItem("D"), workItem("E")},
	}
	result := EnforceParallelism(waves, 2)
	if len(result) != 3 {
		t.Fatalf("expected 3 waves, got %d", len(result))
	}
	if len(result[0]) != 2 {
		t.Fatalf("expected wave 0 to have 2 items, got %d", len(result[0]))
	}
	if len(result[1]) != 1 {
		t.Fatalf("expected wave 1 to have 1 item, got %d", len(result[1]))
	}
	if len(result[2]) != 2 {
		t.Fatalf("expected wave 2 to have 2 items, got %d", len(result[2]))
	}
}

func TestEnforceParallelism_ExactFit(t *testing.T) {
	// Wave has exactly maxParallelism items -> no split.
	wave := []*WorkItem{workItem("A"), workItem("B"), workItem("C")}
	result := EnforceParallelism([][]*WorkItem{wave}, 3)
	if len(result) != 1 {
		t.Fatalf("expected 1 wave (exact fit), got %d", len(result))
	}
}

// =============================================================================
// ExecutionPlan Tests
// =============================================================================

func TestExecutionPlan_CurrentWave(t *testing.T) {
	plan := &ExecutionPlan{
		Waves: [][]*WorkItem{
			{workItem("A")},
			{workItem("B")},
		},
		MaxParallelism: 1,
	}
	if plan.CurrentWave() != 0 {
		t.Fatalf("expected current wave 0, got %d", plan.CurrentWave())
	}
}

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

func TestExecutionPlan_TotalWaves(t *testing.T) {
	plan := &ExecutionPlan{
		Waves: [][]*WorkItem{
			{workItem("A")},
			{workItem("B")},
		},
	}
	if plan.TotalWaves() != 2 {
		t.Fatalf("expected 2 total waves, got %d", plan.TotalWaves())
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

func TestScheduleDAG_CycleError(t *testing.T) {
	items := []*WorkItem{
		workItem("A", "C"),
		workItem("B", "A"),
		workItem("C", "B"),
	}
	_, err := ScheduleDAG(items, 2)
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !errors.Is(err, ErrCyclicDependency) {
		t.Fatalf("expected ErrCyclicDependency, got: %v", err)
	}
}

func TestScheduleDAG_MissingDependencyError(t *testing.T) {
	items := []*WorkItem{workItem("A", "Z")}
	_, err := ScheduleDAG(items, 2)
	if err == nil {
		t.Fatal("expected missing dependency error")
	}
	if !errors.Is(err, ErrMissingDependency) {
		t.Fatalf("expected ErrMissingDependency, got: %v", err)
	}
}

func TestScheduleDAG_EmptyItems(t *testing.T) {
	plan, err := ScheduleDAG([]*WorkItem{}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.TotalWaves() != 0 {
		t.Fatalf("expected 0 waves, got %d", plan.TotalWaves())
	}
	if !plan.IsComplete() {
		t.Fatal("expected empty plan to be complete")
	}
}

func TestScheduleDAG_NilWorkItem(t *testing.T) {
	items := []*WorkItem{workItem("A"), nil}
	_, err := ScheduleDAG(items, 2)
	if err == nil {
		t.Fatal("expected error for nil work item")
	}
	if !errors.Is(err, ErrNilWorkItem) {
		t.Fatalf("expected ErrNilWorkItem, got: %v", err)
	}
}

// =============================================================================
// Error Message Tests
// =============================================================================

func TestCyclicDependencyError_Message(t *testing.T) {
	err := &CyclicDependencyError{Cycle: []string{"A", "B", "C", "A"}}
	expected := "dag: cyclic dependency detected: A -> B -> C -> A"
	if err.Error() != expected {
		t.Fatalf("expected %q, got %q", expected, err.Error())
	}
}

func TestMissingDependencyError_Message(t *testing.T) {
	err := &MissingDependencyError{ItemID: "X", DependencyID: "Y"}
	expected := "dag: missing dependency: item X depends on Y"
	if err.Error() != expected {
		t.Fatalf("expected %q, got %q", expected, err.Error())
	}
}

func TestDuplicateWorkItemError_Message(t *testing.T) {
	err := &DuplicateWorkItemError{ItemID: "A"}
	expected := "dag: duplicate work item ID: A"
	if err.Error() != expected {
		t.Fatalf("expected %q, got %q", expected, err.Error())
	}
}
