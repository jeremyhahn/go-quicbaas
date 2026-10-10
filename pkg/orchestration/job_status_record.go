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
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

// jobStatusKeys is every JSON key a JobStatus encodes to: the part of a stored
// job record this build owns.
var jobStatusKeys = jsonKeys(reflect.TypeOf(JobStatus{}))

// jsonKeys lists the JSON keys the exported fields of struct type t encode to.
func jsonKeys(t reflect.Type) []string {
	keys := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		switch name {
		case "-":
			continue
		case "":
			name = field.Name
		}
		keys = append(keys, name)
	}
	return keys
}

// encodeJobStatusOver encodes status on top of held, the stored record it
// replaces, and returns the bytes to write.
//
// A RECORD IS NOT ONLY WHAT THIS BUILD KNOWS. A newer daemon sharing the store
// may have written fields JobStatus does not declare, and encoding the decoded
// struct alone would erase them on every amendment. So the replacement starts
// from held's own keys, drops every key JobStatus owns — a field status has
// cleared, which omitempty leaves out, must not keep its stored value — and
// lays status's encoding over what is left.
//
// held may be nil, for a record that does not exist yet.
func encodeJobStatusOver(held []byte, status *JobStatus) ([]byte, error) {
	record := map[string]json.RawMessage{}
	if len(held) > 0 {
		if err := json.Unmarshal(held, &record); err != nil {
			return nil, err
		}
	}
	for _, key := range jobStatusKeys {
		delete(record, key)
	}

	own, err := json.Marshal(status)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(own, &record); err != nil {
		return nil, err
	}
	return json.Marshal(record)
}

// mergeJobAmendments returns snapshot with the amendments stored already holds
// folded in, for a whole-snapshot write that must not erase them.
//
// A job's run publishes its state as a whole snapshot, but other writers amend
// the same record in between (updateJobStatus): a spawn appends a child or sets
// the lineage, and a session conclusion drops the session and appends the audit
// line saying so. The run's snapshot was built without those, so writing it as
// it stands would undo each of them. The snapshot keeps every field it owns;
// what an amender owns comes from the stored record:
//
//   - lineage (ParentJobID, SpawnedBy, SpawnedAt) where the snapshot has none;
//   - every child the stored record lists, ahead of any only the snapshot has;
//   - every audit event on the stored trail that the snapshot lacks;
//   - no session the stored trail records as concluded.
//
// Neither argument is modified.
func mergeJobAmendments(snapshot, stored *JobStatus) *JobStatus {
	merged := *snapshot
	if merged.ParentJobID == "" {
		merged.ParentJobID = stored.ParentJobID
	}
	if merged.SpawnedBy == "" {
		merged.SpawnedBy = stored.SpawnedBy
	}
	if merged.SpawnedAt == 0 {
		merged.SpawnedAt = stored.SpawnedAt
	}
	merged.ChildJobIDs = unionStrings(stored.ChildJobIDs, snapshot.ChildJobIDs)
	merged.Events = mergeAuditTrails(snapshot.Events, stored.Events)
	merged.SessionIDs = withoutConcludedSessions(snapshot.SessionIDs, stored)
	return &merged
}

// unionStrings returns first followed by each element of then that first does
// not already hold.
func unionStrings(first, then []string) []string {
	if len(then) == 0 {
		return first
	}
	seen := make(map[string]struct{}, len(first)+len(then))
	union := make([]string, 0, len(first)+len(then))
	for _, list := range [][]string{first, then} {
		for _, item := range list {
			if _, dup := seen[item]; dup {
				continue
			}
			seen[item] = struct{}{}
			union = append(union, item)
		}
	}
	return union
}

// mergeAuditTrails returns own with every event of stored that own lacks
// added, ordered by timestamp. Events are compared whole and counted, so an
// event recorded twice on purpose is kept twice. own is returned unchanged
// when stored adds nothing.
func mergeAuditTrails(own, stored []JobAuditEvent) []JobAuditEvent {
	held := make(map[JobAuditEvent]int, len(own))
	for _, event := range own {
		held[event]++
	}
	var missing []JobAuditEvent
	for _, event := range stored {
		if held[event] > 0 {
			held[event]--
			continue
		}
		missing = append(missing, event)
	}
	if len(missing) == 0 {
		return own
	}

	merged := make([]JobAuditEvent, 0, len(own)+len(missing))
	merged = append(merged, own...)
	merged = append(merged, missing...)
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].Timestamp < merged[j].Timestamp
	})
	return merged
}

// withoutConcludedSessions returns ids minus every session stored's audit
// trail records as concluded. ids is returned unchanged when none is.
func withoutConcludedSessions(ids []string, stored *JobStatus) []string {
	live := make([]string, 0, len(ids))
	for _, id := range ids {
		if !sessionHasConclusionEvent(stored, id) {
			live = append(live, id)
		}
	}
	if len(live) == len(ids) {
		return ids
	}
	return live
}
