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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	auditServiceName = "quicbaas.IAM.Audit"
)

// AuditClient provides audit log operations.
//
// The client supports querying and managing audit events for:
//   - Security monitoring and incident response
//   - Compliance reporting (SOC 2, HIPAA, GDPR, etc.)
//   - User activity tracking
//   - Access pattern analysis
type AuditClient interface {
	// Query searches audit events based on filter criteria.
	Query(ctx context.Context, filter *AuditFilter) (*AuditQueryResponse, error)

	// Get retrieves a specific audit event by ID within a tenant.
	Get(ctx context.Context, tenantID, eventID string) (*AuditEvent, error)

	// Delete removes audit events matching the criteria.
	Delete(ctx context.Context, req *AuditDeleteRequest) error

	// Record synchronously emits event onto the platform audit chain and
	// returns the chain-assigned event ID (P5.3: the remote-write
	// counterpart to Query/Get, closing the last hole in the unified audit
	// plane — until this, a remote caller like go-trusted-platform could
	// read the chain but never write to it). event.TenantID selects the
	// target tenant; empty defaults to the caller's own authenticated
	// tenant, and any other value is honored only when the caller holds
	// cross-tenant (system-tenant) scope. TP-specific extra fields ride in
	// event.Details — there is no separate schema for them.
	Record(ctx context.Context, event *AuditEvent) (string, error)

	// RecordAsync fire-and-forgets event onto the platform audit chain
	// (LogAsync server-side): it returns as soon as the request is
	// accepted, before the event is durably chained. Prefer Record for
	// anything that must observably land before the call returns.
	RecordAsync(ctx context.Context, event *AuditEvent) error
}

// AuditEventType represents the type of audit event.
type AuditEventType int

const (
	// AuditEventTypeAuthentication is an authentication-related event.
	AuditEventTypeAuthentication AuditEventType = iota
	// AuditEventTypeAuthorization is an authorization decision event.
	AuditEventTypeAuthorization
	// AuditEventTypeUserManagement is a user management event.
	AuditEventTypeUserManagement
	// AuditEventTypeRoleManagement is a role management event.
	AuditEventTypeRoleManagement
	// AuditEventTypePolicyManagement is a policy management event.
	AuditEventTypePolicyManagement
	// AuditEventTypeDataAccess is a data access event.
	AuditEventTypeDataAccess
	// AuditEventTypeConfiguration is a configuration change event.
	AuditEventTypeConfiguration
	// AuditEventTypeSystem is a system event.
	AuditEventTypeSystem
	// AuditEventTypeGDPR is a GDPR-related event.
	AuditEventTypeGDPR
	// AuditEventTypeAdmin is an administrative action event.
	AuditEventTypeAdmin
)

// String returns the string representation of the audit event type.
func (t AuditEventType) String() string {
	switch t {
	case AuditEventTypeAuthentication:
		return "authentication"
	case AuditEventTypeAuthorization:
		return "authorization"
	case AuditEventTypeUserManagement:
		return "user_management"
	case AuditEventTypeRoleManagement:
		return "role_management"
	case AuditEventTypePolicyManagement:
		return "policy_management"
	case AuditEventTypeDataAccess:
		return "data_access"
	case AuditEventTypeConfiguration:
		return "configuration"
	case AuditEventTypeSystem:
		return "system"
	case AuditEventTypeGDPR:
		return "gdpr"
	case AuditEventTypeAdmin:
		return "admin"
	default:
		return "unknown"
	}
}

// AuditOutcome represents the outcome of an audited action.
type AuditOutcome int

const (
	// AuditOutcomeSuccess indicates the action succeeded.
	AuditOutcomeSuccess AuditOutcome = iota
	// AuditOutcomeFailure indicates the action failed.
	AuditOutcomeFailure
	// AuditOutcomeDenied indicates the action was denied.
	AuditOutcomeDenied
	// AuditOutcomeError indicates an error occurred.
	AuditOutcomeError
)

