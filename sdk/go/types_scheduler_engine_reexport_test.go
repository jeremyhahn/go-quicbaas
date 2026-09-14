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

package quicbaas

import (
	"reflect"
	"testing"
)

// The engine's method set is the yardstick here, derived independently of the
// aliases under test: it is what a consumer holding a SchedulerEngine must
// satisfy, and it comes from go-quicbaas/pkg/scheduler rather than from this
// file. An alias re-pointed at a lookalike struct would still compile and
// would fail these comparisons.
//
// Without these aliases each method below is present on the engine and
// uncallable through the SDK, because naming its parameter or reading its
// result requires importing go-quicbaas/pkg/scheduler — which the Layer-5
// boundary forbids and pkg/arch/layerguard_test.go fails the build on.

// schedulerEngineMethod resolves one method on *SchedulerEngine, failing the
// test if the engine no longer has it under that name.
func schedulerEngineMethod(t *testing.T, name string) reflect.Method {
	t.Helper()

	m, ok := reflect.TypeOf((*SchedulerEngine)(nil)).MethodByName(name)
	if !ok {
		t.Fatalf("SchedulerEngine has no method %s", name)
	}
	return m
}

// TestJobUpdateParamsIsUpdateJobsParameterType is the gap reported in #70:
// UpdateJob was reachable and its argument type was not.
func TestJobUpdateParamsIsUpdateJobsParameterType(t *testing.T) {
	// UpdateJob(receiver, ctx, jobID, tenantID, updates)
	m := schedulerEngineMethod(t, "UpdateJob")

	if got, want := m.Type.In(4), reflect.TypeOf(&JobUpdateParams{}); got != want {
		t.Errorf("UpdateJob's updates parameter is %s, SDK alias is %s", got, want)
	}
}

// TestJobExecutionIsGetJobHistorysElementType covers the second unreachable
// type: GetJobHistory returns []*JobExecution.
func TestJobExecutionIsGetJobHistorysElementType(t *testing.T) {
	m := schedulerEngineMethod(t, "GetJobHistory")

	if got, want := m.Type.Out(0), reflect.TypeOf([]*JobExecution(nil)); got != want {
		t.Errorf("GetJobHistory returns %s, SDK alias slice is %s", got, want)
	}
}

// TestSchedulerEngineStatsIsStatsReturnType covers Stats.
func TestSchedulerEngineStatsIsStatsReturnType(t *testing.T) {
	m := schedulerEngineMethod(t, "Stats")

	if got, want := m.Type.Out(0), reflect.TypeOf(&SchedulerEngineStats{}); got != want {
		t.Errorf("Stats returns %s, SDK alias is %s", got, want)
	}
}

// TestSchedulerEngineConfigIsConfigReturnType covers Config, and guards the
// name specifically: the unqualified SchedulerConfig in server.go is the
// deployment config type, a different type entirely, and aliasing the engine's
// config to that name does not compile.
func TestSchedulerEngineConfigIsConfigReturnType(t *testing.T) {
	m := schedulerEngineMethod(t, "Config")

	if got, want := m.Type.Out(0), reflect.TypeOf(&SchedulerEngineConfig{}); got != want {
		t.Errorf("Config returns %s, SDK alias is %s", got, want)
	}

	if reflect.TypeOf(SchedulerEngineConfig{}) == reflect.TypeOf(SchedulerConfig{}) {
		t.Error("SchedulerEngineConfig and SchedulerConfig resolve to the same type; " +
			"one of them no longer names what its documentation says")
	}
}

