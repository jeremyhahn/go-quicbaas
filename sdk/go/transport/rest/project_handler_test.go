// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
		{"GrantFleetNodeRole", "GrantFleetNodeRole",
			&project.GrantFleetNodeRoleRequest{TenantID: "t1", ProjectID: "p1", FleetNodeID: "a1", Role: project.ProjectRole("lead")},
			http.MethodPut, "/api/v1/projects/p1/permissions/a1"},
		{"RevokeFleetNodeRole", "RevokeFleetNodeRole",
			&project.RevokeFleetNodeRoleRequest{TenantID: "t1", ProjectID: "p1", FleetNodeID: "a1"},
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
		"List", "GrantFleetNodeRole", "RevokeFleetNodeRole"}
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

// TestProjectHandler_NewFieldsAreOnTheJSONWire pins the flat REST keys the
// server reads for the default workflow and git identity, and that an unset
// signing switch on update is omitted rather than sent as false.
func TestProjectHandler_NewFieldsAreOnTheJSONWire(t *testing.T) {
	var bodies []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	if _, err := tr.Request(ctx, "Project", "Create", &project.CreateProjectRequest{
		TenantID: "t1", Name: "api",
		DefaultWorkflow:     "sdlc",
		GitAuthorName:       "agent-api",
		GitAuthorEmail:      "agent-api@example.test",
		GitSigningEnabled:   true,
		GitSigningAlgorithm: "ECDSA-P256",
		GitSigningKeyName:   "api-signing",
		GitSigningKeyScope:  "tenant",
		GitSigningKeyOwner:  "t1",
		GitSSHKeyName:       "api-access",
		GitSSHKeyScope:      "service",
		GitSSHKeyOwner:      "deploy",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	off := false
	if _, err := tr.Request(ctx, "Project", "Update", &project.UpdateProjectRequest{
		TenantID: "t1", ProjectID: "p1", ClearDefaultWorkflow: true, GitSigningEnabled: &off,
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, err := tr.Request(ctx, "Project", "Update", &project.UpdateProjectRequest{
		TenantID: "t1", ProjectID: "p1", Name: "renamed",
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if len(bodies) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(bodies))
	}
	create := bodies[0]
	for key, want := range map[string]interface{}{
		"default_workflow":      "sdlc",
		"git_author_name":       "agent-api",
		"git_author_email":      "agent-api@example.test",
		"git_signing_enabled":   true,
		"git_signing_algorithm": "ECDSA-P256",
		"git_signing_key_name":  "api-signing",
		"git_signing_key_scope": "tenant",
		"git_signing_key_owner": "t1",
		"git_ssh_key_name":      "api-access",
		"git_ssh_key_scope":     "service",
		"git_ssh_key_owner":     "deploy",
	} {
		if create[key] != want {
			t.Errorf("create body %s = %v, want %v", key, create[key], want)
		}
	}
	if bodies[1]["clear_default_workflow"] != true || bodies[1]["git_signing_enabled"] != false {
		t.Errorf("update body lost the tri-state switch: %v", bodies[1])
	}
	if _, present := bodies[2]["git_signing_enabled"]; present {
		t.Errorf("an unset signing switch was sent: %v", bodies[2])
	}
}

// =============================================================================
// Autonomy + context repositories over REST
// =============================================================================

// TestProjectHandler_AutonomyAndContextReposRoundTrip drives create → get and
// update → get against a stateful fake, asserting both fields ride the JSON
// body out and the project shape back.
func TestProjectHandler_AutonomyAndContextReposRoundTrip(t *testing.T) {
	stored := project.ProjectInfo{ID: "p1", TenantID: "t1", Name: "api"}
	var lastUpdate map[string]json.RawMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var req project.CreateProjectRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode create: %v", err)
			}
			stored.Autonomous = req.Autonomous
			stored.ContextRepos = req.ContextRepos
		case http.MethodPut:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read update: %v", err)
			}
			lastUpdate = map[string]json.RawMessage{}
			if err := json.Unmarshal(body, &lastUpdate); err != nil {
				t.Errorf("decode update envelope: %v", err)
			}
			var req project.UpdateProjectRequest
			if err := json.Unmarshal(body, &req); err != nil {
				t.Errorf("decode update: %v", err)
			}
			if req.Autonomous != nil {
				stored.Autonomous = *req.Autonomous
			}
			switch {
			case req.ClearContextRepos:
				stored.ContextRepos = nil
			case req.ContextRepos != nil:
				stored.ContextRepos = req.ContextRepos
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(stored); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	get := func() *project.ProjectInfo {
		t.Helper()
		resp, err := tr.Request(ctx, "Project", "Get",
			&project.GetProjectRequest{TenantID: "t1", ProjectID: "p1"})
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		info, ok := resp.(*project.ProjectInfo)
		if !ok {
			t.Fatalf("wrong response type: %T", resp)
		}
		return info
	}

	created, err := tr.Request(ctx, "Project", "Create", &project.CreateProjectRequest{
		TenantID: "t1", Name: "api",
		Autonomous: true,
		ContextRepos: []project.ContextRepo{
			{Name: "handbook", RepoURL: "https://forge.example/org/handbook", Ref: "main"},
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	info, ok := created.(*project.ProjectInfo)
	if !ok {
		t.Fatalf("wrong response type: %T", created)
	}
	if !info.Autonomous || len(info.ContextRepos) != 1 || info.ContextRepos[0].Name != "handbook" {
		t.Fatalf("create response lost fields: %+v", info)
	}
	if info = get(); !info.Autonomous || len(info.ContextRepos) != 1 {
		t.Fatalf("create did not survive a get: %+v", info)
	}

	// An unset switch must not appear in the body at all, so the server
	// reads it as "unchanged".
	if _, err = tr.Request(ctx, "Project", "Update",
		&project.UpdateProjectRequest{TenantID: "t1", ProjectID: "p1", Name: "renamed"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, present := lastUpdate["autonomous"]; present {
		t.Error("an unset autonomous was sent on the wire")
	}
	if _, present := lastUpdate["context_repos"]; present {
		t.Error("an unset context_repos was sent on the wire")
	}
	if info = get(); !info.Autonomous || len(info.ContextRepos) != 1 {
		t.Fatalf("an unrelated update changed the fields: %+v", info)
	}

	off := false
	if _, err = tr.Request(ctx, "Project", "Update", &project.UpdateProjectRequest{
		TenantID: "t1", ProjectID: "p1",
		Autonomous: &off,
		ContextRepos: []project.ContextRepo{
			{Name: "specs", RepoURL: "https://forge.example/org/specs", Ref: "v2"},
		},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if string(lastUpdate["autonomous"]) != "false" {
		t.Errorf("autonomous on the wire = %s, want false", lastUpdate["autonomous"])
	}
	info = get()
	if info.Autonomous {
		t.Error("an explicit false did not turn autonomy off")
	}
	if len(info.ContextRepos) != 1 || info.ContextRepos[0].Name != "specs" {
		t.Fatalf("ContextRepos = %+v, want exactly the specs repo", info.ContextRepos)
	}

	if _, err = tr.Request(ctx, "Project", "Update", &project.UpdateProjectRequest{
		TenantID: "t1", ProjectID: "p1", ClearContextRepos: true,
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if string(lastUpdate["clear_context_repos"]) != "true" {
		t.Errorf("clear_context_repos on the wire = %s, want true", lastUpdate["clear_context_repos"])
	}
	if info = get(); len(info.ContextRepos) != 0 {
		t.Fatalf("ContextRepos = %+v after clear, want none", info.ContextRepos)
	}
}

// TestProjectHandler_ServerRefusalSurfaces: a 400 from the project endpoint
// reaches the caller as an error rather than an empty project.
func TestProjectHandler_ServerRefusalSurfaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"project invalid: context_repos: name must be one path segment"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	_, err := tr.Request(context.Background(), "Project", "Create", &project.CreateProjectRequest{
		TenantID: "t1", Name: "api",
		ContextRepos: []project.ContextRepo{{Name: "a/b", RepoURL: "https://forge.example/a", Ref: "main"}},
	})
	if err == nil {
		t.Fatal("expected the server refusal to surface as an error")
	}
}

// =============================================================================
// Storage tier over REST
// =============================================================================

// TestProjectHandler_StorageIsOnTheFlatJSONWire asserts the three storage keys
// ride the create and update bodies FLAT — the names
// pkg/api/rest.CreateProjectRESTRequest / UpdateProjectRESTRequest decode —
// and that an unset update size is ABSENT while an explicit zero is sent, so
// the server can tell "leave it alone" from "lift the limit".
func TestProjectHandler_StorageIsOnTheFlatJSONWire(t *testing.T) {
	var bodies []map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	if _, err := tr.Request(ctx, "Project", "Create", &project.CreateProjectRequest{
		TenantID: "t1", Name: "api",
		WorkspaceTier:     project.WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  4096,
		ContainerMemoryMB: 8192,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := tr.Request(ctx, "Project", "Update", &project.UpdateProjectRequest{
		TenantID: "t1", ProjectID: "p1", Description: "renamed",
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	zero := 0
	if _, err := tr.Request(ctx, "Project", "Update", &project.UpdateProjectRequest{
		TenantID: "t1", ProjectID: "p1", WorkspaceTmpfsMB: &zero, ContainerMemoryMB: &zero,
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if len(bodies) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(bodies))
	}
	if _, nested := bodies[0]["storage"]; nested {
		t.Errorf("create body nested the storage keys: %v", bodies[0])
	}
	for key, want := range map[string]string{
		"workspace_tier":      `"ephemeral"`,
		"workspace_tmpfs_mb":  "4096",
		"container_memory_mb": "8192",
	} {
		if got := string(bodies[0][key]); got != want {
			t.Errorf("create body %s = %s, want %s", key, got, want)
		}
	}
	for _, key := range []string{"workspace_tier", "workspace_tmpfs_mb", "container_memory_mb"} {
		if _, present := bodies[1][key]; present {
			t.Errorf("an unset %s was sent on the update wire", key)
		}
	}
	for _, key := range []string{"workspace_tmpfs_mb", "container_memory_mb"} {
		if got := string(bodies[2][key]); got != "0" {
			t.Errorf("update body %s = %q, want an explicit 0", key, got)
		}
	}
}

// TestProjectHandler_SessionRotationIsOnTheJSONWire asserts the policy rides
// the create and update bodies under the key pkg/api/rest decodes, that an
// update not naming it leaves it off the wire, and that the policy the server
// reports is hydrated back onto the project.
func TestProjectHandler_SessionRotationIsOnTheJSONWire(t *testing.T) {
	var bodies []map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"p1","session_rotation":"fresh"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	resp, err := tr.Request(ctx, "Project", "Create", &project.CreateProjectRequest{
		TenantID: "t1", Name: "api", SessionRotation: project.SessionRotationFresh,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if info, ok := resp.(*project.ProjectInfo); !ok || info.SessionRotation != project.SessionRotationFresh {
		t.Errorf("Create returned %#v, want the reported policy hydrated", resp)
	}
	if _, err := tr.Request(ctx, "Project", "Update", &project.UpdateProjectRequest{
		TenantID: "t1", ProjectID: "p1", Description: "renamed",
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if len(bodies) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(bodies))
	}
	if got := string(bodies[0]["session_rotation"]); got != `"fresh"` {
		t.Errorf("create body session_rotation = %s, want \"fresh\"", got)
	}
	if raw, present := bodies[1]["session_rotation"]; present {
		t.Errorf("an unset session_rotation was sent on the update wire: %s", raw)
	}
}

// TestProjectHandler_ParkAndResumeOverREST asserts the park switch rides the
// update body only when set, and that the park record decodes off the
// project that comes back.
func TestProjectHandler_ParkAndResumeOverREST(t *testing.T) {
	stored := project.ProjectInfo{ID: "p1", TenantID: "t1", Name: "api"}
	parkedAt := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	var lastUpdate map[string]json.RawMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read update: %v", err)
		}
		lastUpdate = map[string]json.RawMessage{}
		if err := json.Unmarshal(body, &lastUpdate); err != nil {
			t.Errorf("decode update envelope: %v", err)
		}
		var req project.UpdateProjectRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("decode update: %v", err)
		}
		if req.Parked != nil {
			stored.Parked, stored.ParkedAt, stored.ParkedBy = false, time.Time{}, ""
			if *req.Parked {
				stored.Parked, stored.ParkedAt, stored.ParkedBy = true, parkedAt, "ops"
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(stored); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()
	update := func(req *project.UpdateProjectRequest) *project.ProjectInfo {
		t.Helper()
		resp, err := tr.Request(ctx, "Project", "Update", req)
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		info, ok := resp.(*project.ProjectInfo)
		if !ok {
			t.Fatalf("wrong response type: %T", resp)
		}
		return info
	}

	update(&project.UpdateProjectRequest{TenantID: "t1", ProjectID: "p1", Name: "renamed"})
	if _, present := lastUpdate["parked"]; present {
		t.Error("an unset parked was sent on the wire")
	}

	park := true
	info := update(&project.UpdateProjectRequest{TenantID: "t1", ProjectID: "p1", Parked: &park})
	if !info.Parked || !info.ParkedAt.Equal(parkedAt) || info.ParkedBy != "ops" {
		t.Fatalf("parked info = %+v, want the park record", info)
	}

	resume := false
	info = update(&project.UpdateProjectRequest{TenantID: "t1", ProjectID: "p1", Parked: &resume})
	if string(lastUpdate["parked"]) != "false" {
		t.Errorf("resume sent parked=%s, want false", lastUpdate["parked"])
	}
	if info.Parked || !info.ParkedAt.IsZero() {
		t.Fatalf("resumed info = %+v, want an open project", info)
	}
}
