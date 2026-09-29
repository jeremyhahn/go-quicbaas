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
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// recordingAuditLogger captures the events the adapter emits. Only LogAsync is
// exercised by the adapter; the remaining AuditLogger methods exist to satisfy
// the interface.
type recordingAuditLogger struct {
	mu     sync.Mutex
	events []*iam.AuditEvent
}

func (l *recordingAuditLogger) Log(_ context.Context, event *iam.AuditEvent) error {
	l.LogAsync(event)
	return nil
}

func (l *recordingAuditLogger) LogAsync(event *iam.AuditEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *recordingAuditLogger) Query(context.Context, *iam.AuditFilter) ([]*iam.AuditEvent, int, error) {
	return nil, 0, nil
}

func (l *recordingAuditLogger) GetEvent(context.Context, string, string) (*iam.AuditEvent, error) {
	return nil, nil
}

func (l *recordingAuditLogger) DeleteOlderThan(context.Context, string, time.Time) (int, error) {
	return 0, nil
}

func (l *recordingAuditLogger) Close() error { return nil }

func (l *recordingAuditLogger) recorded() []*iam.AuditEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]*iam.AuditEvent, len(l.events))
	copy(out, l.events)
	return out
}

// A store mutation must land on the platform audit trail with the tenant,
// resource, action and outcome an auditor needs to reconstruct what happened.
func TestOrchestrationStoreAuditor_TranslatesStoreEvents(t *testing.T) {
	tests := []struct {
		name        string
		event       orchestration.StoreAuditEvent
		wantAction  string
		wantOutcome iam.AuditOutcome
	}{
		{
			name: "project create",
			event: orchestration.StoreAuditEvent{
				TenantID:   "tenant-a",
				Resource:   orchestration.AuditResourceProject,
				ResourceID: "proj-1",
				Action:     orchestration.AuditActionCreate,
				Success:    true,
			},
			wantAction:  orchestration.AuditResourceProject + ":" + orchestration.AuditActionCreate,
			wantOutcome: iam.AuditOutcomeSuccess,
		},
		{
			name: "agent update",
			event: orchestration.StoreAuditEvent{
				TenantID:   "tenant-b",
				Resource:   orchestration.AuditResourceFleetNode,
				ResourceID: "agent-7",
				Action:     orchestration.AuditActionUpdate,
				Success:    true,
			},
			wantAction:  orchestration.AuditResourceFleetNode + ":" + orchestration.AuditActionUpdate,
			wantOutcome: iam.AuditOutcomeSuccess,
		},
		{
			name: "denied task delete is recorded as a failure",
			event: orchestration.StoreAuditEvent{
				TenantID:   "tenant-c",
				Resource:   orchestration.AuditResourceTask,
				ResourceID: "task-9",
				Action:     orchestration.AuditActionDelete,
				Success:    false,
			},
			wantAction:  orchestration.AuditResourceTask + ":" + orchestration.AuditActionDelete,
			wantOutcome: iam.AuditOutcomeFailure,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger := &recordingAuditLogger{}
			auditor := newOrchestrationStoreAuditor(func() iam.AuditLogger { return logger })

			auditor.RecordStoreEvent(context.Background(), tc.event)

			recorded := logger.recorded()
			if len(recorded) != 1 {
				t.Fatalf("want exactly 1 audit event, got %d", len(recorded))
			}
			got := recorded[0]

			if got.TenantID != tc.event.TenantID {
				t.Errorf("tenant = %q, want %q", got.TenantID, tc.event.TenantID)
			}
			if got.Action != tc.wantAction {
				t.Errorf("action = %q, want %q", got.Action, tc.wantAction)
			}
			if got.Outcome != tc.wantOutcome {
				t.Errorf("outcome = %q, want %q", got.Outcome, tc.wantOutcome)
			}
			if got.Category != iam.AuditCategoryAdministration {
				t.Errorf("category = %q, want %q", got.Category, iam.AuditCategoryAdministration)
			}
			if got.Resource == nil {
				t.Fatal("audit event must carry a resource")
			}
			if got.Resource.Type != tc.event.Resource || got.Resource.ID != tc.event.ResourceID {
				t.Errorf("resource = %+v, want type=%q id=%q", got.Resource, tc.event.Resource, tc.event.ResourceID)
			}
		})
	}
}

// When the request context identifies the caller, the event must be
// attributable to them.
func TestOrchestrationStoreAuditor_AttributesTheCaller(t *testing.T) {
	logger := &recordingAuditLogger{}
	auditor := newOrchestrationStoreAuditor(func() iam.AuditLogger { return logger })

	ctx := iam.ContextWithUser(context.Background(), "user-42")
	auditor.RecordStoreEvent(ctx, orchestration.StoreAuditEvent{
		TenantID:   "tenant-a",
		Resource:   orchestration.AuditResourceProject,
		ResourceID: "proj-1",
		Action:     orchestration.AuditActionCreate,
		Success:    true,
	})

	recorded := logger.recorded()
	if len(recorded) != 1 {
		t.Fatalf("want 1 audit event, got %d", len(recorded))
	}
	if recorded[0].Actor == nil || recorded[0].Actor.UserID != "user-42" {
		t.Fatalf("event must be attributed to the calling user, got actor %+v", recorded[0].Actor)
	}
}

