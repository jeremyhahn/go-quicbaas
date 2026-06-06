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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbconfig "github.com/jeremyhahn/go-qrdb/pkg/config"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func TestKVWorkflowPersistor_KeyFor(t *testing.T) {
	p := &kvWorkflowPersistor{}
	got := p.keyFor("alpha")
	assert.True(t, strings.HasPrefix(got, "_orchestration/workflows/"),
		"workflow keys must live under the canonical orchestration prefix")
	assert.True(t, strings.HasSuffix(got, "alpha"))
}

func TestKVWorkflowPersistor_Save_RejectsEmptyName(t *testing.T) {
	p := &kvWorkflowPersistor{}

	t.Run("nil definition", func(t *testing.T) {
		err := p.Save(context.Background(), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "workflow name is required")
	})

	t.Run("empty name", func(t *testing.T) {
		err := p.Save(context.Background(), &orchestration.WorkflowDefinition{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "workflow name is required")
	})
}

func TestKVWorkflowPersistor_Remove_RejectsEmptyName(t *testing.T) {
	p := &kvWorkflowPersistor{}
	err := p.Remove(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workflow name is required")
}

func TestNewKVWorkflowPersistor_StoresArgs(t *testing.T) {
	p := newKVWorkflowPersistor(nil, 7)
	require.NotNil(t, p)
	assert.Equal(t, uint64(7), p.shardID)
}

func TestKVWorkflowPersistor_RoundTrip(t *testing.T) {
	// Use a real qrdb standalone node to obtain a usable SDK client.
	// Phase E: the bare ShardedClient no longer provides a working in-process
	// transport; the qrdb node is required for all embedded mode operations.
	ctx := context.Background()
	qcfg := &qrdbconfig.Config{}
	qcfg.Databases = []qrdbconfig.DatabaseConfig{
		{Name: "default", Engine: "mvcc", Backend: "memory"},
	}
	node, err := qrdbnode.NewStandaloneNode(ctx, qcfg, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = node.Close() })

	client := node.Client()
	require.NotNil(t, client)

	p := newKVWorkflowPersistor(client, 1)

	t.Run("Save then LoadAll returns the definition", func(t *testing.T) {
		def := &orchestration.WorkflowDefinition{Name: "wf-A", Description: "alpha workflow"}
		require.NoError(t, p.Save(ctx, def))

		all, err := p.LoadAll(ctx)
		require.NoError(t, err)
		require.Len(t, all, 1)
		assert.Equal(t, "wf-A", all[0].Name)
	})

	t.Run("Save second workflow and LoadAll returns both", func(t *testing.T) {
		require.NoError(t, p.Save(ctx, &orchestration.WorkflowDefinition{Name: "wf-B"}))
		all, err := p.LoadAll(ctx)
		require.NoError(t, err)
		names := make([]string, 0, len(all))
		for _, d := range all {
			names = append(names, d.Name)
		}
		assert.ElementsMatch(t, []string{"wf-A", "wf-B"}, names)
	})

	t.Run("Remove deletes the workflow from KV", func(t *testing.T) {
		require.NoError(t, p.Remove(ctx, "wf-A"))
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
	qcfg := &qrdbconfig.Config{}
	qcfg.Databases = []qrdbconfig.DatabaseConfig{
		{Name: "default", Engine: "mvcc", Backend: "memory"},
	}
	node, nodeErr := qrdbnode.NewStandaloneNode(ctx, qcfg, nil)
	require.NoError(t, nodeErr)
	t.Cleanup(func() { _ = node.Close() })
	client := node.Client()
	p := newKVWorkflowPersistor(client, 1)

	// Write a malformed entry directly under the workflow prefix to
	// exercise the json.Unmarshal failure branch in LoadAll. The
	// persistor must silently skip it rather than aborting the entire
	// boot.
	require.NoError(t, client.KV().Put(ctx, workflowKeyPrefix+"broken", []byte("not-json")))

	// Save a valid one alongside.
	require.NoError(t, p.Save(ctx, &orchestration.WorkflowDefinition{Name: "good"}))

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
	ctx := context.Background()
	qcfg := &qrdbconfig.Config{}
	qcfg.Databases = []qrdbconfig.DatabaseConfig{
		{Name: "default", Engine: "mvcc", Backend: "memory"},
	}
	node, err := qrdbnode.NewStandaloneNode(ctx, qcfg, nil)
	require.NoError(t, err)
	client := node.Client()
	require.NotNil(t, client)
	// Close the node so all subsequent KV operations on its client fail.
	require.NoError(t, node.Close())
	return newKVWorkflowPersistor(client, 1)
}

// TestKVWorkflowPersistor_Save_PutError covers the Put-error branch of Save:
// a closed client makes the KV Put fail and Save wraps the error.
func TestKVWorkflowPersistor_Save_PutError(t *testing.T) {
	p := newClosedPersistorClient(t)
	err := p.Save(context.Background(), &orchestration.WorkflowDefinition{Name: "wf"})
	require.Error(t, err, "Save over a closed client must surface the KV put error")
}

// TestKVWorkflowPersistor_Remove_DeleteError covers the Delete-error branch of
// Remove: a closed client makes the KV Delete fail and Remove wraps the error.
func TestKVWorkflowPersistor_Remove_DeleteError(t *testing.T) {
	p := newClosedPersistorClient(t)
	err := p.Remove(context.Background(), "wf")
	require.Error(t, err, "Remove over a closed client must surface the KV delete error")
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
	qcfg := &qrdbconfig.Config{}
	qcfg.Databases = []qrdbconfig.DatabaseConfig{
		{Name: "default", Engine: "mvcc", Backend: "memory"},
	}
	node, err := qrdbnode.NewStandaloneNode(ctx, qcfg, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = node.Close() })
	client := node.Client()
	p := newKVWorkflowPersistor(client, 1)

	// Store a valid JSON workflow with an empty Name directly under the prefix.
	require.NoError(t, client.KV().Put(ctx, workflowKeyPrefix+"nameless", []byte(`{"Name":""}`)))
	// Plus a valid one.
	require.NoError(t, p.Save(ctx, &orchestration.WorkflowDefinition{Name: "named"}))

	all, err := p.LoadAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1, "the empty-name entry must be skipped")
	require.Equal(t, "named", all[0].Name)
}
