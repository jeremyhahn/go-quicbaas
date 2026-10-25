//go:build !integration

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
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// recordingPromptReconciler records the tenants reconciled, in order, and
// fails the ones named in failOn.
// newReconcileTestLogger returns a quiet logger for the boot-reconcile tests.
func newReconcileTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	return log
}

// fixedTenantLister answers ListTenants with a fixed list or error.
type fixedTenantLister struct {
	tenants []*iam.Tenant
	err     error
}

func (l *fixedTenantLister) ListTenants(context.Context, iam.ListOptions) ([]*iam.Tenant, error) {
	return l.tenants, l.err
}

type recordingPromptReconciler struct {
	seen   []string
	failOn map[string]error
}

func (r *recordingPromptReconciler) Reconcile(_ context.Context, tenantID string) (*prompts.SeedReport, error) {
	r.seen = append(r.seen, tenantID)
	if err := r.failOn[tenantID]; err != nil {
		return nil, err
	}
	return &prompts.SeedReport{TenantID: tenantID}, nil
}

// TestReconcileTenantPrompts_SystemTenantLeads proves the system set is
// reconciled before any tenant copies from it, and every listed tenant is
// visited exactly once.
func TestReconcileTenantPrompts_SystemTenantLeads(t *testing.T) {
	s := &Server{logger: newReconcileTestLogger(t)}
	seeder := &recordingPromptReconciler{}
	tenants := &fixedTenantLister{tenants: []*iam.Tenant{
		{ID: "acme"}, nil, {ID: ""}, {ID: qrdbsdk.SystemTenantID}, {ID: "globex"},
	}}

	require.NoError(t, s.reconcileTenantPrompts(context.Background(), seeder, tenants))
	require.Equal(t, []string{qrdbsdk.SystemTenantID, "acme", "globex"}, seeder.seen)
}

// TestReconcileTenantPrompts_AFailingTenantDoesNotStopTheOthers proves a
// sealed tenant is reported by id while the rest are still reconciled.
func TestReconcileTenantPrompts_AFailingTenantDoesNotStopTheOthers(t *testing.T) {
	s := &Server{logger: newReconcileTestLogger(t)}
	sealed := errors.New("barrier sealed")
	seeder := &recordingPromptReconciler{failOn: map[string]error{"acme": sealed}}
	tenants := &fixedTenantLister{tenants: []*iam.Tenant{{ID: "acme"}, {ID: "globex"}}}

	err := s.reconcileTenantPrompts(context.Background(), seeder, tenants)
	require.ErrorIs(t, err, ErrPromptReconcile)
	require.ErrorIs(t, err, sealed)
	var reconcileErr *PromptReconcileError
	require.ErrorAs(t, err, &reconcileErr)
	require.Equal(t, "acme", reconcileErr.TenantID)
	require.Equal(t, []string{qrdbsdk.SystemTenantID, "acme", "globex"}, seeder.seen)
}

// TestReconcileTenantPrompts_ListingFailure proves a tenant listing that
// fails reconciles nothing and is reported without a tenant id.
func TestReconcileTenantPrompts_ListingFailure(t *testing.T) {
	s := &Server{logger: newReconcileTestLogger(t)}
	down := errors.New("tenant registry unavailable")
	seeder := &recordingPromptReconciler{}

	err := s.reconcileTenantPrompts(context.Background(), seeder, &fixedTenantLister{err: down})
	require.ErrorIs(t, err, down)
	var reconcileErr *PromptReconcileError
	require.ErrorAs(t, err, &reconcileErr)
	require.Empty(t, reconcileErr.TenantID)
	require.Empty(t, seeder.seen)
}

// TestReconcileTenantPrompts_NoStoreIsANoOp proves a server without a prompt
// store — no embedded node — boots without reconciling anything.
func TestReconcileTenantPrompts_NoStoreIsANoOp(t *testing.T) {
	s := &Server{logger: newReconcileTestLogger(t)}
	require.NoError(t, s.ensurePromptStore())
	require.Nil(t, s.promptStore)
	require.Nil(t, s.promptReconciler())
	require.NoError(t, s.reconcileTenantPrompts(context.Background(), s.promptReconciler(), &fixedTenantLister{err: errors.New("never listed")}))
}

// TestReconcileTenantPrompts_SeedsTheSystemSetThenTheTenants runs the real
// seeder over an in-memory resolver: the system set is written from the
// build, and an existing tenant receives a following copy of every prompt.
func TestReconcileTenantPrompts_SeedsTheSystemSetThenTheTenants(t *testing.T) {
	ctx := context.Background()
	store, err := prompts.NewStore(&memoryTenantResolver{stores: map[string]qrdbsdk.KVStore{}})
	require.NoError(t, err)
	seeder, err := prompts.NewSeeder(store, "test-build")
	require.NoError(t, err)
	s := &Server{logger: newReconcileTestLogger(t), promptStore: store, promptSeeder: seeder}

	require.NoError(t, s.reconcileTenantPrompts(ctx, s.promptReconciler(), &fixedTenantLister{tenants: []*iam.Tenant{{ID: "acme"}}}))

	system, err := store.List(ctx, qrdbsdk.SystemTenantID, "", "")
	require.NoError(t, err)
	require.NotEmpty(t, system, "the system set was not written")
	for _, record := range system {
		require.True(t, record.FollowParent, "system record %s/%s does not follow the build", record.Set, record.Name)
		require.Equal(t, "build test-build", record.Revisions[0].Author)
	}
	tenant, err := store.List(ctx, "acme", "", "")
	require.NoError(t, err)
	require.Len(t, tenant, len(system), "the tenant was not seeded with every system prompt")
	for _, record := range tenant {
		require.True(t, record.FollowParent)
		require.Equal(t, prompts.SeedAuthor, record.Revisions[0].Author)
	}

	// A second boot writes nothing.
	for _, tenantID := range []string{qrdbsdk.SystemTenantID, "acme"} {
		report, err := seeder.Reconcile(ctx, tenantID)
		require.NoError(t, err)
		require.False(t, report.Changed(), "a second boot rewrote %s", tenantID)
	}
}

// recordingStoreAuditor captures the store events it is handed.
type recordingStoreAuditor struct {
	events []orchestration.StoreAuditEvent
}

func (a *recordingStoreAuditor) RecordStoreEvent(_ context.Context, event orchestration.StoreAuditEvent) {
	a.events = append(a.events, event)
}

// TestPromptAuditor_ForwardsToTheStoreTrail proves a prompt write lands on the
// orchestration audit trail unchanged, and that an unwired sink is harmless.
func TestPromptAuditor_ForwardsToTheStoreTrail(t *testing.T) {
	sink := &recordingStoreAuditor{}
	event := prompts.AuditEvent{TenantID: "acme", Resource: "prompt", ResourceID: "sdlc/charter", Action: "write", Success: true}

	promptAuditor{sink: sink}.RecordPromptEvent(context.Background(), event)
	require.Equal(t, []orchestration.StoreAuditEvent{{
		TenantID: "acme", Resource: "prompt", ResourceID: "sdlc/charter", Action: "write", Success: true,
	}}, sink.events)

	require.NotPanics(t, func() {
		promptAuditor{}.RecordPromptEvent(context.Background(), event)
	})
}
