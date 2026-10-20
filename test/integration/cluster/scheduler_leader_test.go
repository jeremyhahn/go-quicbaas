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

//go:build integration && scheduler_proof

package cluster

// Leader-gated scheduler firing on a 3-node cluster (#105 item 2).
//
// These tests carry the scheduler_proof build tag so that no runner
// compiling only the integration tag (make integration-test, the cluster
// suite) picks them up: they need the ordered run and the leader stop that
// only the Makefile target provides.
//
// A cluster-wide job (empty NodeAffinity) must fire exactly once per
// scheduled tick across the whole cluster, before and after the platform
// shard leader is lost. Firings are counted from the replicated run records
// (GetJobHistory), never from logs.
//
// The leader-stop case needs a node stopped between two test runs, so it runs
// only from `make integration-test-cluster-scheduler`: the steady-state test
// records the job and the leader in the file named by
// QUICBAAS_SCHEDULER_PROOF_STATE, the Makefile stops that leader's container,
// and TestSchedulerLeaderFiring_AfterLeaderStop checks the survivors.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/handlers"
	"github.com/jeremyhahn/go-quicbaas/pkg/shards"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

const (
	// schedulerProofPeriod is the period of every job these tests create.
	schedulerProofPeriod   = 4 * time.Second
	schedulerProofSchedule = "@every 4s"

	// schedulerMinGap is the smallest gap two distinct firings can have. The
	// next firing is due one period after the previous run completes, rounded
	// down to the whole second by the cron library, so consecutive firings
	// start more than period-1s apart. Two executions closer than that are one
	// firing run twice.
	schedulerMinGap = schedulerProofPeriod - time.Second

	// schedulerMaxGap is the largest gap two consecutive firings have while a
	// leader is stable: due one period after the previous run, picked up by
	// the next 1s scheduler tick. A larger gap means a firing was skipped.
	schedulerMaxGap = 2 * schedulerProofPeriod

	// schedulerResumeWithin bounds how long after the new leader is observed
	// the job must fire again.
	schedulerResumeWithin = 3 * schedulerProofPeriod

	schedulerSteadyExecutions = 5
	schedulerResumeExecutions = 3
	schedulerHistoryLimit     = 500
	schedulerRequestTimeout   = 10 * time.Second

	schedulerProofStateEnv = "QUICBAAS_SCHEDULER_PROOF_STATE"

	stateKeyJobID        = "SCHEDULER_PROOF_JOB_ID"
	stateKeyLeaderNodeID = "SCHEDULER_PROOF_LEADER_NODE_ID"
	stateKeyLeaderURL    = "SCHEDULER_PROOF_LEADER_URL"
)

// schedulerNode is one cluster node: its REST URL, its Raft node ID as the
// node itself reports it, and an SDK client bound to it.
type schedulerNode struct {
	url    string
	nodeID uint64
	client quicbaas.Client
}

// schedulerProofState is what the steady-state run hands to the leader-stop
// run. It is written as KEY=value lines so the Makefile can source it.
type schedulerProofState struct {
	jobID        uint64
	leaderNodeID uint64
	leaderURL    string
}

// clusterStatusView is the part of GET /api/v1/cluster/status these tests read.
type clusterStatusView struct {
	NodeID uint64 `json:"node_id"`
	Shards []struct {
		ShardID  uint64 `json:"shard_id"`
		LeaderID uint64 `json:"leader_id"`
	} `json:"shards"`
}

// clusterStatusHTTPError is a cluster status read a node answered with a
// non-OK status.
type clusterStatusHTTPError struct {
	url        string
	statusCode int
}

func (e *clusterStatusHTTPError) Error() string {
	return fmt.Sprintf("cluster status of %s: HTTP %d", e.url, e.statusCode)
}

// historyTruncatedError is a run history longer than one page; comparing a
// partial history across nodes would prove nothing.
type historyTruncatedError struct {
	jobID uint64
	limit int
}

func (e *historyTruncatedError) Error() string {
	return fmt.Sprintf("job %d has more than %d runs", e.jobID, e.limit)
}

