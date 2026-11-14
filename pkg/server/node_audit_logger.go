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

package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// nodeAuditMaxScan bounds how many chain events NodeAuditLogger.Query walks
// (via the sink's opaque cursor) while assembling one page: the node chain
// has no offset-based pagination, only cursor chaining, so satisfying an
// iam.AuditFilter's Offset/Limit contract means walking pages until enough
// post-filter matches accumulate. This caps that walk so a pathological
// filter (a huge Offset against a huge chain) cannot loop unbounded.
const nodeAuditMaxScan = 5000

// NodeAuditLogger implements iam.AuditLogger backed by the embedded qrdb
// node's compliance-grade audit chain (Server.qrdbAuditSink,
// qrdb_node.go newQRDBAuditSink) — the ONE audit plane every subsystem in
// this daemon emits onto (P5.2 of the login-plane plan). It replaces the
// per-tenant KV-backed DefaultAuditLogger and the in-memory gRPC fallback
// (iam_init.go): there is no parallel logger anywhere in the stack, and no
// record is lost on restart — the sink itself is the durable, tamper-evident
// HMAC-chained file logger (or, in embedded/test contexts without a
// persistent path, the queryable in-memory ring the sink construction
// already falls back to).
//
// Every iam.AuditEvent field with no direct qrdbsdk.NodeAuditEvent
// counterpart rides in Attributes, never dropped and never a second
// parallel schema (P3 Option B).
type NodeAuditLogger struct {
	sink qrdbsdk.NodeAuditLogger
}

// NewNodeAuditLogger wraps sink (the node's audit chain) as an
// iam.AuditLogger. sink must be non-nil; every IAM component that consumes
// GetAuditLogger() shares this SAME instance, so audit events from every
// emit site land on the ONE chain in a single, coherent order.
func NewNodeAuditLogger(sink qrdbsdk.NodeAuditLogger) *NodeAuditLogger {
	return &NodeAuditLogger{sink: sink}
}

// Compile-time interface check.
var _ iam.AuditLogger = (*NodeAuditLogger)(nil)

// Log synchronously emits event onto the node's audit chain.
func (l *NodeAuditLogger) Log(ctx context.Context, event *iam.AuditEvent) error {
	if event == nil {
		return &NilAuditEventError{}
	}
	stampAuditEvent(event)
	// Emit is documented non-blocking and best-effort, so its error is not
	// the caller's to act on and Log has always returned nil here. Discarding
	// it explicitly says that is deliberate. Whether Log should surface it
	// instead is a contract question, not a lint fix.
	_ = l.sink.Emit(ctx, toNodeAuditEvent(event))
	return nil
}

// LogAsync emits event onto the node's audit chain without blocking the
// caller. The qrdbsdk.NodeAuditLogger.Emit contract is itself documented
// non-blocking and best-effort, so no additional goroutine indirection is
// needed here — unlike the retired DefaultAuditLogger, which needed its own
// async channel because its synchronous Log wrote through to a KV store.
func (l *NodeAuditLogger) LogAsync(event *iam.AuditEvent) {
	if event == nil {
		return
	}
	stampAuditEvent(event)
	// Best-effort by contract, and LogAsync has no way to report anyway.
	_ = l.sink.Emit(context.Background(), toNodeAuditEvent(event))
}

// stampAuditEvent assigns event's ID and Timestamp when unset, mirroring
// the retired DefaultAuditLogger's Log/LogAsync contract so callers that
// depended on auto-stamping keep working unchanged.
func stampAuditEvent(event *iam.AuditEvent) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
}

