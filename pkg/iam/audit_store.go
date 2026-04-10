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
	"sync"
	"time"
)

// AuditStorage defines the interface for audit event storage.
type AuditStorage interface {
	// SaveEvent persists an audit event
	SaveEvent(ctx context.Context, event *AuditEvent) error

	// GetEvent retrieves a specific audit event
	GetEvent(ctx context.Context, tenantID, eventID string) (*AuditEvent, error)

	// QueryEvents retrieves audit events matching the filter
	QueryEvents(ctx context.Context, filter *AuditFilter) ([]*AuditEvent, int, error)

	// DeleteEventsBefore deletes audit events older than the specified time
	DeleteEventsBefore(ctx context.Context, tenantID string, before time.Time) (int, error)

	// Close closes the storage
	Close() error
}

// MemoryAuditStorage is an in-memory implementation of AuditStorage.
type MemoryAuditStorage struct {
	mu sync.RWMutex

	// events stores all audit events by ID
	events map[string]*AuditEvent

	// tenantIndex indexes events by tenant for fast filtering
	tenantIndex map[string][]string

	// maxEventsPerTenant limits memory growth
	maxEventsPerTenant int
}

// MemoryAuditStorageOption configures the memory audit storage.
type MemoryAuditStorageOption func(*MemoryAuditStorage)

// WithMaxEventsPerTenant sets the maximum events to retain per tenant.
func WithMaxEventsPerTenant(max int) MemoryAuditStorageOption {
	return func(s *MemoryAuditStorage) {
		s.maxEventsPerTenant = max
	}
}

// NewMemoryAuditStorage creates a new in-memory audit storage.
func NewMemoryAuditStorage(opts ...MemoryAuditStorageOption) *MemoryAuditStorage {
	s := &MemoryAuditStorage{
		events:             make(map[string]*AuditEvent),
		tenantIndex:        make(map[string][]string),
		maxEventsPerTenant: 10000, // Default limit
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// SaveEvent persists an audit event.
func (s *MemoryAuditStorage) SaveEvent(ctx context.Context, event *AuditEvent) error {
	if err := event.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Store event
	s.events[event.ID] = event

	// Update tenant index
	tenantEvents, exists := s.tenantIndex[event.TenantID]
	if !exists {
		tenantEvents = make([]string, 0, 100)
	}
	tenantEvents = append(tenantEvents, event.ID)
	s.tenantIndex[event.TenantID] = tenantEvents

	// Enforce max events per tenant (FIFO eviction)
	if len(tenantEvents) > s.maxEventsPerTenant {
		// Remove oldest event
		oldestID := tenantEvents[0]
		delete(s.events, oldestID)
		s.tenantIndex[event.TenantID] = tenantEvents[1:]
	}

	return nil
}

// GetEvent retrieves a specific audit event.
func (s *MemoryAuditStorage) GetEvent(ctx context.Context, tenantID, eventID string) (*AuditEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	event, exists := s.events[eventID]
	if !exists {
		return nil, ErrAuditEventNotFound
	}

	// Verify tenant ownership
	if event.TenantID != tenantID {
		return nil, ErrAuditEventNotFound
	}

	return event, nil
}

// QueryEvents retrieves audit events matching the filter.
func (s *MemoryAuditStorage) QueryEvents(ctx context.Context, filter *AuditFilter) ([]*AuditEvent, int, error) {
	if err := filter.Validate(); err != nil {
		return nil, 0, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get all events for tenant
	eventIDs, exists := s.tenantIndex[filter.TenantID]
	if !exists {
		return []*AuditEvent{}, 0, nil
	}

	// Filter and collect matching events
	matched := make([]*AuditEvent, 0)
	for _, eventID := range eventIDs {
		event := s.events[eventID]
		if event == nil {
			continue
		}

		if s.matchesFilter(event, filter) {
			matched = append(matched, event)
		}
	}

	total := len(matched)

	// Sort by timestamp (newest first)
	s.sortEventsByTimestamp(matched)

	// Apply pagination
	start := filter.Offset
	if start >= len(matched) {
		return []*AuditEvent{}, total, nil
	}

	end := start + filter.Limit
	if end > len(matched) {
		end = len(matched)
	}

	return matched[start:end], total, nil
}

// DeleteEventsBefore deletes audit events older than the specified time.
func (s *MemoryAuditStorage) DeleteEventsBefore(ctx context.Context, tenantID string, before time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	eventIDs, exists := s.tenantIndex[tenantID]
	if !exists {
		return 0, nil
	}

	count := 0
	remaining := make([]string, 0, len(eventIDs))

	for _, eventID := range eventIDs {
		event := s.events[eventID]
		if event != nil && event.Timestamp.Before(before) {
			delete(s.events, eventID)
			count++
		} else {
			remaining = append(remaining, eventID)
		}
	}

	s.tenantIndex[tenantID] = remaining

	return count, nil
}

// Close closes the storage.
func (s *MemoryAuditStorage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = nil
	s.tenantIndex = nil

	return nil
}

// matchesFilter checks if an event matches the filter criteria.
func (s *MemoryAuditStorage) matchesFilter(event *AuditEvent, filter *AuditFilter) bool {
	// Time range filter
	if filter.StartTime != nil && event.Timestamp.Before(*filter.StartTime) {
		return false
	}
	if filter.EndTime != nil && event.Timestamp.After(*filter.EndTime) {
		return false
	}

	// Event type filter
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

	// Category filter
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

	// Outcome filter
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

	// Actor filter
	if filter.ActorID != "" && (event.Actor == nil || event.Actor.UserID != filter.ActorID) {
		return false
	}

	// Resource filter
	if filter.ResourceID != "" && (event.Resource == nil || event.Resource.ID != filter.ResourceID) {
		return false
	}

	return true
}

// sortEventsByTimestamp sorts events by timestamp in descending order (newest first).
func (s *MemoryAuditStorage) sortEventsByTimestamp(events []*AuditEvent) {
	// Simple bubble sort for now - could use sort.Slice for better performance
	n := len(events)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if events[j].Timestamp.Before(events[j+1].Timestamp) {
				events[j], events[j+1] = events[j+1], events[j]
			}
		}
	}
}

// Ensure MemoryAuditStorage implements AuditStorage.
var _ AuditStorage = (*MemoryAuditStorage)(nil)
