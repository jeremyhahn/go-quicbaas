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
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

var errAuditStore = errors.New("audit store unavailable")

// auditLoggerDouble records what the handlers pass to the iam.AuditLogger.
type auditLoggerDouble struct {
	mu       sync.Mutex
	events   map[string]*iam.AuditEvent
	logged   []*iam.AuditEvent
	async    []*iam.AuditEvent
	filter   *iam.AuditFilter
	deleted  []string
	deleteAt time.Time
	err      error
}

func newAuditLoggerDouble() *auditLoggerDouble {
	return &auditLoggerDouble{events: map[string]*iam.AuditEvent{}}
}

func (d *auditLoggerDouble) Log(_ context.Context, e *iam.AuditEvent) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return d.err
	}
	e.ID = "evt-new"
	d.logged = append(d.logged, e)
	return nil
}

func (d *auditLoggerDouble) LogAsync(e *iam.AuditEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.async = append(d.async, e)
}

func (d *auditLoggerDouble) Query(_ context.Context, f *iam.AuditFilter) ([]*iam.AuditEvent, int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.filter = f
	if d.err != nil {
		return nil, 0, d.err
	}
	var out []*iam.AuditEvent
	for _, e := range d.events {
		if e.TenantID == f.TenantID {
			out = append(out, e)
		}
	}
	return out, len(out), nil
}

func (d *auditLoggerDouble) GetEvent(_ context.Context, tenantID, eventID string) (*iam.AuditEvent, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return nil, d.err
	}
	e, ok := d.events[eventID]
	if !ok || e.TenantID != tenantID {
		return nil, iam.ErrAuditEventNotFound
	}
	return e, nil
}

func (d *auditLoggerDouble) DeleteOlderThan(_ context.Context, tenantID string, before time.Time) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return 0, d.err
	}
	d.deleted = append(d.deleted, tenantID)
	d.deleteAt = before
	return 3, nil
}

func (d *auditLoggerDouble) Close() error { return nil }

// mockQuicBaaSWithAudit is the service double: it supplies the audit logger
// and, for Delete, the admin manager.
type mockQuicBaaSWithAudit struct {
	service.QuicBaaS
	logger iam.AuditLogger
	admin  *iam.AdminManager
}

func (m *mockQuicBaaSWithAudit) GetAuditLogger(_ context.Context) (iam.AuditLogger, error) {
	return m.logger, nil
}

func (m *mockQuicBaaSWithAudit) GetAdminManager(_ context.Context) (*iam.AdminManager, error) {
	return m.admin, nil
}

const auditTenant = "acme"

var auditEventTime = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

// requireAuditEventSurvives checks the fields the SDK parser reads from the
// response map. The stored event's category (authentication) differs from
// its event type (authorization), so a category key would show here.
func requireAuditEventSurvives(t *testing.T, verb string, e *iamsdk.AuditEvent) {
	t.Helper()
	if e.Actor == nil || e.Actor.ID != "u-1" {
		t.Errorf("%s: actor = %+v, want id u-1", verb, e.Actor)
	}
	if e.Resource == nil || e.Resource.ID != "r-1" {
		t.Errorf("%s: resource = %+v, want id r-1", verb, e.Resource)
	}
	if !e.Timestamp.Equal(auditEventTime) {
		t.Errorf("%s: timestamp = %v, want %v", verb, e.Timestamp, auditEventTime)
	}
	if e.EventType != iamsdk.AuditEventTypeAuthorization {
		t.Errorf("%s: event type = %v, want authorization", verb, e.EventType)
	}
}

