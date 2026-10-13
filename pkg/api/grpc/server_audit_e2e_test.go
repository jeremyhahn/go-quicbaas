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

package grpc

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	realgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

const auditE2EBufSize = 1 << 20 // 1 MiB

// tenantInjectingInterceptor mimics the production authn/authz middleware's
// finalizeIdentity step (iam.ContextWithTenant, pkg/api/rest/auth_support.go
// / pkg/api/grpc/authz_interceptor.go): it binds a fixed caller tenant into
// every request's context BEFORE the RPC handler runs, exactly as the real
// interceptor chain does from the verified identity. Tests use it to
// exercise RecordAuditEvent's tenant-scoping enforcement
// (resolveRecordAuditTenant, server_audit.go) without standing up the full
// auth stack.
func tenantInjectingInterceptor(callerTenant string) realgrpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, _ *realgrpc.UnaryServerInfo, handler realgrpc.UnaryHandler) (interface{}, error) {
		return handler(tenantctx.WithTenant(ctx, callerTenant), req)
	}
}

// newAuditServiceBufconn starts an in-process AuditService gRPC server
// backed by a real iam.AuditLogger and returns a connected
// iampb.AuditServiceClient plus a cleanup function. It exists in pkg/api/grpc
// (rather than importing this package from pkg/server, which would be a
// cycle) so it can construct the AuditServer with any iam.AuditLogger,
// including the node-backed one pkg/server.NewNodeAuditLogger builds in
// production. callerTenant, when non-empty, is bound into every request's
// context via tenantInjectingInterceptor, simulating an authenticated
// caller in that tenant.
func newAuditServiceBufconn(t *testing.T, logger iam.AuditLogger, callerTenant string) (iampb.AuditServiceClient, func()) {
	t.Helper()
	lis := bufconn.Listen(auditE2EBufSize)
	var opts []realgrpc.ServerOption
	if callerTenant != "" {
		opts = append(opts, realgrpc.UnaryInterceptor(tenantInjectingInterceptor(callerTenant)))
	}
	srv := realgrpc.NewServer(opts...)
	adapter := NewAuditServiceAdapter(logger)
	iampb.RegisterAuditServiceServer(srv, NewAuditServer(adapter, newTestLogger()))
	go func() { _ = srv.Serve(lis) }()

	conn, err := realgrpc.NewClient("passthrough:///bufnet",
		realgrpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		realgrpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	return iampb.NewAuditServiceClient(conn), func() {
		_ = conn.Close()
		srv.Stop()
	}
}

// TestAuditService_GRPC_RoundTrip_NodeBackedChain proves the full audit
// read-back path end-to-end: an event emitted onto a real qrdb audit sink
// (via the SAME NodeAuditLogger production wiring uses) is retrievable
// through the real AuditService gRPC wire (QueryAuditEvents and
// GetAuditEvent) — the P5.2 unified audit plane, not the retired
// always-empty stub.
func TestAuditService_GRPC_RoundTrip_NodeBackedChain(t *testing.T) {
	sink := qrdbsdk.NewAuditRingBuffer(1000)

	// pkg/server.NewNodeAuditLogger cannot be imported here (pkg/server
	// imports pkg/api/grpc, which would cycle); a minimal iam.AuditLogger
	// backed by the SAME qrdbsdk.NodeAuditLogger sink type reproduces
	// exactly what pkg/server/node_audit_logger.go does, proving the wire
	// path this test targets (AuditServiceAdapter -> AuditServer ->
	// iampb.AuditServiceClient) without re-deriving that adapter's own
	// already-unit-tested conversion logic.
	logger := &grpcTestNodeBackedAuditLogger{sink: sink}

	event := &iam.AuditEvent{
		TenantID:  "tenant-e2e",
		EventType: iam.AuditEventAuthentication,
		Category:  iam.AuditCategoryAuthentication,
		Action:    "login",
		Outcome:   iam.AuditOutcomeSuccess,
		Actor:     &iam.AuditActor{UserID: "alice"},
	}
	require.NoError(t, logger.Log(context.Background(), event))

	// Authenticate as the tenant whose chain is being read: with the #356
	// operator lift removed, only tenant-e2e itself may read tenant-e2e.
	client, cleanup := newAuditServiceBufconn(t, logger, "tenant-e2e")
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	queryResp, err := client.QueryAuditEvents(ctx, &iampb.QueryAuditEventsRequest{
		TenantId: "tenant-e2e",
	})
	require.NoError(t, err)
	require.Len(t, queryResp.Events, 1)
	assert.Equal(t, event.ID, queryResp.Events[0].Id)
	assert.Equal(t, "login", queryResp.Events[0].Action)
	assert.Equal(t, "tenant-e2e", queryResp.Events[0].TenantId)

	getResp, err := client.GetAuditEvent(ctx, &iampb.GetAuditEventRequest{
		TenantId: "tenant-e2e",
		EventId:  event.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, event.ID, getResp.Id)

	_, err = client.GetAuditEvent(ctx, &iampb.GetAuditEventRequest{
		TenantId: "tenant-e2e",
		EventId:  "does-not-exist",
	})
	require.Error(t, err)
}

// grpcTestNodeBackedAuditLogger is the minimal iam.AuditLogger test double
// this file uses to exercise the AuditServiceAdapter/AuditServer wire path
// against a real qrdbsdk.NodeAuditLogger sink, without importing
// pkg/server (which would import pkg/api/grpc, a cycle).
type grpcTestNodeBackedAuditLogger struct {
	sink qrdbsdk.NodeAuditLogger
	seq  int
}

func (l *grpcTestNodeBackedAuditLogger) Log(ctx context.Context, e *iam.AuditEvent) error {
	if e.ID == "" {
		l.seq++
		e.ID = fmt.Sprintf("test-event-%d", l.seq)
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	var actor string
	if e.Actor != nil {
		actor = e.Actor.UserID
	}
	l.sink.Emit(ctx, qrdbsdk.NodeAuditEvent{
		Timestamp:  e.Timestamp,
		Actor:      actor,
		TenantID:   e.TenantID,
		Resource:   "",
		Action:     e.Action,
		Outcome:    string(e.Outcome),
		Attributes: map[string]any{"iam_id": e.ID, "iam_event_type": string(e.EventType), "iam_category": string(e.Category)},
	})
	return nil
}

func (l *grpcTestNodeBackedAuditLogger) LogAsync(e *iam.AuditEvent) {
	_ = l.Log(context.Background(), e)
}

func (l *grpcTestNodeBackedAuditLogger) Query(ctx context.Context, filter *iam.AuditFilter) ([]*iam.AuditEvent, int, error) {
	page, err := l.sink.Query(ctx, qrdbsdk.NodeAuditFilter{TenantID: filter.TenantID})
	if err != nil {
		return nil, 0, err
	}
	events := make([]*iam.AuditEvent, 0, len(page.Events))
	for _, ne := range page.Events {
		id, _ := ne.Attributes["iam_id"].(string)
		evType, _ := ne.Attributes["iam_event_type"].(string)
		cat, _ := ne.Attributes["iam_category"].(string)
		events = append(events, &iam.AuditEvent{
			ID:        id,
			TenantID:  ne.TenantID,
			Timestamp: ne.Timestamp,
			EventType: iam.AuditEventType(evType),
			Category:  iam.AuditCategory(cat),
			Action:    ne.Action,
			Outcome:   iam.AuditOutcome(ne.Outcome),
			Actor:     &iam.AuditActor{UserID: ne.Actor},
		})
	}
	return events, len(events), nil
}

func (l *grpcTestNodeBackedAuditLogger) GetEvent(ctx context.Context, tenantID, eventID string) (*iam.AuditEvent, error) {
	events, _, err := l.Query(ctx, &iam.AuditFilter{TenantID: tenantID})
	if err != nil {
		return nil, err
	}
	for _, e := range events {
		if e.ID == eventID {
			return e, nil
		}
	}
	return nil, &AuditEventNotFoundTestError{EventID: eventID}
}

func (l *grpcTestNodeBackedAuditLogger) DeleteOlderThan(context.Context, string, time.Time) (int, error) {
	return 0, nil
}

func (l *grpcTestNodeBackedAuditLogger) Close() error { return nil }

var _ iam.AuditLogger = (*grpcTestNodeBackedAuditLogger)(nil)

// AuditEventNotFoundTestError mirrors pkg/server.AuditEventNotFoundError for
// this package's test double (pkg/server cannot be imported here).
type AuditEventNotFoundTestError struct {
	EventID string
}

func (e *AuditEventNotFoundTestError) Error() string {
	return "audit: event " + e.EventID + " not found"
}

// TestAuditService_GRPC_RecordThenQuery_RoundTrip is the P5.3 guard: a
// caller with no wire-level write path before this change now records an
// event through the REAL RecordAuditEvent RPC, and the event is observably
// present via QueryAuditEvents/GetAuditEvent afterward — proving the event
// actually landed on the chain, not just that the RPC returned success.
func TestAuditService_GRPC_RecordThenQuery_RoundTrip(t *testing.T) {
	sink := qrdbsdk.NewAuditRingBuffer(1000)
	logger := &grpcTestNodeBackedAuditLogger{sink: sink}

	client, cleanup := newAuditServiceBufconn(t, logger, "tenant-e2e")
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Run("synchronous Record returns the assigned event ID and is queryable", func(t *testing.T) {
		resp, err := client.RecordAuditEvent(ctx, &iampb.RecordAuditEventRequest{
			Event: &iampb.AuditEvent{
				EventType: "authentication",
				Category:  "authentication",
				Action:    "login",
				Outcome:   "success",
				ActorId:   "carol",
			},
			Async: false,
		})
		require.NoError(t, err)
		require.NotEmpty(t, resp.EventId, "synchronous Record must return the chain-assigned event ID")

		got, err := client.GetAuditEvent(ctx, &iampb.GetAuditEventRequest{
			TenantId: "tenant-e2e",
			EventId:  resp.EventId,
		})
		require.NoError(t, err)
		assert.Equal(t, "login", got.Action)
		assert.Equal(t, "tenant-e2e", got.TenantId, "the event lands in the CALLER's own authenticated tenant")
	})

	t.Run("async RecordAsync returns no event ID but the event still lands", func(t *testing.T) {
		resp, err := client.RecordAuditEvent(ctx, &iampb.RecordAuditEventRequest{
			Event: &iampb.AuditEvent{
				EventType: "authentication",
				Category:  "authentication",
				Action:    "logout",
				Outcome:   "success",
				ActorId:   "carol",
			},
			Async: true,
		})
		require.NoError(t, err)
		assert.Empty(t, resp.EventId, "async Record must not return an event_id")

		queryResp, err := client.QueryAuditEvents(ctx, &iampb.QueryAuditEventsRequest{TenantId: "tenant-e2e"})
		require.NoError(t, err)
		var found bool
		for _, e := range queryResp.Events {
			if e.Action == "logout" {
				found = true
			}
		}
		assert.True(t, found, "the async-recorded event must still be queryable")
	})
}

// TestAuditService_GRPC_RecordAuditEvent_TenantScoping is the authz guard:
// a caller may record into its OWN authenticated tenant (or leave tenant_id
// empty, defaulting to it) but may NOT record into a DIFFERENT tenant —
// no caller identity lifts that, the system tenant included (#356).
func TestAuditService_GRPC_RecordAuditEvent_TenantScoping(t *testing.T) {
	newEvent := func(action string) *iampb.AuditEvent {
		return &iampb.AuditEvent{EventType: "authentication", Category: "authentication", Action: action, Outcome: "success"}
	}

	t.Run("caller's own tenant is honored", func(t *testing.T) {
		sink := qrdbsdk.NewAuditRingBuffer(1000)
		logger := &grpcTestNodeBackedAuditLogger{sink: sink}
		client, cleanup := newAuditServiceBufconn(t, logger, "tenant-a")
		defer cleanup()

		resp, err := client.RecordAuditEvent(context.Background(), &iampb.RecordAuditEventRequest{
			TenantId: "tenant-a",
			Event:    newEvent("own-tenant"),
		})
		require.NoError(t, err)
		assert.NotEmpty(t, resp.EventId)
	})

	t.Run("empty tenant_id defaults to caller's own tenant", func(t *testing.T) {
		sink := qrdbsdk.NewAuditRingBuffer(1000)
		logger := &grpcTestNodeBackedAuditLogger{sink: sink}
		client, cleanup := newAuditServiceBufconn(t, logger, "tenant-a")
		defer cleanup()

		resp, err := client.RecordAuditEvent(context.Background(), &iampb.RecordAuditEventRequest{
			Event: newEvent("default-tenant"),
		})
		require.NoError(t, err)
		assert.NotEmpty(t, resp.EventId)

		got, err := client.GetAuditEvent(context.Background(), &iampb.GetAuditEventRequest{
			TenantId: "tenant-a",
			EventId:  resp.EventId,
		})
		require.NoError(t, err)
		assert.Equal(t, "tenant-a", got.TenantId)
	})

	t.Run("cross-tenant tenant_id from a non-system caller is rejected", func(t *testing.T) {
		sink := qrdbsdk.NewAuditRingBuffer(1000)
		logger := &grpcTestNodeBackedAuditLogger{sink: sink}
		client, cleanup := newAuditServiceBufconn(t, logger, "tenant-a")
		defer cleanup()

		_, err := client.RecordAuditEvent(context.Background(), &iampb.RecordAuditEventRequest{
			TenantId: "tenant-b",
			Event:    newEvent("cross-tenant-attempt"),
		})
		require.Error(t, err)
		assert.Equal(t, codes.PermissionDenied, status.Code(err))

		// A second bufconn, bound to the SAME underlying sink but
		// authenticated as tenant-b itself, verifies the event never
		// landed — reading a tenant's chain requires being that tenant,
		// now with no system-tenant exception (#356).
		bClient, bCleanup := newAuditServiceBufconn(t, logger, "tenant-b")
		defer bCleanup()

		queryResp, qerr := bClient.QueryAuditEvents(context.Background(), &iampb.QueryAuditEventsRequest{TenantId: "tenant-b"})
		require.NoError(t, qerr)
		for _, e := range queryResp.Events {
			assert.NotEqual(t, "cross-tenant-attempt", e.Action, "a rejected cross-tenant record must never land on the target tenant's chain")
		}
	})

	t.Run("cross-tenant tenant_id from the system tenant is rejected (no operator lift, #356)", func(t *testing.T) {
		sink := qrdbsdk.NewAuditRingBuffer(1000)
		logger := &grpcTestNodeBackedAuditLogger{sink: sink}
		client, cleanup := newAuditServiceBufconn(t, logger, qrdbsdk.SystemTenantID)
		defer cleanup()

		_, err := client.RecordAuditEvent(context.Background(), &iampb.RecordAuditEventRequest{
			TenantId: "tenant-c",
			Event:    newEvent("system-cross-tenant"),
		})
		require.Error(t, err, "a system-tenant caller must not be able to record into another tenant's chain")
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
	})

	t.Run("system tenant may still record into its own chain", func(t *testing.T) {
		sink := qrdbsdk.NewAuditRingBuffer(1000)
		logger := &grpcTestNodeBackedAuditLogger{sink: sink}
		client, cleanup := newAuditServiceBufconn(t, logger, qrdbsdk.SystemTenantID)
		defer cleanup()

		resp, err := client.RecordAuditEvent(context.Background(), &iampb.RecordAuditEventRequest{
			TenantId: qrdbsdk.SystemTenantID,
			Event:    newEvent("system-own-chain"),
		})
		require.NoError(t, err)
		assert.NotEmpty(t, resp.EventId)
	})
}

// TestAuditService_GRPC_QueryGetAuditEvent_TenantScoping is the P5.2
// closing-gap guard: QueryAuditEvents and GetAuditEvent enforce the SAME
// tenant-scoping rule as RecordAuditEvent (iam.ResolveRequestTenant) — the
// caller reads its own authenticated tenant's chain and ONLY that chain; a
// cross-tenant request from ANY caller — the system tenant included
// (#356) — is rejected PermissionDenied BEFORE ever reaching storage, so a
// rejected query leaks no events.
func TestAuditService_GRPC_QueryGetAuditEvent_TenantScoping(t *testing.T) {
	// Seed tenant-a and tenant-b with distinguishable events on a shared
	// sink. Each tenant records into its own chain — with the #356 lift
	// removed there is no identity that may record into another tenant.
	sink := qrdbsdk.NewAuditRingBuffer(1000)
	logger := &grpcTestNodeBackedAuditLogger{sink: sink}
	sysClient, sysCleanup := newAuditServiceBufconn(t, logger, qrdbsdk.SystemTenantID)
	defer sysCleanup()

	seedEvent := func(tenantID, action string) string {
		t.Helper()
		client, cleanup := newAuditServiceBufconn(t, logger, tenantID)
		defer cleanup()
		resp, err := client.RecordAuditEvent(context.Background(), &iampb.RecordAuditEventRequest{
			TenantId: tenantID,
			Event:    &iampb.AuditEvent{EventType: "authentication", Category: "authentication", Action: action, Outcome: "success"},
		})
		require.NoError(t, err)
		return resp.EventId
	}
	tenantASecretID := seedEvent("tenant-a", "tenant-a-secret")
	tenantBSecretID := seedEvent("tenant-b", "tenant-b-secret")

	t.Run("caller reads its own tenant's chain", func(t *testing.T) {
		client, cleanup := newAuditServiceBufconn(t, logger, "tenant-a")
		defer cleanup()

		queryResp, err := client.QueryAuditEvents(context.Background(), &iampb.QueryAuditEventsRequest{TenantId: "tenant-a"})
		require.NoError(t, err)
		var found bool
		for _, e := range queryResp.Events {
			if e.Action == "tenant-a-secret" {
				found = true
			}
		}
		assert.True(t, found)

		got, err := client.GetAuditEvent(context.Background(), &iampb.GetAuditEventRequest{TenantId: "tenant-a", EventId: tenantASecretID})
		require.NoError(t, err)
		assert.Equal(t, "tenant-a-secret", got.Action)
	})

	t.Run("empty tenant_id defaults to caller's own tenant", func(t *testing.T) {
		client, cleanup := newAuditServiceBufconn(t, logger, "tenant-a")
		defer cleanup()

		queryResp, err := client.QueryAuditEvents(context.Background(), &iampb.QueryAuditEventsRequest{})
		require.NoError(t, err)
		for _, e := range queryResp.Events {
			assert.NotEqual(t, "tenant-b-secret", e.Action, "an empty tenant_id must never leak another tenant's events")
		}

		got, err := client.GetAuditEvent(context.Background(), &iampb.GetAuditEventRequest{EventId: tenantASecretID})
		require.NoError(t, err)
		assert.Equal(t, "tenant-a-secret", got.Action)
	})

	t.Run("cross-tenant query from a non-system caller is rejected and leaks no events", func(t *testing.T) {
		client, cleanup := newAuditServiceBufconn(t, logger, "tenant-a")
		defer cleanup()

		_, err := client.QueryAuditEvents(context.Background(), &iampb.QueryAuditEventsRequest{TenantId: "tenant-b"})
		require.Error(t, err, "tenant-a must not be able to query tenant-b's chain")
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
	})

	t.Run("cross-tenant get from a non-system caller is rejected", func(t *testing.T) {
		client, cleanup := newAuditServiceBufconn(t, logger, "tenant-a")
		defer cleanup()

		_, err := client.GetAuditEvent(context.Background(), &iampb.GetAuditEventRequest{TenantId: "tenant-b", EventId: tenantBSecretID})
		require.Error(t, err, "tenant-a must not be able to fetch a tenant-b event by ID")
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
	})

	t.Run("system-tenant caller is denied another tenant's chain (no operator lift, #356)", func(t *testing.T) {
		_, err := sysClient.QueryAuditEvents(context.Background(), &iampb.QueryAuditEventsRequest{TenantId: "tenant-b"})
		require.Error(t, err, "the system tenant must not be able to query another tenant's chain")
		assert.Equal(t, codes.PermissionDenied, status.Code(err))

		_, err = sysClient.GetAuditEvent(context.Background(), &iampb.GetAuditEventRequest{TenantId: "tenant-b", EventId: tenantBSecretID})
		require.Error(t, err, "the system tenant must not be able to fetch another tenant's event by ID")
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
	})
}
