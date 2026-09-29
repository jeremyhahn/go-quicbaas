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
	"errors"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
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

func (s *Store) trainEntryDAO(ctx context.Context, op, tenantID string) (qrdbsdk.GenericDAO[*TrainEntry], error) {
	return entityDAO(ctx, s, op, tenantID, trainEntryEntityType, func() *TrainEntry { return &TrainEntry{} })
}

func (s *Store) trainLedgerDAO(ctx context.Context, op, tenantID string) (qrdbsdk.GenericDAO[*TrainLedger], error) {
	return entityDAO(ctx, s, op, tenantID, trainLedgerEntityType, func() *TrainLedger { return &TrainLedger{} })
}

// GetTrainEntry implements TrainStore.
func (s *Store) GetTrainEntry(ctx context.Context, tenantID, projectID, taskID string) (*TrainEntry, error) {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return nil, err
	}
	if err := validateKeySegment("task_id", taskID); err != nil {
		return nil, err
	}
	dao, err := s.trainEntryDAO(ctx, "GetTrainEntry", tenantID)
	if err != nil {
		return nil, err
	}
	id := entityIDFor(trainEntryKey(projectID, taskID))
	entry, err := dao.Get(ctx, id)
	if err != nil {
		if qrdbsdk.IsDAONotFound(err) {
			return nil, &NotFoundError{Entity: trainEntryEntityName, TenantID: tenantID, ID: projectID + "/" + taskID}
		}
		return nil, backendError("GetTrainEntry", err)
	}
	if entry.ProjectID != projectID || entry.TaskID != taskID {
		// A digest collision: the slot holds another task's entry.
		return nil, &NotFoundError{Entity: trainEntryEntityName, TenantID: tenantID, ID: projectID + "/" + taskID}
	}
	entry.ID = id
	return entry, nil
}

// SaveTrainEntry implements TrainStore. On success the entry's Version and
// stamps are advanced in place.
func (s *Store) SaveTrainEntry(ctx context.Context, tenantID string, entry *TrainEntry) error {
	if err := entry.Validate(); err != nil {
		return err
	}
	dao, err := s.trainEntryDAO(ctx, "SaveTrainEntry", tenantID)
	if err != nil {
		return err
	}
	now := s.now()
	next := *entry
	next.ID = entityIDFor(trainEntryKey(entry.ProjectID, entry.TaskID))
	next.Version = entry.Version + 1
	next.UpdatedAt = now
	if entry.Version == 0 {
		next.CreatedAt = now
	}
	cond := qrdbsdk.Condition{Target: qrdbsdk.ConditionVersion, Operator: qrdbsdk.OpEqual, Value: entry.Version}
	if err := dao.SaveWithCondition(ctx, &next, cond); err != nil {
		var failed qrdbsdk.DAOConditionFailedError
		if errors.As(err, &failed) {
			return &VersionConflictError{
				Entity: trainEntryEntityName, TenantID: tenantID,
				ID: entry.ProjectID + "/" + entry.TaskID, Expected: entry.Version,
			}
		}
		return backendError("SaveTrainEntry", err)
	}
	*entry = next
	return nil
}

// DeleteTrainEntry implements TrainStore.
func (s *Store) DeleteTrainEntry(ctx context.Context, tenantID, projectID, taskID string) error {
	held, err := s.GetTrainEntry(ctx, tenantID, projectID, taskID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	dao, err := s.trainEntryDAO(ctx, "DeleteTrainEntry", tenantID)
	if err != nil {
		return err
	}
	if err := dao.Delete(ctx, held); err != nil {
		return backendError("DeleteTrainEntry", err)
	}
	return nil
}

// ListTrainEntries implements TrainStore.
func (s *Store) ListTrainEntries(ctx context.Context, tenantID, projectID string) ([]*TrainEntry, error) {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return nil, err
	}
	dao, err := s.trainEntryDAO(ctx, "ListTrainEntries", tenantID)
	if err != nil {
		return nil, err
	}
	var out []*TrainEntry
	pageErr := dao.ForEachPage(ctx, qrdbsdk.PageQuery{PageSize: listPageSize}, func(page qrdbsdk.PageResult[*TrainEntry]) error {
		for _, entry := range page.Entities {
			if entry.ProjectID == projectID {
				entry.ID = entityIDFor(trainEntryKey(entry.ProjectID, entry.TaskID))
				out = append(out, entry)
			}
		}
		return nil
	})
	if pageErr != nil {
		return nil, backendError("ListTrainEntries", pageErr)
	}
	return out, nil
}

// GetTrainLedger implements TrainStore.
func (s *Store) GetTrainLedger(ctx context.Context, tenantID, projectID string) (*TrainLedger, error) {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return nil, err
	}
	dao, err := s.trainLedgerDAO(ctx, "GetTrainLedger", tenantID)
	if err != nil {
		return nil, err
	}
	id := entityIDFor(trainLedgerKey(projectID))
	ledger, err := dao.Get(ctx, id)
	if err != nil {
		if qrdbsdk.IsDAONotFound(err) {
			return &TrainLedger{ID: id, ProjectID: projectID, Next: 1}, nil
		}
		return nil, backendError("GetTrainLedger", err)
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
	dao, err := s.trainLedgerDAO(ctx, "SaveTrainLedger", tenantID)
	if err != nil {
		return err
	}
	now := s.now()
	next := *ledger
	next.ID = entityIDFor(trainLedgerKey(ledger.ProjectID))
	next.Version = ledger.Version + 1
	next.UpdatedAt = now
	if ledger.Version == 0 {
		next.CreatedAt = now
	}
	cond := qrdbsdk.Condition{Target: qrdbsdk.ConditionVersion, Operator: qrdbsdk.OpEqual, Value: ledger.Version}
	if err := dao.SaveWithCondition(ctx, &next, cond); err != nil {
		var failed qrdbsdk.DAOConditionFailedError
		if errors.As(err, &failed) {
			return &VersionConflictError{
				Entity: trainLedgerEntityName, TenantID: tenantID, ID: ledger.ProjectID, Expected: ledger.Version,
			}
		}
		return backendError("SaveTrainLedger", err)
	}
	*ledger = next
	return nil
}
