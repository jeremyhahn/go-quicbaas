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

//go:build integration && deployment_proof

package cluster

// The deployment row's 3-node proof: every node runs one go-quicbaas build,
// the nodes form one Raft cluster and serve data, and the cluster survives
// the loss of its leader.
//
// These tests carry the deployment_proof build tag so that no runner
// compiling only the integration tag (make integration-test, the cluster
// suite) picks them up: they need the ordered run and the leader stop that
// only the Makefile target provides.
//
// They run only from `make integration-test-cluster-failover`: it builds the
// node image once with VERSION and the commit compiled in and starts three
// nodes on that one tag. TestDeploymentProof_OneBuildFormsAndServes checks
// the build, the formation and a replicated write, and records the
// platform-shard leader in the file named by QUICBAAS_DEPLOYMENT_PROOF_STATE;
// the Makefile stops that leader's container, and
// TestDeploymentProof_AfterLeaderStop checks the two survivors.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

const (
	deploymentProofStateEnv   = "QUICBAAS_DEPLOYMENT_PROOF_STATE"
	deploymentProofVersionEnv = "QUICBAAS_EXPECTED_VERSION"

	deploymentProofKeyLeaderNodeID = "DEPLOYMENT_PROOF_LEADER_NODE_ID"
	deploymentProofKeyLeaderURL    = "DEPLOYMENT_PROOF_LEADER_URL"
	deploymentProofKeyKey          = "DEPLOYMENT_PROOF_KEY"

	deploymentProofRequestTimeout = 30 * time.Second
)

// dpState is what the formation run hands to the leader-stop run. It is
// written as KEY=value lines so the Makefile can source it.
type dpState struct {
	leaderNodeID uint64
	leaderURL    string
	key          string
}

// dpStatusView is the part of GET /api/v1/cluster/status these tests read.
type dpStatusView struct {
	NodeID uint64 `json:"node_id"`
	Shards []struct {
		ShardID  uint64 `json:"shard_id"`
		LeaderID uint64 `json:"leader_id"`
	} `json:"shards"`
}

// dpVersionView is the part of GET /api/v1/version these tests read, with the
// JSON names of rest.VersionResponse (pkg/api/rest builds only with the rest
// tag, which this proof does not carry).
type dpVersionView struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
}

// TestDeploymentProof_OneBuildFormsAndServes proves that the three nodes run
// one build, form one cluster with an agreed leader, and replicate a write.
func TestDeploymentProof_OneBuildFormsAndServes(t *testing.T) {
	urls := dpNodeURLs(t)
	for _, url := range urls {
		require.True(t, waitReady(t, url, 120*time.Second), "node %s must become ready", url)
	}

	// One build: every node reports the release version and one commit.
	want := os.Getenv(deploymentProofVersionEnv)
	require.NotEmpty(t, want, "%s must be set; run 'make integration-test-cluster-failover'", deploymentProofVersionEnv)
	var commit string
	for _, url := range urls {
		v, err := dpVersion(t.Context(), url)
		require.NoError(t, err, "version of %s", url)
		assert.Equal(t, want, v.Version, "node %s runs another version", url)
		require.NotEmpty(t, v.GitCommit, "node %s reports no commit", url)
		require.NotEqual(t, "unknown", v.GitCommit, "node %s was built without its commit", url)
		if commit == "" {
			commit = v.GitCommit
		}
		assert.Equal(t, commit, v.GitCommit, "node %s runs another commit", url)
	}
	t.Logf("all three nodes run version %s, commit %s", want, commit)

	// Formation: three distinct members and one agreed leader among them.
	ids := make([]uint64, 0, len(urls))
	for _, url := range urls {
		status, err := dpStatus(t.Context(), url)
		require.NoError(t, err, "cluster status of %s", url)
		require.NotZero(t, status.NodeID, "node %s reports no node ID", url)
		require.NotContains(t, ids, status.NodeID, "node %s reports a node ID another node has", url)
		ids = append(ids, status.NodeID)
	}
	formed, _ := dpWaitForLeader(t, urls, 0)
	require.Contains(t, ids, formed, "the leader must be one of the three nodes")

	// Smoke: a write on node1 is read back from the other two.
	key := fmt.Sprintf("deployment-proof-%d", time.Now().UnixNano())
	value := []byte("one build, " + commit)
	kvPut(t, urls[0], key, value)
	for _, url := range urls[1:] {
		waitForKey(t, url, key)
		assert.Equal(t, value, kvGet(t, url, key), "node %s serves another value for %s", url, key)
	}

	statePath := os.Getenv(deploymentProofStateEnv)
	if statePath == "" {
		return
	}
	// Read the leader last, so the node the Makefile stops is the leader when
	// the test ends.
	leaderID, leaderURL := dpWaitForLeader(t, urls, 0)
	dpWriteState(t, statePath, dpState{leaderNodeID: leaderID, leaderURL: leaderURL, key: key})
	t.Logf("recorded platform-shard leader node %d (%s) and key %s in %s", leaderID, leaderURL, key, statePath)
}

