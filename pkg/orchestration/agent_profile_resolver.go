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
	"context"
	"strconv"
	"strings"
)

// =============================================================================
// Resolving a pool to the profile a spawn will use
// =============================================================================
//
// WHAT THIS REPLACES. A role named one backend, so a saturated resource was a
// stalled queue: when the NPU serving triage was busy, triage waited, because
// the binding could express no alternative. A pool says "prefer the local
// model, spill to the metered one", and the spill is data rather than a
// hardcoded fallback in the runner.
//
// SELECTION IS FIRST-FIT BY PREFERENCE, NOT LEAST-LOADED. Order is the
// operator's policy — cheapest or most private first — and picking the
// least-loaded profile would silently invert it, sending work to the expensive
// provider whenever the preferred one had any load at all. First-fit keeps the
// declared preference and uses load only to decide when a profile is FULL.

// ProfileCatalogue is the set of profiles and pools in force for a tenant.
type ProfileCatalogue struct {
	Profiles map[string]AgentProfile
	Pools    map[string]AgentPool
}

// Profile returns a profile by name.
func (c ProfileCatalogue) Profile(name string) (AgentProfile, bool) {
	p, ok := c.Profiles[strings.TrimSpace(name)]
	return p, ok
}

// ProfileResolver picks the profile a spawn should use.
type ProfileResolver struct {
	catalogue ProfileCatalogue
	load      ProfileLoadReader
	demand    RoleDemandReader
}

// NewProfileResolver builds a resolver. A nil load reader is allowed and makes
// every profile look idle, which is the correct degradation for a deployment
// with no lease store: preference order still holds, and the concurrency cap
// still refuses what would overrun.
func NewProfileResolver(catalogue ProfileCatalogue, load ProfileLoadReader) *ProfileResolver {
	return &ProfileResolver{catalogue: catalogue, load: load}
}

// WithRoleDemand returns a resolver that can also see what each role has
// QUEUED, which is what lets a profile yield capacity its preferred role is
// not asking for. Without one, every non-exclusive profile is shareable — the
// correct degradation, since a deployment that cannot measure its queues
// should not have its accelerators idled by a policy it cannot evaluate.
func (r *ProfileResolver) WithRoleDemand(demand RoleDemandReader) *ProfileResolver {
	if r == nil {
		return nil
	}
	clone := *r
	clone.demand = demand
	return &clone
}

// Resolve picks the first profile in the pool with capacity.
//
// A DIRECT PROFILE NAME IS ALSO A POOL OF ONE, so a caller that wants exactly
// one profile says so by naming it and needs no second mechanism.
func (r *ProfileResolver) Resolve(ctx context.Context, tenantID, poolOrProfile string) (AgentProfile, error) {
	return r.ResolveForRole(ctx, tenantID, poolOrProfile, "")
}

// ResolveForRole picks the first profile in the pool that has capacity AND
// will take work of this role.
//
// THE TWO TESTS ARE SEPARATE AND BOTH MUST PASS. Capacity is arithmetic about
// what is running; the role policy is a statement about what the profile is
// FOR. A profile with a free slot can still decline — it is being held for
// review, and the triage job in front of it should spill to the next profile
// rather than take the slot the operator set aside.
//
// An empty role names no policy subject and is admitted everywhere, which is
// what a caller that does not know its own role gets.
func (r *ProfileResolver) ResolveForRole(ctx context.Context, tenantID, poolOrProfile string, role Role) (AgentProfile, error) {
	name := strings.TrimSpace(poolOrProfile)
	if name == "" {
		return AgentProfile{}, &InputValidationError{Field: "pool", Message: "no pool or profile named"}
	}

	candidates := []string{name}
	if pool, ok := r.catalogue.Pools[name]; ok {
		candidates = pool.Profiles
	}

	var full []string
	for _, candidate := range candidates {
		profile, ok := r.catalogue.Profile(candidate)
		if !ok {
			return AgentProfile{}, &ProfileNotFoundError{Name: candidate, Pool: name}
		}
		live := 0
		if profile.MaxConcurrent > 0 {
			var err error
			live, err = r.liveFor(ctx, tenantID, profile.Runtime)
			if err != nil {
				return AgentProfile{}, err
			}
			if live >= profile.MaxConcurrent {
				full = append(full, candidate+" ("+strconv.Itoa(live)+"/"+strconv.Itoa(profile.MaxConcurrent)+")")
				continue
			}
		}
		if admitted, why := profile.Roles.admits(ctx, r.demand, tenantID, role, profile.MaxConcurrent, live); !admitted {
			full = append(full, candidate+" ("+why+")")
			continue
		}
		return profile, nil
	}

	// EVERY PROFILE FULL IS A TYPED REFUSAL NAMING WHAT WAS FULL, not a
	// fallback to the first. Spawning onto a saturated profile anyway is how a
	// pool becomes decoration: the work would queue behind the cap and the
	// operator would see a slow fleet with no reason recorded anywhere.
	return AgentProfile{}, &ProfilePoolExhaustedError{Pool: name, Full: full}
}

func (r *ProfileResolver) liveFor(ctx context.Context, tenantID, runtime string) (int, error) {
	if r.load == nil {
		return 0, nil
	}
	return r.load.LiveJobsForBackend(ctx, tenantID, runtime)
}

// SpawnArgs renders the provider configuration a runtime needs on argv.
//
// IT IS PER RUNTIME BECAUSE THE DIALECT IS. codex takes `-c dotted.key=TOML`
// and has no --provider flag; a runtime that reads an endpoint from its own
// configuration needs nothing here. A runtime this does not know gets no args
// rather than a guess, because argv a CLI does not understand is a startup
// failure rather than a warning.
func (p AgentProfile) SpawnArgs() []string {
	if !p.Provider.Configured() {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(p.Runtime)) {
	case "codex":
		name := strings.TrimSpace(p.Provider.Name)
		if name == "" {
			name = "quicbaas_provider"
		}
		key := "model_providers." + name
		return []string{
			"-c", "model_provider=" + strconv.Quote(name),
			"-c", key + ".name=" + strconv.Quote(name),
			"-c", key + ".base_url=" + strconv.Quote(p.Provider.BaseURL),
			"-c", key + ".wire_api=" + strconv.Quote(strings.ToLower(p.Provider.WireAPI)),
			"-c", key + ".requires_openai_auth=" + strconv.FormatBool(p.Provider.RequiresAuth),
		}
	default:
		return nil
	}
}
