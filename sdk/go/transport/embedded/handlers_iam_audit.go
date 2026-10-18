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

package embedded

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// auditServiceName is the service the SDK audit client dispatches on.
const auditServiceName = "quicbaas.IAM.Audit"

// iamAuditHandlers routes the SDK audit client (sdk/go/client/iam/audit.go)
// to the same iam.AuditLogger the REST and gRPC transports call. The handlers
// add no storage, filtering or permission logic of their own: each applies
// the tenant rule the other transports apply (iam.ResolveRequestTenant), and
// Delete adds the audit:delete admin check REST makes.
var iamAuditHandlers = map[string]handlerFunc{
	auditServiceName + "/Query":       handleIAMAuditQuery,
	auditServiceName + "/Get":         handleIAMAuditGet,
	auditServiceName + "/Record":      handleIAMAuditRecord,
	auditServiceName + "/RecordAsync": handleIAMAuditRecordAsync,
	auditServiceName + "/Delete":      handleIAMAuditDelete,
}

func init() {
	for k, v := range iamAuditHandlers {
		unaryHandlers[k] = v
	}
}

// AuditDeleteDryRunUnsupportedError reports a dry-run Delete. The audit
// logger cannot count the events DeleteOlderThan would remove without
// removing them, so a preview is refused rather than answered destructively.
type AuditDeleteDryRunUnsupportedError struct{}

func (e *AuditDeleteDryRunUnsupportedError) Error() string {
	return "embedded: " + auditServiceName + "/Delete: dry run is not supported by the audit service"
}

// AuditDeleteEventTypesUnsupportedError reports a Delete scoped to event
// types. DeleteOlderThan removes every event before the cutoff, so honouring
// the request would delete more than the caller asked for.
type AuditDeleteEventTypesUnsupportedError struct{}

func (e *AuditDeleteEventTypesUnsupportedError) Error() string {
	return "embedded: " + auditServiceName + "/Delete: deleting by event type is not supported by the audit service"
}

// InvalidAuditRequestError reports an audit request the service cannot
// honour, such as a filter field the audit logger has no counterpart for.
// Refusing it is safer than dropping the field and returning a wider result.
type InvalidAuditRequestError struct {
	Method string
	Reason string
}

func (e *InvalidAuditRequestError) Error() string {
	return "embedded: " + auditServiceName + "/" + e.Method + ": " + e.Reason
}

// auditLoggerFor returns the audit logger REST and gRPC use.
func auditLoggerFor(ctx context.Context, svc service.QuicBaaS) (iam.AuditLogger, error) {
	source, ok := svc.(service.AuditLoggerSource)
	if !ok {
		return nil, NewServiceNotConfiguredError("IAM.Audit")
	}
	logger, err := source.GetAuditLogger(ctx)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		return nil, NewServiceNotConfiguredError("IAM.Audit")
	}
	return logger, nil
}

// resolveAuditTenant applies the REST and gRPC tenant rule: the caller's own
// tenant only, with an empty request defaulting to it.
func resolveAuditTenant(ctx context.Context, method, requested string) (string, error) {
	tenantID, err := iam.ResolveRequestTenant(ctx, requested)
	if err != nil {
		return "", NewPermissionDeniedError(auditServiceName, method, err.Error())
	}
	return tenantID, nil
}

func handleIAMAuditQuery(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	logger, err := auditLoggerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	filter, ok := iamRequestPayload[iamsdk.AuditFilter](req, "Filter")
	if !ok {
		return nil, NewInvalidRequestTypeError(auditServiceName, "Query", "*iam.AuditFilter", typeNameOf(req))
	}
	tenantID, err := resolveAuditTenant(ctx, "Query", filter.TenantID)
	if err != nil {
		return nil, err
	}
	serviceFilter, err := auditFilterToService(filter, tenantID)
	if err != nil {
		return nil, err
	}

	events, total, err := logger.Query(ctx, serviceFilter)
	if err != nil {
		return nil, err
	}
	wire := make([]interface{}, 0, len(events))
	for _, event := range events {
		wire = append(wire, auditEventToWire(event))
	}
	return map[string]interface{}{
		"events": wire,
		"pagination": map[string]interface{}{
			"total_count": float64(total),
			"limit":       float64(serviceFilter.Limit),
			"offset":      float64(serviceFilter.Offset),
		},
	}, nil
}