// An unattributed mutation must still be recorded. Dropping the event because
// the actor is unknown would hide that the change happened at all.
func TestOrchestrationStoreAuditor_RecordsUnattributedMutations(t *testing.T) {
	logger := &recordingAuditLogger{}
	auditor := newOrchestrationStoreAuditor(func() iam.AuditLogger { return logger })

	auditor.RecordStoreEvent(context.Background(), orchestration.StoreAuditEvent{
		TenantID:   "tenant-a",
		Resource:   orchestration.AuditResourceTask,
		ResourceID: "task-1",
		Action:     orchestration.AuditActionCreate,
		Success:    true,
	})

	recorded := logger.recorded()
	if len(recorded) != 1 {
		t.Fatalf("want 1 audit event, got %d", len(recorded))
	}
	if recorded[0].Actor != nil {
		t.Fatalf("want no actor, got %+v", recorded[0].Actor)
	}
}

// The stores are constructed before IAM initialization publishes the audit
// logger, so the adapter resolves it per event. A store that starts before the
// logger exists must record once it does, and must not panic before then.
func TestOrchestrationStoreAuditor_ResolvesLoggerLate(t *testing.T) {
	var logger iam.AuditLogger
	auditor := newOrchestrationStoreAuditor(func() iam.AuditLogger { return logger })

	event := orchestration.StoreAuditEvent{
		TenantID:   "tenant-a",
		Resource:   orchestration.AuditResourceProject,
		ResourceID: "proj-1",
		Action:     orchestration.AuditActionCreate,
		Success:    true,
	}

	// Before IAM init: no logger, no panic, nothing recorded.
	auditor.RecordStoreEvent(context.Background(), event)

	// After IAM init publishes the logger, the same auditor records.
	recorder := &recordingAuditLogger{}
	logger = recorder
	auditor.RecordStoreEvent(context.Background(), event)

	if got := len(recorder.recorded()); got != 1 {
		t.Fatalf("want 1 audit event after the logger is published, got %d", got)
	}
}

// A nil resolver means auditing is disabled, which the stores signal by
// receiving a nil StoreAuditor.
func TestNewOrchestrationStoreAuditor_NilResolverDisablesAuditing(t *testing.T) {
	if auditor := newOrchestrationStoreAuditor(nil); auditor != nil {
		t.Fatalf("a nil resolver must disable auditing, got %#v", auditor)
	}
}

// auditLoggerForStores must tolerate a server that has not initialized IAM.
func TestServer_AuditLoggerForStores_WithoutIAM(t *testing.T) {
	srv := &Server{}
	if got := srv.auditLoggerForStores(); got != nil {
		t.Fatalf("want nil audit logger before IAM init, got %#v", got)
	}
}

// TestSigningRelayAuditor_RecordsTheSignatureNotTheData: a signature relayed to
// a fleet node lands on the platform trail with the session, agent, key
// fingerprint and data digest — never the data — and a refusal is a failure
// carrying its reason (go-quicbaas#1013).
func TestSigningRelayAuditor_RecordsTheSignatureNotTheData(t *testing.T) {
	logger := &recordingAuditLogger{}
	auditor := newSigningRelayAuditor(func() iam.AuditLogger { return logger })
	auditor.RecordSigningRelaySign(context.Background(), orchestration.SigningRelaySignEvent{
		TenantID: "tenant-a", SessionID: "sess-1", AgentID: "agent-2",
		KeyFingerprint: "SHA256:abc", DataSHA256: "deadbeef", Success: true,
	})
	auditor.RecordSigningRelaySign(context.Background(), orchestration.SigningRelaySignEvent{
		TenantID: "tenant-a", SessionID: "sess-1", AgentID: "agent-2", Reason: "refused",
	})

	events := logger.recorded()
	if len(events) != 2 {
		t.Fatalf("recorded %d events, want 2", len(events))
	}
	signed := events[0]
	if signed.Action != signingRelayAuditAction || signed.TenantID != "tenant-a" ||
		signed.Outcome != iam.AuditOutcomeSuccess || signed.Resource.ID != "sess-1" ||
		signed.Details["key_fingerprint"] != "SHA256:abc" || signed.Details["data_sha256"] != "deadbeef" ||
		signed.Details["agent_id"] != "agent-2" {
		t.Fatalf("signed event = %+v", signed)
	}
	if _, hasReason := signed.Details["reason"]; hasReason {
		t.Error("a successful signature carries a refusal reason")
	}
	if refused := events[1]; refused.Outcome != iam.AuditOutcomeFailure || refused.Details["reason"] != "refused" {
		t.Fatalf("refused event = %+v", refused)
	}
}

// TestSigningRelayAuditor_WithNoLoggerRecordsNothing: no resolver is no
// auditor, and a resolver with no logger yet drops the event rather than
// panicking.
func TestSigningRelayAuditor_WithNoLoggerRecordsNothing(t *testing.T) {
	if newSigningRelayAuditor(nil) != nil {
		t.Fatal("a nil resolver built an auditor")
	}
	auditor := newSigningRelayAuditor(func() iam.AuditLogger { return nil })
	auditor.RecordSigningRelaySign(context.Background(), orchestration.SigningRelaySignEvent{TenantID: "tenant-a"})
}
