//go:build !integration

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

// orchestration_workflow_persistor_test.go covers the structural helpers
// in kvWorkflowPersistor that do not require a live qrdb client. The
// integration test in test/integration/orchestration exercises Save /
// Remove / LoadAll against a real KV; this unit test verifies the
// canonical key derivation and the empty-name validation guards on Save
// and Remove that gate every persistence call.

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/qrdbservice"
)

func TestKVWorkflowPersistor_KeyFor(t *testing.T) {
	p := &kvWorkflowPersistor{}
	got, err := p.keyFor("acme", "alpha")
	require.NoError(t, err)
	assert.Equal(t, "_orchestration/workflows/acme/alpha", got,
		"workflow keys are tenant-scoped under the canonical orchestration prefix")

	t.Run("tenant and name are key segments", func(t *testing.T) {
		cases := map[string][2]string{
			"empty tenant":  {"", "alpha"},
			"empty name":    {"acme", ""},
			"tenant with /": {"acme/evil", "alpha"},
			"name with /":   {"acme", "../beta"},
		}
		for name, tc := range cases {
			t.Run(name, func(t *testing.T) {
				_, err := p.keyFor(tc[0], tc[1])
				require.Error(t, err)
				var msg *ServerMessageError
				assert.True(t, errors.As(err, &msg), "expected *ServerMessageError, got %T", err)
			})
		}
	})
}

func TestSplitWorkflowKey(t *testing.T) {
	cases := map[string]struct {
		key    string
		tenant string
		name   string
		ok     bool
	}{
		"canonical":      {"_orchestration/workflows/acme/alpha", "acme", "alpha", true},
		"wrong prefix":   {"_orchestration/tasks/acme/alpha", "", "", false},
		"no tenant":      {"_orchestration/workflows/alpha", "", "", false},
		"empty tenant":   {"_orchestration/workflows//alpha", "", "", false},
		"empty name":     {"_orchestration/workflows/acme/", "", "", false},
		"nested segment": {"_orchestration/workflows/acme/a/b", "", "", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			tenant, wfName, ok := splitWorkflowKey(tc.key)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.tenant, tenant)
			assert.Equal(t, tc.name, wfName)
		})
	}
}

func TestKVWorkflowPersistor_Save_RejectsUnaddressable(t *testing.T) {
	p := &kvWorkflowPersistor{}

	t.Run("nil definition", func(t *testing.T) {
		err := p.Save(context.Background(), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "workflow name is required")
	})

	t.Run("empty name", func(t *testing.T) {
		err := p.Save(context.Background(), &orchestration.WorkflowDefinition{TenantID: "acme"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "workflow name is required")
	})

	t.Run("empty tenant", func(t *testing.T) {
		err := p.Save(context.Background(), &orchestration.WorkflowDefinition{Name: "wf"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "workflow tenant is required")
	})
}

func TestKVWorkflowPersistor_Remove_RejectsUnaddressable(t *testing.T) {
	p := &kvWorkflowPersistor{}
	require.Error(t, p.Remove(context.Background(), "acme", ""))
	require.Error(t, p.Remove(context.Background(), "", "wf"))
	require.Error(t, p.Remove(context.Background(), "acme", "a/b"))
}

func TestKVWorkflowPersistor_Load_RejectsUnaddressableTenant(t *testing.T) {
	p := &kvWorkflowPersistor{}
	_, err := p.Load(context.Background(), "")
	require.Error(t, err)
	_, err = p.Load(context.Background(), "a/b")
	require.Error(t, err)
}

func TestNewKVWorkflowPersistor_StoresArgs(t *testing.T) {
	p := newKVWorkflowPersistor(nil, 7)
	require.NotNil(t, p)
	assert.Equal(t, uint64(7), p.shardID)
}

// newPersistorNode starts a standalone qrdb node and returns a persistor over
// its client. Phase E: the bare ShardedClient no longer provides a working
// in-process transport; the qrdb node is required for embedded operations.
func newPersistorNode(t *testing.T) (*kvWorkflowPersistor, *qrdbsdk.Client) {
	t.Helper()
	node := newPrincipalNode(t)

	client := node.Client()
	require.NotNil(t, client)
	return newKVWorkflowPersistor(qrdbservice.KV(node, client.KV()), 1), client
}

func workflowNames(defs []*orchestration.WorkflowDefinition) []string {
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.TenantID+"/"+d.Name)
	}
	return names
}