// TestSchedulerLeaderFiring_SteadyState proves that on a stable 3-node cluster
// a cluster-wide recurring job runs once per firing, and that every node
// serves the same run history.
func TestSchedulerLeaderFiring_SteadyState(t *testing.T) {
	nodes := connectSchedulerNodes(t, clusterNodeURLs(t))
	waitForPlatformLeader(t, nodes, 0)

	jobID := createRecurringJob(t, nodes[0], "leader-firing", "")
	statePath := os.Getenv(schedulerProofStateEnv)
	if statePath == "" {
		// Not part of the leader-stop run: nothing needs the job afterwards.
		t.Cleanup(func() { deleteJob(t, nodes[0], jobID) })
	}

	execs := waitForSharedHistory(t, nodes, jobID,
		schedulerSteadyExecutions*schedulerMaxGap+30*time.Second,
		func(execs []scheduler.JobExecution) bool { return len(execs) >= schedulerSteadyExecutions })

	assertOneExecutionPerFiring(t, execs, jobID)
	assertNoMissedFiring(t, execs)

	if statePath == "" {
		return
	}
	// Read the leader last, so the node the Makefile stops is the one
	// running the job when the test ends.
	leader := waitForPlatformLeader(t, nodes, 0)
	writeSchedulerProofState(t, statePath, schedulerProofState{
		jobID:        jobID,
		leaderNodeID: leader.nodeID,
		leaderURL:    leader.url,
	})
	t.Logf("recorded job %d and platform-shard leader node %d (%s) in %s",
		jobID, leader.nodeID, leader.url, statePath)
}

// TestSchedulerLeaderFiring_NodeAffinity is the control: a job pinned to one
// follower runs on that follower, whoever leads, and the other follower runs
// nothing at all. Run history does not carry the executing node, so the
// executing node is read from each node's own success counter.
func TestSchedulerLeaderFiring_NodeAffinity(t *testing.T) {
	nodes := connectSchedulerNodes(t, clusterNodeURLs(t))
	leader := waitForPlatformLeader(t, nodes, 0)

	var followers []schedulerNode
	for _, node := range nodes {
		if node.nodeID != leader.nodeID {
			followers = append(followers, node)
		}
	}
	require.Len(t, followers, 2, "a 3-node cluster has two followers")
	pinned, idle := followers[0], followers[1]

	before := make(map[uint64]uint64, len(nodes))
	for _, node := range nodes {
		before[node.nodeID] = completedRuns(t, node)
	}

	affinity := strconv.FormatUint(pinned.nodeID, 10)
	jobID := createRecurringJob(t, nodes[0], "affinity-control", affinity)
	t.Cleanup(func() { deleteJob(t, nodes[0], jobID) })

	execs := waitForSharedHistory(t, nodes, jobID,
		schedulerResumeExecutions*schedulerMaxGap+30*time.Second,
		func(execs []scheduler.JobExecution) bool { return len(execs) >= schedulerResumeExecutions })
	assertOneExecutionPerFiring(t, execs, jobID)

	// A run is recorded before the node's counter moves, so wait for the
	// pinned node to account for every run the history shows.
	pollUntil(t, 30*time.Second, "pinned node accounts for every run", func(ctx context.Context) (bool, error) {
		status, err := pinned.client.Scheduler().GetStatus(ctx)
		if err != nil {
			return false, err
		}
		return status.CompletedJobs-before[pinned.nodeID] >= uint64(len(execs)), nil
	})

	require.Equal(t, leader.nodeID, waitForPlatformLeader(t, nodes, 0).nodeID,
		"platform leadership moved during the affinity window; the idle-follower check needs a stable leader")
	assert.Equal(t, before[idle.nodeID], completedRuns(t, idle),
		"follower %d has no pinned job and is not leader, so it must not run anything", idle.nodeID)
}

