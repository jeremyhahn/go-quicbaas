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
)

// =============================================================================
// The merge train's records, tenant-resolved
// =============================================================================
//
// Two records, both written under a version guard: the queue ENTRY, one per
// task, which the delivery that approved the change and the train that carries
// it both write; and the project's LEDGER, which holds the train in flight.
// Every write that loses to a concurrent one is refused with a
// VersionConflictError and writes nothing — a train and a delivery racing on
// one entry must not silently undo each other.
//
// THE RECORDS ARE WHAT A RESTART RESUMES FROM. A train job that dies mid-test
// finds its train, its probes and its verdicts on the ledger; a delivery that
// dies while its change rides finds the entry and waits on it again.

const (
	trainEntryEntityType  = "sdlc_train_entry"
	trainLedgerEntityType = "sdlc_train_ledger"

	trainEntryEntityName  = "merge train entry"
	trainLedgerEntityName = "merge train ledger"
)

// TrainStore reads and writes the merge train's records. *Store satisfies it.
type TrainStore interface {
	// GetTrainEntry returns a task's queue entry, or a NotFoundError.
	GetTrainEntry(ctx context.Context, tenantID, projectID, taskID string) (*TrainEntry, error)

	// SaveTrainEntry writes an entry guarded on the version it was read at;
	// zero creates one that must not exist yet.
	SaveTrainEntry(ctx context.Context, tenantID string, entry *TrainEntry) error

	// DeleteTrainEntry removes a task's entry. An absent entry is not an
	// error.
	DeleteTrainEntry(ctx context.Context, tenantID, projectID, taskID string) error

	// ListTrainEntries returns every entry of one project.
	ListTrainEntries(ctx context.Context, tenantID, projectID string) ([]*TrainEntry, error)

	// GetTrainLedger returns the project's ledger; a project that never
	// trained has an empty one at version zero.
	GetTrainLedger(ctx context.Context, tenantID, projectID string) (*TrainLedger, error)

	// SaveTrainLedger writes the ledger guarded on the version it was read at.
	SaveTrainLedger(ctx context.Context, tenantID string, ledger *TrainLedger) error
}

var _ TrainStore = (*Store)(nil)

// trainEntryRecordKey is where a task's queue entry lives.
func trainEntryRecordKey(projectID, taskID string) string {
	return recordKey(trainEntryEntityType, entityIDFor(trainEntryKey(projectID, taskID)))
}

// trainLedgerRecordKey is where a project's train ledger lives.
func trainLedgerRecordKey(projectID string) string {
	return recordKey(trainLedgerEntityType, entityIDFor(trainLedgerKey(projectID)))
}

// GetTrainEntry implements TrainStore.
func (s *Store) GetTrainEntry(ctx context.Context, tenantID, projectID, taskID string) (*TrainEntry, error) {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return nil, err
	}
	if err := validateKeySegment("task_id", taskID); err != nil {
		return nil, err
	}
	store, err := s.tenantKV(ctx, "GetTrainEntry", tenantID)
	if err != nil {
		return nil, err
	}
	entry, _, err := loadRecord[TrainEntry](ctx, "GetTrainEntry", store, trainEntryRecordKey(projectID, taskID))
	if err != nil {
		return nil, err
	}
	if entry == nil || entry.ProjectID != projectID || entry.TaskID != taskID {
		// Absent, or a digest collision: the slot holds another task's entry.
		return nil, &NotFoundError{Entity: trainEntryEntityName, TenantID: tenantID, ID: projectID + "/" + taskID}
	}
	entry.ID = entityIDFor(trainEntryKey(projectID, taskID))
	return entry, nil
}

// SaveTrainEntry implements TrainStore. On success the entry's Version and
// stamps are advanced in place.
func (s *Store) SaveTrainEntry(ctx context.Context, tenantID string, entry *TrainEntry) error {
	if err := entry.Validate(); err != nil {
		return err
	}
	store, err := s.guardedKV(ctx, "SaveTrainEntry", tenantID)
	if err != nil {
		return err
	}
	key := trainEntryRecordKey(entry.ProjectID, entry.TaskID)
	held, raw, err := loadRecord[TrainEntry](ctx, "SaveTrainEntry", store, key)
	if err != nil {
		return err
	}
	conflict := &VersionConflictError{
		Entity: trainEntryEntityName, TenantID: tenantID,
		ID: entry.ProjectID + "/" + entry.TaskID, Expected: entry.Version,
	}
	switch {
	case held == nil && entry.Version != 0:
		// Only a writer that read no entry, at version 0, may create one.
		return conflict
	case held != nil && (held.ProjectID != entry.ProjectID || held.TaskID != entry.TaskID || held.Version != entry.Version):
		return conflict
	}
	now := s.now()
	next := *entry
	next.ID = entityIDFor(trainEntryKey(entry.ProjectID, entry.TaskID))
	next.Version = entry.Version + 1
	next.UpdatedAt = now
	if entry.Version == 0 {
		next.CreatedAt = now
	}
	applied, err := storeRecord(ctx, "SaveTrainEntry", store, key, raw, &next)
	if err != nil {
		return err
	}
	if !applied {
		return conflict
	}
	*entry = next
	return nil
}

