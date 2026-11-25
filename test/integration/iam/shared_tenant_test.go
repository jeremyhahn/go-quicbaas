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

//go:build integration
// +build integration

package iam_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// sharedTenantDeleteTimeout bounds the delete of one shared tenant after
// m.Run.
const sharedTenantDeleteTimeout = 60 * time.Second

// sharedTenant is one tenant that tenant-relative tests share, serially (the
// runner uses -p 1 and no iam test calls t.Parallel). Creating a tenant and
// its principals costs the cluster 10-20 s and deleting it about 8 s, so a
// test whose every assertion is about objects it names uniquely
// (generateUnique* IDs) or a delta from a baseline it reads first
// (auditBaseline) takes a shared tenant instead of a fresh one. A test that
// checks tenant isolation, tenant create/update/delete, tenant deletion side
// effects, or an absolute count over the whole tenant keeps its own fresh
// tenant.
//
// The tenant outlives the test that created it: no t.Cleanup is registered,
// and TestMain deletes every shared tenant after m.Run
// (deleteSharedTenants).
type sharedTenant struct {
	id      string
	baseURL string

	// admin is the tenant admin's session, set when the tenant was
	// provisioned with its admin (provisionSharedTenant).
	admin *helpers.TenantAdminSession
	// auditor is the cached tenant auditor view (auditorView).
	auditor *testCluster
}

var sharedTenants = struct {
	sync.Mutex
	byName map[string]*sharedTenant
	ids    map[string]bool
}{byName: make(map[string]*sharedTenant), ids: make(map[string]bool)}

// sharedTenantFor returns the shared tenant named name, creating it on first
// use with create, which must create the tenant and return its ID. create
// is the calling file's own tenant-creation path, so a shared tenant is
// created exactly as that file's fresh tenants are.
func sharedTenantFor(t *testing.T, baseURL, name string, create func(t *testing.T) string) *sharedTenant {
	t.Helper()
	return sharedTenantWith(t, name, func(t *testing.T) *sharedTenant {
		return &sharedTenant{id: create(t), baseURL: baseURL}
	})
}

// provisionSharedTenant returns the shared tenant named name, provisioned
// with its admin on first use the way helpers.TenantAdmin provisions a fresh
// one (helpers.ProvisionTenant as the system admin), but without the
// per-test delete: the tenant is deleted after m.Run.
func provisionSharedTenant(t *testing.T, baseURL, name string, newID func() string) *sharedTenant {
	t.Helper()
	return sharedTenantWith(t, name, func(t *testing.T) *sharedTenant {
		baseURL := strings.TrimRight(baseURL, "/")
		tenantID := newID()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		systemBearer, err := helpers.AuthSystemAdminContext(ctx, baseURL)
		require.NoError(t, err, "system admin login for shared tenant %q", name)
		tenant, err := helpers.ProvisionTenant(ctx, baseURL, tenantID, systemBearer)
		require.NoError(t, err, "provision shared tenant %q", name)
		return &sharedTenant{
			id:      tenantID,
			baseURL: baseURL,
			admin:   helpers.NewTenantAdminSession(baseURL, tenant),
		}
	})
}

func sharedTenantWith(t *testing.T, name string, create func(t *testing.T) *sharedTenant) *sharedTenant {
	t.Helper()
	sharedTenants.Lock()
	defer sharedTenants.Unlock()
	if s, ok := sharedTenants.byName[name]; ok {
		return s
	}
	s := create(t)
	require.NotEmpty(t, s.id, "shared tenant %q id", name)
	sharedTenants.byName[name] = s
	sharedTenants.ids[s.id] = true
	t.Logf("shared tenant %q: %s", name, s.id)
	return s
}

// isSharedTenant reports whether tenantID is a shared tenant, which a test's
// own cleanup must leave for TestMain to delete.
func isSharedTenant(tenantID string) bool {
	sharedTenants.Lock()
	defer sharedTenants.Unlock()
	return sharedTenants.ids[tenantID]
}

// deleteTenantUnlessShared is a test's own tenant delete: it deletes
// tenantID through view, unless tenantID is a shared tenant, which TestMain
// deletes after m.Run.
func deleteTenantUnlessShared(view *testCluster, tenantID string) {
	if isSharedTenant(tenantID) {
		return
	}
	_ = view.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))
}

// auditorView returns the shared tenant's auditor view, enrolled through
// asTenantAuditor on first use.
func (s *sharedTenant) auditorView(t *testing.T, cluster *testCluster) *testCluster {
	t.Helper()
	sharedTenants.Lock()
	defer sharedTenants.Unlock()
	if s.auditor == nil {
		s.auditor = cluster.asTenantAuditor(t, s.id)
	}
	return s.auditor
}

// deleteSharedTenants deletes every shared tenant as the system admin. It
// runs in TestMain after m.Run and logs, but does not fail on, a delete
// error: every test has already reported.
func deleteSharedTenants() {
	sharedTenants.Lock()
	defer sharedTenants.Unlock()
	for name, s := range sharedTenants.byName {
		ctx, cancel := context.WithTimeout(context.Background(), sharedTenantDeleteTimeout)
		systemBearer, err := helpers.AuthSystemAdminContext(ctx, s.baseURL)
		if err == nil {
			err = helpers.DeleteTenant(ctx, s.baseURL, s.id, systemBearer)
		}
		cancel()
		if err != nil {
			fmt.Printf(">>> delete shared tenant %q (%s): %v\n", name, s.id, err)
		}
	}
}
