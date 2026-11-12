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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// newTestNodeAuditLogger builds a NodeAuditLogger over a real (in-memory)
// qrdbsdk audit ring, so these tests exercise the actual sink contract
// (Emit/Query/pagination), not a hand-rolled fake.
func newTestNodeAuditLogger() *NodeAuditLogger {
	return NewNodeAuditLogger(qrdbsdk.NewAuditRingBuffer(1000))
}

func TestNodeAuditLogger_Log_StampsIDAndTimestamp(t *testing.T) {
	l := newTestNodeAuditLogger()
	event := &iam.AuditEvent{
		TenantID:  "tenant-a",
		EventType: iam.AuditEventAuthentication,
		Category:  iam.AuditCategoryAuthentication,
		Action:    "login",
		Outcome:   iam.AuditOutcomeSuccess,
		Actor:     &iam.AuditActor{UserID: "alice"},
	}

	require.NoError(t, l.Log(context.Background(), event))
	assert.NotEmpty(t, event.ID)
	assert.False(t, event.Timestamp.IsZero())
}

func TestNodeAuditLogger_Log_NilEvent(t *testing.T) {
	l := newTestNodeAuditLogger()
	err := l.Log(context.Background(), nil)
	var nilErr *NilAuditEventError
	assert.ErrorAs(t, err, &nilErr)
}

func TestNodeAuditLogger_LogAsync_NilEventIsNoop(t *testing.T) {
	l := newTestNodeAuditLogger()
	// Must not panic.
	l.LogAsync(nil)
}

// refusingAuditSink is a real ring-buffer sink whose Emit reports that the
// event never reached its durability boundary, the way a failing chain file
// does.
type refusingAuditSink struct {
	qrdbsdk.NodeAuditLogger
	emits int
}

func (s *refusingAuditSink) Emit(context.Context, qrdbsdk.NodeAuditEvent) error {
	s.emits++
	return &qrdbsdk.NodeAuditWriteError{Backend: "refusing-test-sink", Err: &diskFullError{}}
}

// diskFullError is the backend cause the refusing sink reports.
type diskFullError struct{}

func (*diskFullError) Error() string { return "no space left on device" }

func newRefusingAuditSink() *refusingAuditSink {
	return &refusingAuditSink{NodeAuditLogger: qrdbsdk.NewAuditRingBuffer(10)}
}

func TestNodeAuditLogger_Log_PropagatesSinkWriteError(t *testing.T) {
	sink := newRefusingAuditSink()
	l := NewNodeAuditLogger(sink)

	err := l.Log(context.Background(), &iam.AuditEvent{Action: "login"})

	var writeErr *qrdbsdk.NodeAuditWriteError
	require.ErrorAs(t, err, &writeErr)
	assert.Equal(t, "refusing-test-sink", writeErr.Backend)
	assert.Equal(t, 1, sink.emits)
}

func TestNodeAuditLogger_LogAsync_SinkWriteErrorIsBestEffort(t *testing.T) {
	sink := newRefusingAuditSink()
	l := NewNodeAuditLogger(sink)

	assert.NotPanics(t, func() { l.LogAsync(&iam.AuditEvent{Action: "login"}) })
	assert.Equal(t, 1, sink.emits, "LogAsync must still attempt the sink")
}

// TestNodeAuditLogger_RoundTrip proves an event emitted via LogAsync
// round-trips through Query with every field (including the fields bucketed
// into Attributes — P3 Option B) intact.
func TestNodeAuditLogger_RoundTrip(t *testing.T) {
	l := newTestNodeAuditLogger()
	event := &iam.AuditEvent{
		TenantID:  "tenant-a",
		EventType: iam.AuditEventUserManagement,
		Category:  iam.AuditCategoryAdministration,
		Action:    "user.create",
		Outcome:   iam.AuditOutcomeSuccess,
		Actor: &iam.AuditActor{
			UserID:   "admin-1",
			Username: "admin",
			Email:    "admin@example.com",
			Roles:    []string{"admin", "operator"},
			Scopes:   []string{"users:create"},
		},
		Resource:      &iam.AuditResource{Type: "user", ID: "u-42", Name: "bob"},
		IPAddress:     "10.0.0.5",
		UserAgent:     "test-agent/1.0",
		CorrelationID: "corr-123",
	}
	l.LogAsync(event)

	events, total, err := l.Query(context.Background(), &iam.AuditFilter{TenantID: "tenant-a"})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, events, 1)

	got := events[0]
	assert.Equal(t, "tenant-a", got.TenantID)
	assert.Equal(t, iam.AuditEventUserManagement, got.EventType)
	assert.Equal(t, iam.AuditCategoryAdministration, got.Category)
	assert.Equal(t, "user.create", got.Action)
	assert.Equal(t, iam.AuditOutcomeSuccess, got.Outcome)
	require.NotNil(t, got.Actor)
	assert.Equal(t, "admin-1", got.Actor.UserID)
	assert.Equal(t, "admin", got.Actor.Username)
	assert.Equal(t, "admin@example.com", got.Actor.Email)
	assert.Equal(t, []string{"admin", "operator"}, got.Actor.Roles)
	assert.Equal(t, []string{"users:create"}, got.Actor.Scopes)
	require.NotNil(t, got.Resource)
	assert.Equal(t, "user", got.Resource.Type)
	assert.Equal(t, "u-42", got.Resource.ID)
	assert.Equal(t, "bob", got.Resource.Name)
	assert.Equal(t, "10.0.0.5", got.IPAddress)
	assert.Equal(t, "test-agent/1.0", got.UserAgent)
	assert.Equal(t, "corr-123", got.CorrelationID)
}

