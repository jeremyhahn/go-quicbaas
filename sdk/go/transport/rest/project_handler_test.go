// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
)

// TestProjectHandlerHappyPaths drives every Project method through the REST
// handler, asserting the HTTP method and URL path built by each handler.
func TestProjectHandlerHappyPaths(t *testing.T) {
	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	cases := []struct {
		name       string
		method     string
		req        interface{}
		wantMethod string
		wantPath   string
	}{
		{"Create", "Create",
			&project.CreateProjectRequest{TenantID: "t1", Name: "proj"},
			http.MethodPost, "/api/v1/projects"},
		{"Get", "Get",
			&project.GetProjectRequest{TenantID: "t1", ProjectID: "p1"},
			http.MethodGet, "/api/v1/projects/p1"},
		{"GetByName", "GetByName",
			&project.GetProjectByNameRequest{TenantID: "t1", Name: "proj"},
			http.MethodGet, "/api/v1/projects"},
		{"Update", "Update",
			&project.UpdateProjectRequest{TenantID: "t1", ProjectID: "p1", Name: "renamed"},
			http.MethodPut, "/api/v1/projects/p1"},
		{"Delete", "Delete",
			&project.DeleteProjectRequest{TenantID: "t1", ProjectID: "p1"},
			http.MethodDelete, "/api/v1/projects/p1"},
		{"List", "List",
			&project.ListProjectsRequest{TenantID: "t1"},
			http.MethodGet, "/api/v1/projects"},
		{"GrantAgentRole", "GrantAgentRole",
			&project.GrantAgentRoleRequest{TenantID: "t1", ProjectID: "p1", AgentID: "a1", Role: project.ProjectRole("lead")},
			http.MethodPut, "/api/v1/projects/p1/permissions/a1"},
		{"RevokeAgentRole", "RevokeAgentRole",
			&project.RevokeAgentRoleRequest{TenantID: "t1", ProjectID: "p1", AgentID: "a1"},
			http.MethodDelete, "/api/v1/projects/p1/permissions/a1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotMethod, gotPath = "", ""
			resp, err := tr.Request(ctx, "Project", tc.method, tc.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp == nil {
				t.Fatal("expected non-nil response")
			}
			if gotMethod != tc.wantMethod {
				t.Errorf("HTTP method: want %s, got %s", tc.wantMethod, gotMethod)
			}
			if gotPath != tc.wantPath {
				t.Errorf("path: want %s, got %s", tc.wantPath, gotPath)
			}
		})
	}
}

// TestProjectHandlerInvalidRequestType exercises the type-assertion error
// branch on every Project handler.
func TestProjectHandlerInvalidRequestType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be reached, path %s", r.URL.Path)
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	methods := []string{"Create", "Get", "GetByName", "Update", "Delete",
		"List", "GrantAgentRole", "RevokeAgentRole"}
	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			_, err := tr.Request(ctx, "Project", m, 42)
			if err == nil {
				t.Fatalf("expected invalid-input error for method %s", m)
			}
		})
	}
}

// TestProjectHandlerUnknownMethodFallback verifies fallthrough to the generic path.
func TestProjectHandlerUnknownMethodFallback(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	_, err := tr.Request(context.Background(), "Project", "Archive", map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/v1/project/archive" {
		t.Errorf("unexpected fallback path: %s", gotPath)
	}
}

// TestProjectHandlerServerError exercises the doJSONRequest error branch.
func TestProjectHandlerServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"missing"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	_, err := tr.Request(context.Background(), "Project", "Get",
		&project.GetProjectRequest{TenantID: "t1", ProjectID: "nope"})
	if err == nil {
		t.Fatal("expected error from server 404")
	}
}