// TestSchedulerLeaderFiring_AfterLeaderStop runs after the Makefile stopped
// the leader recorded by the steady-state test. On the two survivors the job
// must resume under the new leader, and no firing, including those spanning
// the election, may run twice.
func TestSchedulerLeaderFiring_AfterLeaderStop(t *testing.T) {
	state := readSchedulerProofState(t)

	var survivors []string
	for _, url := range clusterNodeURLs(t) {
		if url != state.leaderURL {
			survivors = append(survivors, url)
		}
	}
	require.Len(t, survivors, 2, "the stopped leader %s must be one of the three nodes", state.leaderURL)

	nodes := connectSchedulerNodes(t, survivors)
	t.Cleanup(func() { deleteJob(t, nodes[0], state.jobID) })

	newLeader := waitForPlatformLeader(t, nodes, state.leaderNodeID)
	elected := time.Now()
	t.Logf("node %d leads the platform shard after node %d was stopped", newLeader.nodeID, state.leaderNodeID)

	// A firing claimed by the old leader just before it stopped is stranded
	// rather than retried (docs/18 V4 known window); a timeout here with the
	// job stuck in that firing is that window, not a double fire.
	execs := waitForSharedHistory(t, nodes, state.jobID,
		schedulerResumeWithin+schedulerResumeExecutions*schedulerMaxGap,
		func(execs []scheduler.JobExecution) bool {
			return len(executionsSince(execs, elected)) >= schedulerResumeExecutions
		})

	// The whole history: before the stop, across the election, and after.
	assertOneExecutionPerFiring(t, execs, state.jobID)

	resumed := executionsSince(execs, elected)
	assert.LessOrEqual(t, resumed[0].StartedAt.Sub(elected), schedulerResumeWithin,
		"the job must fire again within %s of the new leader", schedulerResumeWithin)
	assertNoMissedFiring(t, resumed)
}

// clusterNodeURLs returns the three node URLs. A missing URL fails the test:
// the proof is meaningless without the cluster.
func clusterNodeURLs(t *testing.T) []string {
	t.Helper()
	var urls []string
	for _, env := range []string{"QUICBAAS_NODE1_URL", "QUICBAAS_NODE2_URL", "QUICBAAS_NODE3_URL"} {
		url := os.Getenv(env)
		require.NotEmpty(t, url, "%s must be set; run 'make integration-test-cluster-scheduler'", env)
		urls = append(urls, url)
	}
	return urls
}

// connectSchedulerNodes waits for each node to be ready, learns its node ID
// from the node itself, and opens an SDK client over REST to it.
func connectSchedulerNodes(t *testing.T, urls []string) []schedulerNode {
	t.Helper()
	opts := []quicbaas.Option{quicbaas.WithREST(), quicbaas.WithTimeout(schedulerRequestTimeout)}
	if tlsCfg, err := helpers.IntegrationTLSConfig(); err == nil {
		opts = append(opts, quicbaas.WithTLSConfig(tlsCfg))
	}

	nodes := make([]schedulerNode, 0, len(urls))
	for _, url := range urls {
		require.True(t, waitReady(t, url, 120*time.Second), "node %s must become ready", url)
		status, err := clusterStatus(t.Context(), url)
		require.NoError(t, err, "cluster status of %s", url)

		client, err := quicbaas.NewClient(url, opts...)
		require.NoError(t, err, "SDK client for %s", url)
		t.Cleanup(func() {
			if err := client.Close(); err != nil {
				t.Logf("closing SDK client for %s: %v", url, err)
			}
		})
		nodes = append(nodes, schedulerNode{url: url, nodeID: status.NodeID, client: client})
	}
	return nodes
}

// clusterStatus reads GET /api/v1/cluster/status from one node.
func clusterStatus(ctx context.Context, url string) (*clusterStatusView, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/v1/cluster/status", nil)
	if err != nil {
		return nil, err
	}
	resp, err := newInsecureHTTPClient(schedulerRequestTimeout).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &clusterStatusHTTPError{url: url, statusCode: resp.StatusCode}
	}
	var status clusterStatusView
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, err
	}
	return &status, nil
}

