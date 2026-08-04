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

		// Per-tenant barrier — the leaf special rules MUST win over the
		// /api/v1/tenants prefix (resource "tenants"), so these authorize as
		// tenant_barrier, tenant-scoped. unseal-share maps to the unseal action.
		{"tenant barrier init", "/api/v1/tenants/acme/barrier/init", http.MethodPost, "tenant_barrier", "init", true},
		{"tenant barrier unseal", "/api/v1/tenants/acme/barrier/unseal", http.MethodPost, "tenant_barrier", "unseal", true},
		{"tenant barrier unseal-share", "/api/v1/tenants/acme/barrier/unseal-share", http.MethodPost, "tenant_barrier", "unseal", true},
		{"tenant barrier seal", "/api/v1/tenants/acme/barrier/seal", http.MethodPost, "tenant_barrier", "seal", true},
		{"tenant barrier status", "/api/v1/tenants/acme/barrier/status", http.MethodGet, "tenant_barrier", "status", true},

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

		// Cluster + cache control plane — reads authorize as admin:status,
		// membership writes as admin:add_member/remove_member, data-movement as
		// admin:drain, rebalance as the rebalance resource. Mirrors the gRPC canon.
		{"cluster status", "/api/v1/cluster/status", http.MethodGet, "admin", "status", true},
		{"cluster collection", "/api/v1/cluster", http.MethodGet, "admin", "status", true},
		{"cluster nodes list", "/api/v1/cluster/nodes", http.MethodGet, "admin", "status", true},
		{"cluster add node", "/api/v1/cluster/nodes", http.MethodPost, "admin", "add_member", true},
		{"cluster remove node", "/api/v1/cluster/nodes/n1", http.MethodDelete, "admin", "remove_member", true},
		{"cluster replace node", "/api/v1/cluster/nodes/n1/replace", http.MethodPut, "admin", "add_member", true},
		{"cluster rebalance", "/api/v1/cluster/rebalance", http.MethodPost, "rebalance", "execute", true},
		{"cluster rebalance plan", "/api/v1/cluster/rebalance/plan", http.MethodGet, "rebalance", "plan", true},
		{"cluster rebalance status", "/api/v1/cluster/rebalance/status", http.MethodGet, "rebalance", "report", true},
		{"cluster migrate", "/api/v1/cluster/migrate", http.MethodPost, "admin", "drain", true},
		{"cluster migrate cancel", "/api/v1/cluster/migrate", http.MethodDelete, "admin", "drain", true},
		{"cluster upgrade", "/api/v1/cluster/upgrade", http.MethodPost, "admin", "drain", true},
		{"cache stats", "/api/v1/cache/stats", http.MethodGet, "admin", "status", true},
		{"cache clear", "/api/v1/cache/clear", http.MethodPost, "admin", "capabilities", true},

		// Vector data plane — coarse CRUD + non-CRUD search/namespace/index verbs.
		{"vector health", "/api/v1/vector/health", http.MethodGet, "vector", "namespace_list", true},
		{"vector namespaces list", "/api/v1/vector/namespaces", http.MethodGet, "vector", "namespace_list", true},
		{"vector namespace create", "/api/v1/vector/namespaces", http.MethodPost, "vector", "namespace_create", true},
		{"vector namespace delete", "/api/v1/vector/namespaces/ns1", http.MethodDelete, "vector", "namespace_delete", true},
		{"vector search similar", "/api/v1/vector/namespaces/ns1/search/similar", http.MethodPost, "vector", "search_similar", true},
		{"vector search by id", "/api/v1/vector/namespaces/ns1/search/by-id/v1", http.MethodPost, "vector", "search_by_id", true},
		{"vector hybrid search", "/api/v1/vector/hybrid-search", http.MethodPost, "vector", "hybrid_search", true},
		{"vector search by metadata", "/api/v1/vector/search-by-metadata", http.MethodPost, "vector", "search_by_metadata", true},
		{"vector upsert", "/api/v1/vector/namespaces/ns1/vectors", http.MethodPost, "vector", "upsert", true},
		{"vector list", "/api/v1/vector/namespaces/ns1/vectors", http.MethodGet, "vector", "list", true},
		{"vector batch upsert", "/api/v1/vector/namespaces/ns1/vectors/batch", http.MethodPost, "vector", "batch_upsert", true},

		// Embedding (singular surface — UI EmbeddingsView).
		{"embedding generate", "/api/v1/embedding/generate", http.MethodPost, "embedding", "embed", true},
		{"embedding providers", "/api/v1/embedding/providers", http.MethodGet, "embedding", "list_models", true},
		{"embedding set default", "/api/v1/embedding/providers/default", http.MethodPut, "embedding", "list_models", true},

		// RAG (UI RAGView).
		{"rag query", "/api/v1/rag/query", http.MethodPost, "rag", "query", true},
		{"rag context", "/api/v1/rag/context", http.MethodPost, "rag", "query", true},
		{"rag list documents", "/api/v1/rag/documents", http.MethodGet, "rag", "list_documents", true},
		{"rag delete document", "/api/v1/rag/documents/doc1", http.MethodDelete, "rag", "delete_document", true},
		{"rag reindex", "/api/v1/rag/documents/doc1/reindex", http.MethodPost, "rag", "ingest", true},

		// Trust-anchor bootstrap (UI BootstrapView). All four modes are POSTs
		// that establish a trust anchor -> trust:write.
		{"bootstrap auto", "/api/v1/bootstrap/auto", http.MethodPost, "trust", "write", true},
		{"bootstrap dane", "/api/v1/bootstrap/dane", http.MethodPost, "trust", "write", true},
		{"bootstrap noise", "/api/v1/bootstrap/noise", http.MethodPost, "trust", "write", true},
		{"bootstrap spki", "/api/v1/bootstrap/spki", http.MethodPost, "trust", "write", true},
		{"bootstrap read", "/api/v1/bootstrap/auto", http.MethodGet, "trust", "read", true},

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

		// ---- SSH key management (BUG 1 regression) ----
		//
		// SSH has no ssh:* permission family in the qrdb registry; operations
		// map to the nearest registered keys permissions (keys:get, keys:list,
		// keys:create, keys:delete, keys:import, keys:sign, keys:verify).
		//
		// Canonical /api/v1/ssh mount.
		{"ssh keys list", "/api/v1/ssh/keys", http.MethodGet, "keys", "list", true},
		{"ssh keys create", "/api/v1/ssh/keys", http.MethodPost, "keys", "create", true},
		{"ssh keys import", "/api/v1/ssh/keys/import", http.MethodPost, "keys", "import", true},
		{"ssh keys get", "/api/v1/ssh/keys/system/admin/my-key", http.MethodGet, "keys", "get", true},
		{"ssh keys get public", "/api/v1/ssh/keys/system/admin/my-key/public", http.MethodGet, "keys", "get", true},
		{"ssh keys delete", "/api/v1/ssh/keys/system/admin/my-key", http.MethodDelete, "keys", "delete", true},
		{"ssh keys sign", "/api/v1/ssh/keys/system/admin/my-key/sign", http.MethodPost, "keys", "sign", true},
		{"ssh known-hosts list", "/api/v1/ssh/known-hosts", http.MethodGet, "keys", "list", true},
		{"ssh known-hosts add", "/api/v1/ssh/known-hosts", http.MethodPost, "keys", "create", true},
		{"ssh known-hosts get", "/api/v1/ssh/known-hosts/example.com", http.MethodGet, "keys", "get", true},
		{"ssh known-hosts remove", "/api/v1/ssh/known-hosts/example.com", http.MethodDelete, "keys", "delete", true},
		{"ssh known-hosts verify", "/api/v1/ssh/known-hosts/example.com/verify", http.MethodPost, "keys", "verify", true},
		{"ssh authorized emit", "/api/v1/ssh/authorized", http.MethodPost, "keys", "get", true},
		{"ssh authorized verify", "/api/v1/ssh/authorized/verify", http.MethodPost, "keys", "verify", true},
		// xkms-SDK alias — identical semantics, different mount prefix (lets CLIs
		// point XKMSD_URL at quicbaasd and reuse the go-xkms REST client unchanged).
		{"ssh xkms alias keys list", "/api/v1/xkms/v1/ssh/keys", http.MethodGet, "keys", "list", true},
		{"ssh xkms alias keys create", "/api/v1/xkms/v1/ssh/keys", http.MethodPost, "keys", "create", true},
		{"ssh xkms alias keys import", "/api/v1/xkms/v1/ssh/keys/import", http.MethodPost, "keys", "import", true},
		{"ssh xkms alias keys get", "/api/v1/xkms/v1/ssh/keys/system/admin/k", http.MethodGet, "keys", "get", true},
		{"ssh xkms alias keys delete", "/api/v1/xkms/v1/ssh/keys/system/admin/k", http.MethodDelete, "keys", "delete", true},
		{"ssh xkms alias keys sign", "/api/v1/xkms/v1/ssh/keys/system/admin/k/sign", http.MethodPost, "keys", "sign", true},
		{"ssh xkms alias known-hosts list", "/api/v1/xkms/v1/ssh/known-hosts", http.MethodGet, "keys", "list", true},
		{"ssh xkms alias known-hosts add", "/api/v1/xkms/v1/ssh/known-hosts", http.MethodPost, "keys", "create", true},
		{"ssh xkms alias known-hosts verify", "/api/v1/xkms/v1/ssh/known-hosts/h.example/verify", http.MethodPost, "keys", "verify", true},
		{"ssh xkms alias authorized emit", "/api/v1/xkms/v1/ssh/authorized", http.MethodPost, "keys", "get", true},
		{"ssh xkms alias authorized verify", "/api/v1/xkms/v1/ssh/authorized/verify", http.MethodPost, "keys", "verify", true},

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

