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

package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	pt "github.com/jeremyhahn/go-quicbaas/pkg/testutil/prompttest"
)

func tenantCharter() service.PromptAddress {
	return service.PromptAddress{TenantID: pt.Tenant, Scope: "tenant", Set: pt.Set, Name: pt.Charter}
}

func projectCharter() service.PromptAddress {
	return service.PromptAddress{TenantID: pt.Tenant, Scope: "project", ProjectID: pt.Project, Set: pt.Set, Name: pt.Charter}
}

func systemCharter() service.PromptAddress {
	return service.PromptAddress{Scope: "system", Set: pt.Set, Name: pt.Charter}
}

// requireFault fails unless err classifies as want.
func requireFault(t *testing.T, err error, want service.PromptFault) {
	t.Helper()
	if err == nil {
		t.Fatalf("got no error, want fault %d", want)
	}
	got, ok := service.ClassifyPromptError(err)
	if !ok || got != want {
		t.Fatalf("fault of %v = %d (%v), want %d", err, got, ok, want)
	}
}

func TestNewPromptService(t *testing.T) {
	f := pt.New(t)
	if _, err := service.NewPromptService(service.PromptServiceConfig{Store: f.Store, Resolver: prompts.NewResolver(f.Store)}); err != nil {
		t.Fatalf("without a project lookup: %v", err)
	}
	cases := map[string]service.PromptServiceConfig{
		"Store":    {Resolver: prompts.NewResolver(f.Store)},
		"Resolver": {Store: f.Store},
	}
	for field, cfg := range cases {
		_, err := service.NewPromptService(cfg)
		var configErr *service.PromptServiceConfigError
		if !errors.As(err, &configErr) || configErr.Field != field || !errors.Is(err, service.ErrPromptServiceConfig) {
			t.Errorf("missing %s: got %v", field, err)
		}
	}
}

func TestPromptService_List(t *testing.T) {
	f := pt.New(t)
	f.Save(t, pt.Tenant, "", pt.Charter, "Tenant charter.\nSTOP:\n", 0, false)
	f.Save(t, pt.Tenant, "", pt.Note, "Mind ${topic}.", 0, true)

	got, err := f.Service.List(pt.Context(), &service.ListPromptsRequest{TenantID: pt.Tenant, Set: pt.Set})
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != prompts.ScopeTenant || len(got.Prompts) != 2 {
		t.Fatalf("got %+v", got)
	}
	byName := map[string]service.PromptListEntry{}
	for _, entry := range got.Prompts {
		byName[entry.Name] = entry
	}
	charter := byName[pt.Charter]
	if !charter.Overridden || charter.Version != 1 || charter.Effective.Scope != prompts.ScopeTenant {
		t.Errorf("charter = %+v", charter)
	}
	note := byName[pt.Note]
	if note.Overridden || !note.FollowParent || note.Effective.Scope != prompts.ScopeEmbedded {
		t.Errorf("note = %+v", note)
	}
}

func TestPromptService_ListErrors(t *testing.T) {
	f := pt.New(t)
	ctx := pt.Context()
	if _, err := f.Service.List(ctx, nil); !errors.Is(err, service.ErrPromptRequest) {
		t.Errorf("nil request: %v", err)
	}
	_, err := f.Service.List(ctx, &service.ListPromptsRequest{TenantID: pt.Tenant, Set: "nope"})
	requireFault(t, err, service.PromptFaultInvalid)
	_, err = f.Service.List(ctx, &service.ListPromptsRequest{Set: pt.Set})
	requireFault(t, err, service.PromptFaultInvalid)

	f.Resolver.Fail()
	_, err = f.Service.List(ctx, &service.ListPromptsRequest{TenantID: pt.Tenant, Set: pt.Set})
	requireFault(t, err, service.PromptFaultUnavailable)
}