// waitForPlatformLeader waits until every node names the same platform-shard
// leader, that leader is one of nodes, and it is not formerLeader (0 for
// none). It returns the leader.
func waitForPlatformLeader(t *testing.T, nodes []schedulerNode, formerLeader uint64) schedulerNode {
	t.Helper()
	var leader schedulerNode
	pollUntil(t, 90*time.Second, "agreed platform-shard leader", func(ctx context.Context) (bool, error) {
		var agreed uint64
		for _, node := range nodes {
			status, err := clusterStatus(ctx, node.url)
			if err != nil {
				return false, err
			}
			var id uint64
			for _, shard := range status.Shards {
				if shard.ShardID == shards.PlatformShardID {
					id = shard.LeaderID
				}
			}
			if id == 0 || id == formerLeader || (agreed != 0 && id != agreed) {
				return false, nil
			}
			agreed = id
		}
		for _, node := range nodes {
			if node.nodeID == agreed {
				leader = node
				return true, nil
			}
		}
		return false, nil
	})
	return leader
}

// createRecurringJob creates a system-owned recurring job on the proof
// schedule. An empty affinity makes it cluster-wide.
func createRecurringJob(t *testing.T, node schedulerNode, name, affinity string) uint64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), schedulerRequestTimeout)
	defer cancel()
	resp, err := node.client.Scheduler().CreateJob(ctx, &scheduler.CreateJobRequest{
		Name:         fmt.Sprintf("%s-%d", name, time.Now().UnixNano()),
		JobType:      scheduler.JobTypeRecurring,
		Handler:      handlers.HandlerCron,
		Schedule:     schedulerProofSchedule,
		OwnerType:    scheduler.OwnerTypeSystem,
		NodeAffinity: affinity,
	})
	require.NoError(t, err, "create job %s on %s", name, node.url)
	t.Logf("created job %d (%s, affinity %q) via %s", resp.JobID, name, affinity, node.url)
	return resp.JobID
}

// deleteJob removes a job at cleanup. A failure is logged, not fatal: the
// stack is torn down after the run.
func deleteJob(t *testing.T, node schedulerNode, jobID uint64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), schedulerRequestTimeout)
	defer cancel()
	if err := node.client.Scheduler().DeleteJob(ctx, jobID); err != nil {
		t.Logf("deleting job %d via %s: %v", jobID, node.url, err)
	}
}

// completedRuns returns how many job runs this node itself has completed.
func completedRuns(t *testing.T, node schedulerNode) uint64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), schedulerRequestTimeout)
	defer cancel()
	status, err := node.client.Scheduler().GetStatus(ctx)
	require.NoError(t, err, "scheduler status of node %d", node.nodeID)
	return status.CompletedJobs
}

// jobHistory returns a job's run records as one node serves them, oldest
// first.
func jobHistory(ctx context.Context, node schedulerNode, jobID uint64) ([]scheduler.JobExecution, error) {
	resp, err := node.client.Scheduler().GetJobHistory(ctx, jobID, &scheduler.HistoryOptions{Limit: schedulerHistoryLimit})
	if err != nil {
		return nil, err
	}
	if resp.HasMore {
		return nil, &historyTruncatedError{jobID: jobID, limit: schedulerHistoryLimit}
	}
	execs := slices.Clone(resp.Executions)
	slices.SortFunc(execs, func(a, b scheduler.JobExecution) int { return a.StartedAt.Compare(b.StartedAt) })
	return execs, nil
}

// waitForSharedHistory polls every node until they all serve the same run
// history for the job and that history satisfies done. It returns it.
func waitForSharedHistory(t *testing.T, nodes []schedulerNode, jobID uint64, timeout time.Duration,
	done func([]scheduler.JobExecution) bool) []scheduler.JobExecution {
	t.Helper()
	var shared []scheduler.JobExecution
	pollUntil(t, timeout, fmt.Sprintf("shared run history of job %d", jobID), func(ctx context.Context) (bool, error) {
		var first []scheduler.JobExecution
		for i, node := range nodes {
			execs, err := jobHistory(ctx, node, jobID)
			if err != nil {
				return false, err
			}
			if i == 0 {
				first = execs
				continue
			}
			if !sameExecutions(first, execs) {
				return false, nil
			}
		}
		if !done(first) {
			return false, nil
		}
		shared = first
		return true, nil
	})
	return shared
}

