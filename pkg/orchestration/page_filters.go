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

import "strings"

// =============================================================================
// List Filter Parsing
//
// ONE PARSER PER FILTER SHAPE, SHARED BY EVERY TRANSPORT, for the same reason
// ParsePage is the only place a page is parsed: `statuses=executng` — one
// letter short — must be the same refusal on REST, on MCP and in-process, and
// a transport that spelled the check itself would eventually spell it
// differently.
//
// A VALUE OUTSIDE THE ENUM IS REFUSED, NOT IGNORED. It is the one exception to
// "an unknown filter value matches nothing": a status the engine does not have
// cannot be a question about this tenant's records, so answering "there are no
// such tasks" tells the caller its typo is a fact about the data. Every other
// filter — a project id, an agent id, a runner name — names a record that may
// simply not exist, and an empty page is the honest answer to those.
// =============================================================================

// SplitCSVFilter reads a comma-separated list filter — `statuses`, `job_ids` —
// into its values. Blank entries are dropped, so "executing,,failed" and
// "executing,failed" ask the same question, and a wholly blank value narrows
// NOTHING rather than narrowing to nothing.
func SplitCSVFilter(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

// TaskStatusesFrom converts a transport's raw status names into the enum,
// refusing the first that is not a task status this engine has.
//
// It takes the values already split because two transports arrive with them
// in different shapes — REST with a comma-separated string, the SDK with a
// []string — and only the conversion and the refusal must be shared.
func TaskStatusesFrom(param string, values []string) ([]TaskStatus, error) {
	if len(values) == 0 {
		return nil, nil
	}
	statuses := make([]TaskStatus, 0, len(values))
	for _, value := range values {
		status := TaskStatus(value)
		if !status.Valid() {
			return nil, &InvalidFilterParamError{
				Param: param,
				Value: value + "; known task statuses are " + strings.Join(TaskStatusNames(), ", "),
			}
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// ParseTaskStatuses reads a comma-separated task-status filter.
func ParseTaskStatuses(param, raw string) ([]TaskStatus, error) {
	return TaskStatusesFrom(param, SplitCSVFilter(raw))
}

// JobStatesFrom converts a transport's raw state names into the enum,
// refusing the first that is not a job state this engine has.
func JobStatesFrom(param string, values []string) ([]JobState, error) {
	if len(values) == 0 {
		return nil, nil
	}
	states := make([]JobState, 0, len(values))
	for _, value := range values {
		state := JobState(value)
		if !state.Valid() {
			return nil, &InvalidFilterParamError{
				Param: param,
				Value: value + "; known job states are " + strings.Join(JobStateNames(), ", "),
			}
		}
		states = append(states, state)
	}
	return states, nil
}

// ParseJobStates reads a comma-separated job-state filter.
func ParseJobStates(param, raw string) ([]JobState, error) {
	return JobStatesFrom(param, SplitCSVFilter(raw))
}

// matchesAny reports whether value is one of the allowed set, or whether the
// set is empty — "no narrowing asked for" lists everything, everywhere.
func matchesAny[T ~string](value T, allowed []T) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

// matchesFilterValue reports whether a record's field satisfies one optional
// scalar filter. An empty filter matches every record, INCLUDING one whose
// field is itself empty; a set filter matches only an exact value, so an
// agent id nothing carries narrows to nothing rather than to everything.
func matchesFilterValue(field, filter string) bool {
	return filter == "" || field == filter
}
