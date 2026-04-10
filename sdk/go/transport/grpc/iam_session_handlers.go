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

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// handleListSessions handles ListSessions requests.
func (h *iamHandler) handleListSessions(ctx context.Context, client iampb.SessionServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleListSessions", "invalid request type")
	}

	// Extract from nested "request" field if present (SDK wraps in listSessionsRequestInternal).
	if nested, ok := reqMap["request"].(map[string]interface{}); ok {
		reqMap = nested
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")
	limit, _ := getInt(reqMap, "limit")

	pbReq := &iampb.ListSessionsRequest{
		TenantId: tenantID,
		UserId:   userID,
		Limit:    int32(limit),
	}

	resp, err := client.ListSessions(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleListSessions")
	}

	sessions := make([]iam.Session, 0, len(resp.Sessions))
	for _, s := range resp.Sessions {
		if converted := convertProtoSessionToSDK(s); converted != nil {
			sessions = append(sessions, *converted)
		}
	}

	return &iam.ListSessionsResponse{
		Sessions:   sessions,
		TotalCount: int(resp.Total),
	}, nil
}

// handleGetSession handles GetSession requests.
func (h *iamHandler) handleGetSession(ctx context.Context, client iampb.SessionServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleGetSession", "invalid request type")
	}

	sessionID, _ := getString(reqMap, "session_id")

	pbReq := &iampb.GetSessionRequest{
		SessionId: sessionID,
	}

	resp, err := client.GetSession(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleGetSession")
	}

	return convertProtoSessionToSDK(resp), nil
}

// handleRevokeSession handles RevokeSession requests.
func (h *iamHandler) handleRevokeSession(ctx context.Context, client iampb.SessionServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleRevokeSession", "invalid request type")
	}

	sessionID, _ := getString(reqMap, "session_id")

	pbReq := &iampb.RevokeSessionRequest{
		SessionId: sessionID,
	}

	_, err := client.RevokeSession(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleRevokeSession")
	}

	// Return nil to indicate success - the SDK client accepts nil response.
	return nil, nil
}

// handleRevokeAllSessions handles RevokeAllSessions requests.
func (h *iamHandler) handleRevokeAllSessions(ctx context.Context, client iampb.SessionServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleRevokeAllSessions", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")

	pbReq := &iampb.RevokeAllSessionsRequest{
		TenantId: tenantID,
		UserId:   userID,
	}

	_, err := client.RevokeAllSessions(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleRevokeAllSessions")
	}

	// Return nil to indicate success - the SDK client accepts nil response.
	return nil, nil
}

// handleRevokeOtherSessions handles RevokeOtherSessions requests.
func (h *iamHandler) handleRevokeOtherSessions(ctx context.Context, client iampb.SessionServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleRevokeOtherSessions", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	keepSessionID, _ := getString(reqMap, "keep_session_id")

	pbReq := &iampb.RevokeOtherSessionsRequest{
		TenantId:      tenantID,
		KeepSessionId: keepSessionID,
	}

	_, err := client.RevokeOtherSessions(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleRevokeOtherSessions")
	}

	// Return nil to indicate success - the SDK client accepts nil response.
	return nil, nil
}

// handleQueryAuditEvents handles QueryAuditEvents requests.
func (h *iamHandler) handleQueryAuditEvents(ctx context.Context, client iampb.AuditServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleQueryAuditEvents", "invalid request type")
	}

	// Extract from nested "filter" field if present (SDK wraps in queryAuditRequest).
	if nested, ok := reqMap["filter"].(map[string]interface{}); ok {
		reqMap = nested
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	eventTypes, _ := getStringSlice(reqMap, "event_types")
	categories, _ := getStringSlice(reqMap, "categories")
	outcomes, _ := getStringSlice(reqMap, "outcomes")
	actorID, _ := getString(reqMap, "actor_id")
	resourceID, _ := getString(reqMap, "resource_id")
	limit, _ := getInt(reqMap, "limit")
	offset, _ := getInt(reqMap, "offset")

	pbReq := &iampb.QueryAuditEventsRequest{
		TenantId:   tenantID,
		EventTypes: eventTypes,
		Categories: categories,
		Outcomes:   outcomes,
		ActorId:    actorID,
		ResourceId: resourceID,
		Limit:      int32(limit),
		Offset:     int32(offset),
	}

	resp, err := client.QueryAuditEvents(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleQueryAuditEvents")
	}

	events := make([]iam.AuditEvent, 0, len(resp.Events))
	for _, e := range resp.Events {
		if converted := convertProtoAuditEventToSDK(e); converted != nil {
			events = append(events, *converted)
		}
	}

	return &iam.AuditQueryResponse{
		Events:     events,
		TotalCount: int64(resp.Total),
	}, nil
}

// handleGetAuditEvent handles GetAuditEvent requests.
func (h *iamHandler) handleGetAuditEvent(ctx context.Context, client iampb.AuditServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleGetAuditEvent", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	eventID, _ := getString(reqMap, "event_id")

	pbReq := &iampb.GetAuditEventRequest{
		TenantId: tenantID,
		EventId:  eventID,
	}

	resp, err := client.GetAuditEvent(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleGetAuditEvent")
	}

	return convertProtoAuditEventToSDK(resp), nil
}

// convertProtoSessionToSDK converts a protobuf Session to SDK Session.
func convertProtoSessionToSDK(s *iampb.Session) *iam.Session {
	if s == nil {
		return nil
	}

	resp := &iam.Session{
		ID:          s.Id,
		UserID:      s.UserId,
		TenantID:    s.TenantId,
		MFAVerified: s.MfaVerified,
		IPAddress:   s.IpAddress,
		UserAgent:   s.UserAgent,
		DeviceInfo:  s.DeviceInfo,
	}

	if s.CreatedAt != nil {
		resp.CreatedAt = s.CreatedAt.AsTime()
		resp.Created = s.CreatedAt.AsTime()
	}
	if s.LastActive != nil {
		resp.LastActive = s.LastActive.AsTime()
	}
	if s.ExpiresAt != nil {
		resp.ExpiresAt = s.ExpiresAt.AsTime()
	}

	return resp
}

// convertProtoAuditEventToSDK converts a protobuf AuditEvent to SDK AuditEvent.
func convertProtoAuditEventToSDK(e *iampb.AuditEvent) *iam.AuditEvent {
	if e == nil {
		return nil
	}

	resp := &iam.AuditEvent{
		ID:       e.Id,
		TenantID: e.TenantId,
		Action:   e.Action,
		Actor: &iam.AuditActor{
			ID:        e.ActorId,
			Type:      e.ActorType,
			IPAddress: e.IpAddress,
			UserAgent: e.UserAgent,
		},
		Target: &iam.AuditTarget{
			ID: e.ResourceId,
		},
	}

	if e.Details != nil {
		details := make(map[string]interface{}, len(e.Details))
		for k, v := range e.Details {
			details[k] = v
		}
		resp.Details = details
	}

	if e.Timestamp != nil {
		resp.Timestamp = e.Timestamp.AsTime()
	}

	return resp
}
