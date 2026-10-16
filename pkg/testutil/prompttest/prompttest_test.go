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

package prompttest

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestResolver_KeepsOneStorePerTenantUntilItFails(t *testing.T) {
	f := New(t)
	ctx := context.Background()
	first, err := f.Resolver.Resolve(ctx, Tenant)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.Resolver.Resolve(ctx, Tenant)
	if err != nil || again != first {
		t.Fatalf("a tenant's second resolve returned another store: %v", err)
	}

	f.Resolver.Fail()
	if _, err := f.Resolver.Resolve(ctx, Tenant); !errors.Is(err, ErrBackendDown) {
		t.Fatalf("failed resolver: %v", err)
	}
}

func TestFixture_SaveWritesThroughTheStore(t *testing.T) {
	f := New(t)
	record := f.Save(t, Tenant, Project, Charter, CharterDefault, 0, false)
	if record.Version != 1 || record.ProjectID != Project {
		t.Fatalf("record = %+v", record)
	}
	got, err := f.Service.Get(Context(), &service.GetPromptRequest{PromptAddress: service.PromptAddress{
		TenantID: Tenant, ProjectID: Project, Set: Set, Name: Charter,
	}})
	if err != nil || got.Content != CharterDefault {
		t.Fatalf("read back %+v, %v", got, err)
	}
}

func TestProjects_GetProject(t *testing.T) {
	projects := Projects{Tenant: {Project: true}}
	if _, err := projects.GetProject(context.Background(), &service.GetProjectRequest{TenantID: Tenant, ProjectID: Project}); err != nil {
		t.Fatalf("known project: %v", err)
	}
	_, err := projects.GetProject(context.Background(), &service.GetProjectRequest{TenantID: Tenant, ProjectID: "ghost"})
	var notFound *service.ProjectNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("unknown project: %v", err)
	}
}

func TestContext_AuthenticatesTheAuthor(t *testing.T) {
	if got := auth.PrincipalFromContext(Context()); got != Author {
		t.Fatalf("principal = %q, want %q", got, Author)
	}
	if got := auth.PrincipalFromContext(context.Background()); got == Author {
		t.Fatal("a bare context must not carry the author")
	}
}
