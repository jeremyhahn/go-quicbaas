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
	"strconv"
	"strings"
)

// =============================================================================
// Agent catalog sentinel errors
// =============================================================================

var (
	// ErrAgentUnknown indicates a referenced Agent is not defined.
	ErrAgentUnknown = errors.New("agent: not found")

	// ErrAgentPoolUnknown indicates a referenced pool is not defined.
	ErrAgentPoolUnknown = errors.New("agent pool: not found")

	// ErrAgentExists indicates a create named an Agent or pool that already
	// exists.
	ErrAgentExists = errors.New("agent catalog: already exists")

	// ErrAgentInUse indicates an Agent or pool cannot be removed because
	// something still references it.
	ErrAgentInUse = errors.New("agent catalog: in use")

	// ErrAgentRoleMismatch indicates a slot referenced an Agent that does not
	// declare the slot's role.
	ErrAgentRoleMismatch = errors.New("agent: role not declared")

	// ErrAgentCatalogInvalid indicates an Agent names a backend or model the
	// live catalog does not offer.
	ErrAgentCatalogInvalid = errors.New("agent: not in the live catalog")

	// ErrAgentPoolExhausted indicates every Agent a reference resolves to was
	// tried and none could take the work.
	//
	// IT IS NOT ALWAYS A FAILURE, AND CALLERS MUST BE ABLE TO TELL. A pool
	// that is entirely at capacity is working as designed and clears on its
	// own; a pool whose every account is spent does not. The error carries each
	// attempt and unwraps to the last cause, so a caller classifies it the way
	// it classifies a single spawn's refusal.
	ErrAgentPoolExhausted = errors.New("agent pool: no agent could take the work")

	// ErrAgentCatalogUnavailable indicates the deployment wires no agent
	// catalog, so a reference cannot be resolved.
	ErrAgentCatalogUnavailable = errors.New("agent catalog: not configured")

	// ErrAgentCatalogStore indicates an internal catalog-store failure.
	ErrAgentCatalogStore = errors.New("agent catalog: store error")
)

// =============================================================================
// Typed errors
// =============================================================================

// UnknownAgentError reports an Agent nobody defined. Pool names the pool that
// referred to it, when the reference came from one.
type UnknownAgentError struct {
	ID   string
	Pool string
}