// TestActionForMethod_KnownMethods asserts every method in the registered set
// resolves to the expected verb, including the collection-vs-item GET split, and
// does NOT regress when the fail-closed default was tightened.
func TestActionForMethod_KnownMethods(t *testing.T) {
	verbs := crudVerbs{get: "get", list: "list", create: "put", del: "delete"}
	tests := []struct {
		name         string
		method       string
		isCollection bool
		wantAction   string
	}{
		{"GET item", http.MethodGet, false, "get"},
		{"GET collection", http.MethodGet, true, "list"},
		{"HEAD item", http.MethodHead, false, "get"},
		{"HEAD collection", http.MethodHead, true, "list"},
		{"OPTIONS item", http.MethodOptions, false, "get"},
		{"OPTIONS collection", http.MethodOptions, true, "list"},
		{"POST", http.MethodPost, false, "put"},
		{"PUT", http.MethodPut, false, "put"},
		{"PATCH", http.MethodPatch, false, "put"},
		{"DELETE", http.MethodDelete, false, "delete"},
		// Case-insensitive method matching is preserved.
		{"lowercase get", "get", false, "get"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, ok := actionForMethod(tt.method, verbs, tt.isCollection)
			if !ok {
				t.Fatalf("actionForMethod(%q) ok = false, want true", tt.method)
			}
			if action != tt.wantAction {
				t.Errorf("actionForMethod(%q) = %q, want %q", tt.method, action, tt.wantAction)
			}
		})
	}
}

