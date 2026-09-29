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

package server

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// orchestrationStoreAuditor adapts the platform audit logger to the narrow
// sink the durable orchestration stores emit through.
//
// Orchestration records are tenant-owned business data, so create/update/delete
// on them belongs on the same tamper-evident trail as IAM changes rather than a
// parallel log: FedRAMP AU-2/AU-3, PCI-DSS 10.2 and HIPAA 164.312(b) all expect
// one attributable record of who changed what. Events carry identifiers only —
// never record contents — so the trail never becomes a second copy of the data.
// The logger is resolved per event rather than captured at construction: the
// stores are built while the service is assembled, which is before IAM
// initialization publishes the audit logger. Resolving late means an early
// store still records onto the trail once it exists, instead of being wired to
// a nil logger for the life of the process.
type orchestrationStoreAuditor struct {
	resolve func() iam.AuditLogger
}

// Compile-time interface satisfaction check.
var _ orchestration.StoreAuditor = (*orchestrationStoreAuditor)(nil)

// newOrchestrationStoreAuditor binds store events to the platform audit logger
// returned by resolve. A nil resolver yields a nil auditor, which the stores
// treat as auditing disabled.
func newOrchestrationStoreAuditor(resolve func() iam.AuditLogger) orchestration.StoreAuditor {
	if resolve == nil {
		return nil
	}
	return &orchestrationStoreAuditor{resolve: resolve}
}

// auditLoggerForStores returns the platform audit logger once IAM
// initialization has published it, or nil when the server is running without
// an IAM subsystem (storage-less and embedded boots).
func (s *Server) auditLoggerForStores() iam.AuditLogger {
	if s.iamComponents == nil {
		return nil
	}
	return s.iamComponents.GetAuditLogger()
}

// RecordStoreEvent appends one store mutation to the audit trail.
//
// The write is asynchronous: a store mutation must not block on audit I/O, and
// LogAsync already buffers and persists through the same BackendAuditStorage
// the IAM subsystem uses.
func (a *orchestrationStoreAuditor) RecordStoreEvent(ctx context.Context, event orchestration.StoreAuditEvent) {
	logger := a.resolve()
	if logger == nil {
		return
	}

	outcome := iam.AuditOutcomeSuccess
	if !event.Success {
		outcome = iam.AuditOutcomeFailure
	}

	auditEvent := &iam.AuditEvent{
		EventType: iam.AuditEventAppEvent,
		Category:  iam.AuditCategoryAdministration,
		TenantID:  event.TenantID,
		Action:    event.Resource + ":" + event.Action,
		Outcome:   outcome,
		Resource: &iam.AuditResource{
			Type: event.Resource,
			ID:   event.ResourceID,
		},
	}

	if actor := auditActorFromContext(ctx); actor != nil {
		auditEvent.Actor = actor
	}

	logger.LogAsync(auditEvent)
}

// auditActorFromContext attributes the mutation to the calling principal when
// the request context carries one. An unattributed event is still recorded —
// losing the whole event because the actor is unknown would be worse than
// recording that the change happened.
func auditActorFromContext(ctx context.Context) *iam.AuditActor {
	userID := iam.UserFromContext(ctx)
	if userID == "" {
		return nil
	}
	return &iam.AuditActor{UserID: userID}
}

// Compile-time interface satisfaction check.
var _ orchestration.SigningRelayAuditor = (*orchestrationStoreAuditor)(nil)

// signingRelayAuditAction is the audit action a relayed signature is recorded
// under.
const signingRelayAuditAction = "orchestration_signing_relay:sign"

// newSigningRelayAuditor binds relayed-signature events to the platform audit
// logger returned by resolve, the same trail the orchestration stores record
// on. A nil resolver yields a nil auditor.
func newSigningRelayAuditor(resolve func() iam.AuditLogger) orchestration.SigningRelayAuditor {
	if resolve == nil {
		return nil
	}
	return &orchestrationStoreAuditor{resolve: resolve}
}

// RecordSigningRelaySign appends one signature relayed to a fleet node
// (go-quicbaas#1013): the session, the tenant, the agent, the key's
// fingerprint and the SHA-256 of the data. Never the data itself — the trail
// records that a signature was made and over what, not a copy of what was
// signed. Written asynchronously, like every store event.
func (a *orchestrationStoreAuditor) RecordSigningRelaySign(ctx context.Context, event orchestration.SigningRelaySignEvent) {
	logger := a.resolve()
	if logger == nil {
		return
	}
	outcome := iam.AuditOutcomeSuccess
	if !event.Success {
		outcome = iam.AuditOutcomeFailure
	}
	details := map[string]interface{}{
		"session_id":      event.SessionID,
		"agent_id":        event.AgentID,
		"key_fingerprint": event.KeyFingerprint,
		"data_sha256":     event.DataSHA256,
	}
	if event.Reason != "" {
		details["reason"] = event.Reason
	}
	auditEvent := &iam.AuditEvent{
		EventType: iam.AuditEventAppEvent,
		Category:  iam.AuditCategorySecurity,
		TenantID:  event.TenantID,
		Action:    signingRelayAuditAction,
		Outcome:   outcome,
		Resource:  &iam.AuditResource{Type: "orchestration_session", ID: event.SessionID},
		Details:   details,
	}
	if actor := auditActorFromContext(ctx); actor != nil {
		auditEvent.Actor = actor
	}
	logger.LogAsync(auditEvent)
}