// TestEveryEngineMethodTypeIsReachableThroughTheSDK is the enumeration that
// stops this gap from recurring one type at a time. It walks the engine's
// whole exported method set and requires every named type in every signature
// to come from a package the SDK re-exports, rather than checking only the
// types someone has already reported.
func TestEveryEngineMethodTypeIsReachableThroughTheSDK(t *testing.T) {
	// Types reachable through this package, keyed by the engine type they
	// alias. Built from the aliases themselves, so adding an alias admits its
	// type and removing one withdraws it.
	reachable := map[reflect.Type]string{
		reflect.TypeOf(SchedulerEngine{}):         "SchedulerEngine",
		reflect.TypeOf(Job{}):                     "Job",
		reflect.TypeOf(JobQuery{}):                "JobQuery",
		reflect.TypeOf(JobResult{}):               "JobResult",
		reflect.TypeOf(JobUpdateParams{}):         "JobUpdateParams",
		reflect.TypeOf(JobExecution{}):            "JobExecution",
		reflect.TypeOf(SchedulerEngineStats{}):    "SchedulerEngineStats",
		reflect.TypeOf(SchedulerEngineConfig{}):   "SchedulerEngineConfig",
		reflect.TypeOf(JobType("")):               "JobType",
		reflect.TypeOf(JobStatus("")):             "JobStatus",
		reflect.TypeOf(OwnerType("")):             "OwnerType",
		reflect.TypeOf(JobHandler(nil)):           "JobHandler",
		reflect.TypeOf(AbandonedRunAttestation{}): "AbandonedRunAttestation",
	}

	enginePkg := reflect.TypeOf(SchedulerEngine{}).PkgPath()
	engine := reflect.TypeOf((*SchedulerEngine)(nil))

	for i := range engine.NumMethod() {
		m := engine.Method(i)

		var sigTypes []reflect.Type
		for j := range m.Type.NumIn() {
			sigTypes = append(sigTypes, m.Type.In(j))
		}
		for j := range m.Type.NumOut() {
			sigTypes = append(sigTypes, m.Type.Out(j))
		}

		for _, typ := range sigTypes {
			for _, named := range namedTypesIn(typ) {
				if named.PkgPath() != enginePkg {
					continue
				}
				if _, ok := reachable[named]; !ok {
					t.Errorf("SchedulerEngine.%s uses %s.%s, which this SDK does not re-export; "+
						"the method is present on the engine and uncallable by a consumer",
						m.Name, named.PkgPath(), named.Name())
				}
			}
		}
	}
}

// namedTypesIn collects every named type a signature position depends on.
//
// It descends through the type constructors a signature can be written with —
// pointers, slices, arrays, channels, maps (both key and value) and funcs
// (both parameters and results) — and reports the named types it reaches.
// A named func, map, slice, chan or pointer type is reported AND descended
// into, because a consumer that must write a value of it needs every type in
// its shape: JobHandler is a named func whose signature mentions Job and
// JobResult, and re-exporting the name alone would not make it writable.
//
// Named struct types are reported but not descended into: their fields are
// reachable through the alias itself, and descending would drag in every
// field's type rather than the signature's.
//
// The visited set makes the descent terminate on recursive types.
func namedTypesIn(t reflect.Type) []reflect.Type {
	var found []reflect.Type
	visited := map[reflect.Type]bool{}

	var walk func(reflect.Type)
	walk = func(t reflect.Type) {
		if t == nil || visited[t] {
			return
		}
		visited[t] = true

		if t.Name() != "" {
			found = append(found, t)
			if t.Kind() == reflect.Struct || t.Kind() == reflect.Interface {
				return
			}
		}

		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Chan:
			walk(t.Elem())
		case reflect.Map:
			walk(t.Key())
			walk(t.Elem())
		case reflect.Func:
			for i := range t.NumIn() {
				walk(t.In(i))
			}
			for i := range t.NumOut() {
				walk(t.Out(i))
			}
		}
	}

	walk(t)
	return found
}

// TestNamedTypesInReachesNestedPositions substantiates namedTypesIn's contract
// directly. No engine method currently carries an engine type nested inside a
// map or a function signature, so nothing else here would notice if the helper
// stopped reaching those positions — and a reachability guard that silently
// stopped looking would be worse than none.
func TestNamedTypesInReachesNestedPositions(t *testing.T) {
	tests := []struct {
		name string
		typ  reflect.Type
		want []reflect.Type
	}{
		{
			name: "through a pointer",
			typ:  reflect.TypeOf(&Job{}),
			want: []reflect.Type{reflect.TypeOf(Job{})},
		},
		{
			name: "through a slice of pointers",
			typ:  reflect.TypeOf([]*JobExecution(nil)),
			want: []reflect.Type{reflect.TypeOf(JobExecution{})},
		},
		{
			name: "through both halves of a map",
			typ:  reflect.TypeOf(map[JobStatus]*JobExecution(nil)),
			want: []reflect.Type{reflect.TypeOf(JobStatus("")), reflect.TypeOf(JobExecution{})},
		},
		{
			name: "through an unnamed func's parameters and results",
			typ:  reflect.TypeOf(func(*JobQuery) (*JobResult, error) { return nil, nil }),
			want: []reflect.Type{reflect.TypeOf(JobQuery{}), reflect.TypeOf(JobResult{})},
		},
		{
			name: "into a named func's own signature",
			typ:  reflect.TypeOf(JobHandler(nil)),
			want: []reflect.Type{
				reflect.TypeOf(JobHandler(nil)),
				reflect.TypeOf(Job{}),
				reflect.TypeOf(JobResult{}),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			found := map[reflect.Type]bool{}
			for _, typ := range namedTypesIn(tc.typ) {
				found[typ] = true
			}

			for _, want := range tc.want {
				if !found[want] {
					t.Errorf("namedTypesIn(%s) did not reach %s", tc.typ, want)
				}
			}
		})
	}
}
