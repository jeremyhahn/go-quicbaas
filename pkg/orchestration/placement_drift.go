// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package orchestration

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
)

// The drift report compares a placement spec against the hosts that exist
// and the work that runs on them. It is read-only: it moves nothing. It is
// what an operator trusts instead of looking, so a silent omission would read
// as "no drift". Two rules keep it from answering wrongly:
//
//   - Every PlacementSpec field is classified, at construction, as a
//     constraint (with the check that drives it) or as deliberately not one.
//     A field in neither, in both, or named but absent from the struct
//     refuses to build a reporter.
//   - Work the report cannot name (no tenant, no id, no recorded host, or an
//     id seen twice) is refused, never compared: one finding about two
//     indistinguishable sessions could be applied to the wrong one.

// DriftFindingKind is one kind of drift, from a closed set.
type DriftFindingKind string

const (
	// DriftHostNotAdmitted: work runs on a host the spec's Hosts do not name.
	DriftHostNotAdmitted DriftFindingKind = "host_not_admitted"
	// DriftCapabilityMissing: work runs on a host that lacks a required
	// capability.
	DriftCapabilityMissing DriftFindingKind = "capability_missing"
	// DriftHostUnregistered: work runs on a host the inventory does not
	// contain, so its capabilities cannot be checked and compliance cannot
	// be claimed.
	DriftHostUnregistered DriftFindingKind = "host_unregistered"
	// DriftSpecHostAbsent: the spec names a host the inventory does not
	// contain.
	DriftSpecHostAbsent DriftFindingKind = "spec_host_absent"
	// DriftNoAdmissibleHost: no host in the inventory satisfies the spec.
	DriftNoAdmissibleHost DriftFindingKind = "no_admissible_host"
	// DriftHostCountShort: fewer hosts satisfy the spec than its host_count.
	DriftHostCountShort DriftFindingKind = "host_count_short"
	// DriftHostCountExceeded: work of the role runs on more distinct hosts
	// than the spec's host_count.
	DriftHostCountExceeded DriftFindingKind = "host_count_exceeded"
	// DriftRoleUnclassified: the spec's role is not in the class table, so
	// whether its work may move without a person's consent is unknown.
	DriftRoleUnclassified DriftFindingKind = "role_unclassified"
)

// DriftRemedyAction is what a finding asks for.
type DriftRemedyAction string

const (
	// DriftRemedyRelocate: move the work to one of Destinations.
	DriftRemedyRelocate DriftRemedyAction = "relocate"
	// DriftRemedyNoCandidate: the work should move, but no host may take
	// it. Its own action rather than an empty destination list, because the
	// two need different responses.
	DriftRemedyNoCandidate DriftRemedyAction = "no_candidate"
	// DriftRemedyOperatorDecision: only the operator can resolve it (edit
	// the spec, enrol a host).
	DriftRemedyOperatorDecision DriftRemedyAction = "operator_decision"
)

// DriftRemedy is a finding's remedy. Destinations are the admissible hosts
// other than From, ordered by host; the actuator asks admission about the
// one it picks immediately before it moves anything.
type DriftRemedy struct {
	Action       DriftRemedyAction `json:"action"`
	From         string            `json:"from,omitempty"`
	Destinations []string          `json:"destinations,omitempty"`
}

// DriftFinding is one difference between a spec and the fleet.
type DriftFinding struct {
	Kind DriftFindingKind `json:"kind"`
	// Field is the spec field violated, by its JSON name; empty for a
	// finding no single field causes.
	Field string `json:"field,omitempty"`
	// Host is the host the finding is about, if any.
	Host string `json:"host,omitempty"`
	// WorkID names the work the finding is about, if any.
	WorkID string `json:"work_id,omitempty"`
	// Missing lists the required capabilities the host lacks.
	Missing []string    `json:"missing,omitempty"`
	Remedy  DriftRemedy `json:"remedy"`
}

// PlacementDisposition is what the actuator may do with a spec's drift.
type PlacementDisposition string

const (
	// PlacementForbidden: the spec's actuate is false; report only.
	PlacementForbidden PlacementDisposition = "forbidden"
	// PlacementAutomatic: fungible work, moved without asking.
	PlacementAutomatic PlacementDisposition = "automatic"
	// PlacementNeedsConsent: custody work, moved only on a person's
	// recorded consent.
	PlacementNeedsConsent PlacementDisposition = "needs_consent"
	// PlacementUnclassified: the role is not in the class table; refused.
	PlacementUnclassified PlacementDisposition = "unclassified"
)

