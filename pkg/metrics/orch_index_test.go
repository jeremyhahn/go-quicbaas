// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// histogramObservation extracts the sample count and sum for a histogram
// metric matching the supplied labels. It returns ok=false when no series
// with the given labels is present in the collector output.
func histogramObservation(t *testing.T, c prometheus.Collector, want prometheus.Labels) (count uint64, sum float64, ok bool) {
	t.Helper()

	ch := make(chan prometheus.Metric, 64)
	go func() {
		c.Collect(ch)
		close(ch)
	}()

	for metric := range ch {
		var pb dto.Metric
		require.NoError(t, metric.Write(&pb))

		if pb.Histogram == nil {
			continue
		}
		if labelsMatch(pb.GetLabel(), want) {
			return pb.Histogram.GetSampleCount(), pb.Histogram.GetSampleSum(), true
		}
	}
	return 0, 0, false
}

// labelsMatch reports whether the protobuf label pairs contain every label in
// want with the matching value.
func labelsMatch(pairs []*dto.LabelPair, want prometheus.Labels) bool {
	got := make(map[string]string, len(pairs))
	for _, p := range pairs {
		got[p.GetName()] = p.GetValue()
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}

func TestRecordOrchPeerMessage(t *testing.T) {
	Reset()
	m := InitMetrics()

	tests := []struct {
		name   string
		action string
		tenant string
		times  int
	}{
		{name: "single send", action: "send", tenant: "tenant-1", times: 1},
		{name: "multiple receive", action: "receive", tenant: "tenant-2", times: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < tt.times; i++ {
				m.RecordOrchPeerMessage(tt.action, tt.tenant)
			}

			labels := prometheus.Labels{"action": tt.action, "tenant": tt.tenant}
			value := testutil.ToFloat64(m.OrchPeerMessagesTotal.With(labels))
			assert.Equal(t, float64(tt.times), value, "OrchPeerMessagesTotal should match increment count")
		})
	}
}

func TestRecordOrchPeerMessage_IndependentLabels(t *testing.T) {
	Reset()
	m := InitMetrics()

	m.RecordOrchPeerMessage("send", "tenant-a")
	m.RecordOrchPeerMessage("receive", "tenant-a")
	m.RecordOrchPeerMessage("send", "tenant-b")

	send := testutil.ToFloat64(m.OrchPeerMessagesTotal.With(prometheus.Labels{"action": "send", "tenant": "tenant-a"}))
	recv := testutil.ToFloat64(m.OrchPeerMessagesTotal.With(prometheus.Labels{"action": "receive", "tenant": "tenant-a"}))
	other := testutil.ToFloat64(m.OrchPeerMessagesTotal.With(prometheus.Labels{"action": "send", "tenant": "tenant-b"}))

	assert.Equal(t, 1.0, send, "send/tenant-a counter must be isolated")
	assert.Equal(t, 1.0, recv, "receive/tenant-a counter must be isolated")
	assert.Equal(t, 1.0, other, "send/tenant-b counter must be isolated")
}

func TestSetOrchPeerInboxSize(t *testing.T) {
	Reset()
	m := InitMetrics()

	tests := []struct {
		name    string
		agentID string
		tenant  string
		size    float64
	}{
		{name: "empty inbox", agentID: "agent-1", tenant: "tenant-1", size: 0},
		{name: "queued messages", agentID: "agent-2", tenant: "tenant-1", size: 42},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.SetOrchPeerInboxSize(tt.agentID, tt.tenant, tt.size)

			labels := prometheus.Labels{"agent_id": tt.agentID, "tenant": tt.tenant}
			value := testutil.ToFloat64(m.OrchPeerInboxSize.With(labels))
			assert.Equal(t, tt.size, value, "OrchPeerInboxSize should match")
		})
	}
}

func TestSetOrchPeerInboxSize_Overwrite(t *testing.T) {
	Reset()
	m := InitMetrics()

	labels := prometheus.Labels{"agent_id": "agent-1", "tenant": "tenant-1"}
	m.SetOrchPeerInboxSize("agent-1", "tenant-1", 10)
	m.SetOrchPeerInboxSize("agent-1", "tenant-1", 3)

	value := testutil.ToFloat64(m.OrchPeerInboxSize.With(labels))
	assert.Equal(t, 3.0, value, "gauge set must overwrite, not accumulate")
}