// sameExecutions reports whether two histories hold the same runs.
func sameExecutions(a, b []scheduler.JobExecution) bool {
	return slices.EqualFunc(a, b, func(x, y scheduler.JobExecution) bool {
		return x.ExecutionID == y.ExecutionID && x.StartedAt.Equal(y.StartedAt)
	})
}

// executionsSince returns the runs that started after since.
func executionsSince(execs []scheduler.JobExecution, since time.Time) []scheduler.JobExecution {
	start := slices.IndexFunc(execs, func(e scheduler.JobExecution) bool { return e.StartedAt.After(since) })
	if start < 0 {
		return nil
	}
	return execs[start:]
}

// assertOneExecutionPerFiring checks that every run belongs to the job, no
// run is recorded twice, and no two runs fall inside one firing.
func assertOneExecutionPerFiring(t *testing.T, execs []scheduler.JobExecution, jobID uint64) {
	t.Helper()
	seen := make(map[uint64]bool, len(execs))
	for i, exec := range execs {
		assert.Equal(t, jobID, exec.JobID, "run %d belongs to another job", exec.ExecutionID)
		assert.False(t, seen[exec.ExecutionID], "run %d is recorded twice", exec.ExecutionID)
		seen[exec.ExecutionID] = true
		if i == 0 {
			continue
		}
		gap := exec.StartedAt.Sub(execs[i-1].StartedAt)
		assert.Greater(t, gap, schedulerMinGap,
			"runs %d and %d started %s apart: one firing ran twice", execs[i-1].ExecutionID, exec.ExecutionID, gap)
	}
}

// assertNoMissedFiring checks that consecutive runs are at most one firing
// apart, so the run count equals the number of firings in the window.
func assertNoMissedFiring(t *testing.T, execs []scheduler.JobExecution) {
	t.Helper()
	for i := 1; i < len(execs); i++ {
		gap := execs[i].StartedAt.Sub(execs[i-1].StartedAt)
		assert.LessOrEqual(t, gap, schedulerMaxGap,
			"runs %d and %d started %s apart: a firing was skipped", execs[i-1].ExecutionID, execs[i].ExecutionID, gap)
	}
}

// writeSchedulerProofState records the job and leader for the leader-stop run.
func writeSchedulerProofState(t *testing.T, path string, state schedulerProofState) {
	t.Helper()
	content := fmt.Sprintf("%s=%d\n%s=%d\n%s=%s\n",
		stateKeyJobID, state.jobID,
		stateKeyLeaderNodeID, state.leaderNodeID,
		stateKeyLeaderURL, state.leaderURL)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600), "write %s", path)
}

// readSchedulerProofState reads the state the steady-state run recorded. It
// fails the test when there is none: this test only has meaning after that
// run and a leader stop.
func readSchedulerProofState(t *testing.T) schedulerProofState {
	t.Helper()
	path := os.Getenv(schedulerProofStateEnv)
	require.NotEmpty(t, path, "%s must name the steady-state record; run 'make integration-test-cluster-scheduler'",
		schedulerProofStateEnv)
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

	jobID, err := strconv.ParseUint(values[stateKeyJobID], 10, 64)
	require.NoError(t, err, "%s in %s", stateKeyJobID, path)
	leaderNodeID, err := strconv.ParseUint(values[stateKeyLeaderNodeID], 10, 64)
	require.NoError(t, err, "%s in %s", stateKeyLeaderNodeID, path)
	require.NotEmpty(t, values[stateKeyLeaderURL], "%s in %s", stateKeyLeaderURL, path)

	return schedulerProofState{jobID: jobID, leaderNodeID: leaderNodeID, leaderURL: values[stateKeyLeaderURL]}
}
