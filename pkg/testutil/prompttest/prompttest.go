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

// Package prompttest builds a prompt service over an in-memory prompt store,
// for the service and transport tests of the prompt verbs.
package prompttest

import (
	"context"
	"errors"
	"sync"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

const (
	// Set is the fixture's prompt set.
	Set = "fixture"

	// Charter is a prompt that must keep the Marker literal.
	Charter = "charter"

	// Note is a prompt with an optional ${topic} variable.
	Note = "note"

	// Marker is the literal Charter must keep.
	Marker = "STOP:"

	// CharterDefault is Charter's embedded body.
	CharterDefault = "Work the issue.\nEnd with STOP: when done.\n"

	// Tenant is the tenant the fixture's projects belong to.
	Tenant = "tenant-a"

	// Project is a project of Tenant the fixture's lookup knows.
	Project = "project-1"

	// Author is the principal Context authenticates.
	Author = "alice"
)

// ErrBackendDown is the cause a failed resolver reports.
var ErrBackendDown = errors.New("prompttest: backend down")

// Fixture is a prompt service over an in-memory store.
type Fixture struct {
	Service  *service.PromptService
	Store    *prompts.Store
	Resolver *Resolver
}

// New builds a fixture whose project lookup knows Project of Tenant.
func New(t testing.TB) *Fixture {
	t.Helper()
	registry := prompts.NewRegistry()
	if _, err := registry.Register(Spec()); err != nil {
		t.Fatal(err)
	}
	resolver := &Resolver{stores: map[string]qrdbsdk.KVStore{}}
	store, err := prompts.NewStore(resolver, prompts.WithRegistry(registry))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.NewPromptService(service.PromptServiceConfig{
		Store:    store,
		Resolver: prompts.NewResolver(store),
		Projects: Projects{Tenant: {Project: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &Fixture{Service: svc, Store: store, Resolver: resolver}
}

// Spec is the fixture's set: Charter carries Marker, Note a variable.
func Spec() prompts.SetSpec {
	return prompts.SetSpec{
		Kind:        Set,
		Description: "a set for prompt service tests",
		Prompts: []prompts.PromptSpec{
			{
				Name:        Charter,
				Description: "the worker's charter",
				Default:     CharterDefault,
				Markers:     []prompts.Marker{{Literal: Marker, Reason: "the runner parses " + Marker}},
			},
			{
				Name:        Note,
				Description: "a note to the worker",
				Default:     "Mind ${topic}.",
				Variables:   []prompts.Variable{{Name: "topic", Description: "what to mind"}},
			},
		},
	}
}

// Context is a context authenticated as Author.
func Context() context.Context {
	return auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: Author, Authenticated: true})
}

// Save writes body for name at (tenantID, projectID), against expected.
func (f *Fixture) Save(t testing.TB, tenantID, projectID, name, body string, expected int64, followParent bool) *prompts.Record {
	t.Helper()
	ref := prompts.Ref{TenantID: tenantID, ProjectID: projectID, Set: Set, Name: name}
	record, err := f.Store.Save(context.Background(), ref, prompts.Change{
		Stamp:        prompts.Stamp{Author: "seed", Note: "fixture", ExpectedVersion: expected},
		Content:      body,
		FollowParent: followParent,
	})
	if err != nil {
		t.Fatal(err)
	}
	return record
}

// Resolver is an in-memory qrdbsdk.TenantStoreResolver that can be made to
// fail.
type Resolver struct {
	mu     sync.Mutex
	stores map[string]qrdbsdk.KVStore
	down   bool
}

// Resolve implements qrdbsdk.TenantStoreResolver.
func (r *Resolver) Resolve(_ context.Context, tenantID string) (qrdbsdk.KVStore, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.down {
		return nil, ErrBackendDown
	}
	store, ok := r.stores[tenantID]
	if !ok {
		store = qrdbsdk.NewMemoryKVStore()
		r.stores[tenantID] = store
	}
	return store, nil
}

// Fail makes every later Resolve fail with ErrBackendDown.
func (r *Resolver) Fail() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.down = true
}

// Projects is a project lookup over tenant -> project id -> exists.
type Projects map[string]map[string]bool

// GetProject implements service.PromptProjectLookup.
func (p Projects) GetProject(_ context.Context, req *service.GetProjectRequest) (*service.GetProjectResponse, error) {
	if !p[req.TenantID][req.ProjectID] {
		return nil, &service.ProjectNotFoundError{TenantID: req.TenantID, ProjectID: req.ProjectID}
	}
	return &service.GetProjectResponse{}, nil
}