func TestSetOrchFileLocksActive(t *testing.T) {
	Reset()
	m := InitMetrics()

	tests := []struct {
		name      string
		projectID string
		tenant    string
		count     float64
	}{
		{name: "no locks", projectID: "proj-1", tenant: "tenant-1", count: 0},
		{name: "several locks", projectID: "proj-2", tenant: "tenant-2", count: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.SetOrchFileLocksActive(tt.projectID, tt.tenant, tt.count)

			labels := prometheus.Labels{"project_id": tt.projectID, "tenant": tt.tenant}
			value := testutil.ToFloat64(m.OrchFileLocksActive.With(labels))
			assert.Equal(t, tt.count, value, "OrchFileLocksActive should match")
		})
	}
}

func TestSetOrchFileLocksActive_Decrement(t *testing.T) {
	Reset()
	m := InitMetrics()

	labels := prometheus.Labels{"project_id": "proj-1", "tenant": "tenant-1"}
	m.SetOrchFileLocksActive("proj-1", "tenant-1", 5)
	m.SetOrchFileLocksActive("proj-1", "tenant-1", 2)

	value := testutil.ToFloat64(m.OrchFileLocksActive.With(labels))
	assert.Equal(t, 2.0, value, "gauge must reflect last set value when locks are released")
}

func TestRecordOrchFileLockWaitDuration(t *testing.T) {
	Reset()
	m := InitMetrics()

	tests := []struct {
		name      string
		projectID string
		duration  float64
	}{
		{name: "fast acquire", projectID: "proj-1", duration: 0.005},
		{name: "slow acquire", projectID: "proj-2", duration: 2.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.RecordOrchFileLockWaitDuration(tt.projectID, tt.duration)

			count, sum, ok := histogramObservation(t, m.OrchFileLockWaitDuration, prometheus.Labels{"project_id": tt.projectID})
			require.True(t, ok, "observation for project should be present")
			assert.Equal(t, uint64(1), count, "exactly one observation expected")
			assert.InDelta(t, tt.duration, sum, 1e-9, "recorded sum should equal observed duration")
		})
	}
}

func TestRecordOrchFileLockWaitDuration_Accumulates(t *testing.T) {
	Reset()
	m := InitMetrics()

	m.RecordOrchFileLockWaitDuration("proj-1", 0.1)
	m.RecordOrchFileLockWaitDuration("proj-1", 0.4)

	count, sum, ok := histogramObservation(t, m.OrchFileLockWaitDuration, prometheus.Labels{"project_id": "proj-1"})
	require.True(t, ok)
	assert.Equal(t, uint64(2), count, "two observations expected for same project")
	assert.InDelta(t, 0.5, sum, 1e-9, "sum should accumulate observed durations")
}

func TestRecordOrchAgentTokens(t *testing.T) {
	Reset()
	m := InitMetrics()

	tests := []struct {
		name    string
		agentID string
		jobID   string
		tenant  string
		tokens  float64
	}{
		{name: "prompt tokens", agentID: "agent-1", jobID: "job-1", tenant: "tenant-1", tokens: 1500},
		{name: "completion tokens", agentID: "agent-2", jobID: "job-2", tenant: "tenant-2", tokens: 320},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.RecordOrchAgentTokens(tt.agentID, tt.jobID, tt.tenant, tt.tokens)

			labels := prometheus.Labels{"agent_id": tt.agentID, "job_id": tt.jobID, "tenant": tt.tenant}
			value := testutil.ToFloat64(m.OrchAgentTokensTotal.With(labels))
			assert.Equal(t, tt.tokens, value, "OrchAgentTokensTotal should match added tokens")
		})
	}
}

func TestRecordOrchAgentTokens_Accumulates(t *testing.T) {
	Reset()
	m := InitMetrics()

	labels := prometheus.Labels{"agent_id": "agent-1", "job_id": "job-1", "tenant": "tenant-1"}
	m.RecordOrchAgentTokens("agent-1", "job-1", "tenant-1", 100)
	m.RecordOrchAgentTokens("agent-1", "job-1", "tenant-1", 250)

	value := testutil.ToFloat64(m.OrchAgentTokensTotal.With(labels))
	assert.Equal(t, 350.0, value, "token counter must accumulate across calls")
}