// Query retrieves audit events matching filter from the node's audit
// chain. The chain paginates by opaque cursor, not by offset, and its
// Filter carries only a single Outcome prefix (no EventTypes/Categories
// lists) — so Query walks the chain via cursor chaining (bounded by
// nodeAuditMaxScan), applies the full iam.AuditFilter server-side after
// translating each event back, and then applies Offset/Limit to the
// post-filter result set.
func (l *NodeAuditLogger) Query(ctx context.Context, filter *iam.AuditFilter) ([]*iam.AuditEvent, int, error) {
	if filter == nil {
		filter = &iam.AuditFilter{}
	}
	nf := toNodeAuditFilter(filter)
	nf.Limit = 0 // walk full chain pages; post-filtering decides what counts

	var matched []*iam.AuditEvent
	cursor := ""
	scanned := 0
	for {
		nf.Cursor = cursor
		page, err := l.sink.Query(ctx, nf)
		if err != nil {
			return nil, 0, err
		}
		for i := range page.Events {
			ev := fromNodeAuditEvent(&page.Events[i])
			if auditEventMatchesFilter(ev, filter) {
				matched = append(matched, ev)
			}
		}
		scanned += len(page.Events)
		if page.Next == "" || len(page.Events) == 0 || scanned >= nodeAuditMaxScan {
			break
		}
		cursor = page.Next
	}

	total := len(matched)
	offset := filter.Offset
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	if offset >= total {
		return []*iam.AuditEvent{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return matched[offset:end], total, nil
}

// GetEvent retrieves a specific audit event by ID. The node chain has no
// ID-indexed lookup (events are queried by filter, not fetched by a
// synthetic ID), so GetEvent scans a bounded window for the matching ID.
func (l *NodeAuditLogger) GetEvent(ctx context.Context, tenantID, eventID string) (*iam.AuditEvent, error) {
	events, _, err := l.Query(ctx, &iam.AuditFilter{TenantID: tenantID, Limit: nodeAuditMaxScan})
	if err != nil {
		return nil, err
	}
	for _, e := range events {
		if e.ID == eventID {
			return e, nil
		}
	}
	return nil, &AuditEventNotFoundError{EventID: eventID}
}

// DeleteOlderThan is a no-op: the chain is append-only and tamper-evident
// by design (P5 explicitly forbids a lossy fallback), so retention is
// enforced by the sink's own file rotation/pruning
// (go-qrdb pkg/audit.ChainFileLogger), never by ad hoc per-tenant deletion
// that would break the hash chain.
func (l *NodeAuditLogger) DeleteOlderThan(_ context.Context, _ string, _ time.Time) (int, error) {
	return 0, nil
}

// Close is a no-op: the sink's lifecycle is owned exclusively by
// Server.stopQRDBNode (closed AFTER the qrdb node, qrdb_node.go), because
// the SAME sink instance backs the node's own audit emission — an IAM
// component teardown must never close a chain the node is still writing
// to.
func (l *NodeAuditLogger) Close() error {
	return nil
}

// NilAuditEventError is returned by Log when event is nil.
type NilAuditEventError struct{}

// Error implements the error interface.
func (e *NilAuditEventError) Error() string {
	return "audit: nil event"
}

// AuditEventNotFoundError is returned by GetEvent when no event with the
// given ID is found in the scanned window.
type AuditEventNotFoundError struct {
	EventID string
}

// Error implements the error interface.
func (e *AuditEventNotFoundError) Error() string {
	return "audit: event " + e.EventID + " not found"
}

// Is reports true for iam.ErrAuditEventNotFound, so callers that map the
// IAM sentinel (the REST audit handler's 404) match the node chain's error.
func (e *AuditEventNotFoundError) Is(target error) bool {
	return target == iam.ErrAuditEventNotFound
}

// --- iam.AuditEvent <-> qrdbsdk.NodeAuditEvent conversion (P3 Option B) ---

// toNodeAuditEvent projects an iam.AuditEvent onto the node chain's fixed
// schema. Every field with no direct counterpart is bucketed into
// Attributes under an "iam_"/"actor_"/"resource_" prefixed key so it is
// never dropped and round-trips through fromNodeAuditEvent.
func toNodeAuditEvent(e *iam.AuditEvent) qrdbsdk.NodeAuditEvent {
	attrs := map[string]any{
		"iam_id":         e.ID,
		"iam_event_type": string(e.EventType),
		"iam_category":   string(e.Category),
	}
	if e.UserAgent != "" {
		attrs["user_agent"] = e.UserAgent
	}
	if e.ImpersonationSessionID != "" {
		attrs["impersonation_session_id"] = e.ImpersonationSessionID
	}
	for k, v := range e.Details {
		attrs["detail_"+k] = v
	}

	var actor, role string
	if e.Actor != nil {
		actor = e.Actor.UserID
		if actor == "" {
			actor = e.Actor.Username
		}
		if actor == "" {
			actor = e.Actor.ClientID
		}
		if len(e.Actor.Roles) > 0 {
			role = strings.Join(e.Actor.Roles, ",")
		}
		if e.Actor.Username != "" {
			attrs["actor_username"] = e.Actor.Username
		}
		if e.Actor.Email != "" {
			attrs["actor_email"] = e.Actor.Email
		}
		if e.Actor.SessionID != "" {
			attrs["actor_session_id"] = e.Actor.SessionID
		}
		if e.Actor.ClientID != "" {
			attrs["actor_client_id"] = e.Actor.ClientID
		}
		if e.Actor.ImpersonatingUserID != "" {
			attrs["actor_impersonating_user_id"] = e.Actor.ImpersonatingUserID
		}
		if e.Actor.ImpersonatingUsername != "" {
			attrs["actor_impersonating_username"] = e.Actor.ImpersonatingUsername
		}
		if e.Actor.ServiceIdentity != "" {
			attrs["actor_service_identity"] = e.Actor.ServiceIdentity
		}
		if e.Actor.JTI != "" {
			attrs["actor_jti"] = e.Actor.JTI
		}
		if len(e.Actor.Scopes) > 0 {
			attrs["actor_scopes"] = strings.Join(e.Actor.Scopes, ",")
		}
		if len(e.Actor.Roles) > 0 {
			attrs["actor_roles"] = strings.Join(e.Actor.Roles, ",")
		}
	}

	var resource string
	if e.Resource != nil {
		resource = e.Resource.Type
		if e.Resource.ID != "" {
			resource = resource + ":" + e.Resource.ID
		}
		if e.Resource.Name != "" {
			attrs["resource_name"] = e.Resource.Name
		}
	}

	return qrdbsdk.NodeAuditEvent{
		Timestamp:  e.Timestamp,
		Actor:      actor,
		TenantID:   e.TenantID,
		Role:       role,
		Resource:   resource,
		Action:     e.Action,
		Outcome:    string(e.Outcome), // AuditOutcome* constants already match qrdb's "success"/"failure"/"denied"
		Attributes: attrs,
		ClientIP:   e.IPAddress,
		RequestID:  e.CorrelationID,
	}
}

// fromNodeAuditEvent is the best-effort reverse of toNodeAuditEvent, used
// by Query to project chain events back into iam's read model. Attributes
// bucketed by toNodeAuditEvent are unpacked back onto their original
// fields.
func fromNodeAuditEvent(ne *qrdbsdk.NodeAuditEvent) *iam.AuditEvent {
	attrStr := func(key string) string {
		if ne.Attributes == nil {
			return ""
		}
		if v, ok := ne.Attributes[key].(string); ok {
			return v
		}
		return ""
	}

	id := attrStr("iam_id")
	if id == "" {
		id = ne.RequestID
	}
	if id == "" {
		id = nodeAuditContentID(ne)
	}

	details := map[string]interface{}{}
	for k, v := range ne.Attributes {
		if strings.HasPrefix(k, "detail_") {
			details[strings.TrimPrefix(k, "detail_")] = v
		}
	}
	if len(details) == 0 {
		details = nil
	}

	actor := &iam.AuditActor{
		UserID:                ne.Actor,
		Username:              attrStr("actor_username"),
		Email:                 attrStr("actor_email"),
		SessionID:             attrStr("actor_session_id"),
		ClientID:              attrStr("actor_client_id"),
		ImpersonatingUserID:   attrStr("actor_impersonating_user_id"),
		ImpersonatingUsername: attrStr("actor_impersonating_username"),
		ServiceIdentity:       attrStr("actor_service_identity"),
		JTI:                   attrStr("actor_jti"),
	}
	if scopes := attrStr("actor_scopes"); scopes != "" {
		actor.Scopes = strings.Split(scopes, ",")
	}
	if roles := attrStr("actor_roles"); roles != "" {
		actor.Roles = strings.Split(roles, ",")
	} else if ne.Role != "" {
		actor.Roles = strings.Split(ne.Role, ",")
	}

	var resource *iam.AuditResource
	if ne.Resource != "" {
		resType, resID, _ := strings.Cut(ne.Resource, ":")
		resource = &iam.AuditResource{
			Type: resType,
			ID:   resID,
			Name: attrStr("resource_name"),
		}
	}

	return &iam.AuditEvent{
		ID:                     id,
		TenantID:               ne.TenantID,
		Timestamp:              ne.Timestamp,
		EventType:              iam.AuditEventType(attrStr("iam_event_type")),
		Category:               iam.AuditCategory(attrStr("iam_category")),
		Actor:                  actor,
		Resource:               resource,
		Action:                 ne.Action,
		Outcome:                iam.AuditOutcome(ne.Outcome),
		Details:                details,
		IPAddress:              ne.ClientIP,
		UserAgent:              attrStr("user_agent"),
		ImpersonationSessionID: attrStr("impersonation_session_id"),
		CorrelationID:          ne.RequestID,
	}
}

// nodeAuditContentID derives the ID of a chain event that carries neither an
// iam_id nor a request ID (qrdb's authorization decision logger stamps
// neither) from the event's own recorded content. The chain is append-only,
// so the same event reads back with the same content, and so the same ID, on
// every read: a listed event stays fetchable by GetEvent and recognizable
// across reads. encoding/json sorts map keys, so the encoding is canonical.
func nodeAuditContentID(ne *qrdbsdk.NodeAuditEvent) string {
	encoded, err := json.Marshal(ne)
	if err != nil {
		// Attributes decoded from the chain are always JSON values; an
		// in-process value json cannot encode still prints deterministically
		// (fmt sorts map keys).
		encoded = []byte(fmt.Sprintf("%+v", *ne))
	}
	sum := sha256.Sum256(encoded)
	return "chain-" + hex.EncodeToString(sum[:16])
}

// toNodeAuditFilter translates the tenant/time-range portion of an
// iam.AuditFilter into a qrdbsdk.NodeAuditFilter. EventTypes/Categories/
// multi-value Outcomes/ActorID/ResourceID are NOT translated here — the
// node Filter has no equivalent (a single Outcome prefix, single Actor/
// Resource string) — Query applies them client-side via
// auditEventMatchesFilter instead, so no criterion is silently dropped.
func toNodeAuditFilter(f *iam.AuditFilter) qrdbsdk.NodeAuditFilter {
	nf := qrdbsdk.NodeAuditFilter{TenantID: f.TenantID}
	if f.StartTime != nil {
		nf.From = *f.StartTime
	}
	if f.EndTime != nil {
		nf.To = *f.EndTime
	}
	return nf
}

// auditEventMatchesFilter applies the criteria toNodeAuditFilter could not
// express server-side: EventTypes, Categories, Outcomes (list),
// ActorID, and ResourceID.
func auditEventMatchesFilter(e *iam.AuditEvent, f *iam.AuditFilter) bool {
	if len(f.EventTypes) > 0 {
		found := false
		for _, t := range f.EventTypes {
			if e.EventType == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(f.Categories) > 0 {
		found := false
		for _, c := range f.Categories {
			if e.Category == c {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(f.Outcomes) > 0 {
		found := false
		for _, o := range f.Outcomes {
			if e.Outcome == o {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if f.ActorID != "" {
		if e.Actor == nil || e.Actor.UserID != f.ActorID {
			return false
		}
	}
	if f.ResourceID != "" {
		if e.Resource == nil || e.Resource.ID != f.ResourceID {
			return false
		}
	}
	return true
}