// String returns the string representation of the audit outcome.
func (o AuditOutcome) String() string {
	switch o {
	case AuditOutcomeSuccess:
		return "success"
	case AuditOutcomeFailure:
		return "failure"
	case AuditOutcomeDenied:
		return "denied"
	case AuditOutcomeError:
		return "error"
	default:
		return "unknown"
	}
}

// AuditSeverity represents the severity level of an audit event.
type AuditSeverity int

const (
	// AuditSeverityInfo is an informational event.
	AuditSeverityInfo AuditSeverity = iota
	// AuditSeverityLow is a low severity event.
	AuditSeverityLow
	// AuditSeverityMedium is a medium severity event.
	AuditSeverityMedium
	// AuditSeverityHigh is a high severity event.
	AuditSeverityHigh
	// AuditSeverityCritical is a critical severity event.
	AuditSeverityCritical
)

// String returns the string representation of the audit severity.
func (s AuditSeverity) String() string {
	switch s {
	case AuditSeverityInfo:
		return "info"
	case AuditSeverityLow:
		return "low"
	case AuditSeverityMedium:
		return "medium"
	case AuditSeverityHigh:
		return "high"
	case AuditSeverityCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// AuditFilter contains the filter criteria for querying audit events.
type AuditFilter struct {
	// TenantID filters by tenant.
	TenantID string `json:"tenant_id,omitempty"`

	// ActorID filters by the actor who performed the action.
	ActorID string `json:"actor_id,omitempty"`

	// ActorType filters by actor type (user, service, system).
	ActorType string `json:"actor_type,omitempty"`

	// TargetID filters by the target of the action.
	TargetID string `json:"target_id,omitempty"`

	// TargetType filters by target type (user, role, policy, etc.).
	TargetType string `json:"target_type,omitempty"`

	// EventType filters by event type.
	EventType AuditEventType `json:"event_type,omitempty"`

	// EventTypes filters by multiple event types.
	EventTypes []AuditEventType `json:"event_types,omitempty"`

	// Action filters by specific action name.
	Action string `json:"action,omitempty"`

	// Actions filters by multiple actions.
	Actions []string `json:"actions,omitempty"`

	// Outcome filters by action outcome.
	Outcome *AuditOutcome `json:"outcome,omitempty"`

	// Severity filters by minimum severity.
	Severity *AuditSeverity `json:"severity,omitempty"`

	// StartTime is the start of the time range.
	StartTime time.Time `json:"start_time,omitempty"`

	// EndTime is the end of the time range.
	EndTime time.Time `json:"end_time,omitempty"`

	// IPAddress filters by source IP address.
	IPAddress string `json:"ip_address,omitempty"`

	// IPAddresses filters by multiple IP addresses.
	IPAddresses []string `json:"ip_addresses,omitempty"`

	// SessionID filters by session ID.
	SessionID string `json:"session_id,omitempty"`

	// CorrelationID filters by correlation ID for related events.
	CorrelationID string `json:"correlation_id,omitempty"`

	// ResourceType filters by resource type.
	ResourceType string `json:"resource_type,omitempty"`

	// ResourceID filters by resource ID.
	ResourceID string `json:"resource_id,omitempty"`

	// Limit is the maximum number of events to return.
	Limit int `json:"limit,omitempty"`

	// Offset is the number of events to skip.
	Offset int `json:"offset,omitempty"`

	// OrderBy specifies the sort order.
	OrderBy string `json:"order_by,omitempty"`

	// OrderDesc sorts in descending order.
	OrderDesc bool `json:"order_desc,omitempty"`

	// IncludeDetails includes full event details.
	IncludeDetails bool `json:"include_details,omitempty"`
}

// AuditEvent represents an audit log entry.
type AuditEvent struct {
	// ID is the unique identifier for this event.
	ID string `json:"id"`

	// TenantID is the tenant this event belongs to.
	TenantID string `json:"tenant_id"`

	// Timestamp is when the event occurred.
	Timestamp time.Time `json:"timestamp"`

	// EventType is the type of event.
	EventType AuditEventType `json:"event_type"`

	// Action is the specific action that was performed.
	Action string `json:"action"`

	// Outcome is the result of the action.
	Outcome AuditOutcome `json:"outcome"`

	// Severity is the severity level.
	Severity AuditSeverity `json:"severity"`

	// Actor contains information about who performed the action.
	Actor *AuditActor `json:"actor,omitempty"`

	// Target contains information about what was acted upon.
	Target *AuditTarget `json:"target,omitempty"`

	// Resource contains resource information.
	Resource *AuditResource `json:"resource,omitempty"`

	// Request contains request details.
	Request *AuditRequest `json:"request,omitempty"`

	// Response contains response details.
	Response *AuditResponse `json:"response,omitempty"`

	// Context contains additional contextual information.
	Context *AuditContext `json:"context,omitempty"`

	// CorrelationID links related events.
	CorrelationID string `json:"correlation_id,omitempty"`

	// SessionID is the session this event occurred in.
	SessionID string `json:"session_id,omitempty"`

	// Message is a human-readable description.
	Message string `json:"message,omitempty"`

	// Details contains additional event-specific data.
	Details map[string]interface{} `json:"details,omitempty"`

	// Tags are searchable labels.
	Tags []string `json:"tags,omitempty"`
}

// AuditActor represents the entity that performed an action.
type AuditActor struct {
	// ID is the actor's unique identifier.
	ID string `json:"id"`

	// Type is the actor type (user, service, system).
	Type string `json:"type"`

	// Name is the actor's display name.
	Name string `json:"name,omitempty"`

	// Email is the actor's email (if applicable).
	Email string `json:"email,omitempty"`

	// Roles are the actor's roles at the time of the action.
	Roles []string `json:"roles,omitempty"`

	// Groups are the actor's groups at the time of the action.
	Groups []string `json:"groups,omitempty"`

	// IPAddress is the actor's IP address.
	IPAddress string `json:"ip_address,omitempty"`

	// UserAgent is the actor's user agent string.
	UserAgent string `json:"user_agent,omitempty"`

	// Impersonator contains details if the actor was being impersonated.
	Impersonator *AuditActor `json:"impersonator,omitempty"`
}

// AuditTarget represents the entity that was acted upon.
type AuditTarget struct {
	// ID is the target's unique identifier.
	ID string `json:"id"`

	// Type is the target type (user, role, policy, etc.).
	Type string `json:"type"`

	// Name is the target's display name.
	Name string `json:"name,omitempty"`

	// TenantID is the target's tenant.
	TenantID string `json:"tenant_id,omitempty"`

	// PreviousState contains the state before the action.
	PreviousState map[string]interface{} `json:"previous_state,omitempty"`

	// NewState contains the state after the action.
	NewState map[string]interface{} `json:"new_state,omitempty"`
}

// AuditResource represents a resource involved in an action.
type AuditResource struct {
	// ID is the resource identifier.
	ID string `json:"id"`

	// Type is the resource type.
	Type string `json:"type"`

	// Name is the resource name.
	Name string `json:"name,omitempty"`

	// Path is the resource path.
	Path string `json:"path,omitempty"`

	// Owner is the resource owner.
	Owner string `json:"owner,omitempty"`
}

// AuditRequest contains request details.
type AuditRequest struct {
	// Method is the request method (GET, POST, etc.).
	Method string `json:"method"`

	// Path is the request path.
	Path string `json:"path"`

	// Query contains query parameters.
	Query map[string]string `json:"query,omitempty"`

	// Headers contains relevant request headers.
	Headers map[string]string `json:"headers,omitempty"`

	// Body contains the request body (if captured).
	Body string `json:"body,omitempty"`

	// Size is the request size in bytes.
	Size int64 `json:"size,omitempty"`
}

// AuditResponse contains response details.
type AuditResponse struct {
	// StatusCode is the response status code.
	StatusCode int `json:"status_code"`

	// Duration is how long the request took.
	Duration time.Duration `json:"duration,omitempty"`

	// Size is the response size in bytes.
	Size int64 `json:"size,omitempty"`

	// ErrorCode is the error code if applicable.
	ErrorCode string `json:"error_code,omitempty"`

	// ErrorMessage is the error message if applicable.
	ErrorMessage string `json:"error_message,omitempty"`
}

// AuditContext contains contextual information.
type AuditContext struct {
	// Location contains geographic location info.
	Location *AuditLocation `json:"location,omitempty"`

	// Device contains device information.
	Device *AuditDevice `json:"device,omitempty"`

	// Environment is the environment (production, staging, etc.).
	Environment string `json:"environment,omitempty"`

	// Service is the service that generated the event.
	Service string `json:"service,omitempty"`

	// Version is the service version.
	Version string `json:"version,omitempty"`

	// NodeID is the cluster node that handled the request.
	NodeID string `json:"node_id,omitempty"`
}

// AuditLocation contains geographic location information.
type AuditLocation struct {
	// Country is the country code.
	Country string `json:"country,omitempty"`

	// Region is the region/state.
	Region string `json:"region,omitempty"`

	// City is the city.
	City string `json:"city,omitempty"`

	// Latitude is the latitude.
	Latitude float64 `json:"latitude,omitempty"`

	// Longitude is the longitude.
	Longitude float64 `json:"longitude,omitempty"`

	// ISP is the internet service provider.
	ISP string `json:"isp,omitempty"`
}

// AuditDevice contains device information.
type AuditDevice struct {
	// Type is the device type (desktop, mobile, tablet).
	Type string `json:"type,omitempty"`

	// OS is the operating system.
	OS string `json:"os,omitempty"`

	// Browser is the browser name.
	Browser string `json:"browser,omitempty"`

	// Fingerprint is the device fingerprint.
	Fingerprint string `json:"fingerprint,omitempty"`
}

// AuditQueryResponse contains the query results.
type AuditQueryResponse struct {
	// Events is the list of audit events.
	Events []AuditEvent `json:"events"`

	// TotalCount is the total number of matching events.
	TotalCount int64 `json:"total_count"`

	// HasMore indicates if there are more events.
	HasMore bool `json:"has_more"`

	// NextOffset is the offset for the next page.
	NextOffset int `json:"next_offset,omitempty"`
}

// AuditDeleteRequest contains the criteria for deleting audit events.
type AuditDeleteRequest struct {
	// TenantID is the tenant to delete events from.
	TenantID string `json:"tenant_id,omitempty"`

	// Before deletes events before this time.
	Before time.Time `json:"before,omitempty"`

	// EventTypes limits deletion to specific event types.
	EventTypes []AuditEventType `json:"event_types,omitempty"`

	// DryRun returns the count without deleting.
	DryRun bool `json:"dry_run,omitempty"`

	// Confirm must be true to actually delete.
	Confirm bool `json:"confirm,omitempty"`
}

// AuditDeleteResponse contains the delete operation results.
type AuditDeleteResponse struct {
	// DeletedCount is the number of events deleted.
	DeletedCount int64 `json:"deleted_count"`

	// DryRun indicates if this was a dry run.
	DryRun bool `json:"dry_run"`
}

// Internal request/response types for transport.

type queryAuditRequest struct {
	Filter *AuditFilter `json:"filter"`
}

type getAuditEventRequest struct {
	EventID  string `json:"event_id"`
	TenantID string `json:"tenant_id"`
}

type deleteAuditRequest struct {
	Request *AuditDeleteRequest `json:"request"`
}

type deleteAuditResponse struct{}

// recordAuditRequest is the internal Record/RecordAsync request.
type recordAuditRequest struct {
	Event *AuditEvent `json:"event"`
	Async bool        `json:"async"`
}

// RecordAuditResponse carries the chain-assigned event ID (empty for the
// async fire-and-forget call).
type RecordAuditResponse struct {
	EventID string `json:"event_id"`
}

// auditClient implements the AuditClient interface.
type auditClient struct {
	transport transport.Transport
	tenantID  string
}

// NewAuditClient creates a new audit log client.
//
// Returns nil if the transport is nil.
func NewAuditClient(t transport.Transport) AuditClient {
	if t == nil {
		return nil
	}
	return &auditClient{
		transport: t,
	}
}

// Query searches audit events based on filter criteria.
func (c *auditClient) Query(ctx context.Context, filter *AuditFilter) (*AuditQueryResponse, error) {
	if filter == nil {
		return nil, errors.NewInvalidInput("iam.audit.Query", "filter is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.audit.Query", "context is required")
	}

	// Set reasonable defaults
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	if filter.Limit > 1000 {
		filter.Limit = 1000
	}

	resp, err := c.transport.Request(ctx, auditServiceName, "Query", &queryAuditRequest{
		Filter: filter,
	})
	if err != nil {
		return nil, err
	}

	return parseAuditQueryResponse(resp)
}

// Get retrieves a specific audit event by ID within a tenant.
func (c *auditClient) Get(ctx context.Context, tenantID, eventID string) (*AuditEvent, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.audit.Get", "tenant ID is required")
	}
	if eventID == "" {
		return nil, errors.NewInvalidInput("iam.audit.Get", "event ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.audit.Get", "context is required")
	}

	resp, err := c.transport.Request(ctx, auditServiceName, "Get", &getAuditEventRequest{
		EventID:  eventID,
		TenantID: tenantID,
	})
	if err != nil {
		return nil, err
	}

	return parseAuditEventResponse(resp)
}

// Record synchronously emits event onto the platform audit chain. See
// AuditClient.Record for the full contract.
func (c *auditClient) Record(ctx context.Context, event *AuditEvent) (string, error) {
	if event == nil {
		return "", errors.NewInvalidInput("iam.audit.Record", "event is required")
	}
	if ctx == nil {
		return "", errors.NewInvalidInput("iam.audit.Record", "context is required")
	}

	resp, err := c.transport.Request(ctx, auditServiceName, "Record", &recordAuditRequest{
		Event: event,
		Async: false,
	})
	if err != nil {
		return "", err
	}
	return parseRecordAuditResponse(resp)
}

// RecordAsync fire-and-forgets event onto the platform audit chain. See
// AuditClient.RecordAsync for the full contract.
func (c *auditClient) RecordAsync(ctx context.Context, event *AuditEvent) error {
	if event == nil {
		return errors.NewInvalidInput("iam.audit.RecordAsync", "event is required")
	}
	if ctx == nil {
		return errors.NewInvalidInput("iam.audit.RecordAsync", "context is required")
	}

	_, err := c.transport.Request(ctx, auditServiceName, "RecordAsync", &recordAuditRequest{
		Event: event,
		Async: true,
	})
	return err
}

// parseRecordAuditResponse parses the response from Record.
func parseRecordAuditResponse(resp interface{}) (string, error) {
	if resp == nil {
		return "", nil
	}
	switch r := resp.(type) {
	case *RecordAuditResponse:
		return r.EventID, nil
	case map[string]interface{}:
		if v, ok := r["event_id"].(string); ok {
			return v, nil
		}
		return "", nil
	default:
		return "", errors.NewInternal("iam.audit.Record", "invalid response type")
	}
}

// Delete removes audit events matching the criteria.
func (c *auditClient) Delete(ctx context.Context, req *AuditDeleteRequest) error {
	if err := validateAuditDeleteRequest(req); err != nil {
		return err
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.audit.Delete", "context is required")
	}

	resp, err := c.transport.Request(ctx, auditServiceName, "Delete", &deleteAuditRequest{
		Request: req,
	})
	if err != nil {
		return err
	}

	// Accept nil response or valid response
	if resp == nil {
		return nil
	}

	if _, ok := resp.(*deleteAuditResponse); !ok {
		// Also accept map response from transport
		if _, ok := resp.(map[string]interface{}); !ok {
			return errors.NewInternal("iam.audit.Delete", "invalid response type")
		}
	}

	return nil
}

// Validation functions.

func validateAuditDeleteRequest(req *AuditDeleteRequest) error {
	if req == nil {
		return errors.NewInvalidInput("iam.audit.Delete", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("iam.audit.Delete", "tenant ID is required")
	}
	if req.Before.IsZero() {
		return errors.NewInvalidInput("iam.audit.Delete", "before time is required")
	}
	if !req.DryRun && !req.Confirm {
		return errors.NewInvalidInput("iam.audit.Delete", "confirm must be true for actual deletion")
	}
	return nil
}

// parseAuditQueryResponse parses the response from Query.
func parseAuditQueryResponse(resp interface{}) (*AuditQueryResponse, error) {
	if resp == nil {
		return &AuditQueryResponse{Events: []AuditEvent{}}, nil
	}

	// If already a *AuditQueryResponse, return it directly.
	if queryResp, ok := resp.(*AuditQueryResponse); ok {
		return queryResp, nil
	}

	// Handle map response (from gRPC or REST transport).
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("iam.audit.Query", "invalid response type")
	}

	result := &AuditQueryResponse{
		Events: []AuditEvent{},
	}

	// Parse events array.
	if eventsVal, ok := respMap["events"]; ok {
		if events, ok := eventsVal.([]interface{}); ok {
			for _, e := range events {
				if eventMap, ok := e.(map[string]interface{}); ok {
					event := parseAuditEventFromMap(eventMap)
					result.Events = append(result.Events, event)
				}
				// Also handle *sdkAuditEventResponse type from handler
				if eventPtr, ok := e.(*sdkAuditEventResponse); ok {
					event := convertSdkAuditEventToAuditEvent(eventPtr)
					result.Events = append(result.Events, event)
				}
			}
		}
	}

	// Parse pagination.
	if pagination, ok := respMap["pagination"].(map[string]interface{}); ok {
		if total, ok := pagination["total_count"].(float64); ok {
			result.TotalCount = int64(total)
		}
		if total, ok := pagination["total_count"].(int64); ok {
			result.TotalCount = total
		}
		if total, ok := pagination["total_count"].(int32); ok {
			result.TotalCount = int64(total)
		}
		if limit, ok := pagination["limit"].(float64); ok {
			if int(limit) > 0 && len(result.Events) >= int(limit) {
				result.HasMore = true
			}
		}
		if offset, ok := pagination["offset"].(float64); ok {
			result.NextOffset = int(offset) + len(result.Events)
		}
	}

	// Also check for direct total_count field
	if total, ok := respMap["total_count"].(float64); ok {
		result.TotalCount = int64(total)
	}

	return result, nil
}

// parseAuditEventResponse parses the response for a single audit event.
func parseAuditEventResponse(resp interface{}) (*AuditEvent, error) {
	if resp == nil {
		return nil, errors.NewNotFound("iam.audit.Get", "audit event not found")
	}

	// If already a *AuditEvent, return it directly.
	if event, ok := resp.(*AuditEvent); ok {
		return event, nil
	}

	// Handle *sdkAuditEventResponse type from handler
	if eventPtr, ok := resp.(*sdkAuditEventResponse); ok {
		event := convertSdkAuditEventToAuditEvent(eventPtr)
		return &event, nil
	}

	// Handle map response.
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("iam.audit.Get", "invalid response type")
	}

	event := parseAuditEventFromMap(respMap)
	return &event, nil
}

