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
	"strings"
)

// This file is the REST Policy Enforcement Point (PEP) mapping: it translates an
// HTTP (path, method) pair into the qrdb-canonical (resource, action) pair the
// qrdbauthz adapter passes to qrdb's policy decision point.
//
// qrdb's permission registry uses colon-joined "resource:action" permissions
// (e.g. "kv:get", "keys:create", "tenant_barrier:init"). The adapter passes
// resource and action SEPARATELY and qrdb joins them internally, so this mapper
// must emit a resource and an action whose join is a REGISTERED qrdb permission.
// A coarse read/write that does not correspond to a registered action would be
// denied by qrdb, so the mapping is per (path-prefix, method) and intentionally
// explicit.
//
// Resource names are qrdb-canonical, NOT the broker names the legacy
// extractAuthzResource emitted. The Stage-0 correspondence:
//
//	legacy broker name      ->  qrdb-canonical resource
//	-------------------------------------------------------
//	keys (KV data plane)    ->  kv          (kv:get / kv:put / kv:delete / kv:list)
//	kms / crypto            ->  keys        (cryptographic key operations)
//	certificate             ->  certs
//	secrets / credential    ->  credentials
//	barrier                 ->  tenant_barrier
//	iam users               ->  users
//	iam roles               ->  roles
//	iam tenants             ->  tenants
//	timeseries              ->  timeseries
//	vector                  ->  vector
//	watch                   ->  watch
//	transactions            ->  transaction
//	backups                 ->  backup
//
// Unmapped surfaces return ok=false; the caller decides the posture for those
// (see the middleware). This keeps the mapping a closed, registered set rather
// than guessing an action that qrdb would reject.

// methodActions maps an HTTP method to the qrdb action verb for a given
// resource family. Each resource family that follows the standard
// read/create/delete REST shape reuses one of these tables.

// crudActions is the standard mapping for resources whose qrdb verbs are
// get/create/delete/list — used by the KV data plane and the IAM management
// surfaces. The collection vs item distinction (list vs get) is resolved by the
// caller from the path shape.
type crudVerbs struct {
	// get is the action for a GET on a single item.
	get string
	// list is the action for a GET on a collection.
	list string
	// create is the action for POST/PUT/PATCH.
	create string
	// del is the action for DELETE.
	del string
}

// pepResource describes how one REST path prefix maps onto a qrdb resource.
type pepResource struct {
	// resource is the qrdb-canonical resource name.
	resource string
	// verbs is the method->action mapping for this resource.
	verbs crudVerbs
}

// pepMapping is the table-driven REST PEP. Keys are /api/v1 path prefixes; the
// longest matching prefix wins. Every (resource, action) pair produced here is
// a registered qrdb permission (see go-qrdb/pkg/permission/constants.go).
var pepMapping = map[string]pepResource{
	// KV data plane: /api/v1/keys and /api/v1/kv both address key-value data.
	"/api/v1/keys": {resource: "kv", verbs: crudVerbs{get: "get", list: "list", create: "put", del: "delete"}},
	"/api/v1/kv":   {resource: "kv", verbs: crudVerbs{get: "get", list: "list", create: "put", del: "delete"}},

	// Cryptographic key operations.
	"/api/v1/kms":    {resource: "keys", verbs: crudVerbs{get: "get", list: "list", create: "create", del: "delete"}},
	"/api/v1/crypto": {resource: "keys", verbs: crudVerbs{get: "get", list: "list", create: "create", del: "delete"}},

	// Certificate management.
	"/api/v1/certificate":  {resource: "certs", verbs: crudVerbs{get: "read", list: "list", create: "generate", del: "revoke"}},
	"/api/v1/certificates": {resource: "certs", verbs: crudVerbs{get: "read", list: "list", create: "generate", del: "revoke"}},

	// Secret / named-credential rotation.
	"/api/v1/secrets": {resource: "credentials", verbs: crudVerbs{get: "retrieve", list: "list", create: "store", del: "delete"}},

	// Tenant barrier lifecycle.
	"/api/v1/barrier": {resource: "tenant_barrier", verbs: crudVerbs{get: "status", list: "status", create: "init", del: "seal"}},

	// IAM management surfaces.
	"/api/v1/users":   {resource: "users", verbs: crudVerbs{get: "read", list: "list", create: "create", del: "delete"}},
	"/api/v1/roles":   {resource: "roles", verbs: crudVerbs{get: "read", list: "list", create: "create", del: "delete"}},
	"/api/v1/tenants": {resource: "tenants", verbs: crudVerbs{get: "read", list: "list", create: "create", del: "delete"}},

	// Data-service surfaces.
	"/api/v1/timeseries":   {resource: "timeseries", verbs: crudVerbs{get: "query", list: "query", create: "write", del: "delete_series"}},
	"/api/v1/watch":        {resource: "watch", verbs: crudVerbs{get: "watch", list: "watch", create: "watch", del: "watch"}},
	"/api/v1/transactions": {resource: "transaction", verbs: crudVerbs{get: "execute", list: "execute", create: "execute", del: "execute"}},
	"/api/v1/transaction":  {resource: "transaction", verbs: crudVerbs{get: "execute", list: "execute", create: "execute", del: "execute"}},
	"/api/v1/backups":      {resource: "backup", verbs: crudVerbs{get: "read", list: "list", create: "create", del: "delete"}},
}

// mapRequestToPermission translates a request (path, method) into the
// qrdb-canonical (resource, action). ok is false when no canonical mapping
// exists for the path; callers decide the posture for unmapped surfaces.
//
// The collection-vs-item distinction is structural: a GET on the bare resource
// prefix (e.g. /api/v1/keys) is a list; a GET on a sub-path (e.g.
// /api/v1/keys/{id}) is a get. This matches qrdb's distinct list vs get
// permissions.
func mapRequestToPermission(path, method string) (resource, action string, ok bool) {
	normalized := strings.TrimSuffix(path, "/")

	prefix, entry, found := longestPEPPrefix(normalized)
	if !found {
		return "", "", false
	}

	isCollection := normalized == prefix
	return entry.resource, actionForMethod(method, entry.verbs, isCollection), true
}

// longestPEPPrefix returns the longest /api/v1 prefix in pepMapping that matches
// path, along with its mapping entry.
func longestPEPPrefix(path string) (string, pepResource, bool) {
	var (
		bestPrefix string
		bestEntry  pepResource
		found      bool
	)
	for prefix, entry := range pepMapping {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			if len(prefix) > len(bestPrefix) {
				bestPrefix = prefix
				bestEntry = entry
				found = true
			}
		}
	}
	return bestPrefix, bestEntry, found
}

// actionForMethod resolves the qrdb action for an HTTP method against a
// resource's verb table. A GET on a collection maps to list; a GET on an item
// maps to get.
func actionForMethod(method string, verbs crudVerbs, isCollection bool) string {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		if isCollection {
			return verbs.list
		}
		return verbs.get
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return verbs.create
	case http.MethodDelete:
		return verbs.del
	default:
		return verbs.get
	}
}
