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

package sdlc

import (
	"context"
	"strings"
	"time"
)

// =============================================================================
// Priority — where a forge issue's Task lands in the queue
// =============================================================================
//
// The engine orders Tasks by Task.Priority, higher first. This module
// computes that number for the tasks it adopts from two inputs: the blast
// radius of the project (how many projects depend on it, from the dependency
// analyzers) and the binding's default shift. The arithmetic saturates so an
// absurd input cannot wrap into the wrong end of the queue.

const (
	// ComputedFloor and ComputedCeiling bound a computed priority.
	ComputedFloor   = 0
	ComputedCeiling = 999

	// BlastRadiusWeight is how much one dependent project is worth.
	BlastRadiusWeight = 10
)

// PriorityInputs are what a task's priority is computed from.
type PriorityInputs struct {
	// BlastRadius is the number of projects a change to this one affects.
	BlastRadius int

	// ProjectDefault is the binding's RankingPolicy.DefaultPriority.
	ProjectDefault int
}

// Computed returns the priority: blast radius times its weight, shifted by
// the project default, clamped to [ComputedFloor, ComputedCeiling].
func (in PriorityInputs) Computed() int {
	return clamp(saturatingAdd(in.BlastRadius, BlastRadiusWeight, in.ProjectDefault), ComputedFloor, ComputedCeiling)
}

// Priority label shifts move an issue by the tier the repository's
// project-management labels give it. They are sized to outrank blast radius:
// a priority:critical issue in a project nothing depends on still comes before
// an unlabelled issue in the most-depended-on project of a realistic fleet.
//
// AN UNLABELLED ISSUE IS UNCHANGED — Computed, exactly as before — so the
// labels only ever reorder the work a person has marked. Labelled issues may
// leave Computed's [ComputedFloor, ComputedCeiling] band on purpose: a
// priority:low issue must sort below every unlabelled one, including those at
// the floor, and a priority:critical one above every unlabelled one at the
// ceiling.
const (
	PriorityShiftCritical = 600
	PriorityShiftHigh     = 300
	PriorityShiftLow      = -300
)

// priorityLabelShifts maps a repository's priority:* label to its shift.
// "priority:normal", like no label at all, shifts nothing.
var priorityLabelShifts = map[string]int{
	"priority:critical": PriorityShiftCritical,
	"priority:high":     PriorityShiftHigh,
	"priority:low":      PriorityShiftLow,
}

// labelShift is the shift of the most urgent priority:* label among labels,
// or 0 when none is present. An issue carrying two tiers is ranked by the
// more urgent one: a label added to raise an issue must not be cancelled by
// one left over from before.
func labelShift(labels []string) int {
	shift, found := 0, false
	for _, label := range labels {
		s, ok := priorityLabelShifts[strings.ToLower(strings.TrimSpace(label))]
		if !ok {
			continue
		}
		if !found || s > shift {
			shift, found = s, true
		}
	}
	return shift
}

// ComputedFor is the priority of one issue: Computed, shifted by the most
// urgent priority:* label among labels.
func (in PriorityInputs) ComputedFor(labels []string) int {
	base := in.Computed()
	shift := labelShift(labels)
	if shift == 0 {
		return base
	}
	return clamp(base+shift, ComputedFloor+PriorityShiftLow, ComputedCeiling+PriorityShiftCritical)
}

func saturatingAdd(radius, weight, offset int) int {
	if radius > ComputedCeiling {
		radius = ComputedCeiling
	}
	if radius < 0 {
		radius = 0
	}
	product := radius * weight
	switch {
	case offset > ComputedCeiling:
		offset = ComputedCeiling
	case offset < -ComputedCeiling:
		offset = -ComputedCeiling
	}
	return product + offset
}

func clamp(v, low, high int) int { return min(high, max(low, v)) }

// =============================================================================
// Ranking — blast radius from the tenant's bindings
// =============================================================================

// DependencyGraphSnapshot is one analyzer's view of who depends on a project.
type DependencyGraphSnapshot struct {
	// Analyzer produced this snapshot (gomod, declared, neutral).
	Analyzer string `json:"analyzer"`

	// ProjectID is the project analysed.
	ProjectID string `json:"project_id"`

	// Dependents are the projects that depend on this one, transitively.
	Dependents []string `json:"dependents,omitempty"`

	// ComputedAt is when the analysis ran.
	ComputedAt time.Time `json:"computed_at"`
}

// BlastRadius is the number of projects affected by a change to this one.
func (s *DependencyGraphSnapshot) BlastRadius() int { return len(s.Dependents) }

// Ranker computes a project's priority inputs from the tenant's bindings:
// the declared dependents of every binding feed the analyzer, and the
// project's own binding supplies the default shift and the analyzer choice.
type Ranker struct {
	bindings BindingReader
}

// NewRanker builds a ranker over the binding reader.
func NewRanker(bindings BindingReader) (*Ranker, error) {
	if bindings == nil {
		return nil, &InvalidInputError{Field: "bindings", Message: "ranking inputs live on the bindings"}
	}
	return &Ranker{bindings: bindings}, nil
}

// Manifests implements ManifestSource over the tenant's bindings.
func (r *Ranker) Manifests(ctx context.Context, tenantID string) ([]DependencyManifest, error) {
	bindings, err := r.bindings.ListBindings(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	manifests := make([]DependencyManifest, 0, len(bindings))
	for _, b := range bindings {
		manifests = append(manifests, DependencyManifest{
			ProjectID:  b.ProjectID,
			Dependents: append([]string(nil), b.Ranking.DeclaredDependents...),
		})
	}
	return manifests, nil
}

// Inputs returns the priority inputs for a bound project.
func (r *Ranker) Inputs(ctx context.Context, tenantID string, binding *ProjectBinding) (PriorityInputs, error) {
	if binding == nil {
		return PriorityInputs{}, &InvalidInputError{Field: "binding", Message: "must not be nil"}
	}
	manifests, err := r.Manifests(ctx, tenantID)
	if err != nil {
		return PriorityInputs{}, err
	}
	snapshot, err := BlastRadiusWith(binding.Ranking.AnalyzerName, binding.ProjectID, manifests)
	if err != nil {
		return PriorityInputs{}, err
	}
	return PriorityInputs{BlastRadius: snapshot.BlastRadius(), ProjectDefault: binding.Ranking.DefaultPriority}, nil
}
