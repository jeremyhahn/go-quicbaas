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

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The binding's Agents
// =============================================================================
//
// A BINDING NAMES WHO WORKS BY REFERENCE. Its worker and triage slots hold an
// Agent or a pool; each review seat holds an Agent and the forge identity it
// posts as. What runtime, model and image those resolve to lives on the
// tenant's Agents, so this file is where the module asks the catalog — for the
// role check when a binding is saved, for the model families the independence
// rule is decided on, and for the triage agent a project runs with.

// AgentCatalog is the slice of the tenant agent catalog the module reads.
type AgentCatalog interface {
	Catalogue(ctx context.Context, tenantID string) (orchestration.AgentCatalogue, error)
}

// CheckAgents refuses a binding whose slots name nothing the tenant defines,
// or an Agent that does not declare the slot's role, or a review seat that
// could only ever produce a verdict the gate discards.
func (b *ProjectBinding) CheckAgents(catalogue orchestration.AgentCatalogue) error {
	if err := catalogue.CheckRef(b.Worker, "worker", orchestration.AgentRoleCoding); err != nil {
		return err
	}
	if err := catalogue.CheckRef(b.Triage, "triage", orchestration.AgentRoleTriage); err != nil {
		return err
	}
	authors := WorkerFamilies(catalogue, b.Worker)
	for i, seat := range b.ReviewerProfiles() {
		field := seatField(seat, i)
		ref := orchestration.AgentRefTo(seat.Agent)
		if err := catalogue.CheckRef(ref, field+".agent_id", orchestration.AgentRoleReview); err != nil {
			return err
		}
		agent, _ := catalogue.Agent(seat.Agent)
		if err := seat.checkEndpointFor(field, agent); err != nil {
			return err
		}
		family := SeatFamily(catalogue, seat.ReviewerConfig)
		if family == ModelFamilyAnthropic {
			return &ReviewerFamilyRefusedError{ProjectID: b.ProjectID, Agent: seat.Agent, Family: family}
		}
		if b.Review.RequireIndependentReviewer && seat.Counts && (family == "" || slices.Contains(authors, family)) {
			return &ReviewerNotIndependentError{ProjectID: b.ProjectID, Family: family}
		}
	}
	return b.checkReviewerFamilies(catalogue)
}

// checkReviewerFamilies refuses a ReviewPolicy.Reviewers entry that names a
// configured seat's login with a family other than the one its Agent's backend
// and model derive. See reviewPolicy for why the derived family wins.
func (b *ProjectBinding) checkReviewerFamilies(catalogue orchestration.AgentCatalogue) error {
	for _, profile := range b.ReviewerProfiles() {
		derived := SeatFamily(catalogue, profile.ReviewerConfig)
		if derived == "" {
			continue
		}
		for login, family := range b.Review.Reviewers {
			if normalizeLogin(login) != normalizeLogin(profile.Login) {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(family), derived) {
				return &InvalidInputError{
					Field: "review.reviewers[" + strconv.Quote(login) + "]",
					Message: "attributes reviewer profile " + strconv.Quote(profile.Name) + " to family " +
						strconv.Quote(family) + ", but its agent's backend and model derive " + strconv.Quote(derived) +
						"; a configured reviewer's family comes from what it runs, so drop the entry",
				}
			}
		}
	}
	return nil
}

// seatField is the path a refusal about the i-th seat names.
func seatField(seat ReviewerProfile, i int) string {
	if seat.Default {
		return "reviewer"
	}
	return "reviewers[" + strconv.Itoa(i-1) + "]"
}

// WorkerFamilies returns the model family of every Agent ref resolves to, so
// the independence rule can refuse a reviewer from the family of ANY Agent
// that may have written the code. An Agent whose family cannot be named
// contributes "", which the rule reads as "unknown".
func WorkerFamilies(catalogue orchestration.AgentCatalogue, ref orchestration.AgentRef) []string {
	candidates, err := catalogue.Candidates(ref)
	if err != nil {
		return nil
	}
	families := make([]string, 0, len(candidates))
	for _, agent := range candidates {
		family := ModelFamily(agent.Backend, agent.Model)
		if !slices.Contains(families, family) {
			families = append(families, family)
		}
	}
	return families
}

// SeatFamily returns the model family of the Agent a review seat names, empty
// when the Agent is not defined or its family cannot be named.
func SeatFamily(catalogue orchestration.AgentCatalogue, seat ReviewerConfig) string {
	agent, ok := catalogue.Agent(seat.Agent)
	if !ok {
		return ""
	}
	return ModelFamily(agent.Backend, agent.Model)
}

// NextReviewerSeat returns the seat a review fails over to when current's
// Agent cannot take the work: the next seat in binding order that counts
// toward merge exactly when current does.
//
// SEAT ORDER IS THE FAILOVER CHAIN, AND IT NEVER CROSSES THE GATE. A review
// that counts failing over to an advisory seat would post a verdict the gate
// reads past, and the pull request would wait for an approval nobody is
// producing; an advisory review failing over to a counting seat would let an
// operator's "try the new model" decide a merge.
func (b *ProjectBinding) NextReviewerSeat(current ReviewerProfile) (ReviewerProfile, bool) {
	seats := b.ReviewerProfiles()
	for i, seat := range seats {
		if seat.Name != current.Name {
			continue
		}
		for _, next := range seats[i+1:] {
			if next.Counts == current.Counts {
				return next, true
			}
		}
		return ReviewerProfile{}, false
	}
	return ReviewerProfile{}, false
}

