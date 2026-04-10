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
	"strings"
	"sync/atomic"
)

// =============================================================================
// Sentinel Errors
// =============================================================================

var (
	// ErrCyclicDependency indicates the dependency graph contains a cycle.
	ErrCyclicDependency = errors.New("dag: cyclic dependency detected")

	// ErrMissingDependency indicates a work item references a dependency that does not exist.
	ErrMissingDependency = errors.New("dag: missing dependency")

	// ErrInvalidParallelism indicates an invalid max parallelism value.
	ErrInvalidParallelism = errors.New("dag: invalid max parallelism")

	// ErrNilWorkItem indicates a nil work item was provided.
	ErrNilWorkItem = errors.New("dag: nil work item")

	// ErrDuplicateWorkItem indicates duplicate work item IDs were provided.
	ErrDuplicateWorkItem = errors.New("dag: duplicate work item ID")
)

// =============================================================================
// Typed Errors
// =============================================================================

// CyclicDependencyError reports a cycle found in the dependency graph.
type CyclicDependencyError struct {
	Cycle []string
}

// Error implements the error interface.
func (e *CyclicDependencyError) Error() string {
	return ErrCyclicDependency.Error() + ": " + strings.Join(e.Cycle, " -> ")
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *CyclicDependencyError) Unwrap() error {
	return ErrCyclicDependency
}

// MissingDependencyError reports a work item referencing a non-existent dependency.
type MissingDependencyError struct {
	ItemID       string
	DependencyID string
}

// Error implements the error interface.
func (e *MissingDependencyError) Error() string {
	return ErrMissingDependency.Error() + ": item " + e.ItemID + " depends on " + e.DependencyID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *MissingDependencyError) Unwrap() error {
	return ErrMissingDependency
}

// DuplicateWorkItemError reports duplicate work item IDs in the input.
type DuplicateWorkItemError struct {
	ItemID string
}

// Error implements the error interface.
func (e *DuplicateWorkItemError) Error() string {
	return ErrDuplicateWorkItem.Error() + ": " + e.ItemID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *DuplicateWorkItemError) Unwrap() error {
	return ErrDuplicateWorkItem
}

// =============================================================================
// DFS Node Colors
// =============================================================================

const (
	colorWhite = 0 // unvisited
	colorGray  = 1 // in current DFS path
	colorBlack = 2 // fully processed
)

// =============================================================================
// DAG
// =============================================================================

// DAG represents a directed acyclic graph of work items.
// It is built once and used read-only for scheduling — no synchronization needed.
type DAG struct {
	items    map[string]*WorkItem
	edges    map[string][]string // item ID -> dependency IDs (predecessors)
	children map[string][]string // item ID -> dependent IDs (successors)
}

// BuildDAG constructs a dependency graph from work items.
// Returns an error if any item is nil, has a duplicate ID, or references a
// dependency that does not exist in the provided items.
func BuildDAG(items []*WorkItem) (*DAG, error) {
	d := &DAG{
		items:    make(map[string]*WorkItem, len(items)),
		edges:    make(map[string][]string, len(items)),
		children: make(map[string][]string, len(items)),
	}

	// First pass: index all items by ID.
	for _, item := range items {
		if item == nil {
			return nil, ErrNilWorkItem
		}
		if _, exists := d.items[item.ID]; exists {
			return nil, &DuplicateWorkItemError{ItemID: item.ID}
		}
		d.items[item.ID] = item
		d.edges[item.ID] = item.Dependencies
	}

	// Second pass: validate dependencies and build children (reverse) edges.
	for id, deps := range d.edges {
		for _, dep := range deps {
			if _, exists := d.items[dep]; !exists {
				return nil, &MissingDependencyError{ItemID: id, DependencyID: dep}
			}
			d.children[dep] = append(d.children[dep], id)
		}
	}

	return d, nil
}

// DetectCycles returns an error if the graph contains a cycle.
// Uses DFS-based coloring: white (unvisited), gray (in current path), black (done).
// Finding a gray node while visiting means a cycle exists.
func (d *DAG) DetectCycles() error {
	color := make(map[string]int, len(d.items))
	for id := range d.items {
		color[id] = colorWhite
	}

	// path tracks the current DFS path for cycle reporting.
	var path []string

	for id := range d.items {
		if color[id] == colorWhite {
			if cycle := d.dfsVisit(id, color, &path); cycle != nil {
				return &CyclicDependencyError{Cycle: cycle}
			}
		}
	}
	return nil
}

// dfsVisit performs a recursive DFS visit for cycle detection.
// Returns the cycle path if a cycle is found, nil otherwise.
func (d *DAG) dfsVisit(nodeID string, color map[string]int, path *[]string) []string {
	color[nodeID] = colorGray
	*path = append(*path, nodeID)

	for _, child := range d.children[nodeID] {
		switch color[child] {
		case colorGray:
			// Found a cycle. Extract the cycle from the path.
			cycle := d.extractCycle(*path, child)
			return cycle
		case colorWhite:
			if result := d.dfsVisit(child, color, path); result != nil {
				return result
			}
		}
		// colorBlack: already fully processed, skip.
	}

	color[nodeID] = colorBlack
	*path = (*path)[:len(*path)-1]
	return nil
}

