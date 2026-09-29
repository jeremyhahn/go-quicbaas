//go:build rest

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

package rest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// storageRESTRouter wires the project handlers over the REAL project service
// on an in-memory store, so a round trip exercises the request mapping, the
// service's builder and applier, and the response mapping together.
func storageRESTRouter() http.Handler {
	srv := newProjectTestServer(service.NewProjectService(orchestration.NewInMemoryProjectStore()))
	r := chi.NewRouter()
	r.Post("/projects", srv.HandleCreateProject)
	r.Get("/projects/{id}", srv.HandleGetProject)
	r.Put("/projects/{id}", srv.HandleUpdateProject)
	return r
}

func doStorageREST(t *testing.T, h http.Handler, method, path, body string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

// flatProject decodes the project member of a response envelope as the raw
// wire object, so the assertion is on the KEYS the API sends — the #797
// failure was a read path hydrating from a key the API never sent.
func flatProject(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	var env struct {
		Project map[string]json.RawMessage `json:"project"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode envelope: %v (%s)", err, raw)
	}
	if env.Project == nil {
		t.Fatalf("no project in %s", raw)
	}
	if _, nested := env.Project["storage"]; nested {
		t.Errorf("the project shape grew a nested storage member: %s", raw)
	}
	return env.Project
}

func assertFlatStorage(t *testing.T, p map[string]json.RawMessage, tier string, tmpfs, memory string) {
	t.Helper()
	for key, want := range map[string]string{
		"workspace_tier":      tier,
		"workspace_tmpfs_mb":  tmpfs,
		"container_memory_mb": memory,
	} {
		got := string(p[key])
		if got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

// TestProjectREST_StorageRoundTrip: set the tier over REST, read it back, and
// assert it survived — then prove an update that omits the sizes leaves them
// and one that sends zero clears them.
func TestProjectREST_StorageRoundTrip(t *testing.T) {
	h := storageRESTRouter()

	code, body := doStorageREST(t, h, http.MethodPost, "/projects", `{
		"tenant_id": "tenant-1", "name": "api",
		"workspace_tier": "ephemeral", "workspace_tmpfs_mb": 4096, "container_memory_mb": 8192
	}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, body)
	}
	created := flatProject(t, body)
	assertFlatStorage(t, created, `"ephemeral"`, "4096", "8192")
	var id string
	if err := json.Unmarshal(created["id"], &id); err != nil {
		t.Fatalf("decode id: %v", err)
	}
	path := "/projects/" + id + "?tenant_id=tenant-1"

	code, body = doStorageREST(t, h, http.MethodGet, path, "")
	if code != http.StatusOK {
		t.Fatalf("get: %d %s", code, body)
	}
	assertFlatStorage(t, flatProject(t, body), `"ephemeral"`, "4096", "8192")

	// Omitting the sizes leaves the stored ones alone.
	code, body = doStorageREST(t, h, http.MethodPut, path, `{"description": "renamed"}`)
	if code != http.StatusOK {
		t.Fatalf("update: %d %s", code, body)
	}
	code, body = doStorageREST(t, h, http.MethodGet, path, "")
	if code != http.StatusOK {
		t.Fatalf("get: %d %s", code, body)
	}
	assertFlatStorage(t, flatProject(t, body), `"ephemeral"`, "4096", "8192")

	// Sending zero clears them; zero is omitted on the wire like every other
	// zero-valued project field.
	code, body = doStorageREST(t, h, http.MethodPut, path, `{"workspace_tmpfs_mb": 0, "container_memory_mb": 0}`)
	if code != http.StatusOK {
		t.Fatalf("update: %d %s", code, body)
	}
	code, body = doStorageREST(t, h, http.MethodGet, path, "")
	if code != http.StatusOK {
		t.Fatalf("get: %d %s", code, body)
	}
	assertFlatStorage(t, flatProject(t, body), `"ephemeral"`, "", "")
}

// TestProjectREST_InvalidTierIsRefusedByTheService: the 400 is the service's
// *ProjectInvalidError, not a handler-local check.
func TestProjectREST_InvalidTierIsRefusedByTheService(t *testing.T) {
	h := storageRESTRouter()

	code, body := doStorageREST(t, h, http.MethodPost, "/projects",
		`{"tenant_id": "tenant-1", "name": "api", "workspace_tier": "ramdisk"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("create with an unknown tier: %d %s, want 400", code, body)
	}
	if !strings.Contains(string(body), "workspace_tier") {
		t.Errorf("refusal does not name the field: %s", body)
	}

	code, body = doStorageREST(t, h, http.MethodPost, "/projects", `{"tenant_id": "tenant-1", "name": "api"}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, body)
	}
	var id string
	if err := json.Unmarshal(flatProject(t, body)["id"], &id); err != nil {
		t.Fatalf("decode id: %v", err)
	}
	code, body = doStorageREST(t, h, http.MethodPut, "/projects/"+id+"?tenant_id=tenant-1",
		`{"workspace_tier": "ramdisk"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("update with an unknown tier: %d %s, want 400", code, body)
	}
}