// PermitsSilentActuation is the one phrasing of "may the actuator move this
// without a person agreeing". Every other phrasing ("not forbidden", "not
// needs consent") says nothing about unclassified, the value that exists to
// be refused.
func (d PlacementDisposition) PermitsSilentActuation() bool {
	return d == PlacementAutomatic
}

// placementRoleClasses is the single table of role classes. Fungible roles
// are fully described by their inputs; losing one costs a re-run. An agent
// session holds custody (a forge token, a signing key, in-flight work), so
// moving it moves custody.
var placementRoleClasses = map[string]PlacementDisposition{
	"ci":     PlacementAutomatic,
	"review": PlacementAutomatic,
	"agent":  PlacementNeedsConsent,
}

func placementDisposition(spec *PlacementSpec) PlacementDisposition {
	if !spec.Actuate {
		return PlacementForbidden
	}
	if disposition, ok := placementRoleClasses[spec.Role]; ok {
		return disposition
	}
	return PlacementUnclassified
}

// DriftReport is one spec's drift.
type DriftReport struct {
	TenantID    string               `json:"tenant_id"`
	Spec        string               `json:"spec"`
	Role        string               `json:"role"`
	Generation  uint64               `json:"generation"`
	Disposition PlacementDisposition `json:"disposition"`
	// ObservedWork is how much of the role's work was compared: a clean
	// report over forty sessions and one over none are different facts.
	ObservedWork int `json:"observed_work"`
	// AdmissibleHosts are the inventory's hosts that satisfy the spec,
	// ordered by host.
	AdmissibleHosts []string       `json:"admissible_hosts"`
	Findings        []DriftFinding `json:"findings"`
}

// Clean reports whether the report found no drift.
func (r *DriftReport) Clean() bool { return len(r.Findings) == 0 }

// PlacementWork is one running piece of a role's work, as the drift report
// sees it.
type PlacementWork struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
	Role     string `json:"role"`
	Host     string `json:"host"`
}

// PlacementWorkSource lists a tenant's live work of one role.
type PlacementWorkSource interface {
	PlacementWork(ctx context.Context, tenantID, role string) ([]PlacementWork, error)
}

// SessionRoleFunc names the role a session's work belongs to, or "" for
// none. A SessionRecord carries no role yet (open question 2 of the design
// note), so the caller supplies the classification.
type SessionRoleFunc func(*SessionRecord) string

// SessionWorkSource is the PlacementWorkSource over the session store: the
// tenant's sessions that are not closed and not failed, of the asked role,
// each named by
// its session id and the host it recorded landing on.
type SessionWorkSource struct {
	sessions AgentStore
	roleOf   SessionRoleFunc
}

// ErrSessionWorkSourceIncomplete refuses a SessionWorkSource without a
// store or a role function.
var ErrSessionWorkSourceIncomplete = errors.New("placement: session work source needs a session store and a role function")

// NewSessionWorkSource returns the work source over sessions.
func NewSessionWorkSource(sessions AgentStore, roleOf SessionRoleFunc) (*SessionWorkSource, error) {
	if sessions == nil || roleOf == nil {
		return nil, ErrSessionWorkSourceIncomplete
	}
	return &SessionWorkSource{sessions: sessions, roleOf: roleOf}, nil
}