// AgentReferences lists every reference this binding holds into the agent
// catalog, for the "used by" query and the refusal to delete an Agent in use.
func (b *ProjectBinding) AgentReferences() []orchestration.AgentReference {
	usage := func(slot string) orchestration.AgentUsage {
		return orchestration.AgentUsage{Kind: orchestration.AgentUsageKindBinding, ID: b.ProjectID, Slot: slot}
	}
	var references []orchestration.AgentReference
	if !b.Worker.IsZero() {
		references = append(references, orchestration.AgentReference{Ref: b.Worker, Usage: usage(orchestration.AgentUsageSlotWorker)})
	}
	if !b.Triage.IsZero() {
		references = append(references, orchestration.AgentReference{Ref: b.Triage, Usage: usage(orchestration.AgentUsageSlotTriage)})
	}
	for _, seat := range b.ReviewerProfiles() {
		references = append(references, orchestration.AgentReference{
			Ref:   orchestration.AgentRefTo(seat.Agent),
			Usage: usage(orchestration.AgentUsageSlotReviewer + ":" + seat.Name),
		})
	}
	return references
}

// =============================================================================
// The triage agent
// =============================================================================

// TriageAgentPolicy decides the Agent or pool a project triages with.
type TriageAgentPolicy struct {
	// Default is the node's triage agent: what every project triages with
	// unless its own binding names one. It names an Agent or pool of the
	// project's tenant, and is taken only where that tenant defines it.
	Default orchestration.AgentRef

	// Agents is the catalog Default is checked against. Required when
	// Default is set.
	Agents AgentCatalog
}

// For returns the triage reference for one binding, most specific first: the
// binding's own Triage, then the node's default where the tenant defines it
// for the triage role, then the binding's Worker.
//
// THE NODE DEFAULT IS THE RUNG THAT MATTERS FOR COST. Triage writes nothing —
// it decides whether an issue is already satisfied, duplicated or moot — and it
// is by volume the most-run job the factory has; without the default every
// project re-reads its backlog with the model that writes its code.
//
// A TENANT THAT DOES NOT DEFINE THE DEFAULT IS NOT REFUSED. The default is the
// node's, and a node serves tenants that never created the Agent it names;
// those tenants triage with their worker, as they always have.
func (p TriageAgentPolicy) For(ctx context.Context, tenantID string, binding *ProjectBinding) (orchestration.AgentRef, error) {
	if !binding.Triage.IsZero() {
		return binding.Triage, nil
	}
	if p.Default.IsZero() || p.Agents == nil {
		return binding.Worker, nil
	}
	catalogue, err := p.Agents.Catalogue(ctx, tenantID)
	if err != nil {
		return orchestration.AgentRef{}, err
	}
	if catalogue.CheckRef(p.Default, "triage", orchestration.AgentRoleTriage) != nil {
		return binding.Worker, nil
	}
	return p.Default, nil
}

// =============================================================================
// What a binding's references resolved to, for one evaluation
// =============================================================================

// bindingAgents is the model families behind a binding's slots, read from the
// catalog once per evaluation so every decision in it sees the same answer.
type bindingAgents struct {
	// authorFamilies are the families of every Agent the worker slot
	// resolves to.
	authorFamilies []string

	// seatFamilies maps a review seat's name to its Agent's family.
	seatFamilies map[string]string

	// seatAgents maps a review seat's name to the Agent it names, for what
	// the spawn needs beyond the reference: whether it runs opencode on a
	// local model, and on which model. A seat naming no defined Agent is
	// absent.
	seatAgents map[string]orchestration.Agent
}

// resolveBindingAgents reads the families behind binding's slots.
func resolveBindingAgents(ctx context.Context, agents AgentCatalog, tenantID string, binding *ProjectBinding) (bindingAgents, error) {
	if agents == nil {
		return bindingAgents{}, &InvalidInputError{Field: "agents", Message: "the model families behind a binding are read from the agent catalog"}
	}
	catalogue, err := agents.Catalogue(ctx, tenantID)
	if err != nil {
		return bindingAgents{}, err
	}
	return bindingAgentsFrom(catalogue, binding), nil
}

// bindingAgentsFrom derives bindingAgents from a catalogue already read.
func bindingAgentsFrom(catalogue orchestration.AgentCatalogue, binding *ProjectBinding) bindingAgents {
	resolved := bindingAgents{
		authorFamilies: WorkerFamilies(catalogue, binding.Worker),
		seatFamilies:   make(map[string]string),
		seatAgents:     make(map[string]orchestration.Agent),
	}
	for _, seat := range binding.ReviewerProfiles() {
		resolved.seatFamilies[seat.Name] = SeatFamily(catalogue, seat.ReviewerConfig)
		if agent, ok := catalogue.Agent(seat.Agent); ok {
			resolved.seatAgents[seat.Name] = agent
		}
	}
	return resolved
}