// extractCycle extracts the cycle portion from a DFS path.
// The cycle starts at the first occurrence of cycleStart and ends with cycleStart again.
func (d *DAG) extractCycle(path []string, cycleStart string) []string {
	for i, id := range path {
		if id == cycleStart {
			cycle := make([]string, len(path)-i+1)
			copy(cycle, path[i:])
			cycle[len(cycle)-1] = cycleStart
			return cycle
		}
	}
	return []string{cycleStart, cycleStart}
}

// TopologicalWaves computes execution waves using Kahn's algorithm (BFS-based).
// Each wave contains items whose dependencies are all in prior waves.
// Items within the same wave can execute in parallel.
func (d *DAG) TopologicalWaves() [][]*WorkItem {
	if len(d.items) == 0 {
		return nil
	}

	// Compute in-degree for each node.
	inDegree := make(map[string]int, len(d.items))
	for id := range d.items {
		inDegree[id] = len(d.edges[id])
	}

	// Collect all zero in-degree nodes as the first wave.
	var currentWave []string
	for id, deg := range inDegree {
		if deg == 0 {
			currentWave = append(currentWave, id)
		}
	}

	var waves [][]*WorkItem

	for len(currentWave) > 0 {
		// Build the wave of WorkItem pointers.
		wave := make([]*WorkItem, 0, len(currentWave))
		for _, id := range currentWave {
			wave = append(wave, d.items[id])
		}
		waves = append(waves, wave)

		// Process wave: decrement in-degree of dependents (children).
		var nextWave []string
		for _, id := range currentWave {
			for _, child := range d.children[id] {
				inDegree[child]--
				if inDegree[child] == 0 {
					nextWave = append(nextWave, child)
				}
			}
		}
		currentWave = nextWave
	}

	return waves
}

// =============================================================================
// Execution Plan
// =============================================================================

// ExecutionPlan represents a scheduled execution of work items in waves.
// Each wave contains items that can run concurrently because all their
// dependencies have been satisfied in prior waves.
type ExecutionPlan struct {
	Waves          [][]*WorkItem
	MaxParallelism int
	currentWave    atomic.Int32
}

// CurrentWave returns the current wave index (used by the control loop).
func (p *ExecutionPlan) CurrentWave() int {
	return int(p.currentWave.Load())
}

// AdvanceWave increments the current wave index atomically.
// Returns the new wave index.
func (p *ExecutionPlan) AdvanceWave() int {
	return int(p.currentWave.Add(1))
}

// TotalWaves returns the total number of waves in the plan.
func (p *ExecutionPlan) TotalWaves() int {
	return len(p.Waves)
}

// IsComplete returns true if all waves have been processed.
func (p *ExecutionPlan) IsComplete() bool {
	return p.CurrentWave() >= p.TotalWaves()
}

// =============================================================================
// Parallelism Enforcement
// =============================================================================

// EnforceParallelism splits waves to respect a maximum parallelism limit.
// If a wave has more items than maxParallelism, it is split into sub-waves
// of at most maxParallelism items each.
// A maxParallelism of 0 or less returns the waves unchanged.
// A nil input returns nil.
func EnforceParallelism(waves [][]*WorkItem, maxParallelism int) [][]*WorkItem {
	if waves == nil {
		return nil
	}
	if maxParallelism <= 0 {
		return waves
	}

	result := make([][]*WorkItem, 0, len(waves))
	for _, wave := range waves {
		if len(wave) <= maxParallelism {
			result = append(result, wave)
			continue
		}
		// Split wave into chunks of maxParallelism.
		for i := 0; i < len(wave); i += maxParallelism {
			end := i + maxParallelism
			if end > len(wave) {
				end = len(wave)
			}
			chunk := make([]*WorkItem, end-i)
			copy(chunk, wave[i:end])
			result = append(result, chunk)
		}
	}
	return result
}

// =============================================================================
// Schedule (Convenience)
// =============================================================================

// ScheduleDAG builds a DAG from work items, validates it, computes execution
// waves, and applies parallelism constraints. This is the primary entry point
// for the control loop's scheduling phase.
func ScheduleDAG(items []*WorkItem, maxParallelism int) (*ExecutionPlan, error) {
	dag, err := BuildDAG(items)
	if err != nil {
		return nil, err
	}

	if err := dag.DetectCycles(); err != nil {
		return nil, err
	}

	waves := dag.TopologicalWaves()
	if maxParallelism > 0 {
		waves = EnforceParallelism(waves, maxParallelism)
	}

	return &ExecutionPlan{
		Waves:          waves,
		MaxParallelism: maxParallelism,
	}, nil
}