// TestDeploymentProof_AfterLeaderStop runs after the Makefile stopped the
// leader the formation test recorded. The two survivors must elect a new
// leader, keep the data written before the stop, accept new writes, and
// still run the one build.
func TestDeploymentProof_AfterLeaderStop(t *testing.T) {
	state := dpReadState(t)

	var survivors []string
	for _, url := range dpNodeURLs(t) {
		if url != state.leaderURL {
			survivors = append(survivors, url)
		}
	}
	require.Len(t, survivors, 2, "the stopped leader %s must be one of the three nodes", state.leaderURL)

	newLeader, newLeaderURL := dpWaitForLeader(t, survivors, state.leaderNodeID)
	t.Logf("node %d (%s) leads the platform shard after node %d was stopped",
		newLeader, newLeaderURL, state.leaderNodeID)

	// The write made before the stop survives on both survivors.
	want := kvGet(t, survivors[0], state.key)
	require.NotNil(t, want, "key %s written before the stop is gone from %s", state.key, survivors[0])
	waitForKey(t, survivors[1], state.key)
	assert.Equal(t, want, kvGet(t, survivors[1], state.key), "the survivors serve different values for %s", state.key)

	// The survivors accept and replicate a new write.
	key := fmt.Sprintf("deployment-proof-after-stop-%d", time.Now().UnixNano())
	value := []byte("after leader stop")
	kvPut(t, survivors[0], key, value)
	waitForKey(t, survivors[1], key)
	assert.Equal(t, value, kvGet(t, survivors[1], key), "node %s serves another value for %s", survivors[1], key)

	version := os.Getenv(deploymentProofVersionEnv)
	require.NotEmpty(t, version, "%s must be set; run 'make integration-test-cluster-failover'", deploymentProofVersionEnv)
	for _, url := range survivors {
		v, err := dpVersion(t.Context(), url)
		require.NoError(t, err, "version of %s", url)
		assert.Equal(t, version, v.Version, "survivor %s runs another version", url)
	}
}

// dpNodeURLs returns the three node URLs. A missing URL fails the test: the
// proof is meaningless without the cluster.
func dpNodeURLs(t *testing.T) []string {
	t.Helper()
	var urls []string
	for _, env := range []string{"QUICBAAS_NODE1_URL", "QUICBAAS_NODE2_URL", "QUICBAAS_NODE3_URL"} {
		url := os.Getenv(env)
		require.NotEmpty(t, url, "%s must be set; run 'make integration-test-cluster-failover'", env)
		urls = append(urls, url)
	}
	return urls
}

// dpGetJSON decodes one GET response from a node into out.
func dpGetJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := newInsecureHTTPClient(deploymentProofRequestTimeout).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// dpStatus reads GET /api/v1/cluster/status from one node.
func dpStatus(ctx context.Context, url string) (*dpStatusView, error) {
	var status dpStatusView
	if err := dpGetJSON(ctx, url+"/api/v1/cluster/status", &status); err != nil {
		return nil, err
	}
	return &status, nil
}

// dpVersion reads GET /api/v1/version from one node.
func dpVersion(ctx context.Context, url string) (*dpVersionView, error) {
	var v dpVersionView
	if err := dpGetJSON(ctx, url+"/api/v1/version", &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// dpWaitForLeader waits until every node names the same platform-shard leader,
// that leader is one of the nodes, and it is not former (0 for none). It
// returns the leader's ID and the URL of the node reporting that ID as its own.
func dpWaitForLeader(t *testing.T, urls []string, former uint64) (leaderID uint64, leaderURL string) {
	t.Helper()
	pollUntil(t, 90*time.Second, "agreed platform-shard leader", func(ctx context.Context) (bool, error) {
		var agreed uint64
		own := make(map[uint64]string, len(urls))
		for _, url := range urls {
			status, err := dpStatus(ctx, url)
			if err != nil {
				return false, err
			}
			own[status.NodeID] = url
			var id uint64
			for _, shard := range status.Shards {
				if shard.ShardID == shards.PlatformShardID {
					id = shard.LeaderID
				}
			}
			if id == 0 || id == former || (agreed != 0 && id != agreed) {
				return false, nil
			}
			agreed = id
		}
		url, ok := own[agreed]
		if !ok {
			return false, nil
		}
		leaderID, leaderURL = agreed, url
		return true, nil
	})
	return leaderID, leaderURL
}

// dpWriteState records the leader and the key for the leader-stop run.
func dpWriteState(t *testing.T, path string, state dpState) {
	t.Helper()
	content := fmt.Sprintf("%s=%d\n%s=%s\n%s=%s\n",
		deploymentProofKeyLeaderNodeID, state.leaderNodeID,
		deploymentProofKeyLeaderURL, state.leaderURL,
		deploymentProofKeyKey, state.key)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600), "write %s", path)
}

// dpReadState reads the state the formation run recorded. It fails the test
// when there is none: this test only has meaning after that run and a leader
// stop.
func dpReadState(t *testing.T) dpState {
	t.Helper()
	path := os.Getenv(deploymentProofStateEnv)
	require.NotEmpty(t, path, "%s must name the formation record; run 'make integration-test-cluster-failover'",
		deploymentProofStateEnv)
	file, err := os.Open(path)
	require.NoError(t, err, "open %s", path)
	defer file.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if key, value, ok := strings.Cut(scanner.Text(), "="); ok {
			values[key] = value
		}
	}
	require.NoError(t, scanner.Err(), "read %s", path)

	leaderNodeID, err := strconv.ParseUint(values[deploymentProofKeyLeaderNodeID], 10, 64)
	require.NoError(t, err, "%s in %s", deploymentProofKeyLeaderNodeID, path)
	for _, key := range []string{deploymentProofKeyLeaderURL, deploymentProofKeyKey} {
		require.NotEmpty(t, values[key], "%s in %s", key, path)
	}
	require.NotZero(t, leaderNodeID, "%s in %s", deploymentProofKeyLeaderNodeID, path)

	return dpState{
		leaderNodeID: leaderNodeID,
		leaderURL:    values[deploymentProofKeyLeaderURL],
		key:          values[deploymentProofKeyKey],
	}
}