func handleIAMAuditGet(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	logger, err := auditLoggerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	eventID, ok := iamRequestField(req, "EventID")
	if !ok {
		return nil, NewInvalidRequestTypeError(auditServiceName, "Get", "*iam.getAuditEventRequest", typeNameOf(req))
	}
	requested, _ := iamRequestField(req, "TenantID")
	tenantID, err := resolveAuditTenant(ctx, "Get", requested)
	if err != nil {
		return nil, err
	}

	event, err := logger.GetEvent(ctx, tenantID, eventID)
	if err != nil {
		return nil, err
	}
	return auditEventToWire(event), nil
}

func handleIAMAuditRecord(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	logger, event, err := auditRecordInput(ctx, svc, "Record", req)
	if err != nil {
		return nil, err
	}
	if err := logger.Log(ctx, event); err != nil {
		return nil, err
	}
	return &iamsdk.RecordAuditResponse{EventID: event.ID}, nil
}

func handleIAMAuditRecordAsync(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	logger, event, err := auditRecordInput(ctx, svc, "RecordAsync", req)
	if err != nil {
		return nil, err
	}
	// LogAsync cannot return an error, and its worker drops an invalid
	// event, so the event is validated here first.
	if err := validateAuditEventForAsync(event); err != nil {
		return nil, err
	}
	logger.LogAsync(event)
	return &iamsdk.RecordAuditResponse{}, nil
}

// validateAuditEventForAsync runs the service's own validation. LogAsync
// assigns the ID and timestamp, so a probe copy stands in for those two.
func validateAuditEventForAsync(event *iam.AuditEvent) error {
	probe := *event
	if probe.ID == "" {
		probe.ID = "pending"
	}
	if probe.Timestamp.IsZero() {
		probe.Timestamp = time.Now()
	}
	return probe.Validate()
}

// auditCategoryByEventType maps each event type pkg/iam declares to its
// category. A type outside the map is filed under security.
var auditCategoryByEventType = map[iam.AuditEventType]iam.AuditCategory{
	iam.AuditEventAuthentication: iam.AuditCategoryAuthentication,
	iam.AuditEventMFAEvent:       iam.AuditCategoryAuthentication,
	iam.AuditEventSessionEvent:   iam.AuditCategoryAuthentication,
	iam.AuditEventAuthorization:  iam.AuditCategoryAuthorization,
	iam.AuditEventPolicyChange:   iam.AuditCategoryAuthorization,
	iam.AuditEventUserManagement: iam.AuditCategoryAdministration,
	iam.AuditEventRoleManagement: iam.AuditCategoryAdministration,
	iam.AuditEventTenantEvent:    iam.AuditCategoryAdministration,
	iam.AuditEventAppEvent:       iam.AuditCategoryAdministration,
}

func auditCategoryFor(t iam.AuditEventType) iam.AuditCategory {
	if category, ok := auditCategoryByEventType[t]; ok {
		return category
	}
	return iam.AuditCategorySecurity
}

// auditRecordInput resolves the logger and the service-shaped event for the
// Record and RecordAsync verbs. The event lands in the caller's own tenant.
func auditRecordInput(ctx context.Context, svc service.QuicBaaS, method string, req interface{}) (iam.AuditLogger, *iam.AuditEvent, error) {
	logger, err := auditLoggerFor(ctx, svc)
	if err != nil {
		return nil, nil, err
	}
	sdkEvent, ok := iamRequestPayload[iamsdk.AuditEvent](req, "Event")
	if !ok {
		return nil, nil, NewInvalidRequestTypeError(auditServiceName, method, "*iam.AuditEvent", typeNameOf(req))
	}
	tenantID, err := resolveAuditTenant(ctx, method, sdkEvent.TenantID)
	if err != nil {
		return nil, nil, err
	}
	event := auditEventToService(sdkEvent)
	event.TenantID = tenantID
	return logger, event, nil
}