// TestKVWorkflowPersistor_TenantsDoNotCollide is the isolation proof against
// a real KV: two tenants save the same name, each Load sees only its own,
// LoadAll sees both, and removing one leaves the other.
func TestKVWorkflowPersistor_TenantsDoNotCollide(t *testing.T) {
	ctx := context.Background()
	p, _ := newPersistorNode(t)

	require.NoError(t, p.Save(ctx, &orchestration.WorkflowDefinition{TenantID: "alpha", Name: "shared", Description: "alpha's"}))
	require.NoError(t, p.Save(ctx, &orchestration.WorkflowDefinition{TenantID: "beta", Name: "shared", Description: "beta's"}))
	require.NoError(t, p.Save(ctx, &orchestration.WorkflowDefinition{TenantID: "beta", Name: "only-beta"}))

	alpha, err := p.Load(ctx, "alpha")
	require.NoError(t, err)
	require.Len(t, alpha, 1)
	assert.Equal(t, "alpha's", alpha[0].Description)
	assert.Equal(t, "alpha", alpha[0].TenantID)

	beta, err := p.Load(ctx, "beta")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"beta/shared", "beta/only-beta"}, workflowNames(beta))

	gamma, err := p.Load(ctx, "gamma")
	require.NoError(t, err)
	assert.Empty(t, gamma)

	all, err := p.LoadAll(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"alpha/shared", "beta/shared", "beta/only-beta"}, workflowNames(all))

	require.NoError(t, p.Remove(ctx, "alpha", "shared"))
	alpha, err = p.Load(ctx, "alpha")
	require.NoError(t, err)
	assert.Empty(t, alpha)
	beta, err = p.Load(ctx, "beta")
	require.NoError(t, err)
	assert.Len(t, beta, 2, "removing alpha's workflow must not touch beta's")
}

func TestKVWorkflowPersistor_RoundTrip(t *testing.T) {
	// Use a real qrdb standalone node to obtain a usable SDK client.
	// Phase E: the bare ShardedClient no longer provides a working in-process
	// transport; the qrdb node is required for all embedded mode operations.
	ctx := context.Background()
	node := newPrincipalNode(t)

	client := node.Client()
	require.NotNil(t, client)

	p := newKVWorkflowPersistor(qrdbservice.KV(node, client.KV()), 1)

	t.Run("Save then LoadAll returns the definition", func(t *testing.T) {
		def := &orchestration.WorkflowDefinition{TenantID: "acme", Name: "wf-A", Description: "alpha workflow"}
		require.NoError(t, p.Save(ctx, def))

		all, err := p.LoadAll(ctx)
		require.NoError(t, err)
		require.Len(t, all, 1)
		assert.Equal(t, "wf-A", all[0].Name)
	})

	t.Run("Save second workflow and LoadAll returns both", func(t *testing.T) {
		require.NoError(t, p.Save(ctx, &orchestration.WorkflowDefinition{TenantID: "acme", Name: "wf-B"}))
		all, err := p.LoadAll(ctx)
		require.NoError(t, err)
		names := make([]string, 0, len(all))
		for _, d := range all {
			names = append(names, d.Name)
		}
		assert.ElementsMatch(t, []string{"wf-A", "wf-B"}, names)
	})

	t.Run("Remove deletes the workflow from KV", func(t *testing.T) {
		require.NoError(t, p.Remove(ctx, "acme", "wf-A"))
		all, err := p.LoadAll(ctx)
		require.NoError(t, err)
		for _, d := range all {
			assert.NotEqual(t, "wf-A", d.Name)
		}
	})
}

