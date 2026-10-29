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

package cluster

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/id"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

const (
	// pollInterval is how often pollUntil and holdSteady sample cluster state.
	pollInterval = 500 * time.Millisecond

	// pollRequestTimeout bounds one sample so a hung node cannot stall a poll
	// past its deadline.
	pollRequestTimeout = 10 * time.Second

	// replicationTimeout bounds how long a committed write may take to become
	// readable on another node. Apply latency is milliseconds; the margin
	// covers a leader election running concurrently in a slow CI runner.
	replicationTimeout = 30 * time.Second

	// clusterSettleTimeout bounds how long a test waits for the cluster to
	// serve again, which can include a full leader election after an earlier
	// test disturbed it.
	clusterSettleTimeout = 60 * time.Second

	// entityLookupAllowance is the time one entity lookup is budgeted on top
	// of replicationTimeout. Confirming a large write set means one lookup
	// per entity per node, so the wait grows with the work: a CI runner
	// serves about 20 requests a second, and 1000 entities on 3 nodes is
	// 3000 lookups.
	entityLookupAllowance = 75 * time.Millisecond
)

// pollUntil calls check every pollInterval until it reports done, failing the
// test with what and the last error once timeout elapses.
func pollUntil(t *testing.T, timeout time.Duration, what string, check func(context.Context) (bool, error)) {
	t.Helper()
	deadline := time.After(timeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	var lastErr error
	for {
		ctx, cancel := context.WithTimeout(t.Context(), pollRequestTimeout)
		done, err := check(ctx)
		cancel()
		if done {
			return
		}
		if err != nil {
			lastErr = err
		}
		select {
		case <-deadline:
			t.Fatalf("%s: not reached within %s (last error: %v)", what, timeout, lastErr)
		case <-ticker.C:
		}
	}
}

// holdSteady samples check every pollInterval for the whole window and fails
// the test, naming what, at the first sample that reports an error. It proves
// a property holds for a period rather than at one instant.
func holdSteady(t *testing.T, window time.Duration, what string, check func(context.Context) error) {
	t.Helper()
	end := time.After(window)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		ctx, cancel := context.WithTimeout(t.Context(), pollRequestTimeout)
		err := check(ctx)
		cancel()
		if err != nil {
			t.Fatalf("%s: broke during the %s window: %v", what, window, err)
		}
		select {
		case <-end:
			return
		case <-ticker.C:
		}
	}
}

// waitForQuorumReady polls until a majority of nodes report ready, failing the
// test once clusterSettleTimeout elapses. A majority rather than every node
// because the failover targets run with one node deliberately stopped.
func waitForQuorumReady(t *testing.T, nodes []string) {
	t.Helper()
	client := newInsecureHTTPClient(pollRequestTimeout)
	quorum := len(nodes)/2 + 1
	pollUntil(t, clusterSettleTimeout, fmt.Sprintf("%d of %d nodes ready", quorum, len(nodes)),
		func(ctx context.Context) (bool, error) {
			ready := 0
			var lastErr error
			for _, nodeURL := range nodes {
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, nodeURL+"/api/v1/health/ready", nil)
				if err != nil {
					return false, err
				}
				resp, err := client.Do(req)
				if err != nil {
					lastErr = err
					continue
				}
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					ready++
				}
			}
			return ready >= quorum, lastErr
		})
}

// waitForEntitiesOnAllNodes polls every client of cluster until each entity
// is found there, failing the test once replicationTimeout plus an
// entityLookupAllowance for every lookup the sweep needs elapses. Each
// attempt resumes every node where the last one stopped, and each lookup has
// its own entityLookupTimeout. Callers assert the data afterwards.
func waitForEntitiesOnAllNodes(t *testing.T, cluster *helpers.TestCluster, entities []helpers.TestEntity) {
	t.Helper()
	sweep := newEntitySweep(len(cluster.Clients), len(entities))
	lookups := len(cluster.Clients) * len(entities)
	timeout := replicationTimeout + time.Duration(lookups)*entityLookupAllowance
	pollUntil(t, timeout, fmt.Sprintf("%d entities on all %d nodes", len(entities), len(cluster.Clients)),
		func(ctx context.Context) (bool, error) {
			return sweep.advance(ctx, entityLookupTimeout, func(ctx context.Context, node, entity int) (bool, error) {
				resp, err := cluster.Clients[node].KV().Get(ctx, &service.GetRequest{
					EntityType: entities[entity].EntityType,
					ID:         id.HashString(entities[entity].Key),
				})
				if err != nil {
					return false, err
				}
				return resp.Found, nil
			})
		})
}

// clusterHasLeader returns a holdSteady check that fails while cluster has no
// leader.
func clusterHasLeader(cluster *helpers.TestCluster) func(context.Context) error {
	return func(ctx context.Context) error {
		_, _, err := cluster.FindLeader(ctx)
		return err
	}
}

// pace waits d, between retries of a request or for an observation window,
// failing the test if the test context ends first.
func pace(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case <-t.Context().Done():
		t.Fatalf("test context ended while pacing a retry: %v", t.Context().Err())
	case <-time.After(d):
	}
}