// DeleteTrainEntry implements TrainStore. The store removes only the exact
// entry read, so an entry saved after that read is kept and the delete is
// refused with a VersionConflictError.
func (s *Store) DeleteTrainEntry(ctx context.Context, tenantID, projectID, taskID string) error {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return err
	}
	if err := validateKeySegment("task_id", taskID); err != nil {
		return err
	}
	store, err := s.guardedKV(ctx, "DeleteTrainEntry", tenantID)
	if err != nil {
		return err
	}
	held, changed, err := removeRecord(ctx, "DeleteTrainEntry", store, trainEntryRecordKey(projectID, taskID), func(e *TrainEntry) bool {
		return e.ProjectID == projectID && e.TaskID == taskID
	})
	if err != nil {
		return err
	}
	if changed {
		return &VersionConflictError{
			Entity: trainEntryEntityName, TenantID: tenantID,
			ID: projectID + "/" + taskID, Expected: held.Version,
		}
	}
	return nil
}

// ListTrainEntries implements TrainStore.
func (s *Store) ListTrainEntries(ctx context.Context, tenantID, projectID string) ([]*TrainEntry, error) {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return nil, err
	}
	store, err := s.tenantKV(ctx, "ListTrainEntries", tenantID)
	if err != nil {
		return nil, err
	}
	entries, err := scanRecords[TrainEntry](ctx, "ListTrainEntries", store, trainEntryEntityType)
	if err != nil {
		return nil, err
	}
	var out []*TrainEntry
	for _, entry := range entries {
		if entry.ProjectID == projectID {
			entry.ID = entityIDFor(trainEntryKey(entry.ProjectID, entry.TaskID))
			out = append(out, entry)
		}
	}
	return out, nil
}

// GetTrainLedger implements TrainStore.
func (s *Store) GetTrainLedger(ctx context.Context, tenantID, projectID string) (*TrainLedger, error) {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return nil, err
	}
	store, err := s.tenantKV(ctx, "GetTrainLedger", tenantID)
	if err != nil {
		return nil, err
	}
	id := entityIDFor(trainLedgerKey(projectID))
	ledger, _, err := loadRecord[TrainLedger](ctx, "GetTrainLedger", store, trainLedgerRecordKey(projectID))
	if err != nil {
		return nil, err
	}
	if ledger == nil {
		return &TrainLedger{ID: id, ProjectID: projectID, Next: 1}, nil
	}
	if ledger.ProjectID != projectID {
		// A digest collision is refused rather than answered with another
		// project's train.
		return nil, &NotFoundError{Entity: trainLedgerEntityName, TenantID: tenantID, ID: projectID}
	}
	ledger.ID = id
	if ledger.Next < 1 {
		ledger.Next = 1
	}
	return ledger, nil
}

// SaveTrainLedger implements TrainStore. On success the ledger's Version and
// stamps are advanced in place.
func (s *Store) SaveTrainLedger(ctx context.Context, tenantID string, ledger *TrainLedger) error {
	if err := ledger.Validate(); err != nil {
		return err
	}
	store, err := s.guardedKV(ctx, "SaveTrainLedger", tenantID)
	if err != nil {
		return err
	}
	key := trainLedgerRecordKey(ledger.ProjectID)
	held, raw, err := loadRecord[TrainLedger](ctx, "SaveTrainLedger", store, key)
	if err != nil {
		return err
	}
	conflict := &VersionConflictError{
		Entity: trainLedgerEntityName, TenantID: tenantID, ID: ledger.ProjectID, Expected: ledger.Version,
	}
	switch {
	case held == nil && ledger.Version != 0:
		// Only a writer that read no ledger, at version 0, may create one.
		return conflict
	case held != nil && (held.ProjectID != ledger.ProjectID || held.Version != ledger.Version):
		return conflict
	}
	now := s.now()
	next := *ledger
	next.ID = entityIDFor(trainLedgerKey(ledger.ProjectID))
	next.Version = ledger.Version + 1
	next.UpdatedAt = now
	if ledger.Version == 0 {
		next.CreatedAt = now
	}
	applied, err := storeRecord(ctx, "SaveTrainLedger", store, key, raw, &next)
	if err != nil {
		return err
	}
	if !applied {
		return conflict
	}
	*ledger = next
	return nil
}
