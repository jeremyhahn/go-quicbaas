//go:build iam

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

package iam

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Audit storage key helpers. Audit events live under the canonical __iam:
// namespace so they are encrypted-at-rest in the owning tenant's barrier when
// the tenant is barrier-isolated (the routing KVStore selects that tenant's
// DEK and a sealed barrier tenant fails closed).
//
// Every audit read path carries the tenant explicitly — SaveEvent routes by
// event.TenantID, GetEvent/DeleteEventsBefore take a tenantID argument, and
// QueryEvents takes filter.TenantID — so the record key needs no tenant
// encoding: binding the tenant into ctx routes the operation to that tenant's
// store, and the record id alone is unique within it. Query and retention
// sweeps scan the per-tenant record prefix within the bound store.
const (
	keyPrefixAuditRec = KeyPrefixIAM + "audit/rec/"
)

func auditRecKey(id string) string { return keyPrefixAuditRec + id }

// BackendAuditStorage implements AuditStorage over a per-tenant routing
// KVStore. It is the durable, barrier-isolated counterpart to
// MemoryAuditStorage: events survive restarts and are encrypted-at-rest in
// the owning tenant's barrier. Routing every operation through the tenant
// barrier means an event written under tenant A is unreachable from tenant B's
// store, preserving tenant isolation by construction.
type BackendAuditStorage struct {
	kv interfaces.KVStore
}

// NewBackendAuditStorage constructs a persistent, per-tenant audit store
// backed by the routing KVStore.
func NewBackendAuditStorage(kv interfaces.KVStore) *BackendAuditStorage {
	return &BackendAuditStorage{kv: kv}
}

var _ AuditStorage = (*BackendAuditStorage)(nil)

// SaveEvent validates the event, routes by its TenantID, and writes the record
// into that tenant's barrier store.
func (s *BackendAuditStorage) SaveEvent(ctx context.Context, event *AuditEvent) error {
	if err := event.Validate(); err != nil {
		return err
	}
	ctx = withTenant(ctx, event.TenantID)
	data, err := json.Marshal(event)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.audit.save", Err: err}
	}
	return s.kv.Put(ctx, auditRecKey(event.ID), data)
}

// GetEvent routes by the explicit tenantID and reads the record, returning
// ErrAuditEventNotFound when the id is absent from that tenant's store or the
// stored record's tenant does not match (defense-in-depth against a forged id).
func (s *BackendAuditStorage) GetEvent(ctx context.Context, tenantID, eventID string) (*AuditEvent, error) {
	ctx = withTenant(ctx, tenantID)
	data, err := s.kv.Get(ctx, auditRecKey(eventID))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrAuditEventNotFound
		}
		return nil, err
	}
	var event AuditEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.audit.get", Err: err}
	}
	if event.TenantID != tenantID {
		return nil, ErrAuditEventNotFound
	}
	return &event, nil
}

// QueryEvents validates the filter, routes by filter.TenantID, scans the
// tenant's audit records, applies the filter predicates, sorts newest-first,
// and paginates. The total returned is the count of all matching events before
// pagination.
func (s *BackendAuditStorage) QueryEvents(ctx context.Context, filter *AuditFilter) ([]*AuditEvent, int, error) {
	if err := filter.Validate(); err != nil {
		return nil, 0, err
	}
	ctx = withTenant(ctx, filter.TenantID)

	matched := make([]*AuditEvent, 0)
	if err := s.kv.Scan(ctx, keyPrefixAuditRec, func(_ string, value []byte) error {
		var event AuditEvent
		if json.Unmarshal(value, &event) != nil {
			return nil
		}
		if event.TenantID != filter.TenantID {
			return nil
		}
		if auditEventMatchesFilter(&event, filter) {
			e := event
			matched = append(matched, &e)
		}
		return nil
	}); err != nil {
		return nil, 0, err
	}

	total := len(matched)
	sortAuditEventsByTimestamp(matched)

	start := filter.Offset
	if start >= len(matched) {
		return []*AuditEvent{}, total, nil
	}
	end := start + filter.Limit
	if filter.Limit <= 0 || end > len(matched) {
		end = len(matched)
	}
	return matched[start:end], total, nil
}

// DeleteEventsBefore routes by the explicit tenantID and removes every record
// in that tenant's store with a timestamp before the cutoff, returning the
// number deleted.
func (s *BackendAuditStorage) DeleteEventsBefore(ctx context.Context, tenantID string, before time.Time) (int, error) {
	ctx = withTenant(ctx, tenantID)

	var staleIDs []string
	if err := s.kv.Scan(ctx, keyPrefixAuditRec, func(_ string, value []byte) error {
		var event AuditEvent
		if json.Unmarshal(value, &event) != nil {
			return nil
		}
		if event.TenantID != tenantID {
			return nil
		}
		if event.Timestamp.Before(before) {
			staleIDs = append(staleIDs, event.ID)
		}
		return nil
	}); err != nil {
		return 0, err
	}

	count := 0
	for _, id := range staleIDs {
		if err := s.kv.Delete(ctx, auditRecKey(id)); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// Close releases the storage. The routing KVStore is owned by the server, so
// closing the audit store is a no-op here.
func (s *BackendAuditStorage) Close() error { return nil }

// auditEventMatchesFilter reports whether event satisfies every predicate set
// on filter. It mirrors MemoryAuditStorage.matchesFilter exactly so the two
// backends apply identical query semantics.
func auditEventMatchesFilter(event *AuditEvent, filter *AuditFilter) bool {
	if filter.StartTime != nil && event.Timestamp.Before(*filter.StartTime) {
		return false
	}
	if filter.EndTime != nil && event.Timestamp.After(*filter.EndTime) {
		return false
	}

	if len(filter.EventTypes) > 0 {
		found := false
		for _, et := range filter.EventTypes {
			if event.EventType == et {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(filter.Categories) > 0 {
		found := false
		for _, cat := range filter.Categories {
			if event.Category == cat {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(filter.Outcomes) > 0 {
		found := false
		for _, outcome := range filter.Outcomes {
			if event.Outcome == outcome {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if filter.ActorID != "" && (event.Actor == nil || event.Actor.UserID != filter.ActorID) {
		return false
	}

	if filter.ResourceID != "" && (event.Resource == nil || event.Resource.ID != filter.ResourceID) {
		return false
	}

	return true
}

// sortAuditEventsByTimestamp sorts events newest-first (descending timestamp).
func sortAuditEventsByTimestamp(events []*AuditEvent) {
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Timestamp.After(events[j].Timestamp)
	})
}
