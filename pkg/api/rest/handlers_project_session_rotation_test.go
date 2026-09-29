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
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestProjectREST_SessionRotationRoundTrip: the policy set over REST is read
// back under its flat key, an update that omits it leaves it, and one that
// names it replaces it. A project that never chose one sends no key.
func TestProjectREST_SessionRotationRoundTrip(t *testing.T) {
	h := storageRESTRouter()

	code, body := doStorageREST(t, h, http.MethodPost, "/projects",
		`{"tenant_id": "tenant-1", "name": "api", "session_rotation": "fresh"}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, body)
	}
	created := flatProject(t, body)
	if got := string(created["session_rotation"]); got != `"fresh"` {
		t.Fatalf("session_rotation = %s, want \"fresh\"", got)
	}
	var id string
	if err := json.Unmarshal(created["id"], &id); err != nil {
		t.Fatalf("decode id: %v", err)
	}
	path := "/projects/" + id + "?tenant_id=tenant-1"

	if code, body = doStorageREST(t, h, http.MethodPut, path, `{"description": "renamed"}`); code != http.StatusOK {
		t.Fatalf("update: %d %s", code, body)
	}
	code, body = doStorageREST(t, h, http.MethodGet, path, "")
	if code != http.StatusOK {
		t.Fatalf("get: %d %s", code, body)
	}
	if got := string(flatProject(t, body)["session_rotation"]); got != `"fresh"` {
		t.Errorf("an update naming no policy changed session_rotation to %s", got)
	}

	if code, body = doStorageREST(t, h, http.MethodPut, path, `{"session_rotation": "resume"}`); code != http.StatusOK {
		t.Fatalf("update: %d %s", code, body)
	}
	code, body = doStorageREST(t, h, http.MethodGet, path, "")
	if code != http.StatusOK {
		t.Fatalf("get: %d %s", code, body)
	}
	if got := string(flatProject(t, body)["session_rotation"]); got != `"resume"` {
		t.Errorf("session_rotation = %s, want \"resume\"", got)
	}

	code, body = doStorageREST(t, h, http.MethodPost, "/projects", `{"tenant_id": "tenant-1", "name": "unset"}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, body)
	}
	if raw, present := flatProject(t, body)["session_rotation"]; present {
		t.Errorf("a project that never chose a policy sent session_rotation = %s", raw)
	}
}

// TestProjectREST_AnUnknownSessionRotationIsRefused: the 400 is the service's
// refusal, naming the field, on create and on update.
func TestProjectREST_AnUnknownSessionRotationIsRefused(t *testing.T) {
	h := storageRESTRouter()

	code, body := doStorageREST(t, h, http.MethodPost, "/projects",
		`{"tenant_id": "tenant-1", "name": "api", "session_rotation": "restart"}`)
	if code != http.StatusBadRequest || !strings.Contains(string(body), "session_rotation") {
		t.Fatalf("create with an unknown policy: %d %s, want a 400 naming session_rotation", code, body)
	}

	code, body = doStorageREST(t, h, http.MethodPost, "/projects", `{"tenant_id": "tenant-1", "name": "api"}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, body)
	}
	var id string
	if err := json.Unmarshal(flatProject(t, body)["id"], &id); err != nil {
		t.Fatalf("decode id: %v", err)
	}
	code, body = doStorageREST(t, h, http.MethodPut, "/projects/"+id+"?tenant_id=tenant-1", `{"session_rotation": "restart"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("update with an unknown policy: %d %s, want 400", code, body)
	}
}
