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
	"time"

	"github.com/google/uuid"
)

// AuditLogger manages audit logging operations.
type AuditLogger interface {
	// Log synchronously logs an audit event
	Log(ctx context.Context, event *AuditEvent) error

	// LogAsync asynchronously logs an audit event (non-blocking for hot paths)
	LogAsync(event *AuditEvent)

	// Query retrieves audit events matching the filter
	Query(ctx context.Context, filter *AuditFilter) ([]*AuditEvent, int, error)

	// GetEvent retrieves a specific audit event
	GetEvent(ctx context.Context, tenantID, eventID string) (*AuditEvent, error)

	// DeleteOlderThan deletes audit events older than the specified time
	DeleteOlderThan(ctx context.Context, tenantID string, before time.Time) (int, error)

	// Close stops the async logger and closes storage
	Close() error
}

// DefaultAuditLogger implements AuditLogger.
type DefaultAuditLogger struct {
	storage       AuditStorage
	asyncChan     chan *AuditEvent
	asyncChanSize int
	stopChan      chan struct{}
	doneChan      chan struct{}
}

// AuditLoggerOption configures the audit logger.
type AuditLoggerOption func(*DefaultAuditLogger)

// WithAuditStorage sets the audit storage backend.
func WithAuditStorage(storage AuditStorage) AuditLoggerOption {
	return func(l *DefaultAuditLogger) {
		l.storage = storage
	}
}

// WithAsyncChannelSize sets the async event channel buffer size.
func WithAsyncChannelSize(size int) AuditLoggerOption {
	return func(l *DefaultAuditLogger) {
		l.asyncChanSize = size
	}
}

// NewAuditLogger creates a new audit logger.
func NewAuditLogger(opts ...AuditLoggerOption) (*DefaultAuditLogger, error) {
	l := &DefaultAuditLogger{
		storage:       NewMemoryAuditStorage(),
		asyncChanSize: 1000,
		stopChan:      make(chan struct{}),
		doneChan:      make(chan struct{}),
	}

	for _, opt := range opts {
		opt(l)
	}

	// Initialize async channel
	l.asyncChan = make(chan *AuditEvent, l.asyncChanSize)

	// Start async worker
	go l.asyncWorker()

	return l, nil
}

// Log synchronously logs an audit event.
func (l *DefaultAuditLogger) Log(ctx context.Context, event *AuditEvent) error {
	// Set timestamp if not already set
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Generate ID if not set
	if event.ID == "" {
		event.ID = uuid.New().String()
	}

	return l.storage.SaveEvent(ctx, event)
}

// LogAsync asynchronously logs an audit event (non-blocking for hot paths).
func (l *DefaultAuditLogger) LogAsync(event *AuditEvent) {
	// Set timestamp if not already set
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Generate ID if not set
	if event.ID == "" {
		event.ID = uuid.New().String()
	}

	// Try to send to async channel (non-blocking)
	select {
	case l.asyncChan <- event:
		// Event queued successfully
	default:
		// Channel full - drop event silently
		// In production, this could increment a metrics counter
	}
}

// Query retrieves audit events matching the filter.
func (l *DefaultAuditLogger) Query(ctx context.Context, filter *AuditFilter) ([]*AuditEvent, int, error) {
	filter.ApplyDefaults()
	return l.storage.QueryEvents(ctx, filter)
}

// GetEvent retrieves a specific audit event.
func (l *DefaultAuditLogger) GetEvent(ctx context.Context, tenantID, eventID string) (*AuditEvent, error) {
	return l.storage.GetEvent(ctx, tenantID, eventID)
}

// DeleteOlderThan deletes audit events older than the specified time.
func (l *DefaultAuditLogger) DeleteOlderThan(ctx context.Context, tenantID string, before time.Time) (int, error) {
	return l.storage.DeleteEventsBefore(ctx, tenantID, before)
}

// Close stops the async logger and closes storage.
func (l *DefaultAuditLogger) Close() error {
	// Signal stop
	close(l.stopChan)

	// Wait for async worker to finish
	<-l.doneChan

	// Close storage
	return l.storage.Close()
}

// asyncWorker processes async audit events in the background.
func (l *DefaultAuditLogger) asyncWorker() {
	defer close(l.doneChan)

	ctx := context.Background()

	for {
		select {
		case event := <-l.asyncChan:
			// Log event (ignore errors in async mode)
			_ = l.storage.SaveEvent(ctx, event)

		case <-l.stopChan:
			// Drain remaining events
			l.drainAsyncEvents(ctx)
			return
		}
	}
}

// drainAsyncEvents drains any remaining events in the async channel.
func (l *DefaultAuditLogger) drainAsyncEvents(ctx context.Context) {
	for {
		select {
		case event := <-l.asyncChan:
			_ = l.storage.SaveEvent(ctx, event)
		default:
			return
		}
	}
}

// LogAuthentication logs an authentication event.
func (l *DefaultAuditLogger) LogAuthentication(ctx context.Context, tenantID string, actor *AuditActor, action string, outcome AuditOutcome, details map[string]interface{}) error {
	event := &AuditEvent{
		TenantID:  tenantID,
		EventType: AuditEventAuthentication,
		Category:  AuditCategoryAuthentication,
		Actor:     actor,
		Action:    action,
		Outcome:   outcome,
		Details:   details,
	}

	return l.Log(ctx, event)
}

// LogAuthorization logs an authorization check event.
func (l *DefaultAuditLogger) LogAuthorization(ctx context.Context, tenantID string, actor *AuditActor, resource *AuditResource, action string, outcome AuditOutcome) error {
	event := &AuditEvent{
		TenantID:  tenantID,
		EventType: AuditEventAuthorization,
		Category:  AuditCategoryAuthorization,
		Actor:     actor,
		Resource:  resource,
		Action:    action,
		Outcome:   outcome,
	}

	return l.Log(ctx, event)
}

// LogAdminAction logs an administrative action.
func (l *DefaultAuditLogger) LogAdminAction(ctx context.Context, tenantID string, actor *AuditActor, resource *AuditResource, action string, details map[string]interface{}) error {
	// Determine event type based on resource type
	eventType := AuditEventUserManagement
	switch resource.Type {
	case "group":
		eventType = AuditEventGroupManagement
	case "role":
		eventType = AuditEventRoleManagement
	case "policy":
		eventType = AuditEventPolicyChange
	case "tenant":
		eventType = AuditEventTenantEvent
	case "app", "application":
		eventType = AuditEventAppEvent
	}

	event := &AuditEvent{
		TenantID:  tenantID,
		EventType: eventType,
		Category:  AuditCategoryAdministration,
		Actor:     actor,
		Resource:  resource,
		Action:    action,
		Outcome:   AuditOutcomeSuccess,
		Details:   details,
	}

	return l.Log(ctx, event)
}

// Ensure DefaultAuditLogger implements AuditLogger.
var _ AuditLogger = (*DefaultAuditLogger)(nil)
