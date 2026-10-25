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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
	"github.com/jeremyhahn/go-quicbaas/pkg/version"
)

// promptReconciler is the narrow view of the prompt seeder the boot reconcile
// needs, so the visiting order can be proven without a node.
type promptReconciler interface {
	Reconcile(ctx context.Context, tenantID string) (*prompts.SeedReport, error)
}

// ensurePromptStore builds the prompt store and its seeder over the embedded
// node's tenant store resolver, once. Without a node — or a node with no
// resolver — both stay nil and every job resolves to the embedded defaults:
// there is nowhere to keep an edit, so there is nothing to resolve but the
// build.
func (s *Server) ensurePromptStore() error {
	if s.promptStore != nil {
		return nil
	}
	node := s.getQRDBNode()
	if node == nil {
		return nil
	}
	svcs := node.Services()
	if svcs == nil || svcs.StoreResolver == nil {
		return nil
	}
	store, err := prompts.NewStore(svcs.StoreResolver,
		prompts.WithAuditor(promptAuditor{newOrchestrationStoreAuditor(s.auditLoggerForStores)}))
	if err != nil {
		return err
	}
	seeder, err := prompts.NewSeeder(store, version.GitCommit)
	if err != nil {
		return err
	}
	s.promptStore, s.promptSeeder = store, seeder
	return nil
}

// promptResolver is the resolver every consumer of tenant prompts shares —
// the job engine at launch and the SDLC issue source at each poll — so both
// read the same chain over the same store.
func (s *Server) promptResolver() (*prompts.Resolver, error) {
	if err := s.ensurePromptStore(); err != nil {
		return nil, &PromptReconcileError{TenantID: "", Cause: err}
	}
	return prompts.NewResolver(s.promptStore), nil
}

// promptReconciler returns the seeder as a reconciler, or a nil interface when
// there is none — never an interface holding a nil pointer.
func (s *Server) promptReconciler() promptReconciler {
	if s.promptSeeder == nil {
		return nil
	}
	return s.promptSeeder
}

// reconcileTenantPrompts converges every tenant's prompt set at boot: the
// system set onto THIS build's embedded defaults first, then every other
// tenant by seeding the prompts it lacks — a tenant created before the store
// existed, or before a build added a prompt. A record somebody edited is never
// touched. One log line is written per record written.
//
// The system tenant leads the list because a tenant's copy is taken from the
// system set: reconciled first, a new tenant prompt starts from this build's
// body rather than the last one's.
//
// A tenant that fails — its barrier sealed, typically — does not stop the
// others; the failures are returned joined, and the next boot retries them.
// tenantLister is the narrow view of the tenant manager the boot reconcile
// needs.
type tenantLister interface {
	ListTenants(ctx context.Context, opts iam.ListOptions) ([]*iam.Tenant, error)
}

// reconcileTargets enumerates the tenants to reconcile: the system tenant plus
// every tenant the broker knows about. The caller wraps a listing failure in
// its own error.
//
// The system tenant is named explicitly because the broker's tenant listing
// does not carry it — the same composition pkg/server already performs for the
// ABAC policy sweep (abac_sync.go allPolicies), and for the same reason:
// pkg/iam must not depend on qrdb's cross-tenant system-tenant concept, so the
// union is composed here. This is control-plane reconciliation of the control
// plane's own records, not a data-plane path taking the system tenant as a
// default.
func reconcileTargets(ctx context.Context, tenants tenantLister) ([]string, error) {
	ids := []string{qrdbsdk.SystemTenantID}
	if tenants == nil {
		return ids, nil
	}

	listed, err := tenants.ListTenants(ctx, iam.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, t := range listed {
		if t == nil || t.ID == "" || t.ID == qrdbsdk.SystemTenantID {
			continue
		}
		ids = append(ids, t.ID)
	}
	return ids, nil
}

func (s *Server) reconcileTenantPrompts(ctx context.Context, seeder promptReconciler, tenants tenantLister) error {
	if seeder == nil {
		return nil
	}
	tenantIDs, err := reconcileTargets(ctx, tenants)
	if err != nil {
		return &PromptReconcileError{TenantID: "", Cause: err}
	}
	var failures []error
	for _, tenantID := range tenantIDs {
		report, err := seeder.Reconcile(ctx, tenantID)
		if err != nil {
			failures = append(failures, &PromptReconcileError{TenantID: tenantID, Cause: err})
			continue
		}
		s.logPromptReconcile(report)
	}
	return errors.Join(failures...)
}

// logPromptReconcile writes one line per record a reconcile pass wrote.
func (s *Server) logPromptReconcile(report *prompts.SeedReport) {
	for _, ref := range report.Created {
		s.logger.Info("Prompt seeded", "tenant", report.TenantID, "set", ref.Set, "prompt", ref.Name)
	}
	for _, ref := range report.Updated {
		s.logger.Info("Prompt reconciled against this build's default", "tenant", report.TenantID, "set", ref.Set, "prompt", ref.Name)
	}
}

// promptAuditor records prompt-record writes on the same trail, and in the
// same shape, as every orchestration store's.
type promptAuditor struct {
	sink orchestration.StoreAuditor
}

// RecordPromptEvent implements prompts.Auditor.
func (a promptAuditor) RecordPromptEvent(ctx context.Context, event prompts.AuditEvent) {
	if a.sink == nil {
		return
	}
	a.sink.RecordStoreEvent(ctx, orchestration.StoreAuditEvent{
		TenantID:   event.TenantID,
		Resource:   event.Resource,
		ResourceID: event.ResourceID,
		Action:     event.Action,
		Success:    event.Success,
	})
}