func TestSetOrchJobBudgetUtilization(t *testing.T) {
	Reset()
	m := InitMetrics()

	tests := []struct {
		name   string
		jobID  string
		tenant string
		ratio  float64
	}{
		{name: "half consumed", jobID: "job-1", tenant: "tenant-1", ratio: 0.5},
		{name: "fully consumed", jobID: "job-2", tenant: "tenant-2", ratio: 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.SetOrchJobBudgetUtilization(tt.jobID, tt.tenant, tt.ratio)

			labels := prometheus.Labels{"job_id": tt.jobID, "tenant": tt.tenant}
			value := testutil.ToFloat64(m.OrchJobBudgetUtilization.With(labels))
			assert.Equal(t, tt.ratio, value, "OrchJobBudgetUtilization should match")
		})
	}
}

func TestSetOrchJobBudgetUtilization_Update(t *testing.T) {
	Reset()
	m := InitMetrics()

	labels := prometheus.Labels{"job_id": "job-1", "tenant": "tenant-1"}
	m.SetOrchJobBudgetUtilization("job-1", "tenant-1", 0.25)
	m.SetOrchJobBudgetUtilization("job-1", "tenant-1", 0.9)

	value := testutil.ToFloat64(m.OrchJobBudgetUtilization.With(labels))
	assert.Equal(t, 0.9, value, "utilization gauge must reflect latest ratio")
}

func TestRecordOrchAgentRestart(t *testing.T) {
	Reset()
	m := InitMetrics()

	tests := []struct {
		name    string
		agentID string
		tenant  string
		times   int
	}{
		{name: "single restart", agentID: "agent-1", tenant: "tenant-1", times: 1},
		{name: "crash loop", agentID: "agent-2", tenant: "tenant-2", times: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < tt.times; i++ {
				m.RecordOrchAgentRestart(tt.agentID, tt.tenant)
			}

			labels := prometheus.Labels{"agent_id": tt.agentID, "tenant": tt.tenant}
			value := testutil.ToFloat64(m.OrchAgentRestarts.With(labels))
			assert.Equal(t, float64(tt.times), value, "OrchAgentRestarts should match increment count")
		})
	}
}

func TestRecordOrchAgentRestart_IndependentLabels(t *testing.T) {
	Reset()
	m := InitMetrics()

	m.RecordOrchAgentRestart("agent-1", "tenant-1")
	m.RecordOrchAgentRestart("agent-2", "tenant-1")

	a1 := testutil.ToFloat64(m.OrchAgentRestarts.With(prometheus.Labels{"agent_id": "agent-1", "tenant": "tenant-1"}))
	a2 := testutil.ToFloat64(m.OrchAgentRestarts.With(prometheus.Labels{"agent_id": "agent-2", "tenant": "tenant-1"}))

	assert.Equal(t, 1.0, a1, "agent-1 restart counter must be isolated")
	assert.Equal(t, 1.0, a2, "agent-2 restart counter must be isolated")
}

func TestRecordIndexFileIndexed(t *testing.T) {
	Reset()
	m := InitMetrics()

	tests := []struct {
		name      string
		projectID string
		language  string
		times     int
	}{
		{name: "single go file", projectID: "proj-1", language: "go", times: 1},
		{name: "many python files", projectID: "proj-2", language: "python", times: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < tt.times; i++ {
				m.RecordIndexFileIndexed(tt.projectID, tt.language)
			}

			labels := prometheus.Labels{"project_id": tt.projectID, "language": tt.language}
			value := testutil.ToFloat64(m.IndexFilesIndexedTotal.With(labels))
			assert.Equal(t, float64(tt.times), value, "IndexFilesIndexedTotal should match increment count")
		})
	}
}

func TestRecordIndexFileIndexed_IndependentLabels(t *testing.T) {
	Reset()
	m := InitMetrics()

	m.RecordIndexFileIndexed("proj-1", "go")
	m.RecordIndexFileIndexed("proj-1", "rust")

	goFiles := testutil.ToFloat64(m.IndexFilesIndexedTotal.With(prometheus.Labels{"project_id": "proj-1", "language": "go"}))
	rustFiles := testutil.ToFloat64(m.IndexFilesIndexedTotal.With(prometheus.Labels{"project_id": "proj-1", "language": "rust"}))

	assert.Equal(t, 1.0, goFiles, "go file counter must be isolated by language")
	assert.Equal(t, 1.0, rustFiles, "rust file counter must be isolated by language")
}