// TestActionForMethod_UnknownMethodFailsClosed asserts that any HTTP method
// outside the registered set returns ok=false (no action) rather than silently
// coercing to a read. The PEP is a closed set; an unknown verb must deny.
func TestActionForMethod_UnknownMethodFailsClosed(t *testing.T) {
	verbs := crudVerbs{get: "get", list: "list", create: "put", del: "delete"}
	for _, method := range []string{http.MethodTrace, http.MethodConnect, "PROPFIND", "FROBNICATE", ""} {
		t.Run(method, func(t *testing.T) {
			action, ok := actionForMethod(method, verbs, false)
			if ok {
				t.Fatalf("actionForMethod(%q) ok = true, want false (fail-closed)", method)
			}
			if action != "" {
				t.Errorf("actionForMethod(%q) action = %q, want empty", method, action)
			}
		})
	}
}

// TestMapRequestToPermission_UnknownMethodUnmapped asserts that an unexpected
// HTTP verb on an otherwise-mapped path resolves to ok=false so the validator
// gate denies it via the UnmappedResourceError path, instead of authorizing it
// as a read.
func TestMapRequestToPermission_UnknownMethodUnmapped(t *testing.T) {
	mappedPath := "/api/v1/keys"
	for _, method := range []string{http.MethodTrace, http.MethodConnect, "FROBNICATE"} {
		t.Run(method, func(t *testing.T) {
			resource, action, ok := mapRequestToPermission(mappedPath, method)
			if ok {
				t.Fatalf("mapRequestToPermission(%q, %q) ok = true, want false", mappedPath, method)
			}
			if resource != "" || action != "" {
				t.Errorf("mapRequestToPermission(%q, %q) = (%q, %q), want empty", mappedPath, method, resource, action)
			}
		})
	}
}
