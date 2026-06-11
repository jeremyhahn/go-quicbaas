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

package grpc

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// AuditServer implements the Audit gRPC service.
type AuditServer struct {
	iampb.UnimplementedAuditServiceServer
	auditService AuditService
	logger       *logger.Logger
}

// AuditService defines the interface for audit operations.
type AuditService interface {
	QueryAuditEvents(ctx context.Context, tenantID string, filter *iam.AuditFilter) ([]*iam.AuditEvent, int, error)
	GetAuditEvent(ctx context.Context, tenantID, eventID string) (*iam.AuditEvent, error)
}

// NewAuditServer creates a new Audit gRPC server.
func NewAuditServer(auditService AuditService, log *logger.Logger) *AuditServer {
	return &AuditServer{
		auditService: auditService,
		logger:       log.WithField("component", "audit-grpc-server"),
	}
}

// QueryAuditEvents queries audit events with filters.
func (s *AuditServer) QueryAuditEvents(ctx context.Context, req *iampb.QueryAuditEventsRequest) (*iampb.QueryAuditEventsResponse, error) {
	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}

	filter := &iam.AuditFilter{
		EventTypes: convertToAuditEventTypes(req.EventTypes),
		Categories: convertToAuditCategories(req.Categories),
		Outcomes:   convertToAuditOutcomes(req.Outcomes),
		ActorID:    req.ActorId,
		ResourceID: req.ResourceId,
		Limit:      int(req.Limit),
		Offset:     int(req.Offset),
	}

	if req.StartTime != nil {
		t := req.StartTime.AsTime()
		filter.StartTime = &t
	}
	if req.EndTime != nil {
		t := req.EndTime.AsTime()
		filter.EndTime = &t
	}

	events, total, err := s.auditService.QueryAuditEvents(ctx, req.TenantId, filter)
	if err != nil {
		s.logger.WithError(err).Error("Failed to query audit events")
		return nil, status.Errorf(codes.Internal, "failed to query audit events: %v", err)
	}

	pbEvents := make([]*iampb.AuditEvent, len(events))
	for i, event := range events {
		pbEvents[i] = auditEventToProto(event)
	}

	return &iampb.QueryAuditEventsResponse{
		Events: pbEvents,
		Total:  int32(total),
		Limit:  req.Limit,
		Offset: req.Offset,
	}, nil
}

// GetAuditEvent retrieves a specific audit event.
func (s *AuditServer) GetAuditEvent(ctx context.Context, req *iampb.GetAuditEventRequest) (*iampb.AuditEvent, error) {
	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.EventId == "" {
		return nil, status.Error(codes.InvalidArgument, "event_id is required")
	}

	event, err := s.auditService.GetAuditEvent(ctx, req.TenantId, req.EventId)
	if err != nil {
		s.logger.WithError(err).Error("Failed to get audit event")
		return nil, status.Errorf(codes.NotFound, "audit event %s not found", req.EventId)
	}

	return auditEventToProto(event), nil
}

// auditEventToProto converts an iam.AuditEvent to a proto AuditEvent.
func auditEventToProto(e *iam.AuditEvent) *iampb.AuditEvent {
	if e == nil {
		return nil
	}

	details := make(map[string]string)
	for k, v := range e.Details {
		if s, ok := v.(string); ok {
			details[k] = s
		}
	}

	// Extract actor info
	var actorID, actorType string
	if e.Actor != nil {
		actorID = e.Actor.UserID
		// Determine actor type from available fields
		if e.Actor.ClientID != "" {
			actorType = "service"
		} else {
			actorType = "user"
		}
	}

	// Extract resource info
	var resourceID string
	if e.Resource != nil {
		resourceID = e.Resource.ID
	}

	return &iampb.AuditEvent{
		Id:         e.ID,
		TenantId:   e.TenantID,
		EventType:  string(e.EventType),
		Category:   string(e.Category),
		Outcome:    string(e.Outcome),
		ActorId:    actorID,
		ActorType:  actorType,
		ResourceId: resourceID,
		Action:     e.Action,
		Details:    details,
		IpAddress:  e.IPAddress,
		UserAgent:  e.UserAgent,
		Timestamp:  timestamppb.New(e.Timestamp),
	}
}

// AuditServiceAdapter adapts iam.AuditLogger to AuditService interface.
type AuditServiceAdapter struct {
	auditLogger iam.AuditLogger
}

// NewAuditServiceAdapter creates a new audit service adapter.
func NewAuditServiceAdapter(auditLogger iam.AuditLogger) *AuditServiceAdapter {
	return &AuditServiceAdapter{
		auditLogger: auditLogger,
	}
}

// QueryAuditEvents queries audit events.
func (a *AuditServiceAdapter) QueryAuditEvents(ctx context.Context, tenantID string, filter *iam.AuditFilter) ([]*iam.AuditEvent, int, error) {
	// The AuditLogger interface may not have a query method, so we return empty for now
	// In a production implementation, this would query the audit log storage
	return []*iam.AuditEvent{}, 0, nil
}

