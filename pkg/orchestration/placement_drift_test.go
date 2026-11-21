// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package orchestration

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type staticPlacementInventory struct {
	hosts []PlacementHost
	err   error
}

func (s *staticPlacementInventory) PlacementHosts(context.Context, string) ([]PlacementHost, error) {
	return s.hosts, s.err
}

type staticPlacementWork struct {
	work []PlacementWork
	err  error
}

func (s *staticPlacementWork) PlacementWork(context.Context, string, string) ([]PlacementWork, error) {
	return s.work, s.err
}

// driftFleet is stated out of host order on purpose: a fixture already in
// sorted order would let an unsorted admissible list pass.
func driftFleet() []PlacementHost {
	return []PlacementHost{
		{Host: "h3", Capabilities: []string{"docker"}, NodeIDs: []string{"n3"}},
		{Host: "h1", Capabilities: []string{"docker", "gpu"}, NodeIDs: []string{"n1"}},
		{Host: "h2", Capabilities: []string{"docker", "gpu"}, NodeIDs: []string{"n2"}},
	}
}

func work(id, host string) PlacementWork {
	return PlacementWork{TenantID: "tenant-a", ID: id, Role: "review", Host: host}
}

// reportDrift declares spec in tenant-a and reports its drift over hosts and
// running.
func reportDrift(t *testing.T, spec *PlacementSpec, hosts []PlacementHost, running ...PlacementWork) *DriftReport {
	t.Helper()
	report, err := reportDriftErr(t, spec, hosts, running...)
	if err != nil {
		t.Fatalf("ReportDrift: %v", err)
	}
	return report
}

func reportDriftErr(t *testing.T, spec *PlacementSpec, hosts []PlacementHost, running ...PlacementWork) (*DriftReport, error) {
	t.Helper()
	store := newTestPlacementStore(t)
	if _, err := store.DeclarePlacement(context.Background(), "tenant-a", spec); err != nil {
		t.Fatalf("declare: %v", err)
	}
	reporter, err := NewDriftReporter(store, &staticPlacementInventory{hosts: hosts}, &staticPlacementWork{work: running})
	if err != nil {
		t.Fatalf("NewDriftReporter: %v", err)
	}
	return reporter.ReportDrift(context.Background(), "tenant-a", spec.Name)
}

func onlyFinding(t *testing.T, report *DriftReport) DriftFinding {
	t.Helper()
	if len(report.Findings) != 1 {
		t.Fatalf("findings = %+v, want exactly one", report.Findings)
	}
	return report.Findings[0]
}

func TestDriftReport_CleanFleet(t *testing.T) {
	t.Parallel()

	spec := &PlacementSpec{Name: "reviews", Role: "review", HostCount: 2, RequiredCapabilities: []string{"gpu"}, Actuate: true}
	report := reportDrift(t, spec, driftFleet(), work("s1", "h1"), work("s2", "h2"))
	if !report.Clean() || report.ObservedWork != 2 || report.Disposition != PlacementAutomatic || report.Generation != 1 {
		t.Fatalf("report = %+v, want clean over 2 sessions, automatic, generation 1", report)
	}
	if !reflect.DeepEqual(report.AdmissibleHosts, []string{"h1", "h2"}) {
		t.Fatalf("admissible = %v, want [h1 h2] in host order", report.AdmissibleHosts)
	}
}