// parseAuditEventFromMap parses an AuditEvent from a map.
func parseAuditEventFromMap(m map[string]interface{}) AuditEvent {
	event := AuditEvent{}

	if v, ok := m["id"].(string); ok {
		event.ID = v
	}
	if v, ok := m["tenant_id"].(string); ok {
		event.TenantID = v
	}
	if v, ok := m["event_type"].(string); ok {
		event.EventType = parseAuditEventTypeFromString(v)
	}
	if v, ok := m["category"].(string); ok {
		event.EventType = parseAuditEventTypeFromString(v)
	}
	if v, ok := m["action"].(string); ok {
		event.Action = v
	}
	if v, ok := m["outcome"].(string); ok {
		event.Outcome = parseAuditOutcomeFromString(v)
	}
	if v, ok := m["actor_id"].(string); ok {
		if event.Actor == nil {
			event.Actor = &AuditActor{}
		}
		event.Actor.ID = v
	}
	if v, ok := m["actor_type"].(string); ok {
		if event.Actor == nil {
			event.Actor = &AuditActor{}
		}
		event.Actor.Type = v
	}
	if v, ok := m["resource_id"].(string); ok {
		if event.Resource == nil {
			event.Resource = &AuditResource{}
		}
		event.Resource.ID = v
	}
	if v, ok := m["ip_address"].(string); ok {
		if event.Actor == nil {
			event.Actor = &AuditActor{}
		}
		event.Actor.IPAddress = v
	}
	if v, ok := m["user_agent"].(string); ok {
		if event.Actor == nil {
			event.Actor = &AuditActor{}
		}
		event.Actor.UserAgent = v
	}
	if v, ok := m["timestamp"].(time.Time); ok {
		event.Timestamp = v
	}
	if v, ok := m["details"].(map[string]interface{}); ok {
		event.Details = v
	}
	if v, ok := m["details"].(map[string]string); ok {
		event.Details = make(map[string]interface{}, len(v))
		for k, val := range v {
			event.Details[k] = val
		}
	}

	return event
}

