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

	// RecordAuditEvent records event into tenantID's audit chain. When
	// async is true it fire-and-forgets (LogAsync) and the returned ID may
	// be empty before the chain assigns one; when false it records
	// synchronously (Log) and returns the chain-assigned event ID.
	RecordAuditEvent(ctx context.Context, tenantID string, event *iam.AuditEvent, async bool) (string, error)
}

// NewAuditServer creates a new Audit gRPC server.
func NewAuditServer(auditService AuditService, log *logger.Logger) *AuditServer {
	return &AuditServer{
		auditService: auditService,
		logger:       log.WithField("component", "audit-grpc-server"),
	}
}

// QueryAuditEvents queries audit events with filters. The caller reads its
// own authenticated tenant's chain, and ONLY that chain: an empty
// tenant_id defaults to it, and any different tenant_id — from a
// system-tenant principal included — is rejected PermissionDenied before
// ever reaching storage, so a rejected query leaks no events
// (iam.ResolveRequestTenant states the full rule).
func (s *AuditServer) QueryAuditEvents(ctx context.Context, req *iampb.QueryAuditEventsRequest) (*iampb.QueryAuditEventsResponse, error) {
	tenantID, err := iam.ResolveRequestTenant(ctx, req.TenantId)
	if err != nil {
		s.logger.WithError(err).Warn("Rejected cross-tenant audit query request")
		return nil, status.Error(codes.PermissionDenied, err.Error())
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

	events, total, err := s.auditService.QueryAuditEvents(ctx, tenantID, filter)
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

// GetAuditEvent retrieves a specific audit event. Tenant scoping mirrors
// QueryAuditEvents (iam.ResolveRequestTenant): the caller's own tenant
// only — no caller identity lifts it.
func (s *AuditServer) GetAuditEvent(ctx context.Context, req *iampb.GetAuditEventRequest) (*iampb.AuditEvent, error) {
	if req.EventId == "" {
		return nil, status.Error(codes.InvalidArgument, "event_id is required")
	}

	tenantID, err := iam.ResolveRequestTenant(ctx, req.TenantId)
	if err != nil {
		s.logger.WithError(err).Warn("Rejected cross-tenant audit get request")
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}

	event, err := s.auditService.GetAuditEvent(ctx, tenantID, req.EventId)
	if err != nil {
		s.logger.WithError(err).Error("Failed to get audit event")
		return nil, status.Errorf(codes.NotFound, "audit event %s not found", req.EventId)
	}

	return auditEventToProto(event), nil
}

// RecordAuditEvent records a new audit event onto the platform audit
// chain — the remote-write counterpart to QueryAuditEvents/GetAuditEvent.
// Tenant scoping is the SAME rule the read path applies
// (iam.ResolveRequestTenant): the event lands in the caller's own
// authenticated tenant, and a tenant_id naming any other tenant is
// rejected regardless of who the caller is.
func (s *AuditServer) RecordAuditEvent(ctx context.Context, req *iampb.RecordAuditEventRequest) (*iampb.RecordAuditEventResponse, error) {
	if req.Event == nil {
		return nil, status.Error(codes.InvalidArgument, "event is required")
	}

	tenantID, err := iam.ResolveRequestTenant(ctx, req.TenantId)
	if err != nil {
		s.logger.WithError(err).Warn("Rejected cross-tenant audit record request")
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}

	event := protoToAuditEvent(req.Event)
	event.TenantID = tenantID

	eventID, err := s.auditService.RecordAuditEvent(ctx, tenantID, event, req.Async)
	if err != nil {
		s.logger.WithError(err).Error("Failed to record audit event")
		return nil, status.Errorf(codes.Internal, "failed to record audit event: %v", err)
	}

	return &iampb.RecordAuditEventResponse{EventId: eventID}, nil
}

// protoToAuditEvent converts a proto AuditEvent to an iam.AuditEvent — the
// reverse of auditEventToProto below. ActorType, present on the proto
// message for the read path's ActorType heuristic, has no distinct
// iam.AuditActor field beyond ClientID's own "service" heuristic, so a
// proto ActorType of "service" projects onto Actor.ClientID (matching how
// auditEventToProto derives ActorType FROM ClientID) and any other value is
// carried as-is with no ClientID inference.
func protoToAuditEvent(e *iampb.AuditEvent) *iam.AuditEvent {
	if e == nil {
		return nil
	}

	details := make(map[string]interface{}, len(e.Details))
	for k, v := range e.Details {
		details[k] = v
	}
	if len(details) == 0 {
		details = nil
	}

	var actor *iam.AuditActor
	if e.ActorId != "" || e.ActorType != "" {
		actor = &iam.AuditActor{UserID: e.ActorId}
		if e.ActorType == "service" {
			actor.ClientID = e.ActorId
		}
	}

	var resource *iam.AuditResource
	if e.ResourceId != "" {
		resource = &iam.AuditResource{ID: e.ResourceId}
	}

	var timestamp time.Time
	if e.Timestamp != nil {
		timestamp = e.Timestamp.AsTime()
	}

	return &iam.AuditEvent{
		ID:        e.Id,
		TenantID:  e.TenantId,
		EventType: iam.AuditEventType(e.EventType),
		Category:  iam.AuditCategory(e.Category),
		Outcome:   iam.AuditOutcome(e.Outcome),
		Actor:     actor,
		Resource:  resource,
		Action:    e.Action,
		Details:   details,
		IPAddress: e.IpAddress,
		UserAgent: e.UserAgent,
		Timestamp: timestamp,
	}
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

// QueryAuditEvents queries audit events by delegating to the underlying
// iam.AuditLogger's Query — the node-backed audit chain in production
// (NodeAuditLogger, pkg/server/node_audit_logger.go), tenant-filtered. A
// nil auditLogger (no adapter constructed in production reaches this;
// defensive only) answers an empty result rather than panicking.
func (a *AuditServiceAdapter) QueryAuditEvents(ctx context.Context, tenantID string, filter *iam.AuditFilter) ([]*iam.AuditEvent, int, error) {
	if a.auditLogger == nil {
		return []*iam.AuditEvent{}, 0, nil
	}
	if filter == nil {
		filter = &iam.AuditFilter{}
	}
	filter.TenantID = tenantID
	return a.auditLogger.Query(ctx, filter)
}

// GetAuditEvent gets a specific audit event by delegating to the underlying
// iam.AuditLogger's GetEvent. A nil auditLogger (defensive only) answers
// NotFound rather than panicking.
func (a *AuditServiceAdapter) GetAuditEvent(ctx context.Context, tenantID, eventID string) (*iam.AuditEvent, error) {
	if a.auditLogger == nil {
		return nil, status.Errorf(codes.NotFound, "audit event %s not found", eventID)
	}
	event, err := a.auditLogger.GetEvent(ctx, tenantID, eventID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "audit event %s not found", eventID)
	}
	return event, nil
}

// RecordAuditEvent records event by delegating to the underlying
// iam.AuditLogger's Log (async=false) or LogAsync (async=true) — the
// node-backed audit chain in production (NodeAuditLogger,
// pkg/server/node_audit_logger.go). tenantID is stamped onto event
// (the caller already resolved and authorized it, RecordAuditEvent above).
func (a *AuditServiceAdapter) RecordAuditEvent(ctx context.Context, tenantID string, event *iam.AuditEvent, async bool) (string, error) {
	if a.auditLogger == nil {
		return "", status.Error(codes.Unavailable, "audit logger not available")
	}
	if event == nil {
		return "", status.Error(codes.InvalidArgument, "event is required")
	}
	event.TenantID = tenantID

	if async {
		a.auditLogger.LogAsync(event)
		return "", nil
	}
	if err := a.auditLogger.Log(ctx, event); err != nil {
		return "", err
	}
	return event.ID, nil
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

// RecordAuditEvent records event in-memory for testing.
func (m *MemoryAuditService) RecordAuditEvent(ctx context.Context, tenantID string, event *iam.AuditEvent, async bool) (string, error) {
	if event == nil {
		return "", status.Error(codes.InvalidArgument, "event is required")
	}
	event.TenantID = tenantID
	if event.ID == "" {
		event.ID = "event-" + tenantID + "-" + event.Action
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	m.events[event.ID] = event
	if async {
		return "", nil
	}
	return event.ID, nil
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
