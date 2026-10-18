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
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// savedAuditEvent is one SaveEvent call and the result the real store gave.
type savedAuditEvent struct {
	event *iam.AuditEvent
	err   error
}

// observedAuditStorage is the real in-memory store; it reports every save
// so a test can wait for the async worker without sleeping.
type observedAuditStorage struct {
	*iam.MemoryAuditStorage
	saved chan savedAuditEvent
}

func (s *observedAuditStorage) SaveEvent(ctx context.Context, event *iam.AuditEvent) error {
	err := s.MemoryAuditStorage.SaveEvent(ctx, event)
	s.saved <- savedAuditEvent{event: event, err: err}
	return err
}

// newRealAuditLogger returns the default iam audit logger, which validates
// every event, over an observed in-memory store.
func newRealAuditLogger(t *testing.T) (*iam.DefaultAuditLogger, *observedAuditStorage) {
	t.Helper()
	storage := &observedAuditStorage{MemoryAuditStorage: iam.NewMemoryAuditStorage(), saved: make(chan savedAuditEvent, 16)}
	logger, err := iam.NewAuditLogger(iam.WithAuditStorage(storage))
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return logger, storage
}

func newRealAuditClient(t *testing.T) (iamsdk.AuditClient, *iam.DefaultAuditLogger, *observedAuditStorage) {
	t.Helper()
	logger, storage := newRealAuditLogger(t)
	return newAuditClient(t, &mockQuicBaaSWithAudit{logger: logger}), logger, storage
}

func TestIAMAuditRecord_RealLoggerStoresCategory(t *testing.T) {
	client, logger, _ := newRealAuditClient(t)
	id, err := client.Record(auditCtx(), &iamsdk.AuditEvent{
		EventType: iamsdk.AuditEventTypeUserManagement,
		Action:    "create",
		Outcome:   iamsdk.AuditOutcomeSuccess,
		Actor:     &iamsdk.AuditActor{ID: "u-1", Type: "user"},
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	stored, err := logger.GetEvent(context.Background(), auditTenant, id)
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if stored.Category != iam.AuditCategoryAdministration {
		t.Fatalf("category = %q, want %q", stored.Category, iam.AuditCategoryAdministration)
	}
}

func TestIAMAuditRecordAsync_RealLoggerStoresEvent(t *testing.T) {
	client, _, storage := newRealAuditClient(t)
	if err := client.RecordAsync(auditCtx(), &iamsdk.AuditEvent{
		EventType: iamsdk.AuditEventTypeAuthentication,
		Action:    "login",
		Outcome:   iamsdk.AuditOutcomeSuccess,
	}); err != nil {
		t.Fatalf("RecordAsync: %v", err)
	}
	select {
	case saved := <-storage.saved:
		if saved.err != nil {
			t.Fatalf("the async worker could not store the event: %v", saved.err)
		}
		if saved.event.Category != iam.AuditCategoryAuthentication {
			t.Fatalf("category = %q, want %q", saved.event.Category, iam.AuditCategoryAuthentication)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the async worker never stored the event")
	}
}

// TestIAMAuditRecordAsync_InvalidEventRefused proves an event the worker
// would drop is refused to the caller instead.
func TestIAMAuditRecordAsync_InvalidEventRefused(t *testing.T) {
	client, _, storage := newRealAuditClient(t)
	if err := client.RecordAsync(auditCtx(), &iamsdk.AuditEvent{Outcome: iamsdk.AuditOutcomeSuccess}); err == nil {
		t.Fatal("RecordAsync accepted an event with no action")
	}
	select {
	case saved := <-storage.saved:
		t.Fatalf("an invalid event reached the store: %+v", saved.event)
	default:
	}
}

func TestAuditCategoryFor(t *testing.T) {
	tests := map[iam.AuditEventType]iam.AuditCategory{
		iam.AuditEventAuthentication: iam.AuditCategoryAuthentication,
		iam.AuditEventAuthorization:  iam.AuditCategoryAuthorization,
		iam.AuditEventTenantEvent:    iam.AuditCategoryAdministration,
		iam.AuditEventType("other"):  iam.AuditCategorySecurity,
	}
	for eventType, want := range tests {
		if got := auditCategoryFor(eventType); got != want {
			t.Errorf("auditCategoryFor(%q) = %q, want %q", eventType, got, want)
		}
	}
}

// TestIAMAuditActorTypeRoundTrips records each actor kind through the real
// logger and reads it back through Get and Query.
func TestIAMAuditActorTypeRoundTrips(t *testing.T) {
	for _, actorType := range []string{"service", "user"} {
		t.Run(actorType, func(t *testing.T) {
			client, _, _ := newRealAuditClient(t)
			id, err := client.Record(auditCtx(), &iamsdk.AuditEvent{
				EventType: iamsdk.AuditEventTypeAuthorization,
				Action:    "call",
				Outcome:   iamsdk.AuditOutcomeSuccess,
				Actor:     &iamsdk.AuditActor{ID: "actor-1", Type: actorType},
			})
			if err != nil {
				t.Fatalf("Record: %v", err)
			}

			got, err := client.Get(auditCtx(), auditTenant, id)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.Actor == nil || got.Actor.ID != "actor-1" || got.Actor.Type != actorType {
				t.Errorf("Get actor = %+v, want actor-1 as %s", got.Actor, actorType)
			}

			resp, err := client.Query(auditCtx(), &iamsdk.AuditFilter{})
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(resp.Events) != 1 || resp.Events[0].Actor == nil || resp.Events[0].Actor.Type != actorType {
				t.Errorf("Query events = %+v, want one %s actor", resp.Events, actorType)
			}
		})
	}
}