// sdkAuditEventResponse mirrors the type from the grpc handler package
// to enable cross-package type conversion.
type sdkAuditEventResponse struct {
	ID         string            `json:"id"`
	TenantID   string            `json:"tenant_id"`
	EventType  string            `json:"event_type"`
	Category   string            `json:"category"`
	Outcome    string            `json:"outcome"`
	ActorID    string            `json:"actor_id"`
	ActorType  string            `json:"actor_type"`
	ResourceID string            `json:"resource_id"`
	Action     string            `json:"action"`
	Details    map[string]string `json:"details,omitempty"`
	IPAddress  string            `json:"ip_address,omitempty"`
	UserAgent  string            `json:"user_agent,omitempty"`
	Timestamp  time.Time         `json:"timestamp"`
}

// convertSdkAuditEventToAuditEvent converts the SDK response type to AuditEvent.
func convertSdkAuditEventToAuditEvent(e *sdkAuditEventResponse) AuditEvent {
	if e == nil {
		return AuditEvent{}
	}

	event := AuditEvent{
		ID:        e.ID,
		TenantID:  e.TenantID,
		EventType: parseAuditEventTypeFromString(e.EventType),
		Action:    e.Action,
		Outcome:   parseAuditOutcomeFromString(e.Outcome),
		Timestamp: e.Timestamp,
	}

	if e.ActorID != "" || e.ActorType != "" || e.IPAddress != "" || e.UserAgent != "" {
		event.Actor = &AuditActor{
			ID:        e.ActorID,
			Type:      e.ActorType,
			IPAddress: e.IPAddress,
			UserAgent: e.UserAgent,
		}
	}

	if e.ResourceID != "" {
		event.Resource = &AuditResource{
			ID: e.ResourceID,
		}
	}

	if len(e.Details) > 0 {
		event.Details = make(map[string]interface{}, len(e.Details))
		for k, v := range e.Details {
			event.Details[k] = v
		}
	}

	return event
}

// parseAuditEventTypeFromString converts a string to AuditEventType.
func parseAuditEventTypeFromString(s string) AuditEventType {
	switch s {
	case "authentication":
		return AuditEventTypeAuthentication
	case "authorization":
		return AuditEventTypeAuthorization
	case "user_management":
		return AuditEventTypeUserManagement
	case "role_management":
		return AuditEventTypeRoleManagement
	case "policy_management":
		return AuditEventTypePolicyManagement
	case "data_access":
		return AuditEventTypeDataAccess
	case "configuration":
		return AuditEventTypeConfiguration
	case "system":
		return AuditEventTypeSystem
	case "gdpr":
		return AuditEventTypeGDPR
	case "admin":
		return AuditEventTypeAdmin
	default:
		return AuditEventTypeSystem
	}
}

// parseAuditOutcomeFromString converts a string to AuditOutcome.
func parseAuditOutcomeFromString(s string) AuditOutcome {
	switch s {
	case "success":
		return AuditOutcomeSuccess
	case "failure":
		return AuditOutcomeFailure
	case "denied":
		return AuditOutcomeDenied
	case "error":
		return AuditOutcomeError
	default:
		return AuditOutcomeSuccess
	}
}