func TestIAMAuditEventFieldsSurviveQueryAndGet(t *testing.T) {
	_, svc := newAuditFixture(t)
	client := newAuditClient(t, svc)
	resp, err := client.Query(auditCtx(), &iamsdk.AuditFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(resp.Events) != 1 {
		t.Fatalf("Query returned %d events, want 1", len(resp.Events))
	}
	requireAuditEventSurvives(t, "Query", &resp.Events[0])

	event, err := client.Get(auditCtx(), auditTenant, "evt-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	requireAuditEventSurvives(t, "Get", event)
}

// TestIAMAuditQuery_NegativePaginationRefused drives the handler directly:
// the SDK client replaces a negative limit, but a transport caller need not.
func TestIAMAuditQuery_NegativePaginationRefused(t *testing.T) {
	type filterEnvelope struct{ Filter *iamsdk.AuditFilter }
	for name, filter := range map[string]*iamsdk.AuditFilter{
		"negative offset": {Offset: -1},
		"negative limit":  {Limit: -1},
	} {
		t.Run(name, func(t *testing.T) {
			logger, svc := newAuditFixture(t)
			_, err := handleIAMAuditQuery(inProcessTenant(auditCtx()), svc, &filterEnvelope{Filter: filter})
			var invalid *InvalidAuditRequestError
			if !errors.As(err, &invalid) {
				t.Fatalf("err = %v, want InvalidAuditRequestError", err)
			}
			if logger.filter != nil {
				t.Fatal("a negative pagination value reached the service")
			}
		})
	}
}

func TestIAMAuditDelete_UnsupportedScopeRefused(t *testing.T) {
	before := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		req  *iamsdk.AuditDeleteRequest
		want func(error) bool
	}{
		{"dry run", &iamsdk.AuditDeleteRequest{TenantID: auditTenant, Before: before, Confirm: true, DryRun: true},
			func(err error) bool {
				var refused *AuditDeleteDryRunUnsupportedError
				return errors.As(err, &refused)
			}},
		{"event types", &iamsdk.AuditDeleteRequest{TenantID: auditTenant, Before: before, Confirm: true,
			EventTypes: []iamsdk.AuditEventType{iamsdk.AuditEventTypeAuthorization}},
			func(err error) bool {
				var refused *AuditDeleteEventTypesUnsupportedError
				return errors.As(err, &refused)
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, svc := newAuditFixture(t)
			err := newAuditClient(t, svc).Delete(auditAdminCtx(t, svc.admin, true), tt.req)
			if !tt.want(err) {
				t.Fatalf("err = %v, want the %s refusal", err, tt.name)
			}
			if len(logger.deleted) != 0 {
				t.Fatal("a refused Delete reached the service")
			}
		})
	}
}

func newAuditClient(t *testing.T, svc service.QuicBaaS) iamsdk.AuditClient {
	t.Helper()
	tr, err := New(svc)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return iamsdk.NewAuditClient(tr)
}

func newAuditFixture(t *testing.T) (*auditLoggerDouble, *mockQuicBaaSWithAudit) {
	t.Helper()
	logger := newAuditLoggerDouble()
	logger.events["evt-1"] = &iam.AuditEvent{
		ID: "evt-1", TenantID: auditTenant, EventType: "authorization",
		Category: iam.AuditCategoryAuthentication, Action: "login", Outcome: iam.AuditOutcomeSuccess,
		Timestamp: auditEventTime,
		Actor:     &iam.AuditActor{UserID: "u-1"},
		Resource:  &iam.AuditResource{Type: "user", ID: "r-1"},
	}
	logger.events["evt-other"] = &iam.AuditEvent{ID: "evt-other", TenantID: "globex", Action: "login"}
	return logger, &mockQuicBaaSWithAudit{logger: logger, admin: newAdminTestManager(t)}
}

func auditCtx() context.Context {
	return transport.ContextWithTenant(context.Background(), auditTenant)
}

// auditAdminCtx binds an authenticated subject; grant gives it audit:delete
// in the audit tenant.
func auditAdminCtx(t *testing.T, mgr *iam.AdminManager, grant bool) context.Context {
	t.Helper()
	const userID = uint64(42)
	if grant {
		if _, err := mgr.CreateAssignment(context.Background(), iam.CreateAdminAssignmentOptions{
			UserID:      userID,
			AdminRole:   iam.AdminRoleAuditAdmin,
			TenantIDs:   []string{auditTenant},
			Permissions: []iam.AdminPermission{iam.PermAuditDelete},
			GrantedBy:   "test",
		}); err != nil {
			t.Fatalf("CreateAssignment: %v", err)
		}
	}
	return auth.SubjectIntoContext(auditCtx(), interfaces.Subject{
		ID: "42", UserID: userID, TenantID: auditTenant, Authenticated: true,
	})
}

func TestIAMAuditDispatchRegistered(t *testing.T) {
	for _, verb := range []string{"Query", "Get", "Record", "RecordAsync", "Delete"} {
		if _, ok := unaryHandlers["quicbaas.IAM.Audit/"+verb]; !ok {
			t.Errorf("quicbaas.IAM.Audit/%s is not registered", verb)
		}
	}
}

func TestIAMAuditQuery(t *testing.T) {
	logger, svc := newAuditFixture(t)
	resp, err := newAuditClient(t, svc).Query(auditCtx(), &iamsdk.AuditFilter{ActorID: "u-1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(resp.Events) != 1 || resp.Events[0].ID != "evt-1" || resp.TotalCount != 1 {
		t.Fatalf("Query returned %+v, want only evt-1", resp)
	}
	if logger.filter.TenantID != auditTenant || logger.filter.ActorID != "u-1" {
		t.Fatalf("service filter = %+v, want tenant %q actor u-1", logger.filter, auditTenant)
	}
}

func TestIAMAuditQuery_ZeroTimesBecomeNil(t *testing.T) {
	logger, svc := newAuditFixture(t)
	client := newAuditClient(t, svc)
	if _, err := client.Query(auditCtx(), &iamsdk.AuditFilter{}); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if logger.filter.StartTime != nil || logger.filter.EndTime != nil {
		t.Fatalf("zero filter times reached the service as %v / %v, want nil", logger.filter.StartTime, logger.filter.EndTime)
	}
	if len(logger.filter.EventTypes) != 0 {
		t.Fatalf("unset event type reached the service as %v", logger.filter.EventTypes)
	}

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := client.Query(auditCtx(), &iamsdk.AuditFilter{StartTime: start}); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if logger.filter.StartTime == nil || !logger.filter.StartTime.Equal(start) || logger.filter.EndTime != nil {
		t.Fatalf("filter times = %v / %v, want %v / nil", logger.filter.StartTime, logger.filter.EndTime, start)
	}
}

func TestIAMAuditQuery_UnsupportedFilterFieldRefused(t *testing.T) {
	logger, svc := newAuditFixture(t)
	_, err := newAuditClient(t, svc).Query(auditCtx(), &iamsdk.AuditFilter{SessionID: "s-1"})
	if err == nil {
		t.Fatal("Query with an unsupported filter field succeeded")
	}
	if logger.filter != nil {
		t.Fatal("an unsupported filter reached the service")
	}
}

func TestIAMAuditGet(t *testing.T) {
	_, svc := newAuditFixture(t)
	event, err := newAuditClient(t, svc).Get(auditCtx(), auditTenant, "evt-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if event.ID != "evt-1" || event.TenantID != auditTenant || event.Action != "login" {
		t.Fatalf("Get returned %+v", event)
	}
}

func TestIAMAuditRecord(t *testing.T) {
	logger, svc := newAuditFixture(t)
	id, err := newAuditClient(t, svc).Record(auditCtx(), &iamsdk.AuditEvent{
		EventType: iamsdk.AuditEventTypeAuthorization,
		Action:    "grant",
		Outcome:   iamsdk.AuditOutcomeDenied,
		Actor:     &iamsdk.AuditActor{ID: "u-1", Type: "user"},
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if id != "evt-new" || len(logger.logged) != 1 {
		t.Fatalf("Record returned %q with %d logged events", id, len(logger.logged))
	}
	got := logger.logged[0]
	if got.TenantID != auditTenant || got.EventType != "authorization" || got.Outcome != iam.AuditOutcomeDenied ||
		got.Actor == nil || got.Actor.UserID != "u-1" {
		t.Fatalf("logged event = %+v", got)
	}
}

func TestIAMAuditRecordAsync(t *testing.T) {
	logger, svc := newAuditFixture(t)
	if err := newAuditClient(t, svc).RecordAsync(auditCtx(), &iamsdk.AuditEvent{Action: "ping"}); err != nil {
		t.Fatalf("RecordAsync: %v", err)
	}
	if len(logger.async) != 1 || logger.async[0].TenantID != auditTenant || len(logger.logged) != 0 {
		t.Fatalf("async = %v, logged = %v", logger.async, logger.logged)
	}
}

func TestIAMAuditDelete(t *testing.T) {
	logger, svc := newAuditFixture(t)
	before := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	err := newAuditClient(t, svc).Delete(auditAdminCtx(t, svc.admin, true), &iamsdk.AuditDeleteRequest{
		TenantID: auditTenant, Before: before, Confirm: true,
	})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(logger.deleted) != 1 || logger.deleted[0] != auditTenant || !logger.deleteAt.Equal(before) {
		t.Fatalf("deleted %v before %v", logger.deleted, logger.deleteAt)
	}
}

func TestIAMAuditDelete_Refused(t *testing.T) {
	before := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		ctx  func(t *testing.T, mgr *iam.AdminManager) context.Context
	}{
		{"no subject bound", func(*testing.T, *iam.AdminManager) context.Context { return auditCtx() }},
		{"subject without audit:delete", func(t *testing.T, mgr *iam.AdminManager) context.Context {
			return auditAdminCtx(t, mgr, false)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, svc := newAuditFixture(t)
			err := newAuditClient(t, svc).Delete(tt.ctx(t, svc.admin), &iamsdk.AuditDeleteRequest{
				TenantID: auditTenant, Before: before, Confirm: true,
			})
			if err == nil {
				t.Fatal("Delete succeeded")
			}
			if len(logger.deleted) != 0 {
				t.Fatal("a refused Delete reached the service")
			}
		})
	}
}

// TestIAMAuditCrossTenantRefused proves every verb that names a tenant
// applies the REST and gRPC tenant rule before the service call.
func TestIAMAuditCrossTenantRefused(t *testing.T) {
	logger, svc := newAuditFixture(t)
	client := newAuditClient(t, svc)
	ctx := auditAdminCtx(t, svc.admin, true)
	calls := map[string]func() error{
		"Query": func() error {
			_, err := client.Query(ctx, &iamsdk.AuditFilter{TenantID: "globex"})
			return err
		},
		"Get": func() error {
			_, err := client.Get(ctx, "globex", "evt-other")
			return err
		},
		"Record": func() error {
			_, err := client.Record(ctx, &iamsdk.AuditEvent{TenantID: "globex", Action: "x"})
			return err
		},
		"RecordAsync": func() error {
			return client.RecordAsync(ctx, &iamsdk.AuditEvent{TenantID: "globex", Action: "x"})
		},
		"Delete": func() error {
			return client.Delete(ctx, &iamsdk.AuditDeleteRequest{TenantID: "globex", Before: time.Now(), Confirm: true})
		},
	}
	for verb, call := range calls {
		var denied *PermissionDeniedError
		if err := call(); !errors.As(err, &denied) {
			t.Errorf("%s across tenants: err = %v, want PermissionDeniedError", verb, err)
		}
	}
	if logger.filter != nil || len(logger.logged)+len(logger.async)+len(logger.deleted) != 0 {
		t.Fatal("a cross-tenant call reached the service")
	}
}

func TestIAMAuditNotConfigured(t *testing.T) {
	for name, svc := range map[string]service.QuicBaaS{
		"no audit source": &mockQuicBaaSWithAdmin{},
		"nil logger":      &mockQuicBaaSWithAudit{},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newAuditClient(t, svc).Get(auditCtx(), auditTenant, "evt-1")
			var notConfigured *ServiceNotConfiguredError
			if !errors.As(err, &notConfigured) {
				t.Fatalf("err = %v, want ServiceNotConfiguredError", err)
			}
		})
	}
}

func TestIAMAuditServiceError(t *testing.T) {
	logger, svc := newAuditFixture(t)
	logger.err = errAuditStore
	if _, err := newAuditClient(t, svc).Record(auditCtx(), &iamsdk.AuditEvent{Action: "x"}); err == nil {
		t.Fatal("Record succeeded with a failing service")
	}
}

// TestIAMAuditTypedNilRequest drives the handlers directly: the SDK client
// never sends a nil payload, but a caller of the transport can.
func TestIAMAuditTypedNilRequest(t *testing.T) {
	_, svc := newAuditFixture(t)
	type filterEnvelope struct{ Filter *iamsdk.AuditFilter }
	type eventEnvelope struct{ Event *iamsdk.AuditEvent }
	type deleteEnvelope struct{ Request *iamsdk.AuditDeleteRequest }
	cases := map[string]struct {
		handler handlerFunc
		req     interface{}
	}{
		"Query nil filter":      {handleIAMAuditQuery, &filterEnvelope{}},
		"Query typed nil":       {handleIAMAuditQuery, (*filterEnvelope)(nil)},
		"Get typed nil":         {handleIAMAuditGet, (*filterEnvelope)(nil)},
		"Record nil event":      {handleIAMAuditRecord, &eventEnvelope{}},
		"RecordAsync typed nil": {handleIAMAuditRecordAsync, (*eventEnvelope)(nil)},
		"Delete nil request":    {handleIAMAuditDelete, &deleteEnvelope{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := tc.handler(auditCtx(), svc, tc.req)
			var invalid *InvalidRequestTypeError
			if !errors.As(err, &invalid) {
				t.Fatalf("err = %v, want InvalidRequestTypeError", err)
			}
		})
	}
}