// TestNodeAuditLogger_Query_FiltersByEveryCriterion proves Query applies
// TenantID (server-side), plus EventTypes/Categories/Outcomes/ActorID/
// ResourceID (client-side post-filter, since the node chain's own Filter
// has no equivalent for these).
func TestNodeAuditLogger_Query_FiltersByEveryCriterion(t *testing.T) {
	l := newTestNodeAuditLogger()
	seed := func(tenant, actor, resType, resID string, evType iam.AuditEventType, cat iam.AuditCategory, outcome iam.AuditOutcome) {
		l.LogAsync(&iam.AuditEvent{
			TenantID:  tenant,
			EventType: evType,
			Category:  cat,
			Action:    "x",
			Outcome:   outcome,
			Actor:     &iam.AuditActor{UserID: actor},
			Resource:  &iam.AuditResource{Type: resType, ID: resID},
		})
	}
	seed("t1", "alice", "user", "u1", iam.AuditEventAuthentication, iam.AuditCategoryAuthentication, iam.AuditOutcomeSuccess)
	seed("t1", "bob", "user", "u2", iam.AuditEventUserManagement, iam.AuditCategoryAdministration, iam.AuditOutcomeFailure)
	seed("t2", "alice", "user", "u3", iam.AuditEventAuthentication, iam.AuditCategoryAuthentication, iam.AuditOutcomeSuccess)

	cases := []struct {
		name   string
		filter *iam.AuditFilter
		want   int
	}{
		{"tenant only", &iam.AuditFilter{TenantID: "t1"}, 2},
		{"event type", &iam.AuditFilter{TenantID: "t1", EventTypes: []iam.AuditEventType{iam.AuditEventUserManagement}}, 1},
		{"category", &iam.AuditFilter{TenantID: "t1", Categories: []iam.AuditCategory{iam.AuditCategoryAdministration}}, 1},
		{"outcome", &iam.AuditFilter{TenantID: "t1", Outcomes: []iam.AuditOutcome{iam.AuditOutcomeFailure}}, 1},
		{"actor", &iam.AuditFilter{TenantID: "t1", ActorID: "alice"}, 1},
		{"resource", &iam.AuditFilter{TenantID: "t1", ResourceID: "u2"}, 1},
		{"other tenant isolated", &iam.AuditFilter{TenantID: "t2"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			events, total, err := l.Query(context.Background(), tc.filter)
			require.NoError(t, err)
			assert.Equal(t, tc.want, total)
			assert.Len(t, events, tc.want)
		})
	}
}

// TestNodeAuditLogger_Query_Pagination proves Offset/Limit apply to the
// post-filter result set correctly.
func TestNodeAuditLogger_Query_Pagination(t *testing.T) {
	l := newTestNodeAuditLogger()
	for i := 0; i < 5; i++ {
		l.LogAsync(&iam.AuditEvent{
			TenantID: "t1", Action: "x", Outcome: iam.AuditOutcomeSuccess,
			Actor: &iam.AuditActor{UserID: "u"},
		})
	}

	page1, total, err := l.Query(context.Background(), &iam.AuditFilter{TenantID: "t1", Limit: 2, Offset: 0})
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, page1, 2)

	page2, total, err := l.Query(context.Background(), &iam.AuditFilter{TenantID: "t1", Limit: 2, Offset: 2})
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, page2, 2)

	page3, total, err := l.Query(context.Background(), &iam.AuditFilter{TenantID: "t1", Limit: 2, Offset: 4})
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, page3, 1)

	beyond, total, err := l.Query(context.Background(), &iam.AuditFilter{TenantID: "t1", Limit: 2, Offset: 10})
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, beyond, 0)
}

func TestNodeAuditLogger_GetEvent_FoundAndNotFound(t *testing.T) {
	l := newTestNodeAuditLogger()
	event := &iam.AuditEvent{
		TenantID: "t1", Action: "x", Outcome: iam.AuditOutcomeSuccess,
		Actor: &iam.AuditActor{UserID: "u"},
	}
	require.NoError(t, l.Log(context.Background(), event))

	got, err := l.GetEvent(context.Background(), "t1", event.ID)
	require.NoError(t, err)
	assert.Equal(t, event.ID, got.ID)

	_, err = l.GetEvent(context.Background(), "t1", "nonexistent")
	var notFound *AuditEventNotFoundError
	assert.ErrorAs(t, err, &notFound)
}

func TestNodeAuditLogger_DeleteOlderThan_IsNoop(t *testing.T) {
	l := newTestNodeAuditLogger()
	n, err := l.DeleteOlderThan(context.Background(), "t1", time.Now())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// TestNodeAuditLogger_Close_DoesNotCloseSharedSink proves Close is a no-op:
// the sink stays usable afterward (its lifecycle belongs to
// Server.stopQRDBNode, not the IAM component teardown).
func TestNodeAuditLogger_Close_DoesNotCloseSharedSink(t *testing.T) {
	l := newTestNodeAuditLogger()
	require.NoError(t, l.Close())

	// The underlying sink must still accept emits/queries after Close.
	require.NoError(t, l.Log(context.Background(), &iam.AuditEvent{
		TenantID: "t1", Action: "x", Outcome: iam.AuditOutcomeSuccess,
		Actor: &iam.AuditActor{UserID: "u"},
	}))
	_, total, err := l.Query(context.Background(), &iam.AuditFilter{TenantID: "t1"})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
}