// Error implements the error interface.
func (e *UnknownAgentError) Error() string {
	msg := ErrAgentUnknown.Error() + ": " + strconv.Quote(e.ID)
	if e.Pool != "" {
		msg += " (listed by pool " + strconv.Quote(e.Pool) + ")"
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UnknownAgentError) Unwrap() error { return ErrAgentUnknown }

// UnknownAgentPoolError reports a pool nobody defined.
type UnknownAgentPoolError struct {
	ID string
}

// Error implements the error interface.
func (e *UnknownAgentPoolError) Error() string {
	return ErrAgentPoolUnknown.Error() + ": " + strconv.Quote(e.ID)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UnknownAgentPoolError) Unwrap() error { return ErrAgentPoolUnknown }

// AgentExistsError reports a create that named an existing Agent or pool.
type AgentExistsError struct {
	// Kind is "agent" or "pool".
	Kind string
	ID   string
}

// Error implements the error interface.
func (e *AgentExistsError) Error() string {
	return ErrAgentExists.Error() + ": " + e.Kind + " " + strconv.Quote(e.ID)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AgentExistsError) Unwrap() error { return ErrAgentExists }

// AgentUsage is one thing that references an Agent or pool: what it is, which
// one, and through which slot.
type AgentUsage struct {
	// Kind is what holds the reference: "pool", "binding", "project",
	// "config".
	Kind string `json:"kind"`

	// ID identifies the holder: a pool ID, a project ID, a config key.
	ID string `json:"id"`

	// Slot is where on the holder the reference sits: "worker", "triage",
	// "reviewer:<seat>", "default_lead", "member".
	Slot string `json:"slot"`
}

// String renders the usage for a person.
func (u AgentUsage) String() string {
	return u.Kind + " " + strconv.Quote(u.ID) + " (" + u.Slot + ")"
}

// AgentInUseError reports an Agent or pool that is still referenced. It
// carries every user so the operator can act on the refusal without a second
// query.
type AgentInUseError struct {
	Ref    AgentRef
	UsedBy []AgentUsage
}

// Error implements the error interface.
func (e *AgentInUseError) Error() string {
	users := make([]string, 0, len(e.UsedBy))
	for _, usage := range e.UsedBy {
		users = append(users, usage.String())
	}
	return ErrAgentInUse.Error() + ": " + e.Ref.String() + " is used by " + strings.Join(users, ", ")
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AgentInUseError) Unwrap() error { return ErrAgentInUse }

// AgentRoleMismatchError reports a slot bound to an Agent that does not
// declare the slot's role.
type AgentRoleMismatchError struct {
	Field   string
	AgentID string
	Role    AgentRole
}

// Error implements the error interface.
func (e *AgentRoleMismatchError) Error() string {
	return ErrAgentRoleMismatch.Error() + ": " + e.Field + ": agent " + strconv.Quote(e.AgentID) +
		" does not declare the " + strconv.Quote(string(e.Role)) + " role"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AgentRoleMismatchError) Unwrap() error { return ErrAgentRoleMismatch }

// AgentCatalogInvalidError reports an Agent naming a backend or model the live
// catalog does not offer. Known lists what the catalog does offer for the
// field, when it could say.
type AgentCatalogInvalidError struct {
	Field   string
	Value   string
	Known   []string
	Message string
}

// Error implements the error interface.
func (e *AgentCatalogInvalidError) Error() string {
	msg := ErrAgentCatalogInvalid.Error() + ": " + e.Field + " " + strconv.Quote(e.Value)
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if len(e.Known) > 0 {
		msg += " (known: " + strings.Join(e.Known, ", ") + ")"
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AgentCatalogInvalidError) Unwrap() error { return ErrAgentCatalogInvalid }

// AgentAttempt is one Agent a reference resolved to and why it did not take
// the work.
type AgentAttempt struct {
	AgentID string `json:"agent_id"`
	Reason  string `json:"reason"`
	Detail  string `json:"detail,omitempty"`
}

// AgentPoolExhaustedError reports every Agent of a reference tried and none
// taking the work. It unwraps to ErrAgentPoolExhausted AND to the last
// attempt's cause, so errors.Is(err, ErrAccountsSpent) still answers the
// question a caller would have asked of a single spawn.
type AgentPoolExhaustedError struct {
	Ref      AgentRef
	Attempts []AgentAttempt
	Last     error
}

// Error implements the error interface.
func (e *AgentPoolExhaustedError) Error() string {
	tried := make([]string, 0, len(e.Attempts))
	for _, attempt := range e.Attempts {
		tried = append(tried, attempt.AgentID+" ("+attempt.Reason+")")
	}
	return ErrAgentPoolExhausted.Error() + ": " + e.Ref.String() + ": " + strings.Join(tried, ", ")
}

// Unwrap returns the sentinel and the last cause for errors.Is support.
func (e *AgentPoolExhaustedError) Unwrap() []error {
	if e.Last == nil {
		return []error{ErrAgentPoolExhausted}
	}
	return []error{ErrAgentPoolExhausted, e.Last}
}

// AgentCatalogUnavailableError reports a reference that cannot be resolved
// because the deployment wires no agent catalog.
type AgentCatalogUnavailableError struct {
	Ref AgentRef
}

// Error implements the error interface.
func (e *AgentCatalogUnavailableError) Error() string {
	return ErrAgentCatalogUnavailable.Error() + ": cannot resolve " + e.Ref.String()
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AgentCatalogUnavailableError) Unwrap() error { return ErrAgentCatalogUnavailable }

// AgentCatalogStoreError indicates an internal store failure (marshal,
// corruption, backing store unavailable).
type AgentCatalogStoreError struct {
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *AgentCatalogStoreError) Error() string {
	return ErrAgentCatalogStore.Error() + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AgentCatalogStoreError) Unwrap() error { return ErrAgentCatalogStore }

// =============================================================================
// Classification for transports
// =============================================================================

// AgentCatalogFault is what kind of refusal a catalog error is, so every
// transport maps it to its own status the same way.
type AgentCatalogFault int

// The kinds of catalog refusal.
const (
	// AgentCatalogFaultInvalid is a request the catalog cannot accept: a
	// malformed Agent, a backend or model the live catalog lacks, an Agent
	// bound to a role it does not declare.
	AgentCatalogFaultInvalid AgentCatalogFault = iota + 1
	// AgentCatalogFaultNotFound names an Agent, pool or account nobody
	// defined.
	AgentCatalogFaultNotFound
	// AgentCatalogFaultConflict is a create of an ID already taken.
	AgentCatalogFaultConflict
	// AgentCatalogFaultInUse is a delete of something still referenced.
	AgentCatalogFaultInUse
	// AgentCatalogFaultForbidden is a caller acting on another tenant.
	AgentCatalogFaultForbidden
	// AgentCatalogFaultUnavailable is a catalog the deployment cannot reach.
	AgentCatalogFaultUnavailable
	// AgentCatalogFaultExhausted is a pool none of whose Agents took the work.
	AgentCatalogFaultExhausted
)

// agentCatalogFaults maps each catalog sentinel to its kind, in match order:
// exhaustion first, because it also unwraps to its last cause.
var agentCatalogFaults = []struct {
	cause error
	fault AgentCatalogFault
}{
	{ErrAgentPoolExhausted, AgentCatalogFaultExhausted},
	{ErrAgentSpendExhausted, AgentCatalogFaultExhausted},
	{ErrAgentSpendUndecidable, AgentCatalogFaultUnavailable},
	{ErrAgentUnknown, AgentCatalogFaultNotFound},
	{ErrAgentPoolUnknown, AgentCatalogFaultNotFound},
	{ErrAgentRoleMismatch, AgentCatalogFaultInvalid},
	{ErrAgentCatalogInvalid, AgentCatalogFaultInvalid},
	{ErrAgentExists, AgentCatalogFaultConflict},
	{ErrAgentInUse, AgentCatalogFaultInUse},
	{ErrAgentCatalogUnavailable, AgentCatalogFaultUnavailable},
	{ErrAgentCatalogStore, AgentCatalogFaultUnavailable},
}

// agentCatalogVerbFaults are the refusals a catalog VERB also makes that are
// not the catalog's own: shape validation, an unknown account, a caller of
// another tenant.
var agentCatalogVerbFaults = []struct {
	cause error
	fault AgentCatalogFault
}{
	{ErrInvalidInput, AgentCatalogFaultInvalid},
	{ErrAccountNotFound, AgentCatalogFaultNotFound},
	{ErrAccountTenantScope, AgentCatalogFaultForbidden},
}

// ClassifyAgentCatalogError reports which kind of catalog refusal err is,
// and false when it is not one the catalog makes.
func ClassifyAgentCatalogError(err error) (AgentCatalogFault, bool) {
	for _, mapping := range agentCatalogFaults {
		if errors.Is(err, mapping.cause) {
			return mapping.fault, true
		}
	}
	return 0, false
}

// ClassifyAgentCatalogVerbError is ClassifyAgentCatalogError for the answer
// to a catalog verb, which also refuses malformed input, an unknown account
// and a caller of another tenant.
func ClassifyAgentCatalogVerbError(err error) (AgentCatalogFault, bool) {
	if fault, ok := ClassifyAgentCatalogError(err); ok {
		return fault, true
	}
	for _, mapping := range agentCatalogVerbFaults {
		if errors.Is(err, mapping.cause) {
			return mapping.fault, true
		}
	}
	return 0, false
}