func handleIAMAuditDelete(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	logger, err := auditLoggerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	r, ok := iamRequestPayload[iamsdk.AuditDeleteRequest](req, "Request")
	if !ok {
		return nil, NewInvalidRequestTypeError(auditServiceName, "Delete", "*iam.AuditDeleteRequest", typeNameOf(req))
	}
	// The tenant rule runs before the permission check, as on REST, so a
	// caller targeting another tenant never reaches the admin manager.
	tenantID, err := resolveAuditTenant(ctx, "Delete", r.TenantID)
	if err != nil {
		return nil, err
	}
	if r.Before.IsZero() {
		return nil, &InvalidAuditRequestError{Method: "Delete", Reason: "before is required"}
	}
	// Both refusals come before the permission check and the logger: the
	// only delete the service offers is unfiltered and destructive.
	if r.DryRun {
		return nil, &AuditDeleteDryRunUnsupportedError{}
	}
	if len(r.EventTypes) > 0 {
		return nil, &AuditDeleteEventTypesUnsupportedError{}
	}
	if err := requireAuditDeletePermission(ctx, svc, tenantID); err != nil {
		return nil, err
	}

	count, err := logger.DeleteOlderThan(ctx, tenantID, r.Before)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"deleted_count": count,
		"before":        r.Before,
	}, nil
}

// requireAuditDeletePermission is REST's audit:delete check: an authenticated
// subject holding the permission in the target tenant. It fails closed when
// no subject is bound.
func requireAuditDeletePermission(ctx context.Context, svc service.QuicBaaS, tenantID string) error {
	subject, ok := auth.SubjectFromContext(ctx)
	if !ok || !subject.Authenticated || subject.ID == "" {
		return NewPermissionDeniedError(auditServiceName, "Delete", "user not authenticated")
	}
	mgr, err := adminManagerFor(ctx, svc)
	if err != nil {
		return err
	}
	resp, err := mgr.CheckPermission(ctx, &iam.AdminCheckRequest{
		UserID:       subject.UserID,
		Permission:   iam.PermAuditDelete,
		TenantID:     tenantID,
		ResourceType: "audit",
	})
	if err != nil {
		return err
	}
	if resp == nil || !resp.Allowed {
		return NewPermissionDeniedError(auditServiceName, "Delete", "insufficient permissions to delete audit events")
	}
	return nil
}

// auditFilterToService converts the SDK filter to the service filter. The
// service filter has no counterpart for the SDK's actor-type, target,
// action, severity, IP, session, correlation and resource-type filters, so a
// filter setting any of them is refused rather than widened.
func auditFilterToService(f *iamsdk.AuditFilter, tenantID string) (*iam.AuditFilter, error) {
	if field := unsupportedAuditFilterField(f); field != "" {
		return nil, &InvalidAuditRequestError{Method: "Query", Reason: "filter field " + field + " is not supported"}
	}
	// The service slices its results by offset and limit without checking
	// them, so a negative value would panic there.
	if f.Offset < 0 {
		return nil, &InvalidAuditRequestError{Method: "Query", Reason: "offset must not be negative"}
	}
	if f.Limit < 0 {
		return nil, &InvalidAuditRequestError{Method: "Query", Reason: "limit must not be negative"}
	}
	out := &iam.AuditFilter{
		TenantID:   tenantID,
		ActorID:    f.ActorID,
		ResourceID: f.ResourceID,
		StartTime:  timePtrOrNil(f.StartTime),
		EndTime:    timePtrOrNil(f.EndTime),
		Limit:      f.Limit,
		Offset:     f.Offset,
	}
	// The SDK's zero EventType is Authentication, so an unset singular
	// filter cannot be told from an Authentication one: only a non-zero
	// value is read as set. EventTypes has no such ambiguity.
	if f.EventType != iamsdk.AuditEventTypeAuthentication {
		out.EventTypes = append(out.EventTypes, iam.AuditEventType(f.EventType.String()))
	}
	for _, t := range f.EventTypes {
		out.EventTypes = append(out.EventTypes, iam.AuditEventType(t.String()))
	}
	if f.Outcome != nil {
		out.Outcomes = []iam.AuditOutcome{iam.AuditOutcome(f.Outcome.String())}
	}
	return out, nil
}

