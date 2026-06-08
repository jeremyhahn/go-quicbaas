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
	"net/http"
	"testing"
)

func TestMapRequestToPermission(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		method       string
		wantResource string
		wantAction   string
		wantOK       bool
	}{
		// KV data plane: collection vs item.
		{"keys collection list", "/api/v1/keys", http.MethodGet, "kv", "list", true},
		{"keys item get", "/api/v1/keys/foo", http.MethodGet, "kv", "get", true},
		{"keys put", "/api/v1/keys/foo", http.MethodPut, "kv", "put", true},
		{"keys post", "/api/v1/keys", http.MethodPost, "kv", "put", true},
		{"keys delete", "/api/v1/keys/foo", http.MethodDelete, "kv", "delete", true},
		{"kv alias get", "/api/v1/kv/foo", http.MethodGet, "kv", "get", true},

		// Cryptographic key operations.
		{"kms get", "/api/v1/kms/key1", http.MethodGet, "keys", "get", true},
		{"kms list", "/api/v1/kms", http.MethodGet, "keys", "list", true},
		{"kms create", "/api/v1/kms", http.MethodPost, "keys", "create", true},
		{"kms delete", "/api/v1/kms/key1", http.MethodDelete, "keys", "delete", true},
		{"crypto create", "/api/v1/crypto/sign", http.MethodPost, "keys", "create", true},

		// Certificates.
		{"cert read", "/api/v1/certificate/abc", http.MethodGet, "certs", "read", true},
		{"cert generate", "/api/v1/certificate", http.MethodPost, "certs", "generate", true},
		{"cert revoke", "/api/v1/certificate/abc", http.MethodDelete, "certs", "revoke", true},
		{"certs plural list", "/api/v1/certificates", http.MethodGet, "certs", "list", true},

		// Secrets -> credentials.
		{"secrets retrieve", "/api/v1/secrets/db", http.MethodGet, "credentials", "retrieve", true},
		{"secrets store", "/api/v1/secrets", http.MethodPost, "credentials", "store", true},
		{"secrets list", "/api/v1/secrets", http.MethodGet, "credentials", "list", true},
		{"secrets delete", "/api/v1/secrets/db", http.MethodDelete, "credentials", "delete", true},

		// Tenant barrier.
		{"barrier status", "/api/v1/barrier", http.MethodGet, "tenant_barrier", "status", true},
		{"barrier init", "/api/v1/barrier/init", http.MethodPost, "tenant_barrier", "init", true},
		{"barrier seal", "/api/v1/barrier/seal", http.MethodDelete, "tenant_barrier", "seal", true},

		// IAM management.
		{"users read", "/api/v1/users/u1", http.MethodGet, "users", "read", true},
		{"users list", "/api/v1/users", http.MethodGet, "users", "list", true},
		{"users create", "/api/v1/users", http.MethodPost, "users", "create", true},
		{"users delete", "/api/v1/users/u1", http.MethodDelete, "users", "delete", true},
		{"roles list", "/api/v1/roles", http.MethodGet, "roles", "list", true},
		{"tenants create", "/api/v1/tenants", http.MethodPost, "tenants", "create", true},

		// Data services.
		{"timeseries query", "/api/v1/timeseries", http.MethodGet, "timeseries", "query", true},
		{"timeseries write", "/api/v1/timeseries", http.MethodPost, "timeseries", "write", true},
		{"watch", "/api/v1/watch/key", http.MethodGet, "watch", "watch", true},
		{"transaction execute", "/api/v1/transactions", http.MethodPost, "transaction", "execute", true},
		{"backups create", "/api/v1/backups", http.MethodPost, "backup", "create", true},

		// Trailing slash normalization (collection).
		{"keys trailing slash", "/api/v1/keys/", http.MethodGet, "kv", "list", true},

		// ---- Stage 0.5: broker data-plane surfaces ----

		// Object storage CRUD + non-CRUD.
		{"objstore put", "/api/v1/objects/foo.txt", http.MethodPut, "objstore", "put", true},
		{"objstore get", "/api/v1/objects/foo.txt", http.MethodGet, "objstore", "get", true},
		{"objstore delete", "/api/v1/objects/foo.txt", http.MethodDelete, "objstore", "delete", true},
		{"objstore list", "/api/v1/objects", http.MethodGet, "objstore", "list", true},
		{"objstore head", "/api/v1/objects/foo.txt", http.MethodHead, "objstore", "head", true},
		{"objstore batch get", "/api/v1/objects/batch/get", http.MethodPost, "objstore", "batch", true},
		{"objstore tenant usage", "/api/v1/objects/tenants/t1/usage", http.MethodGet, "objstore", "tenant_usage", true},
		{"objstore quota write", "/api/v1/objects/tenants/t1/quotas", http.MethodPut, "objstore", "tenant_quota_write", true},
		{"objstore backend health", "/api/v1/object-backends/b1/health", http.MethodGet, "objstore", "read_backend", true},
		{"object transfer fetch", "/api/v1/object-transfer/fetch", http.MethodPost, "objstore", "get", true},

		// Orchestrator.
		{"orch spawn", "/api/v1/orchestrator/sessions", http.MethodPost, "orchestrator", "spawn", true},
		{"orch prompt", "/api/v1/orchestrator/sessions/s1/prompt", http.MethodPost, "orchestrator", "prompt", true},
		{"orch status", "/api/v1/orchestrator/sessions/s1", http.MethodGet, "orchestrator", "status", true},
		{"orch close", "/api/v1/orchestrator/sessions/s1", http.MethodDelete, "orchestrator", "close", true},
		{"orch task create", "/api/v1/orchestrator/tasks", http.MethodPost, "orchestrator", "task_create", true},
		{"orch task claim", "/api/v1/orchestrator/tasks/t1/claim", http.MethodPost, "orchestrator", "task_claim", true},
		{"orch health", "/api/v1/orchestrator/health", http.MethodGet, "orchestrator", "health", true},
		{"orch workflow write", "/api/v1/orchestrator/workflows/w1", http.MethodPut, "orchestrator", "workflow_write", true},

		// Messaging.
		{"msg channel create", "/api/v1/messaging/channels", http.MethodPost, "messaging", "channel_create", true},
		{"msg channel list", "/api/v1/messaging/channels", http.MethodGet, "messaging", "channel_list", true},
		{"msg channel get", "/api/v1/messaging/channels/c1", http.MethodGet, "messaging", "channel_read", true},
		{"msg channel delete", "/api/v1/messaging/channels/c1", http.MethodDelete, "messaging", "channel_delete", true},
		{"msg publish", "/api/v1/messaging/publish/c1", http.MethodPost, "messaging", "publish", true},
		{"msg subscribe", "/api/v1/messaging/subscribe", http.MethodPost, "messaging", "subscribe", true},
		{"msg unsubscribe", "/api/v1/messaging/subscribe/s1", http.MethodDelete, "messaging", "unsubscribe", true},
		{"msg stats", "/api/v1/messaging/stats", http.MethodGet, "messaging", "stats", true},

		// Scheduler.
		{"sched create", "/api/v1/scheduler/jobs", http.MethodPost, "scheduler", "create", true},
		{"sched list", "/api/v1/scheduler/jobs", http.MethodGet, "scheduler", "list", true},
		{"sched get", "/api/v1/scheduler/jobs/j1", http.MethodGet, "scheduler", "read", true},
		{"sched update", "/api/v1/scheduler/jobs/j1", http.MethodPut, "scheduler", "update", true},
		{"sched delete", "/api/v1/scheduler/jobs/j1", http.MethodDelete, "scheduler", "delete", true},
		{"sched trigger", "/api/v1/scheduler/jobs/j1/trigger", http.MethodPost, "scheduler", "trigger", true},
		{"sched pause", "/api/v1/scheduler/jobs/j1/pause", http.MethodPost, "scheduler", "pause", true},
		{"sched history", "/api/v1/scheduler/jobs/j1/history", http.MethodGet, "scheduler", "history", true},
		{"sched status", "/api/v1/scheduler/status", http.MethodGet, "scheduler", "status", true},

		// Parameters.
		{"param read", "/api/v1/parameters/app/db/host", http.MethodGet, "parameters", "read", true},
		{"param list", "/api/v1/parameters", http.MethodGet, "parameters", "list", true},
		{"param write", "/api/v1/parameters/app/db/host", http.MethodPut, "parameters", "write", true},
		{"param delete", "/api/v1/parameters/app/db/host", http.MethodDelete, "parameters", "delete", true},
		{"param search", "/api/v1/parameters/search", http.MethodPost, "parameters", "search", true},
		{"param batch set", "/api/v1/parameters/batch/set", http.MethodPost, "parameters", "write", true},

		// Dynamic secrets (mounted under /api/v1/secrets/dynamic).
		{"dyn role create", "/api/v1/secrets/dynamic/roles", http.MethodPost, "dynamic_secrets", "role_create", true},
		{"dyn role get", "/api/v1/secrets/dynamic/roles/r1", http.MethodGet, "dynamic_secrets", "role_read", true},
		{"dyn role list", "/api/v1/secrets/dynamic/roles", http.MethodGet, "dynamic_secrets", "role_list", true},
		{"dyn role delete", "/api/v1/secrets/dynamic/roles/r1", http.MethodDelete, "dynamic_secrets", "role_delete", true},
		{"dyn generate", "/api/v1/secrets/dynamic/credentials/r1", http.MethodPost, "dynamic_secrets", "generate", true},
		{"dyn lease renew", "/api/v1/secrets/dynamic/leases/l1/renew", http.MethodPost, "dynamic_secrets", "lease_renew", true},
		{"dyn lease revoke", "/api/v1/secrets/dynamic/leases/l1", http.MethodDelete, "dynamic_secrets", "lease_revoke", true},
		{"dyn lease list", "/api/v1/secrets/dynamic/leases", http.MethodGet, "dynamic_secrets", "lease_list", true},
		// Plain credential vault still resolves to credentials, not dynamic_secrets.
		{"credentials not shadowed", "/api/v1/secrets/db", http.MethodGet, "credentials", "retrieve", true},

		// Project.
		{"project create", "/api/v1/projects", http.MethodPost, "project", "create", true},
		{"project list", "/api/v1/projects", http.MethodGet, "project", "list", true},
		{"project get", "/api/v1/projects/p1", http.MethodGet, "project", "read", true},
		{"project update", "/api/v1/projects/p1", http.MethodPut, "project", "update", true},
		{"project delete", "/api/v1/projects/p1", http.MethodDelete, "project", "delete", true},
		{"project grant", "/api/v1/projects/p1/permissions/a1", http.MethodPut, "project", "grant", true},
		{"project revoke", "/api/v1/projects/p1/permissions/a1", http.MethodDelete, "project", "revoke", true},

		// Ollama.
		{"ollama health", "/api/v1/ollama/health", http.MethodGet, "ollama", "health", true},
		{"ollama models", "/api/v1/ollama/models", http.MethodGet, "ollama", "model_list", true},
		{"ollama model info", "/api/v1/ollama/models/llama3", http.MethodGet, "ollama", "model_info", true},
		{"ollama embed", "/api/v1/ollama/embed", http.MethodPost, "ollama", "embed", true},
		{"ollama embed batch", "/api/v1/ollama/embed/batch", http.MethodPost, "ollama", "embed", true},

		// Unmapped surfaces.
		{"unknown path", "/api/v1/totally-unknown", http.MethodGet, "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action, ok := mapRequestToPermission(tt.path, tt.method)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if resource != tt.wantResource {
				t.Errorf("resource = %q, want %q", resource, tt.wantResource)
			}
			if action != tt.wantAction {
				t.Errorf("action = %q, want %q", action, tt.wantAction)
			}
		})
	}
}

// TestActionForMethod_DefaultFallsBackToGet asserts an unrecognized method maps
// to the resource's get verb rather than panicking or returning empty.
func TestActionForMethod_DefaultFallsBackToGet(t *testing.T) {
	verbs := crudVerbs{get: "get", list: "list", create: "put", del: "delete"}
	if got := actionForMethod("PROPFIND", verbs, false); got != "get" {
		t.Errorf("actionForMethod(PROPFIND) = %q, want get", got)
	}
}