func TestPromptService_Get(t *testing.T) {
	f := pt.New(t)
	f.Save(t, pt.Tenant, "", pt.Charter, "First.\nSTOP:\n", 0, false)
	f.Save(t, pt.Tenant, "", pt.Charter, "Second.\nSTOP:\n", 1, false)
	ctx := pt.Context()

	current, err := f.Service.Get(ctx, &service.GetPromptRequest{PromptAddress: tenantCharter()})
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != 2 || current.Content != "Second.\nSTOP:\n" || current.Scope != prompts.ScopeTenant {
		t.Errorf("current = %+v", current)
	}
	first, err := f.Service.Get(ctx, &service.GetPromptRequest{PromptAddress: tenantCharter(), Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != 1 || first.Content != "First.\nSTOP:\n" || first.ContentHash != prompts.Hash("First.\nSTOP:\n") {
		t.Errorf("version 1 = %+v", first)
	}
}

func TestPromptService_GetErrors(t *testing.T) {
	f := pt.New(t)
	f.Save(t, pt.Tenant, "", pt.Charter, "First.\nSTOP:\n", 0, false)
	f.Save(t, pt.Tenant, "", pt.Note, "Mind ${topic}.", 0, true)
	ctx := pt.Context()

	if _, err := f.Service.Get(ctx, nil); !errors.Is(err, service.ErrPromptRequest) {
		t.Errorf("nil request: %v", err)
	}
	_, err := f.Service.Get(ctx, &service.GetPromptRequest{PromptAddress: tenantCharter(), Version: -1})
	requireFault(t, err, service.PromptFaultInvalid)

	_, err = f.Service.Get(ctx, &service.GetPromptRequest{PromptAddress: tenantCharter(), Version: 9})
	requireFault(t, err, service.PromptFaultNotFound)
	if !errors.Is(err, prompts.ErrUnknownRevision) {
		t.Errorf("unknown revision: %v", err)
	}

	notes := tenantCharter()
	notes.Name = pt.Note
	_, err = f.Service.Get(ctx, &service.GetPromptRequest{PromptAddress: notes})
	requireFault(t, err, service.PromptFaultNotFound)
	if !errors.Is(err, service.ErrPromptNotOverridden) {
		t.Errorf("follow-parent record: %v", err)
	}

	_, err = f.Service.Get(ctx, &service.GetPromptRequest{PromptAddress: systemCharter()})
	requireFault(t, err, service.PromptFaultNotFound)

	unknown := tenantCharter()
	unknown.Name = "nope"
	_, err = f.Service.Get(ctx, &service.GetPromptRequest{PromptAddress: unknown})
	requireFault(t, err, service.PromptFaultInvalid)
}

func TestPromptService_Effective(t *testing.T) {
	f := pt.New(t)
	ctx := pt.Context()

	embedded, err := f.Service.Effective(ctx, ptrAddress(projectCharter()))
	if err != nil {
		t.Fatal(err)
	}
	if embedded.Content != pt.CharterDefault || embedded.Origin.Scope != prompts.ScopeEmbedded {
		t.Fatalf("embedded = %+v", embedded)
	}
	if scopes := layerScopes(embedded.Chain); scopes != "project,tenant,system,embedded" {
		t.Errorf("chain = %s", scopes)
	}

	f.Save(t, qrdbsdk.SystemTenantID, "", pt.Charter, "System.\nSTOP:\n", 0, false)
	f.Save(t, pt.Tenant, "", pt.Charter, "Tenant.\nSTOP:\n", 0, false)
	f.Save(t, pt.Tenant, pt.Project, pt.Charter, pt.CharterDefault, 0, true)

	got, err := f.Service.Effective(ctx, ptrAddress(projectCharter()))
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "Tenant.\nSTOP:\n" || got.Origin.Scope != prompts.ScopeTenant || got.Scope != prompts.ScopeProject {
		t.Fatalf("effective = %+v", got)
	}
	project, tenant, system := got.Chain[0], got.Chain[1], got.Chain[2]
	if !project.Present || !project.FollowParent || project.Answered || project.ScopeID != pt.Project {
		t.Errorf("project layer = %+v", project)
	}
	if !tenant.Present || !tenant.Answered || tenant.Version != 1 || tenant.ScopeID != pt.Tenant {
		t.Errorf("tenant layer = %+v", tenant)
	}
	if !system.Present || system.Answered {
		t.Errorf("system layer = %+v", system)
	}

	atSystem, err := f.Service.Effective(ctx, ptrAddress(systemCharter()))
	if err != nil {
		t.Fatal(err)
	}
	if scopes := layerScopes(atSystem.Chain); scopes != "system,embedded" || atSystem.Origin.Scope != prompts.ScopeSystem {
		t.Errorf("system chain = %s, origin %+v", scopes, atSystem.Origin)
	}
}

func TestPromptService_EffectiveErrors(t *testing.T) {
	f := pt.New(t)
	ctx := pt.Context()
	if _, err := f.Service.Effective(ctx, nil); !errors.Is(err, service.ErrPromptRequest) {
		t.Errorf("nil request: %v", err)
	}
	missing := projectCharter()
	missing.ProjectID = "ghost"
	_, err := f.Service.Effective(ctx, &missing)
	requireFault(t, err, service.PromptFaultNotFound)

	f.Resolver.Fail()
	_, err = f.Service.Effective(ctx, ptrAddress(tenantCharter()))
	requireFault(t, err, service.PromptFaultUnavailable)
}

func TestPromptService_Diff(t *testing.T) {
	f := pt.New(t)
	ctx := pt.Context()

	same, err := f.Service.Diff(ctx, ptrAddress(systemCharter()))
	if err != nil {
		t.Fatal(err)
	}
	if !same.Same || same.ParentOrigin.Scope != prompts.ScopeEmbedded || len(same.Lines) != 2 {
		t.Fatalf("unedited diff = %+v", same)
	}

	f.Save(t, pt.Tenant, "", pt.Charter, "Work the issue.\nEnd with STOP: always.\n", 0, false)
	got, err := f.Service.Diff(ctx, ptrAddress(tenantCharter()))
	if err != nil {
		t.Fatal(err)
	}
	want := []service.PromptDiffLine{
		{Op: service.PromptDiffEqual, Text: "Work the issue."},
		{Op: service.PromptDiffDelete, Text: "End with STOP: when done."},
		{Op: service.PromptDiffInsert, Text: "End with STOP: always."},
	}
	if got.Same || got.Origin.Scope != prompts.ScopeTenant || got.ParentOrigin.Scope != prompts.ScopeEmbedded || !equalLines(got.Lines, want) {
		t.Errorf("tenant diff = %+v", got)
	}

	project, err := f.Service.Diff(ctx, ptrAddress(projectCharter()))
	if err != nil {
		t.Fatal(err)
	}
	if !project.Same || project.ParentOrigin.Scope != prompts.ScopeTenant {
		t.Errorf("project diff = %+v", project)
	}
}

func TestPromptService_DiffErrors(t *testing.T) {
	f := pt.New(t)
	ctx := pt.Context()
	if _, err := f.Service.Diff(ctx, nil); !errors.Is(err, service.ErrPromptRequest) {
		t.Errorf("nil request: %v", err)
	}
	unknown := tenantCharter()
	unknown.Set = "nope"
	_, err := f.Service.Diff(ctx, &unknown)
	requireFault(t, err, service.PromptFaultInvalid)

	f.Resolver.Fail()
	_, err = f.Service.Diff(ctx, ptrAddress(tenantCharter()))
	requireFault(t, err, service.PromptFaultUnavailable)
}

func TestPromptService_History(t *testing.T) {
	f := pt.New(t)
	f.Save(t, pt.Tenant, "", pt.Charter, "First.\nSTOP:\n", 0, false)
	f.Save(t, pt.Tenant, "", pt.Charter, "Second.\nSTOP:\n", 1, false)

	got, err := f.Service.History(pt.Context(), ptrAddress(tenantCharter()))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Revisions) != 2 || got.Scope != prompts.ScopeTenant {
		t.Fatalf("history = %+v", got)
	}
	for _, revision := range got.Revisions {
		if revision.Author != "seed" || revision.Note != "fixture" || revision.ContentHash == "" {
			t.Errorf("revision = %+v", revision)
		}
	}
}

func TestPromptService_HistoryErrors(t *testing.T) {
	f := pt.New(t)
	ctx := pt.Context()
	if _, err := f.Service.History(ctx, nil); !errors.Is(err, service.ErrPromptRequest) {
		t.Errorf("nil request: %v", err)
	}
	_, err := f.Service.History(ctx, ptrAddress(tenantCharter()))
	requireFault(t, err, service.PromptFaultNotFound)
}

func TestPromptService_Put(t *testing.T) {
	f := pt.New(t)
	ctx := pt.Context()
	got, err := f.Service.Put(ctx, &service.PutPromptRequest{
		PromptAddress: projectCharter(), Content: "Project.\nSTOP:\n", Note: "tighten",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || got.Scope != prompts.ScopeProject || got.ProjectID != pt.Project || got.FollowParent {
		t.Fatalf("put = %+v", got)
	}
	history, err := f.Store.History(context.Background(), prompts.Ref{TenantID: pt.Tenant, ProjectID: pt.Project, Set: pt.Set, Name: pt.Charter})
	if err != nil {
		t.Fatal(err)
	}
	if history[0].Author != pt.Author || history[0].Note != "tighten" {
		t.Errorf("stamp = %+v", history[0])
	}
}

func TestPromptService_PutErrors(t *testing.T) {
	f := pt.New(t)
	ctx := pt.Context()
	put := func(addr service.PromptAddress, content, note string, expected int64) error {
		_, err := f.Service.Put(ctx, &service.PutPromptRequest{PromptAddress: addr, Content: content, Note: note, ExpectedVersion: expected})
		return err
	}

	if _, err := f.Service.Put(ctx, nil); !errors.Is(err, service.ErrPromptRequest) {
		t.Errorf("nil request: %v", err)
	}
	requireFault(t, put(tenantCharter(), "Body.\nSTOP:\n", "  ", 0), service.PromptFaultInvalid)

	err := put(systemCharter(), "Body.\nSTOP:\n", "why", 0)
	requireFault(t, err, service.PromptFaultInvalid)
	var requestErr *service.PromptRequestError
	if !errors.As(err, &requestErr) || requestErr.Field != "scope" {
		t.Errorf("system write: %v", err)
	}

	err = put(tenantCharter(), "No marker here.", "why", 0)
	requireFault(t, err, service.PromptFaultInvalid)
	if !errors.Is(err, prompts.ErrMissingMarker) || !strings.Contains(err.Error(), pt.Marker) {
		t.Errorf("missing marker: %v", err)
	}

	if err := put(tenantCharter(), "Body.\nSTOP:\n", "why", 0); err != nil {
		t.Fatal(err)
	}
	requireFault(t, put(tenantCharter(), "Again.\nSTOP:\n", "why", 0), service.PromptFaultConflict)

	unauthenticated, err := f.Service.Put(context.Background(), &service.PutPromptRequest{
		PromptAddress: tenantCharter(), Content: "Body.\nSTOP:\n", Note: "why", ExpectedVersion: 1,
	})
	if unauthenticated != nil {
		t.Fatalf("anonymous put = %+v", unauthenticated)
	}
	requireFault(t, err, service.PromptFaultInvalid)
}

func TestPromptService_Rollback(t *testing.T) {
	f := pt.New(t)
	f.Save(t, pt.Tenant, "", pt.Charter, "First.\nSTOP:\n", 0, false)
	f.Save(t, pt.Tenant, "", pt.Charter, "Second.\nSTOP:\n", 1, false)

	got, err := f.Service.Rollback(pt.Context(), &service.RollbackPromptRequest{
		PromptAddress: tenantCharter(), ToVersion: 1, Note: "revert", ExpectedVersion: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 3 || got.Content != "First.\nSTOP:\n" {
		t.Errorf("rollback = %+v", got)
	}
}

func TestPromptService_RollbackErrors(t *testing.T) {
	f := pt.New(t)
	f.Save(t, pt.Tenant, "", pt.Charter, "First.\nSTOP:\n", 0, false)
	ctx := pt.Context()
	rollback := func(addr service.PromptAddress, to, expected int64) error {
		_, err := f.Service.Rollback(ctx, &service.RollbackPromptRequest{PromptAddress: addr, ToVersion: to, ExpectedVersion: expected})
		return err
	}

	if _, err := f.Service.Rollback(ctx, nil); !errors.Is(err, service.ErrPromptRequest) {
		t.Errorf("nil request: %v", err)
	}
	requireFault(t, rollback(tenantCharter(), 0, 1), service.PromptFaultInvalid)
	requireFault(t, rollback(tenantCharter(), 1, 0), service.PromptFaultInvalid)
	requireFault(t, rollback(systemCharter(), 1, 1), service.PromptFaultInvalid)
	requireFault(t, rollback(tenantCharter(), 7, 1), service.PromptFaultNotFound)
	requireFault(t, rollback(tenantCharter(), 1, 5), service.PromptFaultConflict)
}

func TestPromptService_Reset(t *testing.T) {
	f := pt.New(t)
	f.Save(t, pt.Tenant, "", pt.Charter, "First.\nSTOP:\n", 0, false)

	got, err := f.Service.Reset(pt.Context(), &service.ResetPromptRequest{PromptAddress: tenantCharter(), Note: "back", ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 || !got.FollowParent {
		t.Errorf("reset = %+v", got)
	}
}

func TestPromptService_ResetErrors(t *testing.T) {
	f := pt.New(t)
	ctx := pt.Context()
	reset := func(addr service.PromptAddress, expected int64) error {
		_, err := f.Service.Reset(ctx, &service.ResetPromptRequest{PromptAddress: addr, ExpectedVersion: expected})
		return err
	}

	if _, err := f.Service.Reset(ctx, nil); !errors.Is(err, service.ErrPromptRequest) {
		t.Errorf("nil request: %v", err)
	}
	requireFault(t, reset(tenantCharter(), 0), service.PromptFaultInvalid)
	requireFault(t, reset(systemCharter(), 1), service.PromptFaultInvalid)
	requireFault(t, reset(tenantCharter(), 1), service.PromptFaultNotFound)
}

func TestPromptService_ScopeAddressing(t *testing.T) {
	f := pt.New(t)
	ctx := pt.Context()
	cases := map[string]struct {
		addr  service.PromptAddress
		field string
	}{
		"unknown scope":         {service.PromptAddress{TenantID: pt.Tenant, Scope: "galaxy"}, "scope"},
		"system with project":   {service.PromptAddress{Scope: "system", ProjectID: pt.Project}, "project_id"},
		"no tenant":             {service.PromptAddress{Scope: "tenant"}, "tenant_id"},
		"tenant with project":   {service.PromptAddress{TenantID: pt.Tenant, Scope: "tenant", ProjectID: pt.Project}, "project_id"},
		"project without an id": {service.PromptAddress{TenantID: pt.Tenant, Scope: "project"}, "project_id"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			tc.addr.Set, tc.addr.Name = pt.Set, pt.Charter
			_, err := f.Service.Effective(ctx, &tc.addr)
			var requestErr *service.PromptRequestError
			if !errors.As(err, &requestErr) || requestErr.Field != tc.field {
				t.Errorf("got %v, want a refusal of %s", err, tc.field)
			}
		})
	}

	// An address with a project id and no scope is a project address.
	inferred := service.PromptAddress{TenantID: pt.Tenant, ProjectID: pt.Project, Set: pt.Set, Name: pt.Charter}
	got, err := f.Service.Effective(ctx, &inferred)
	if err != nil || got.Scope != prompts.ScopeProject {
		t.Errorf("inferred project scope: %+v, %v", got, err)
	}
}

func TestClassifyPromptError(t *testing.T) {
	cases := map[string]struct {
		err  error
		want service.PromptFault
	}{
		"not found":        {&prompts.NotFoundError{}, service.PromptFaultNotFound},
		"not overridden":   {&service.PromptNotOverriddenError{}, service.PromptFaultNotFound},
		"conflict":         {prompts.ErrVersionConflict, service.PromptFaultConflict},
		"request":          {&service.PromptRequestError{Field: "note"}, service.PromptFaultInvalid},
		"missing marker":   {&prompts.MissingMarkerError{Marker: pt.Marker}, service.PromptFaultInvalid},
		"unmounted":        {service.ErrPromptsUnavailable, service.PromptFaultUnavailable},
		"backend":          {prompts.ErrBackend, service.PromptFaultUnavailable},
		"no project":       {&service.ProjectNotFoundError{}, service.PromptFaultNotFound},
		"bad project":      {&service.ProjectInvalidError{}, service.PromptFaultInvalid},
		"resolve wrapping": {&prompts.ResolveError{Cause: prompts.ErrBackend}, service.PromptFaultUnavailable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := service.ClassifyPromptError(tc.err)
			if !ok || got != tc.want {
				t.Errorf("got %d (%v), want %d", got, ok, tc.want)
			}
		})
	}
}

func TestClassifyPromptError_ServerFault(t *testing.T) {
	for _, err := range []error{nil, errors.New("disk on fire")} {
		if fault, ok := service.ClassifyPromptError(err); ok {
			t.Errorf("%v classified as %d", err, fault)
		}
	}
}

func TestPromptErrorMessages(t *testing.T) {
	ref := prompts.Ref{TenantID: pt.Tenant, Set: pt.Set, Name: pt.Charter}
	cases := map[string]struct {
		err  error
		want string
	}{
		"request":        {&service.PromptRequestError{Field: "note", Message: "is required"}, "note: is required"},
		"not overridden": {&service.PromptNotOverriddenError{Ref: ref}, ref.String()},
		"config":         {&service.PromptServiceConfigError{Field: "Store"}, "Store is required"},
	}
	for name, tc := range cases {
		if got := tc.err.Error(); !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q does not name %q", name, got, tc.want)
		}
	}
}

func ptrAddress(addr service.PromptAddress) *service.PromptAddress { return &addr }

func layerScopes(chain []service.PromptLayer) string {
	scopes := make([]string, 0, len(chain))
	for _, layer := range chain {
		scopes = append(scopes, string(layer.Scope))
	}
	return strings.Join(scopes, ",")
}

func equalLines(got, want []service.PromptDiffLine) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
