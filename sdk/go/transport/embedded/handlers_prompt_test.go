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

package embedded

import (
	"context"
	goerrors "errors"
	"reflect"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	pt "github.com/jeremyhahn/go-quicbaas/pkg/testutil/prompttest"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/prompt"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// promptFacade mounts a prompt service, or none when it is nil; every other
// verb of the embedded nil interface is unreachable in these tests.
type promptFacade struct {
	service.QuicBaaS
	prompts *service.PromptService
}

func (f promptFacade) Prompts() *service.PromptService { return f.prompts }

func newPromptClient(t *testing.T, svc *service.PromptService) prompt.Client {
	t.Helper()
	tr, err := New(promptFacade{prompts: svc})
	if err != nil {
		t.Fatal(err)
	}
	return prompt.NewClient(tr)
}

func tenantCharter() prompt.Address {
	return prompt.Address{TenantID: pt.Tenant, Scope: "tenant", Set: pt.Set, Name: pt.Charter}
}

// TestPromptEmbedded_RoundTripsEveryVerb drives each verb through the SDK
// client against a real prompt service, so the converters are proven on the
// answers the service gives rather than on hand-built values.
func TestPromptEmbedded_RoundTripsEveryVerb(t *testing.T) {
	f := pt.New(t)
	client := newPromptClient(t, f.Service)
	ctx := pt.Context()
	addr := tenantCharter()

	first, err := client.Put(ctx, &prompt.PutRequest{Address: addr, Content: "First.\nSTOP:\n", Note: "one"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if first.Version != 1 || first.TenantID != pt.Tenant || first.Scope != "tenant" || first.ContentHash == "" {
		t.Fatalf("Put record = %+v", first)
	}
	if _, err := client.Put(ctx, &prompt.PutRequest{Address: addr, Content: "Second.\nSTOP:\n", Note: "two", ExpectedVersion: 1}); err != nil {
		t.Fatalf("second Put: %v", err)
	}

	got, err := client.Get(ctx, &prompt.GetRequest{Address: addr, Version: 1})
	if err != nil || got.Content != "First.\nSTOP:\n" || got.Version != 1 {
		t.Fatalf("Get v1 = %+v, %v", got, err)
	}

	list, err := client.List(ctx, &prompt.ListRequest{TenantID: pt.Tenant, Set: pt.Set})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var charter *prompt.ListEntry
	for i := range list.Prompts {
		if list.Prompts[i].Name == pt.Charter {
			charter = &list.Prompts[i]
		}
	}
	if list.Set != pt.Set || charter == nil || !charter.Overridden || charter.Version != 2 || charter.Effective.Version != 2 {
		t.Fatalf("List = %+v", list)
	}

	effective, err := client.Effective(ctx, &addr)
	if err != nil || effective.Content != "Second.\nSTOP:\n" || effective.Origin.Version != 2 || len(effective.Chain) == 0 {
		t.Fatalf("Effective = %+v, %v", effective, err)
	}

	diff, err := client.Diff(ctx, &addr)
	if err != nil || diff.Same || len(diff.Lines) == 0 {
		t.Fatalf("Diff = %+v, %v", diff, err)
	}

	history, err := client.History(ctx, &addr)
	if err != nil || len(history.Revisions) != 2 || history.Revisions[0].Author != pt.Author {
		t.Fatalf("History = %+v, %v", history, err)
	}

	rolled, err := client.Rollback(ctx, &prompt.RollbackRequest{Address: addr, ToVersion: 1, Note: "back", ExpectedVersion: 2})
	if err != nil || rolled.Version != 3 || rolled.Content != "First.\nSTOP:\n" {
		t.Fatalf("Rollback = %+v, %v", rolled, err)
	}

	reset, err := client.Reset(ctx, &prompt.ResetRequest{Address: addr, Note: "inherit", ExpectedVersion: 3})
	if err != nil || reset.Version != 4 || !reset.FollowParent {
		t.Fatalf("Reset = %+v, %v", reset, err)
	}
}

// TestPromptEmbedded_KeepsTheRefusalClass proves each class of prompt
// refusal reaches the caller with the code every wire transport answers.
func TestPromptEmbedded_KeepsTheRefusalClass(t *testing.T) {
	f := pt.New(t)
	client := newPromptClient(t, f.Service)
	ctx := pt.Context()
	addr := tenantCharter()

	if _, err := client.Put(ctx, &prompt.PutRequest{Address: addr, Content: "No marker.\n", Note: "n"}); !errors.IsInvalidInput(err) {
		t.Errorf("a body without the marker: %v", err)
	}
	if _, err := client.Get(ctx, &prompt.GetRequest{Address: addr}); !errors.IsNotFound(err) {
		t.Errorf("an absent override: %v", err)
	}
	f.Save(t, pt.Tenant, "", pt.Charter, "First.\nSTOP:\n", 0, false)
	if _, err := client.Put(ctx, &prompt.PutRequest{Address: addr, Content: "Again.\nSTOP:\n", Note: "n"}); !errors.IsConflict(err) {
		t.Errorf("a stale expected version: %v", err)
	}
	f.Resolver.Fail()
	if _, err := client.Effective(ctx, &addr); !errors.IsUnavailable(err) {
		t.Errorf("a failed backend: %v", err)
	}
}

// TestPromptEmbedded_RefusesWithoutTheServiceAndOnBadTypes proves a missing
// service is Unavailable and a wrong request type is refused before it.
func TestPromptEmbedded_RefusesWithoutTheServiceAndOnBadTypes(t *testing.T) {
	tr, err := New(promptFacade{})
	if err != nil {
		t.Fatal(err)
	}
	if !tr.SupportsPrompts() {
		t.Fatal("the embedded transport must declare the Prompts service")
	}
	ctx := context.Background()
	addr := tenantCharter()
	for method, req := range map[string]interface{}{
		"List":      &prompt.ListRequest{Set: pt.Set},
		"Get":       &prompt.GetRequest{Address: addr},
		"Effective": &addr,
		"Diff":      &addr,
		"History":   &addr,
		"Put":       &prompt.PutRequest{Address: addr},
		"Rollback":  &prompt.RollbackRequest{Address: addr},
		"Reset":     &prompt.ResetRequest{Address: addr},
	} {
		if _, err := tr.Request(ctx, promptsServiceName, method, req); !errors.IsUnavailable(err) {
			t.Errorf("%s without the service: %v", method, err)
		}
		if _, err := tr.Request(ctx, promptsServiceName, method, "wrong"); err == nil || errors.IsUnavailable(err) {
			t.Errorf("%s with the wrong request type: %v", method, err)
		}
	}
	if _, err := promptServiceOf(nil, "List"); !errors.IsUnavailable(err) {
		t.Errorf("a nil facade: %v", err)
	}
}

// TestPromptEmbedded_UnclassifiedErrorIsInternal proves an error the prompt
// service does not classify is a fault of the server, as on gRPC.
func TestPromptEmbedded_UnclassifiedErrorIsInternal(t *testing.T) {
	if err := promptError("Get", ErrTransportClosed); errors.Code(err) != errors.CodeInternal {
		t.Errorf("an unclassified error = %v, want Internal", err)
	}
	if err := promptError("Get", service.ErrPromptsUnavailable); !errors.IsUnavailable(err) {
		t.Errorf("a classified error = %v, want Unavailable", err)
	}
}

// TestPromptEmbeddedHandlers_CarryEveryVerb pins, in both directions, that
// the in-process transport routes exactly the verbs the SDK declares.
func TestPromptEmbeddedHandlers_CarryEveryVerb(t *testing.T) {
	iface := reflect.TypeOf((*prompt.Client)(nil)).Elem()
	declared := make(map[string]bool, iface.NumMethod())
	for i := 0; i < iface.NumMethod(); i++ {
		verb := iface.Method(i).Name
		declared[verb] = true
		if _, ok := unaryHandlers[promptsServiceName+"/"+verb]; !ok {
			t.Errorf("the embedded transport does not route %s/%s", promptsServiceName, verb)
		}
	}
	if len(declared) == 0 {
		t.Fatal("no prompt verbs were checked; the gate would pass vacuously")
	}
	const prefix = promptsServiceName + "/"
	for key := range unaryHandlers {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix && !declared[key[len(prefix):]] {
			t.Errorf("the embedded transport routes %q, which the SDK interface does not declare", key)
		}
	}
}

// TestPromptEmbedded_OmittedTenantIsTheBoundOne proves every verb fills an
// omitted tenant with the one the call is bound to, as the wire transports
// do, rather than forwarding it empty for the service to refuse.
func TestPromptEmbedded_OmittedTenantIsTheBoundOne(t *testing.T) {
	f := pt.New(t)
	client := newPromptClient(t, f.Service)
	ctx := transport.ContextWithTenant(pt.Context(), pt.Tenant)
	addr := tenantCharter()
	addr.TenantID = ""

	first, err := client.Put(ctx, &prompt.PutRequest{Address: addr, Content: "First.\nSTOP:\n", Note: "one"})
	if err != nil || first.TenantID != pt.Tenant {
		t.Fatalf("Put = %+v, %v", first, err)
	}
	if _, err := client.Put(ctx, &prompt.PutRequest{Address: addr, Content: "Second.\nSTOP:\n", Note: "two", ExpectedVersion: 1}); err != nil {
		t.Fatalf("second Put: %v", err)
	}
	if got, err := client.Get(ctx, &prompt.GetRequest{Address: addr}); err != nil || got.TenantID != pt.Tenant || got.Version != 2 {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if list, err := client.List(ctx, &prompt.ListRequest{Set: pt.Set}); err != nil || list.Scope != "tenant" {
		t.Fatalf("List = %+v, %v", list, err)
	}
	if effective, err := client.Effective(ctx, &addr); err != nil || effective.Origin.Version != 2 {
		t.Fatalf("Effective = %+v, %v", effective, err)
	}
	if _, err := client.Diff(ctx, &addr); err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if rolled, err := client.Rollback(ctx, &prompt.RollbackRequest{Address: addr, ToVersion: 1, Note: "back", ExpectedVersion: 2}); err != nil || rolled.TenantID != pt.Tenant {
		t.Fatalf("Rollback = %+v, %v", rolled, err)
	}
	if reset, err := client.Reset(ctx, &prompt.ResetRequest{Address: addr, Note: "inherit", ExpectedVersion: 3}); err != nil || reset.TenantID != pt.Tenant {
		t.Fatalf("Reset = %+v, %v", reset, err)
	}

	// The writes landed in the bound tenant: naming it explicitly reads them.
	named := tenantCharter()
	history, err := client.History(pt.Context(), &named)
	if err != nil || len(history.Revisions) != 4 {
		t.Fatalf("History of the named tenant = %+v, %v", history, err)
	}
	if omitted, err := client.History(ctx, &addr); err != nil || len(omitted.Revisions) != 4 {
		t.Fatalf("History of the bound tenant = %+v, %v", omitted, err)
	}
}

// TestPromptEmbedded_ExplicitTenantIsHonoured proves a named tenant is
// passed on unchanged: a call bound to no tenant (the system fallback) that
// names one writes in it, not in the system tenant.
func TestPromptEmbedded_ExplicitTenantIsHonoured(t *testing.T) {
	f := pt.New(t)
	client := newPromptClient(t, f.Service)
	addr := tenantCharter()

	view, err := client.Put(pt.Context(), &prompt.PutRequest{Address: addr, Content: "Named.\nSTOP:\n", Note: "named"})
	if err != nil || view.TenantID != pt.Tenant {
		t.Fatalf("Put = %+v, %v", view, err)
	}
	bound := transport.ContextWithTenant(pt.Context(), pt.Tenant)
	if got, err := client.Get(bound, &prompt.GetRequest{Address: prompt.Address{Scope: "tenant", Set: pt.Set, Name: pt.Charter}}); err != nil || got.Content != "Named.\nSTOP:\n" {
		t.Fatalf("Get in the bound tenant = %+v, %v", got, err)
	}
}

// TestPromptTenant_RefusesAContextWithNoTenant proves the resolver refuses an
// omitted tenant when nothing is bound, rather than guessing one, and that a
// named tenant needs no binding.
func TestPromptTenant_RefusesAContextWithNoTenant(t *testing.T) {
	tenantID, err := promptTenant(context.Background(), "Get", "")
	var missing *tenantctx.MissingTenantError
	if !goerrors.As(err, &missing) || tenantID != "" {
		t.Fatalf("promptTenant = %q, %v; want a MissingTenantError", tenantID, err)
	}
	if tenantID, err := promptTenant(context.Background(), "Get", pt.Tenant); err != nil || tenantID != pt.Tenant {
		t.Fatalf("promptTenant(named) = %q, %v", tenantID, err)
	}
}