// unsupportedAuditFilterField names the first set SDK filter field the
// service filter cannot express, or "" when there is none.
func unsupportedAuditFilterField(f *iamsdk.AuditFilter) string {
	set := []struct {
		name string
		used bool
	}{
		{"actor_type", f.ActorType != ""},
		{"target_id", f.TargetID != ""},
		{"target_type", f.TargetType != ""},
		{"action", f.Action != ""},
		{"actions", len(f.Actions) > 0},
		{"severity", f.Severity != nil},
		{"ip_address", f.IPAddress != ""},
		{"ip_addresses", len(f.IPAddresses) > 0},
		{"session_id", f.SessionID != ""},
		{"correlation_id", f.CorrelationID != ""},
		{"resource_type", f.ResourceType != ""},
	}
	for _, s := range set {
		if s.used {
			return s.name
		}
	}
	return ""
}

// timePtrOrNil maps the SDK's zero time ("no bound") to the service's nil.
func timePtrOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// auditEventToWire renders a service event in the JSON shape the REST
// transport writes, which the SDK client decodes with its own map parser.
func auditEventToWire(e *iam.AuditEvent) map[string]interface{} {
	// The keys and value types are the ones the SDK's parseAuditEventFromMap
	// reads: flat actor and resource ids and a time.Time timestamp. No
	// "category" key is sent, because the parser would read it over
	// event_type.
	out := map[string]interface{}{
		"id":         e.ID,
		"tenant_id":  e.TenantID,
		"event_type": string(e.EventType),
		"action":     e.Action,
		"outcome":    string(e.Outcome),
		"timestamp":  e.Timestamp,
	}
	// A service actor carries its id in both UserID and ClientID, so the
	// ClientID marker is read first.
	if e.Actor != nil {
		if e.Actor.ClientID != "" {
			out["actor_id"] = e.Actor.ClientID
			out["actor_type"] = "service"
		} else if e.Actor.UserID != "" {
			out["actor_id"] = e.Actor.UserID
			out["actor_type"] = "user"
		}
	}
	if e.Resource != nil && e.Resource.ID != "" {
		out["resource_id"] = e.Resource.ID
	}
	if e.IPAddress != "" {
		out["ip_address"] = e.IPAddress
	}
	if e.UserAgent != "" {
		out["user_agent"] = e.UserAgent
	}
	if e.Details != nil {
		out["details"] = e.Details
	}
	return out
}

// auditEventToService converts an SDK event to the service event with the
// same field mapping the gRPC transport uses (convertSDKAuditEventToProto
// then protoToAuditEvent): the SDK's enum names become the service's string
// types, and the actor and target or resource IDs carry over.
func auditEventToService(e *iamsdk.AuditEvent) *iam.AuditEvent {
	out := &iam.AuditEvent{
		ID:            e.ID,
		TenantID:      e.TenantID,
		Timestamp:     e.Timestamp,
		EventType:     iam.AuditEventType(e.EventType.String()),
		Outcome:       iam.AuditOutcome(e.Outcome.String()),
		Action:        e.Action,
		Details:       e.Details,
		CorrelationID: e.CorrelationID,
	}
	out.Category = auditCategoryFor(out.EventType)
	if a := e.Actor; a != nil && (a.ID != "" || a.Type != "") {
		out.Actor = &iam.AuditActor{UserID: a.ID}
		if a.Type == "service" {
			out.Actor.ClientID = a.ID
		}
		out.IPAddress = a.IPAddress
		out.UserAgent = a.UserAgent
	}
	switch {
	case e.Target != nil && e.Target.ID != "":
		out.Resource = &iam.AuditResource{ID: e.Target.ID}
	case e.Resource != nil && e.Resource.ID != "":
		out.Resource = &iam.AuditResource{ID: e.Resource.ID}
	}
	return out
}