func TestDriftReport_HostNotAdmitted(t *testing.T) {
	t.Parallel()

	spec := &PlacementSpec{Name: "reviews", Role: "review", Hosts: []string{"h2", "h1"}}
	got := onlyFinding(t, reportDrift(t, spec, driftFleet(), work("s1", "h1"), work("s3", "h3")))
	want := DriftFinding{Kind: DriftHostNotAdmitted, Field: "hosts", Host: "h3", WorkID: "s3",
		Remedy: DriftRemedy{Action: DriftRemedyRelocate, From: "h3", Destinations: []string{"h1", "h2"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("finding = %+v, want %+v", got, want)
	}
}

func TestDriftReport_CapabilityMissing(t *testing.T) {
	t.Parallel()

	spec := &PlacementSpec{Name: "reviews", Role: "review", RequiredCapabilities: []string{"gpu", "docker"}}
	got := onlyFinding(t, reportDrift(t, spec, driftFleet(), work("s3", "h3")))
	want := DriftFinding{Kind: DriftCapabilityMissing, Field: "required_capabilities", Host: "h3", WorkID: "s3",
		Missing: []string{"gpu"}, Remedy: DriftRemedy{Action: DriftRemedyRelocate, From: "h3", Destinations: []string{"h1", "h2"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("finding = %+v, want %+v", got, want)
	}
}

// Work on a host the inventory lacks cannot be checked, so it is drift,
// never compliance.
func TestDriftReport_HostUnregistered(t *testing.T) {
	t.Parallel()

	spec := &PlacementSpec{Name: "reviews", Role: "review", RequiredCapabilities: []string{"gpu"}}
	got := onlyFinding(t, reportDrift(t, spec, driftFleet(), work("s9", "h9")))
	if got.Kind != DriftHostUnregistered || got.Host != "h9" || got.WorkID != "s9" || got.Remedy.Action != DriftRemedyRelocate {
		t.Fatalf("finding = %+v, want s9 on h9 unregistered, relocate", got)
	}
}

func TestDriftReport_SpecHostAbsent(t *testing.T) {
	t.Parallel()

	spec := &PlacementSpec{Name: "reviews", Role: "review", Hosts: []string{"h1", "h7"}}
	got := onlyFinding(t, reportDrift(t, spec, driftFleet()))
	if got.Kind != DriftSpecHostAbsent || got.Host != "h7" || got.Field != "hosts" || got.Remedy.Action != DriftRemedyOperatorDecision {
		t.Fatalf("finding = %+v, want h7 absent, operator decision", got)
	}
}

// No admissible host: the report says so once, and the work's own finding
// says no host can take it rather than offering an empty destination list.
func TestDriftReport_NoAdmissibleHost(t *testing.T) {
	t.Parallel()

	spec := &PlacementSpec{Name: "reviews", Role: "review", RequiredCapabilities: []string{"tpu"}}
	report := reportDrift(t, spec, driftFleet(), work("s1", "h1"))
	if len(report.Findings) != 2 {
		t.Fatalf("findings = %+v, want capability_missing and no_admissible_host", report.Findings)
	}
	missing, none := report.Findings[0], report.Findings[1]
	if missing.Kind != DriftCapabilityMissing || missing.Remedy.Action != DriftRemedyNoCandidate || missing.Remedy.Destinations != nil {
		t.Fatalf("work finding = %+v, want no_candidate", missing)
	}
	if none.Kind != DriftNoAdmissibleHost {
		t.Fatalf("second finding = %+v, want no_admissible_host", none)
	}
}

// The only admissible host is the one the work is on: nowhere else to go.
func TestDriftReport_RelocateExcludesTheSourceHost(t *testing.T) {
	t.Parallel()

	spec := &PlacementSpec{Name: "reviews", Role: "review", Hosts: []string{"h3"}, RequiredCapabilities: []string{"docker"}}
	got := onlyFinding(t, reportDrift(t, spec, driftFleet(), work("s1", "h1")))
	if got.Kind != DriftHostNotAdmitted || !reflect.DeepEqual(got.Remedy.Destinations, []string{"h3"}) {
		t.Fatalf("finding = %+v, want relocate to h3", got)
	}
}

func TestDriftReport_HostCountShort(t *testing.T) {
	t.Parallel()

	spec := &PlacementSpec{Name: "reviews", Role: "review", HostCount: 3, RequiredCapabilities: []string{"gpu"}}
	got := onlyFinding(t, reportDrift(t, spec, driftFleet()))
	if got.Kind != DriftHostCountShort || got.Field != "host_count" {
		t.Fatalf("finding = %+v, want host_count_short", got)
	}
}

func TestDriftReport_HostCountExceeded(t *testing.T) {
	t.Parallel()

	spec := &PlacementSpec{Name: "reviews", Role: "review", HostCount: 1, RequiredCapabilities: []string{"docker"}}
	got := onlyFinding(t, reportDrift(t, spec, driftFleet(), work("s1", "h1"), work("s2", "h2"), work("s3", "h3")))
	if got.Kind != DriftHostCountExceeded || got.Field != "host_count" {
		t.Fatalf("finding = %+v, want host_count_exceeded", got)
	}
}

func TestDriftReport_HostCountMetIsClean(t *testing.T) {
	t.Parallel()

	spec := &PlacementSpec{Name: "reviews", Role: "review", HostCount: 1, RequiredCapabilities: []string{"docker"}}
	if report := reportDrift(t, spec, driftFleet(), work("s1", "h1"), work("s2", "h1")); !report.Clean() {
		t.Fatalf("findings = %+v, want none", report.Findings)
	}
}

func TestDriftReport_RoleUnclassified(t *testing.T) {
	t.Parallel()

	spec := &PlacementSpec{Name: "builds", Role: "build", HostCount: 1, Actuate: true}
	report := reportDrift(t, spec, driftFleet())
	got := onlyFinding(t, report)
	if got.Kind != DriftRoleUnclassified || got.Field != "role" {
		t.Fatalf("finding = %+v, want role_unclassified", got)
	}
	if report.Disposition != PlacementUnclassified || report.Disposition.PermitsSilentActuation() {
		t.Fatalf("disposition = %q, want unclassified and refused", report.Disposition)
	}
}

func TestPlacementDisposition(t *testing.T) {
	t.Parallel()

	cases := []struct {
		role    string
		actuate bool
		want    PlacementDisposition
		silent  bool
	}{
		{"review", false, PlacementForbidden, false},
		{"ci", true, PlacementAutomatic, true},
		{"review", true, PlacementAutomatic, true},
		{"agent", true, PlacementNeedsConsent, false},
		{"build", true, PlacementUnclassified, false},
	}
	for _, tc := range cases {
		got := placementDisposition(&PlacementSpec{Role: tc.role, Actuate: tc.actuate})
		if got != tc.want || got.PermitsSilentActuation() != tc.silent {
			t.Fatalf("%s actuate=%v: %q (silent %v), want %q (silent %v)", tc.role, tc.actuate, got, got.PermitsSilentActuation(), tc.want, tc.silent)
		}
	}
}

func TestDriftReport_RemediesDoNotShareMemory(t *testing.T) {
	t.Parallel()

	spec := &PlacementSpec{Name: "reviews", Role: "review", RequiredCapabilities: []string{"gpu"}}
	report := reportDrift(t, spec, driftFleet(), work("s3", "h3"), work("s9", "h9"))
	if len(report.Findings) != 2 {
		t.Fatalf("findings = %+v, want two", report.Findings)
	}
	report.Findings[0].Remedy.Destinations[0] = "scribbled"
	if report.AdmissibleHosts[0] != "h1" || report.Findings[1].Remedy.Destinations[0] != "h1" {
		t.Fatalf("a consumer's write to one remedy reached the report: %+v", report)
	}
}

// Work the report cannot name is refused with the work named.
func TestDriftReport_RefusesUnnameableWork(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		work  []PlacementWork
		field string
	}{
		"tenantless":   {[]PlacementWork{{ID: "s1", Role: "review", Host: "h1"}}, "tenant_id"},
		"other tenant": {[]PlacementWork{{TenantID: "tenant-b", ID: "s1", Role: "review", Host: "h1"}}, "tenant_id"},
		"idless":       {[]PlacementWork{{TenantID: "tenant-a", Role: "review", Host: "h1"}}, "id"},
		"seen twice":   {[]PlacementWork{work("s1", "h1"), work("s1", "h2")}, "id (seen twice)"},
		"hostless":     {[]PlacementWork{work("s1", "")}, "host"},
		"other role":   {[]PlacementWork{{TenantID: "tenant-a", ID: "s1", Role: "agent", Host: "h1"}}, "role"},
	}
	for name, tc := range cases {
		_, err := reportDriftErr(t, validPlacementSpec(), driftFleet(), tc.work...)
		var unnameable *PlacementWorkUnnameableError
		if !errors.As(err, &unnameable) || !errors.Is(err, ErrPlacementWorkUnnameable) || unnameable.Field != tc.field {
			t.Fatalf("%s: ReportDrift = %v, want unnameable work (%s)", name, err, tc.field)
		}
	}
}

func TestDriftReport_RefusesADuplicateHostDescription(t *testing.T) {
	t.Parallel()

	hosts := append(driftFleet(), PlacementHost{Host: "h1", NodeIDs: []string{"n4"}})
	if _, err := reportDriftErr(t, validPlacementSpec(), hosts); !errors.Is(err, ErrPlacementInventoryAmbiguous) {
		t.Fatalf("ReportDrift = %v, want ErrPlacementInventoryAmbiguous", err)
	}
}

func TestDriftReport_InputErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newTestPlacementStore(t)
	if _, err := store.DeclarePlacement(ctx, "tenant-a", validPlacementSpec()); err != nil {
		t.Fatalf("declare: %v", err)
	}
	boom := errors.New("down")

	for name, reporter := range map[string]*DriftReporter{
		"inventory": mustDriftReporter(t, store, &staticPlacementInventory{err: boom}, &staticPlacementWork{}),
		"work":      mustDriftReporter(t, store, &staticPlacementInventory{}, &staticPlacementWork{err: boom}),
	} {
		if _, err := reporter.ReportDrift(ctx, "tenant-a", "reviews"); !errors.Is(err, boom) {
			t.Fatalf("%s down: ReportDrift = %v, want its error", name, err)
		}
	}
	reporter := mustDriftReporter(t, store, &staticPlacementInventory{}, &staticPlacementWork{})
	if _, err := reporter.ReportDrift(ctx, "tenant-a", "absent"); !errors.Is(err, ErrPlacementNotFound) {
		t.Fatalf("absent spec: ReportDrift = %v, want ErrPlacementNotFound", err)
	}
	if _, err := NewDriftReporter(store, nil, &staticPlacementWork{}); !errors.Is(err, ErrDriftReporterIncomplete) {
		t.Fatalf("no inventory: NewDriftReporter = %v, want ErrDriftReporterIncomplete", err)
	}
}

func mustDriftReporter(t *testing.T, store PlacementStore, inventory PlacementInventory, source PlacementWorkSource) *DriftReporter {
	t.Helper()
	reporter, err := NewDriftReporter(store, inventory, source)
	if err != nil {
		t.Fatalf("NewDriftReporter: %v", err)
	}
	return reporter
}

// --- Field coverage -------------------------------------------------------

func TestPlacementFieldCoverage_AcceptsTheRealSpec(t *testing.T) {
	t.Parallel()

	if err := checkPlacementFieldCoverage(reflect.TypeOf(PlacementSpec{}), defaultPlacementFieldPartition()); err != nil {
		t.Fatalf("coverage of PlacementSpec: %v", err)
	}
}

// A spec carrying a field a later change forgets to classify is refused,
// named.
func TestPlacementFieldCoverage_RefusesAnUnclassifiedField(t *testing.T) {
	t.Parallel()

	// Every PlacementSpec field plus one a later change adds.
	var fields []reflect.StructField
	spec := reflect.TypeOf(PlacementSpec{})
	for i := 0; i < spec.NumField(); i++ {
		f := spec.Field(i)
		fields = append(fields, reflect.StructField{Name: f.Name, Type: f.Type, Tag: f.Tag})
	}
	fields = append(fields, reflect.StructField{Name: "MaxSessionsPerHost", Type: reflect.TypeOf(0)})

	err := checkPlacementFieldCoverage(reflect.StructOf(fields), defaultPlacementFieldPartition())
	var unclassified *PlacementFieldUnclassifiedError
	if !errors.As(err, &unclassified) || unclassified.Field != "MaxSessionsPerHost" || !errors.Is(err, ErrPlacementFieldCoverage) {
		t.Fatalf("coverage = %v, want MaxSessionsPerHost unclassified", err)
	}
}

func TestPlacementFieldCoverage_RefusesUnknownAndDoublyClassifiedFields(t *testing.T) {
	t.Parallel()
	spec := reflect.TypeOf(PlacementSpec{})

	unknown := defaultPlacementFieldPartition()
	unknown.inert["WorkloadClass"] = "renamed long ago"
	var notThere *PlacementFieldUnknownError
	if err := checkPlacementFieldCoverage(spec, unknown); !errors.As(err, &notThere) || notThere.Field != "WorkloadClass" {
		t.Fatalf("unknown field: %v, want WorkloadClass unknown", err)
	}

	both := defaultPlacementFieldPartition()
	both.inert["Hosts"] = "also inert"
	var ambiguous *PlacementFieldAmbiguousError
	if err := checkPlacementFieldCoverage(spec, both); !errors.As(err, &ambiguous) || ambiguous.Field != "Hosts" {
		t.Fatalf("doubly classified: %v, want Hosts ambiguous", err)
	}
}

// The check is wired into construction: a partition that forgets a field
// refuses to build a reporter.
func TestNewDriftReporter_RefusesAnIncompletePartition(t *testing.T) {
	t.Parallel()

	partition := defaultPlacementFieldPartition()
	delete(partition.constraints, "RequiredCapabilities")
	_, err := newDriftReporter(newTestPlacementStore(t), &staticPlacementInventory{}, &staticPlacementWork{}, partition)
	var unclassified *PlacementFieldUnclassifiedError
	if !errors.As(err, &unclassified) || unclassified.Field != "RequiredCapabilities" {
		t.Fatalf("newDriftReporter = %v, want RequiredCapabilities unclassified", err)
	}
}

// --- Session work source --------------------------------------------------

func TestSessionWorkSource_ListsTheRolesLiveSessions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sessions := NewInMemoryAgentStore()
	for _, record := range []*SessionRecord{
		{SessionID: "s1", TenantID: "tenant-a", Backend: "review-bot", HostAffinity: "h1", Status: SessionStateActive},
		{SessionID: "s2", TenantID: "tenant-a", Backend: "review-bot", HostAffinity: "h2", Status: SessionStateClosed},
		{SessionID: "s3", TenantID: "tenant-a", Backend: "claude", HostAffinity: "h1", Status: SessionStateIdle},
		{SessionID: "s4", TenantID: "tenant-b", Backend: "review-bot", HostAffinity: "h1", Status: SessionStateActive},
		{SessionID: "s5", TenantID: "tenant-a", Backend: "review-bot", HostAffinity: "h2", Status: SessionStateError},
	} {
		if err := sessions.PutSession(ctx, record); err != nil {
			t.Fatalf("PutSession: %v", err)
		}
	}
	roleOf := func(r *SessionRecord) string {
		if r.Backend == "review-bot" {
			return "review"
		}
		return "agent"
	}
	source, err := NewSessionWorkSource(sessions, roleOf)
	if err != nil {
		t.Fatalf("NewSessionWorkSource: %v", err)
	}
	got, err := source.PlacementWork(ctx, "tenant-a", "review")
	want := []PlacementWork{{TenantID: "tenant-a", ID: "s1", Role: "review", Host: "h1"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("PlacementWork = %+v, %v; want %+v", got, err, want)
	}

	if _, err := NewSessionWorkSource(sessions, nil); !errors.Is(err, ErrSessionWorkSourceIncomplete) {
		t.Fatalf("no role func: %v, want ErrSessionWorkSourceIncomplete", err)
	}
	if _, err := NewSessionWorkSource(nil, roleOf); !errors.Is(err, ErrSessionWorkSourceIncomplete) {
		t.Fatalf("no store: %v, want ErrSessionWorkSourceIncomplete", err)
	}
}

// A source that observes only some roles refuses a report for another role
// instead of listing no work for it; a source without a role set lists any.
func TestSessionWorkSource_RefusesUnobservedRole(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sessions := NewInMemoryAgentStore()
	for _, record := range []*SessionRecord{
		{SessionID: "s1", TenantID: "tenant-a", Backend: "review-bot", HostAffinity: "h1", Status: SessionStateActive},
		{SessionID: "s2", TenantID: "tenant-a", Backend: "claude", HostAffinity: "h2", Status: SessionStateActive},
	} {
		if err := sessions.PutSession(ctx, record); err != nil {
			t.Fatalf("PutSession: %v", err)
		}
	}
	roleOf := func(r *SessionRecord) string {
		if r.Backend == "review-bot" {
			return "review"
		}
		return "agent"
	}

	source, err := NewSessionWorkSourceForRoles(sessions, roleOf, "agent")
	if err != nil {
		t.Fatalf("NewSessionWorkSourceForRoles: %v", err)
	}
	_, err = source.PlacementWork(ctx, "tenant-a", "review")
	if !errors.Is(err, ErrPlacementRoleUnobserved) {
		t.Fatalf("review work = %v, want ErrPlacementRoleUnobserved", err)
	}
	var unobserved *PlacementRoleUnobservedError
	if !errors.As(err, &unobserved) || unobserved.Role != "review" {
		t.Fatalf("review work = %v, want a PlacementRoleUnobservedError for review", err)
	}
	got, err := source.PlacementWork(ctx, "tenant-a", "agent")
	want := []PlacementWork{{TenantID: "tenant-a", ID: "s2", Role: "agent", Host: "h2"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("agent work = %+v, %v; want %+v", got, err, want)
	}

	all, err := NewSessionWorkSource(sessions, roleOf)
	if err != nil {
		t.Fatalf("NewSessionWorkSource: %v", err)
	}
	got, err = all.PlacementWork(ctx, "tenant-a", "review")
	want = []PlacementWork{{TenantID: "tenant-a", ID: "s1", Role: "review", Host: "h1"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("review work without a role set = %+v, %v; want %+v", got, err, want)
	}

	if _, err := NewSessionWorkSourceForRoles(nil, roleOf, "agent"); !errors.Is(err, ErrSessionWorkSourceIncomplete) {
		t.Fatalf("no store: %v, want ErrSessionWorkSourceIncomplete", err)
	}
}

// A live session of the role that recorded no host reaches the reporter
// through the adapter and is refused there, named.
func TestDriftReport_RefusesAHostlessSessionFromTheStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sessions := NewInMemoryAgentStore()
	if err := sessions.PutSession(ctx, &SessionRecord{SessionID: "local-1", TenantID: "tenant-a", Backend: "claude", Status: SessionStateActive}); err != nil {
		t.Fatalf("PutSession: %v", err)
	}
	source, err := NewSessionWorkSource(sessions, func(*SessionRecord) string { return "review" })
	if err != nil {
		t.Fatalf("NewSessionWorkSource: %v", err)
	}
	store := newTestPlacementStore(t)
	if _, err := store.DeclarePlacement(ctx, "tenant-a", validPlacementSpec()); err != nil {
		t.Fatalf("declare: %v", err)
	}
	reporter := mustDriftReporter(t, store, &staticPlacementInventory{hosts: driftFleet()}, source)
	_, err = reporter.ReportDrift(ctx, "tenant-a", "reviews")
	var unnameable *PlacementWorkUnnameableError
	if !errors.As(err, &unnameable) || unnameable.WorkID != "local-1" || unnameable.Field != "host" {
		t.Fatalf("ReportDrift = %v, want local-1 refused as hostless", err)
	}
}