// PlacementWork lists the tenant's sessions of role that are not closed and
// not failed: a failed session's record is kept, but no process runs for it.
func (s *SessionWorkSource) PlacementWork(ctx context.Context, tenantID, role string) ([]PlacementWork, error) {
	records, err := s.sessions.ListSessions(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	var work []PlacementWork
	for _, record := range records {
		if record == nil || record.Status == SessionStateClosed || record.Status == SessionStateError || s.roleOf(record) != role {
			continue
		}
		work = append(work, PlacementWork{
			TenantID: record.TenantID,
			ID:       record.SessionID,
			Role:     role,
			Host:     record.HostAffinity,
		})
	}
	return work, nil
}

// --- Errors ---------------------------------------------------------------

var (
	// ErrPlacementFieldCoverage is the root of the three coverage refusals.
	ErrPlacementFieldCoverage = errors.New("placement: spec field coverage")
	// ErrPlacementWorkUnnameable refuses work the report cannot name.
	ErrPlacementWorkUnnameable = errors.New("placement: work cannot be named")
	// ErrDriftReporterIncomplete refuses a reporter missing an input.
	ErrDriftReporterIncomplete = errors.New("placement: drift reporter needs a spec store, an inventory and a work source")
)

// PlacementFieldUnclassifiedError: a spec field is neither a constraint nor
// classified as constraining nothing.
type PlacementFieldUnclassifiedError struct{ Field string }

func (e *PlacementFieldUnclassifiedError) Error() string {
	return fmt.Sprintf("placement: spec field %s is unclassified: decide what drift it causes", e.Field)
}
func (e *PlacementFieldUnclassifiedError) Unwrap() error { return ErrPlacementFieldCoverage }

// PlacementFieldUnknownError: the classification names a field the spec
// does not have.
type PlacementFieldUnknownError struct{ Field string }

func (e *PlacementFieldUnknownError) Error() string {
	return fmt.Sprintf("placement: classification names spec field %s, which does not exist", e.Field)
}
func (e *PlacementFieldUnknownError) Unwrap() error { return ErrPlacementFieldCoverage }

// PlacementFieldAmbiguousError: a field is classified both ways.
type PlacementFieldAmbiguousError struct{ Field string }

func (e *PlacementFieldAmbiguousError) Error() string {
	return fmt.Sprintf("placement: spec field %s is classified as a constraint and as none", e.Field)
}
func (e *PlacementFieldAmbiguousError) Unwrap() error { return ErrPlacementFieldCoverage }

// PlacementWorkUnnameableError names work the report refused, and the
// field that made it unnameable.
type PlacementWorkUnnameableError struct {
	TenantID string
	WorkID   string
	Field    string
}

func (e *PlacementWorkUnnameableError) Error() string {
	return fmt.Sprintf("placement: work %q of tenant %q cannot be named: %s", e.WorkID, e.TenantID, e.Field)
}
func (e *PlacementWorkUnnameableError) Unwrap() error { return ErrPlacementWorkUnnameable }

// --- Field coverage -------------------------------------------------------

// driftInput is what a constraint check reads.
type driftInput struct {
	spec       *PlacementSpec
	inventory  map[string]PlacementHost
	work       []PlacementWork // work on registered hosts only
	admissible []string
}

// placementConstraint returns the findings one spec field causes.
type placementConstraint func(in *driftInput) []DriftFinding

// placementFieldPartition classifies every PlacementSpec field (by Go
// name): a constraint with its check, or inert with the reason.
type placementFieldPartition struct {
	constraints map[string]placementConstraint
	inert       map[string]string
}

func defaultPlacementFieldPartition() placementFieldPartition {
	return placementFieldPartition{
		constraints: map[string]placementConstraint{
			"Role":                 roleConstraint,
			"HostCount":            hostCountConstraint,
			"Hosts":                hostsConstraint,
			"RequiredCapabilities": capabilitiesConstraint,
		},
		inert: map[string]string{
			"TenantID":   "identity: the store scopes the spec to it",
			"Name":       "identity within the tenant",
			"Profile":    "admission's divisor for a host's room, asked at actuation time, not a property of where work runs",
			"Actuate":    "decides the disposition, not whether there is drift",
			"Generation": "orders edits; the report carries it",
		},
	}
}

// checkPlacementFieldCoverage refuses a partition that does not classify
// every field of spec exactly once. It takes the type so a test can hand it
// a struct carrying a field a later change forgets to classify.
func checkPlacementFieldCoverage(spec reflect.Type, partition placementFieldPartition) error {
	fields := make(map[string]bool, spec.NumField())
	for i := 0; i < spec.NumField(); i++ {
		name := spec.Field(i).Name
		fields[name] = true
		_, constrained := partition.constraints[name]
		_, inert := partition.inert[name]
		switch {
		case constrained && inert:
			return &PlacementFieldAmbiguousError{Field: name}
		case !constrained && !inert:
			return &PlacementFieldUnclassifiedError{Field: name}
		}
	}
	for _, names := range [][]string{mapKeys(partition.constraints), mapKeys(partition.inert)} {
		for _, name := range names {
			if !fields[name] {
				return &PlacementFieldUnknownError{Field: name}
			}
		}
	}
	return nil
}

func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// --- Reporter -------------------------------------------------------------

// DriftReporter reports a spec's drift. It reads; it never writes.
type DriftReporter struct {
	specs     PlacementStore
	inventory PlacementInventory
	work      PlacementWorkSource
	partition placementFieldPartition
}

// NewDriftReporter returns a reporter over the three inputs. It refuses to
// build when a PlacementSpec field is unclassified.
func NewDriftReporter(specs PlacementStore, inventory PlacementInventory, work PlacementWorkSource) (*DriftReporter, error) {
	return newDriftReporter(specs, inventory, work, defaultPlacementFieldPartition())
}

// newDriftReporter takes the partition so a test proves the coverage check
// is wired into construction, not only correct in isolation.
func newDriftReporter(specs PlacementStore, inventory PlacementInventory, work PlacementWorkSource, partition placementFieldPartition) (*DriftReporter, error) {
	if specs == nil || inventory == nil || work == nil {
		return nil, ErrDriftReporterIncomplete
	}
	if err := checkPlacementFieldCoverage(reflect.TypeOf(PlacementSpec{}), partition); err != nil {
		return nil, err
	}
	return &DriftReporter{specs: specs, inventory: inventory, work: work, partition: partition}, nil
}

// ReportDrift reports the drift of the tenant's spec name.
func (r *DriftReporter) ReportDrift(ctx context.Context, tenantID, name string) (*DriftReport, error) {
	spec, err := r.specs.GetPlacement(ctx, tenantID, name)
	if err != nil {
		return nil, err
	}
	hosts, err := r.inventory.PlacementHosts(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	index, err := IndexPlacementHosts(hosts)
	if err != nil {
		return nil, err
	}
	work, err := r.work.PlacementWork(ctx, tenantID, spec.Role)
	if err != nil {
		return nil, err
	}
	if err := checkPlacementWorkNameable(tenantID, spec.Role, work); err != nil {
		return nil, err
	}

	report := &DriftReport{
		TenantID:        tenantID,
		Spec:            spec.Name,
		Role:            spec.Role,
		Generation:      spec.Generation,
		Disposition:     placementDisposition(spec),
		ObservedWork:    len(work),
		AdmissibleHosts: admissiblePlacementHosts(spec, index),
		Findings:        []DriftFinding{},
	}
	in := &driftInput{spec: spec, inventory: index, admissible: report.AdmissibleHosts}

	// Work on a host the inventory lacks escapes every field check, so it
	// is a finding of its own and the field checks see only the rest.
	for _, w := range work {
		if _, ok := index[w.Host]; !ok {
			report.Findings = append(report.Findings, DriftFinding{
				Kind: DriftHostUnregistered, Host: w.Host, WorkID: w.ID,
				Remedy: relocateRemedy(w.Host, in.admissible),
			})
			continue
		}
		in.work = append(in.work, w)
	}
	if len(in.admissible) == 0 {
		report.Findings = append(report.Findings, DriftFinding{
			Kind: DriftNoAdmissibleHost, Remedy: DriftRemedy{Action: DriftRemedyOperatorDecision},
		})
	}
	for _, field := range mapKeys(r.partition.constraints) {
		report.Findings = append(report.Findings, r.partition.constraints[field](in)...)
	}
	sort.SliceStable(report.Findings, func(i, j int) bool {
		a, b := report.Findings[i], report.Findings[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Host != b.Host {
			return a.Host < b.Host
		}
		return a.WorkID < b.WorkID
	})
	return report, nil
}

// checkPlacementWorkNameable refuses work with no tenant (or another
// tenant's), no id, no recorded host, another role, or an id seen twice.
func checkPlacementWorkNameable(tenantID, role string, work []PlacementWork) error {
	seen := make(map[string]bool, len(work))
	for _, w := range work {
		refuse := func(field string) error {
			return &PlacementWorkUnnameableError{TenantID: w.TenantID, WorkID: w.ID, Field: field}
		}
		switch {
		case w.TenantID == "" || w.TenantID != tenantID:
			return refuse("tenant_id")
		case w.ID == "":
			return refuse("id")
		case seen[w.ID]:
			return refuse("id (seen twice)")
		case w.Host == "":
			return refuse("host")
		case w.Role != role:
			return refuse("role")
		}
		seen[w.ID] = true
	}
	return nil
}

// admissiblePlacementHosts lists the inventory's hosts that satisfy the
// spec's Hosts and RequiredCapabilities, ordered by host.
func admissiblePlacementHosts(spec *PlacementSpec, index map[string]PlacementHost) []string {
	admissible := []string{}
	for _, host := range mapKeys(index) {
		if hostAdmitted(spec, host) && len(missingCapabilities(spec, index[host])) == 0 {
			admissible = append(admissible, host)
		}
	}
	return admissible
}

func hostAdmitted(spec *PlacementSpec, host string) bool {
	if len(spec.Hosts) == 0 {
		return true
	}
	for _, h := range spec.Hosts {
		if h == host {
			return true
		}
	}
	return false
}

func missingCapabilities(spec *PlacementSpec, host PlacementHost) []string {
	have := make(map[string]bool, len(host.Capabilities))
	for _, c := range host.Capabilities {
		have[c] = true
	}
	var missing []string
	for _, c := range spec.RequiredCapabilities {
		if !have[c] {
			missing = append(missing, c)
		}
	}
	sort.Strings(missing)
	return missing
}

// relocateRemedy moves work off from to an admissible host other than
// from, or says no host can take it. Each remedy owns its slice, so a
// consumer ranking one cannot reorder another or the report's list.
func relocateRemedy(from string, admissible []string) DriftRemedy {
	var destinations []string
	for _, host := range admissible {
		if host != from {
			destinations = append(destinations, host)
		}
	}
	if len(destinations) == 0 {
		return DriftRemedy{Action: DriftRemedyNoCandidate, From: from}
	}
	return DriftRemedy{Action: DriftRemedyRelocate, From: from, Destinations: destinations}
}

func roleConstraint(in *driftInput) []DriftFinding {
	if _, ok := placementRoleClasses[in.spec.Role]; ok {
		return nil
	}
	return []DriftFinding{{Kind: DriftRoleUnclassified, Field: "role", Remedy: DriftRemedy{Action: DriftRemedyOperatorDecision}}}
}

func hostCountConstraint(in *driftInput) []DriftFinding {
	if in.spec.HostCount == 0 {
		return nil
	}
	var findings []DriftFinding
	// Zero admissible hosts is already no_admissible_host.
	if len(in.admissible) > 0 && len(in.admissible) < in.spec.HostCount {
		findings = append(findings, DriftFinding{Kind: DriftHostCountShort, Field: "host_count", Remedy: DriftRemedy{Action: DriftRemedyOperatorDecision}})
	}
	// in.work holds work on registered hosts only: work on an unregistered
	// host is already host_unregistered.
	hosts := make(map[string]struct{}, len(in.work))
	for _, w := range in.work {
		hosts[w.Host] = struct{}{}
	}
	if len(hosts) > in.spec.HostCount {
		findings = append(findings, DriftFinding{Kind: DriftHostCountExceeded, Field: "host_count", Remedy: DriftRemedy{Action: DriftRemedyOperatorDecision}})
	}
	return findings
}

func hostsConstraint(in *driftInput) []DriftFinding {
	if len(in.spec.Hosts) == 0 {
		return nil
	}
	var findings []DriftFinding
	for _, host := range in.spec.Hosts {
		if _, ok := in.inventory[host]; !ok {
			findings = append(findings, DriftFinding{
				Kind: DriftSpecHostAbsent, Field: "hosts", Host: host,
				Remedy: DriftRemedy{Action: DriftRemedyOperatorDecision},
			})
		}
	}
	for _, w := range in.work {
		if !hostAdmitted(in.spec, w.Host) {
			findings = append(findings, DriftFinding{
				Kind: DriftHostNotAdmitted, Field: "hosts", Host: w.Host, WorkID: w.ID,
				Remedy: relocateRemedy(w.Host, in.admissible),
			})
		}
	}
	return findings
}

func capabilitiesConstraint(in *driftInput) []DriftFinding {
	var findings []DriftFinding
	for _, w := range in.work {
		if missing := missingCapabilities(in.spec, in.inventory[w.Host]); len(missing) > 0 {
			findings = append(findings, DriftFinding{
				Kind: DriftCapabilityMissing, Field: "required_capabilities", Host: w.Host, WorkID: w.ID,
				Missing: missing, Remedy: relocateRemedy(w.Host, in.admissible),
			})
		}
	}
	return findings
}