// GetAuditEvent gets a specific audit event.
func (a *AuditServiceAdapter) GetAuditEvent(ctx context.Context, tenantID, eventID string) (*iam.AuditEvent, error) {
	return nil, status.Errorf(codes.NotFound, "audit event %s not found", eventID)
}

// Compile-time check that AuditServiceAdapter implements AuditService.
var _ AuditService = (*AuditServiceAdapter)(nil)

// MemoryAuditService is an in-memory implementation for testing.
type MemoryAuditService struct {
	events map[string]*iam.AuditEvent
}

// NewMemoryAuditService creates a new in-memory audit service.
func NewMemoryAuditService() *MemoryAuditService {
	return &MemoryAuditService{
		events: make(map[string]*iam.AuditEvent),
	}
}

// QueryAuditEvents queries audit events.
func (m *MemoryAuditService) QueryAuditEvents(ctx context.Context, tenantID string, filter *iam.AuditFilter) ([]*iam.AuditEvent, int, error) {
	var result []*iam.AuditEvent
	for _, e := range m.events {
		if e.TenantID != tenantID {
			continue
		}

		// Apply filters
		if len(filter.EventTypes) > 0 && !containsEventType(filter.EventTypes, e.EventType) {
			continue
		}
		if len(filter.Categories) > 0 && !containsCategory(filter.Categories, e.Category) {
			continue
		}
		if len(filter.Outcomes) > 0 && !containsOutcome(filter.Outcomes, e.Outcome) {
			continue
		}
		// Extract actor ID from nested Actor struct
		actorID := ""
		if e.Actor != nil {
			actorID = e.Actor.UserID
		}
		if filter.ActorID != "" && actorID != filter.ActorID {
			continue
		}
		// Extract resource ID from nested Resource struct
		resourceID := ""
		if e.Resource != nil {
			resourceID = e.Resource.ID
		}
		if filter.ResourceID != "" && resourceID != filter.ResourceID {
			continue
		}
		if filter.StartTime != nil && e.Timestamp.Before(*filter.StartTime) {
			continue
		}
		if filter.EndTime != nil && e.Timestamp.After(*filter.EndTime) {
			continue
		}

		result = append(result, e)
	}

	total := len(result)

	// Apply pagination
	if filter.Offset >= len(result) {
		return []*iam.AuditEvent{}, total, nil
	}
	result = result[filter.Offset:]
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}

	return result, total, nil
}

// GetAuditEvent gets a specific audit event.
func (m *MemoryAuditService) GetAuditEvent(ctx context.Context, tenantID, eventID string) (*iam.AuditEvent, error) {
	e, ok := m.events[eventID]
	if !ok || e.TenantID != tenantID {
		return nil, status.Errorf(codes.NotFound, "audit event %s not found", eventID)
	}
	return e, nil
}

// AddEvent adds an event for testing.
func (m *MemoryAuditService) AddEvent(e *iam.AuditEvent) {
	m.events[e.ID] = e
}

// CreateTestEvent creates a test audit event.
func (m *MemoryAuditService) CreateTestEvent(tenantID, eventType string) *iam.AuditEvent {
	e := &iam.AuditEvent{
		ID:        "event-" + tenantID + "-" + eventType,
		TenantID:  tenantID,
		EventType: iam.AuditEventType(eventType),
		Category:  iam.AuditCategorySecurity,
		Outcome:   iam.AuditOutcomeSuccess,
		Actor: &iam.AuditActor{
			UserID: "user-1",
		},
		Action:    "test",
		Timestamp: time.Now(),
	}
	m.events[e.ID] = e
	return e
}

// =============================================================================
// Helper Functions
// =============================================================================

// convertToAuditEventTypes converts string slice to AuditEventType slice.
func convertToAuditEventTypes(types []string) []iam.AuditEventType {
	if len(types) == 0 {
		return nil
	}
	result := make([]iam.AuditEventType, len(types))
	for i, t := range types {
		result[i] = iam.AuditEventType(t)
	}
	return result
}

// convertToAuditCategories converts string slice to AuditCategory slice.
func convertToAuditCategories(categories []string) []iam.AuditCategory {
	if len(categories) == 0 {
		return nil
	}
	result := make([]iam.AuditCategory, len(categories))
	for i, c := range categories {
		result[i] = iam.AuditCategory(c)
	}
	return result
}

// convertToAuditOutcomes converts string slice to AuditOutcome slice.
func convertToAuditOutcomes(outcomes []string) []iam.AuditOutcome {
	if len(outcomes) == 0 {
		return nil
	}
	result := make([]iam.AuditOutcome, len(outcomes))
	for i, o := range outcomes {
		result[i] = iam.AuditOutcome(o)
	}
	return result
}

// containsEventType checks if slice contains a value.
func containsEventType(slice []iam.AuditEventType, val iam.AuditEventType) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

// containsCategory checks if slice contains a value.
func containsCategory(slice []iam.AuditCategory, val iam.AuditCategory) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

// containsOutcome checks if slice contains a value.
func containsOutcome(slice []iam.AuditOutcome, val iam.AuditOutcome) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

// Compile-time check that MemoryAuditService implements AuditService.
var _ AuditService = (*MemoryAuditService)(nil)