func TestSetIndexVectorsTotal(t *testing.T) {
	Reset()
	m := InitMetrics()

	tests := []struct {
		name      string
		projectID string
		backend   string
		count     float64
	}{
		{name: "empty index", projectID: "proj-1", backend: "qrdb", count: 0},
		{name: "populated index", projectID: "proj-2", backend: "memory", count: 25000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.SetIndexVectorsTotal(tt.projectID, tt.backend, tt.count)

			labels := prometheus.Labels{"project_id": tt.projectID, "backend": tt.backend}
			value := testutil.ToFloat64(m.IndexVectorsTotal.With(labels))
			assert.Equal(t, tt.count, value, "IndexVectorsTotal should match")
		})
	}
}

func TestSetIndexVectorsTotal_Update(t *testing.T) {
	Reset()
	m := InitMetrics()

	labels := prometheus.Labels{"project_id": "proj-1", "backend": "qrdb"}
	m.SetIndexVectorsTotal("proj-1", "qrdb", 100)
	m.SetIndexVectorsTotal("proj-1", "qrdb", 250)

	value := testutil.ToFloat64(m.IndexVectorsTotal.With(labels))
	assert.Equal(t, 250.0, value, "vectors total gauge must reflect latest count")
}

func TestRecordIndexEmbedDuration(t *testing.T) {
	Reset()
	m := InitMetrics()

	tests := []struct {
		name     string
		provider string
		duration float64
	}{
		{name: "local provider", provider: "ollama", duration: 0.05},
		{name: "remote provider", provider: "openai", duration: 1.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.RecordIndexEmbedDuration(tt.provider, tt.duration)

			count, sum, ok := histogramObservation(t, m.IndexEmbedDuration, prometheus.Labels{"provider": tt.provider})
			require.True(t, ok, "observation for provider should be present")
			assert.Equal(t, uint64(1), count, "exactly one observation expected")
			assert.InDelta(t, tt.duration, sum, 1e-9, "recorded sum should equal observed duration")
		})
	}
}

func TestRecordIndexEmbedDuration_Accumulates(t *testing.T) {
	Reset()
	m := InitMetrics()

	m.RecordIndexEmbedDuration("ollama", 0.2)
	m.RecordIndexEmbedDuration("ollama", 0.3)

	count, sum, ok := histogramObservation(t, m.IndexEmbedDuration, prometheus.Labels{"provider": "ollama"})
	require.True(t, ok)
	assert.Equal(t, uint64(2), count, "two observations expected for same provider")
	assert.InDelta(t, 0.5, sum, 1e-9, "sum should accumulate embedding durations")
}

func TestRecordIndexChunkDuration(t *testing.T) {
	Reset()
	m := InitMetrics()

	tests := []struct {
		name     string
		language string
		duration float64
	}{
		{name: "go chunking", language: "go", duration: 0.005},
		{name: "markdown chunking", language: "markdown", duration: 0.1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.RecordIndexChunkDuration(tt.language, tt.duration)

			count, sum, ok := histogramObservation(t, m.IndexChunkDuration, prometheus.Labels{"language": tt.language})
			require.True(t, ok, "observation for language should be present")
			assert.Equal(t, uint64(1), count, "exactly one observation expected")
			assert.InDelta(t, tt.duration, sum, 1e-9, "recorded sum should equal observed duration")
		})
	}
}

func TestRecordIndexChunkDuration_Accumulates(t *testing.T) {
	Reset()
	m := InitMetrics()

	m.RecordIndexChunkDuration("go", 0.01)
	m.RecordIndexChunkDuration("go", 0.04)

	count, sum, ok := histogramObservation(t, m.IndexChunkDuration, prometheus.Labels{"language": "go"})
	require.True(t, ok)
	assert.Equal(t, uint64(2), count, "two observations expected for same language")
	assert.InDelta(t, 0.05, sum, 1e-9, "sum should accumulate chunking durations")
}

func TestRecordOrchAccountRefreshStampFailure(t *testing.T) {
	Reset()
	m := InitMetrics()

	m.RecordOrchAccountRefreshStampFailure("tenant-1", "success")
	m.RecordOrchAccountRefreshStampFailure("tenant-1", "success")
	m.RecordOrchAccountRefreshStampFailure("tenant-1", "failure")

	success := testutil.ToFloat64(m.OrchAccountRefreshStampFailures.With(prometheus.Labels{"tenant": "tenant-1", "outcome": "success"}))
	failure := testutil.ToFloat64(m.OrchAccountRefreshStampFailures.With(prometheus.Labels{"tenant": "tenant-1", "outcome": "failure"}))

	assert.Equal(t, 2.0, success, "success-stamp failures must be counted per outcome")
	assert.Equal(t, 1.0, failure, "failure-stamp failures must be counted per outcome")
}