func TestKVWorkflowPersistor_LoadAll_SkipsMalformedEntries(t *testing.T) {
	// Phase E: use a real qrdb standalone node.
	ctx := context.Background()
	node := newPrincipalNode(t)
	client := node.Client()
	p := newKVWorkflowPersistor(qrdbservice.KV(node, client.KV()), 1)

	// Write a malformed entry directly under the workflow prefix to
	// exercise the json.Unmarshal failure branch in LoadAll. The
	// persistor must silently skip it rather than aborting the entire
	// boot.
	require.NoError(t, p.kv.Put(ctx, workflowKeyPrefix+"acme/broken", []byte("not-json")))
	// A body whose name disagrees with its key is not the record its key
	// claims; it is skipped rather than surfaced under the wrong name.
	require.NoError(t, p.kv.Put(ctx, workflowKeyPrefix+"acme/misfiled", []byte(`{"name":"other"}`)))
	// A key with no tenant segment cannot be filed.
	require.NoError(t, p.kv.Put(ctx, workflowKeyPrefix+"untenanted", []byte(`{"name":"untenanted"}`)))

	// Save a valid one alongside.
	require.NoError(t, p.Save(ctx, &orchestration.WorkflowDefinition{TenantID: "acme", Name: "good"}))

	all, err := p.LoadAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "good", all[0].Name)
}

// newClosedPersistorClient builds a standalone qrdb node, closes it, and returns
// a persistor over its now-defunct client so that Put/Delete/Scan all fail. This
// drives the KV-error branches of Save/Remove/LoadAll without a fake client.
func newClosedPersistorClient(t *testing.T) *kvWorkflowPersistor {
	t.Helper()
	node := newPrincipalNode(t)
	client := node.Client()
	require.NotNil(t, client)
	// Close the node so all subsequent KV operations on its client fail.
	require.NoError(t, node.Close())
	return newKVWorkflowPersistor(qrdbservice.KV(node, client.KV()), 1)
}

// TestKVWorkflowPersistor_Save_PutError covers the Put-error branch of Save:
// a closed client makes the KV Put fail and Save wraps the error.
func TestKVWorkflowPersistor_Save_PutError(t *testing.T) {
	p := newClosedPersistorClient(t)
	err := p.Save(context.Background(), &orchestration.WorkflowDefinition{TenantID: "acme", Name: "wf"})
	require.Error(t, err, "Save over a closed client must surface the KV put error")
}

// TestKVWorkflowPersistor_Remove_DeleteError covers the Delete-error branch of
// Remove: a closed client makes the KV Delete fail and Remove wraps the error.
func TestKVWorkflowPersistor_Remove_DeleteError(t *testing.T) {
	p := newClosedPersistorClient(t)
	err := p.Remove(context.Background(), "acme", "wf")
	require.Error(t, err, "Remove over a closed client must surface the KV delete error")
}

// TestKVWorkflowPersistor_Load_ScanError covers the Scan-error branch of the
// per-tenant Load.
func TestKVWorkflowPersistor_Load_ScanError(t *testing.T) {
	p := newClosedPersistorClient(t)
	defs, err := p.Load(context.Background(), "acme")
	require.Error(t, err, "Load over a closed client must surface the KV scan error")
	require.Nil(t, defs)
}

// TestKVWorkflowPersistor_LoadAll_ScanError covers the Scan-error branch of
// LoadAll: a closed client makes the KV Scan fail and LoadAll wraps the error.
func TestKVWorkflowPersistor_LoadAll_ScanError(t *testing.T) {
	p := newClosedPersistorClient(t)
	defs, err := p.LoadAll(context.Background())
	require.Error(t, err, "LoadAll over a closed client must surface the KV scan error")
	require.Nil(t, defs)
}

// TestKVWorkflowPersistor_LoadAll_SkipsEmptyName covers the empty-name skip
// branch of LoadAll: a stored entry that decodes cleanly but has an empty Name
// is skipped (it is not a usable workflow) rather than returned.
func TestKVWorkflowPersistor_LoadAll_SkipsEmptyName(t *testing.T) {
	ctx := context.Background()
	node := newPrincipalNode(t)
	client := node.Client()
	p := newKVWorkflowPersistor(qrdbservice.KV(node, client.KV()), 1)

	// Store a valid JSON workflow with an empty Name directly under the prefix.
	require.NoError(t, p.kv.Put(ctx, workflowKeyPrefix+"acme/nameless", []byte(`{"Name":""}`)))
	// Plus a valid one.
	require.NoError(t, p.Save(ctx, &orchestration.WorkflowDefinition{TenantID: "acme", Name: "named"}))

	all, err := p.LoadAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1, "the empty-name entry must be skipped")
	require.Equal(t, "named", all[0].Name)
}
