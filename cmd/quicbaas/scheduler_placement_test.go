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

package main

import (
	"bytes"
	"encoding/json"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement/placementtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// placementCLIService serves a real placement engine to an embedded SDK
// client, so the CLI drives the same Async().Scheduler() path it uses on
// every wire transport.
type placementCLIService struct {
	*testutil.MockQuicBaaS
	placement service.PlacementService
}

func (m *placementCLIService) Placement() service.PlacementService { return m.placement }

// withPlacementCLI installs an embedded SDK client over a fresh engine.
func withPlacementCLI(t *testing.T) {
	t.Helper()
	svc := &placementCLIService{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		placement:    service.NewPlacementService(placement.New(placementtest.New())),
	}
	client, err := quicbaas.NewClient("embedded", quicbaas.WithEmbedded(svc), mockSystemIdentity())
	require.NoError(t, err)
	orig := sdkClientFactory
	sdkClientFactory = func() (quicbaas.Client, error) { return client, nil }
	t.Cleanup(func() {
		sdkClientFactory = orig
		placementCapacity, placementLabels, placementResource = nil, nil, nil
		placementGroup, placementStrategy, placementSpec, placementNodeID = "", "", "", ""
		placementTTL = 30
		schedulerOutputFmt = "table"
	})
}

// runScheduler runs "scheduler <args...>" and returns its output.
func runScheduler(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newSchedulerCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestSchedulerPlacementCLI_EndToEnd(t *testing.T) {
	withPlacementCLI(t)

	out, err := runScheduler(t, "node", "register", "n1", "--capacity", "cpu=8", "--label", "zone=a", "--ttl", "20", "--output", "json")
	require.NoError(t, err, out)
	var node quicbaas.PlacementNodeInfo
	require.NoError(t, json.Unmarshal([]byte(out), &node))
	assert.Equal(t, int64(20), node.TTLSeconds)
	assert.Equal(t, "a", node.Facts.Labels["zone"])

	out, err = runScheduler(t, "node", "renew", "n1")
	require.NoError(t, err, out)
	assert.Contains(t, out, "renewed")

	out, err = runScheduler(t, "placement", "place", "w1", "--group", "web", "--resource", "cpu=3",
		"--spec", `{"constraints":[{"key":"zone","operator":"In","values":["a"]}]}`, "--output", "json")
	require.NoError(t, err, out)
	var res quicbaas.PlaceResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	assert.Equal(t, "n1", res.Placement.NodeID)
	assert.Equal(t, int64(3), res.Placement.Resources["cpu"])

	out, err = runScheduler(t, "node", "get", "n1", "--output", "table")
	require.NoError(t, err, out)
	assert.Contains(t, out, "cpu=3", "the allocation is visible on the node")

	out, err = runScheduler(t, "node", "list", "--output", "table")
	require.NoError(t, err, out)
	assert.Contains(t, out, "n1")

	out, err = runScheduler(t, "placement", "list", "--group", "web", "--output", "table")
	require.NoError(t, err, out)
	assert.Contains(t, out, "w1")

	out, err = runScheduler(t, "placement", "get", "w1", "--output", "yaml")
	require.NoError(t, err, out)
	assert.Contains(t, out, "workload_id: w1")

	for _, verb := range []string{"cordon", "uncordon"} {
		out, err = runScheduler(t, "node", verb, "n1")
		require.NoError(t, err, out)
	}
	out, err = runScheduler(t, "node", "drain", "n1", "--output", "table")
	require.NoError(t, err, out)
	assert.Contains(t, out, "w1")

	out, err = runScheduler(t, "placement", "release", "w1")
	require.NoError(t, err, out)
	assert.Contains(t, out, "released")
	out, err = runScheduler(t, "node", "deregister", "n1")
	require.NoError(t, err, out)
	assert.Contains(t, out, "deregistered")
}

func TestSchedulerPlacementCLI_Errors(t *testing.T) {
	withPlacementCLI(t)

	_, err := runScheduler(t, "node", "register", "n1", "--capacity", "cpu")
	require.Error(t, err)
	_, err = runScheduler(t, "node", "register", "n1", "--capacity", "cpu=lots")
	require.Error(t, err)
	_, err = runScheduler(t, "node", "register", "n1", "--label", "novalue")
	require.Error(t, err)
	_, err = runScheduler(t, "placement", "place", "w", "--spec", "{not json")
	require.Error(t, err)
	_, err = runScheduler(t, "placement", "place", "w", "--resource", "=1")
	require.Error(t, err)
	_, err = runScheduler(t, "placement", "place", "w", "--label", "x")
	require.Error(t, err)
	_, err = runScheduler(t, "node", "get", "missing")
	require.Error(t, err)
	_, err = runScheduler(t, "placement", "place", "w")
	require.Error(t, err, "no nodes are registered")
	_, err = runScheduler(t, "node", "list", "--output", "xml")
	require.Error(t, err)

	boom := stderrors.New("factory boom")
	orig := sdkClientFactory
	sdkClientFactory = func() (quicbaas.Client, error) { return nil, boom }
	t.Cleanup(func() { sdkClientFactory = orig })
	for _, args := range [][]string{
		{"node", "list"},
		{"node", "get", "n"},
		{"node", "register", "n"},
		{"placement", "place", "w"},
		{"placement", "list"},
	} {
		_, err := runScheduler(t, args...)
		assert.ErrorIsf(t, err, boom, "%v", args)
	}
}
