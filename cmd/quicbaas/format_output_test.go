package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// These tests exercise the pure output-formatting functions across the JSON
// and raw (default) branches plus the invalid-format error branch. The
// existing command tests only exercise the table branch, leaving the JSON /
// raw / error paths uncovered.

func TestFormatBarrierStatusResponse_JSONAndRawAndInvalid(t *testing.T) {
	resp := &qrdbtransport.BarrierStatusResponse{
		Sealed:         true,
		Strategy:       "shamir",
		HardwareBacked: true,
		InitializedAt:  "2026-01-01T00:00:00Z",
	}

	var jsonBuf bytes.Buffer
	require.NoError(t, formatBarrierStatusResponse(resp, "json", &jsonBuf))
	out := jsonBuf.String()
	assert.Contains(t, out, `"sealed": true`)
	assert.Contains(t, out, `"strategy": "shamir"`)
	assert.Contains(t, out, `"hardware_backed": true`)

	var rawBuf bytes.Buffer
	require.NoError(t, formatBarrierStatusResponse(resp, "raw", &rawBuf))
	assert.Contains(t, rawBuf.String(), "Sealed: true")
	assert.Contains(t, rawBuf.String(), "Strategy: shamir")

	var badBuf bytes.Buffer
	assert.Error(t, formatBarrierStatusResponse(resp, "not-a-format", &badBuf))
}

func TestFormatBarrierShamirResult_JSONAndRawAndInvalid(t *testing.T) {
	resp := &qrdbtransport.BarrierInitializeShamirResponse{
		Threshold:   2,
		TotalShares: 3,
		ShareCount:  3,
	}

	var jsonBuf bytes.Buffer
	require.NoError(t, formatBarrierShamirResult(resp, "json", &jsonBuf))
	assert.Contains(t, jsonBuf.String(), `"threshold": 2`)
	assert.Contains(t, jsonBuf.String(), `"total_shares": 3`)
	assert.Contains(t, jsonBuf.String(), `"share_count": 3`)
	// Share-free contract: raw shares must never appear in CLI output.
	assert.NotContains(t, jsonBuf.String(), `"shares"`)

	var rawBuf bytes.Buffer
	require.NoError(t, formatBarrierShamirResult(resp, "raw", &rawBuf))
	assert.Contains(t, rawBuf.String(), "Threshold: 2 of 3")
	assert.Contains(t, rawBuf.String(), "Shares sealed to officers: 3")

	assert.Error(t, formatBarrierShamirResult(resp, "bogus", &bytes.Buffer{}))
}

func TestFormatBarrierRekeyResult_JSONAndRawAndInvalid(t *testing.T) {
	resp := &qrdbtransport.BarrierRekeyResponse{
		Threshold:   2,
		TotalShares: 2,
		ShareCount:  2,
	}

	var jsonBuf bytes.Buffer
	require.NoError(t, formatBarrierRekeyResult(resp, "json", &jsonBuf))
	assert.Contains(t, jsonBuf.String(), `"threshold": 2`)
	assert.Contains(t, jsonBuf.String(), `"share_count": 2`)
	// Share-free contract: raw shares must never appear in CLI output.
	assert.NotContains(t, jsonBuf.String(), `"shares"`)

	var rawBuf bytes.Buffer
	require.NoError(t, formatBarrierRekeyResult(resp, "raw", &rawBuf))
	assert.Contains(t, rawBuf.String(), "New Threshold: 2 of 2")
	assert.Contains(t, rawBuf.String(), "Shares sealed to officers: 2")

	assert.Error(t, formatBarrierRekeyResult(resp, "bogus", &bytes.Buffer{}))
}

func TestFormatBarrierRecoveryKeys_JSONAndRawAndInvalid(t *testing.T) {
	resp := &qrdbtransport.BarrierRecoveryKeysResponse{
		Threshold:  2,
		Total:      3,
		ShareCount: 3,
	}

	var jsonBuf bytes.Buffer
	require.NoError(t, formatBarrierRecoveryKeys(resp, "json", &jsonBuf))
	assert.Contains(t, jsonBuf.String(), `"threshold": 2`)
	assert.Contains(t, jsonBuf.String(), `"total": 3`)
	assert.Contains(t, jsonBuf.String(), `"share_count": 3`)
	// Share-free contract: raw recovery keys must never appear in CLI output.
	assert.NotContains(t, jsonBuf.String(), `"keys"`)

	var rawBuf bytes.Buffer
	require.NoError(t, formatBarrierRecoveryKeys(resp, "raw", &rawBuf))
	assert.Contains(t, rawBuf.String(), "Threshold: 2 of 3")
	assert.Contains(t, rawBuf.String(), "Keys sealed to officers: 3")

	assert.Error(t, formatBarrierRecoveryKeys(resp, "bogus", &bytes.Buffer{}))
}

func TestFormatBootstrapAutoResponse_JSONAndRawAndInvalid(t *testing.T) {
	// Ensure the bundle-out gate leaves CA bundle inline.
	originalBundleOut := bootstrapBundleOut
	bootstrapBundleOut = ""
	defer func() { bootstrapBundleOut = originalBundleOut }()

	resp := &BootstrapAutoResponse{
		Method:      "auto",
		CABundlePEM: "-----BEGIN CERTIFICATE-----\nMII\n-----END CERTIFICATE-----\n",
		SPKIPin:     "sha256//abc",
	}

	var jsonBuf bytes.Buffer
	require.NoError(t, formatBootstrapAutoResponse(resp, "json", &jsonBuf))
	assert.Contains(t, jsonBuf.String(), `"method": "auto"`)
	assert.Contains(t, jsonBuf.String(), `"spki_pin": "sha256//abc"`)

	var rawBuf bytes.Buffer
	require.NoError(t, formatBootstrapAutoResponse(resp, "raw", &rawBuf))
	assert.Contains(t, rawBuf.String(), "Method: auto")
	assert.Contains(t, rawBuf.String(), "SPKI Pin: sha256//abc")
	assert.Contains(t, rawBuf.String(), "BEGIN CERTIFICATE")

	assert.Error(t, formatBootstrapAutoResponse(resp, "bogus", &bytes.Buffer{}))
}

func TestFormatBootstrapAutoResponse_RawSuppressesBundleWhenWrittenToFile(t *testing.T) {
	originalBundleOut := bootstrapBundleOut
	bootstrapBundleOut = "/tmp/ca-bundle.pem"
	defer func() { bootstrapBundleOut = originalBundleOut }()

	resp := &BootstrapAutoResponse{
		Method:      "auto",
		CABundlePEM: "-----BEGIN CERTIFICATE-----\nMII\n-----END CERTIFICATE-----\n",
	}

	var rawBuf bytes.Buffer
	require.NoError(t, formatBootstrapAutoResponse(resp, "raw", &rawBuf))
	assert.NotContains(t, rawBuf.String(), "BEGIN CERTIFICATE")
}

func TestFormatInitStatusResponse_JSONAndRawAndInvalid(t *testing.T) {
	resp := &qrdbtransport.CeremonyStatusResponse{
		State:   "completed",
		SPKIPin: "sha256//pin",
	}

	var jsonBuf bytes.Buffer
	require.NoError(t, formatInitStatusResponse(resp, "json", &jsonBuf))
	assert.Contains(t, jsonBuf.String(), `"state": "completed"`)
	assert.Contains(t, jsonBuf.String(), `"spki_pin": "sha256//pin"`)

	var rawBuf bytes.Buffer
	require.NoError(t, formatInitStatusResponse(resp, "raw", &rawBuf))
	assert.Contains(t, rawBuf.String(), "State: completed")
	assert.Contains(t, rawBuf.String(), "SPKI Pin: sha256//pin")

	assert.Error(t, formatInitStatusResponse(resp, "bogus", &bytes.Buffer{}))
}

func TestFormatInitStatusResponse_RawOmitsEmptyPin(t *testing.T) {
	resp := &qrdbtransport.CeremonyStatusResponse{State: "pending"}

	var rawBuf bytes.Buffer
	require.NoError(t, formatInitStatusResponse(resp, "raw", &rawBuf))
	assert.Contains(t, rawBuf.String(), "State: pending")
	assert.NotContains(t, rawBuf.String(), "SPKI Pin")
}

func TestFormatOrchListJobsResponse_JSONYAMLRawAndInvalid(t *testing.T) {
	resp := &orchestratorSDK.ListJobsResponse{
		Jobs: []orchestratorSDK.JobSummary{
			{
				JobID:       "job_1",
				State:       "running",
				Iteration:   3,
				TaskCount:   5,
				Completed:   2,
				Failed:      1,
				EventCount:  7,
				Description: strings.Repeat("x", 50), // exceeds the raw-path length, but raw prints full
				Error:       "boom",
			},
		},
	}

	var jsonBuf bytes.Buffer
	require.NoError(t, formatOrchListJobsResponse(resp, "json", &jsonBuf))
	assert.Contains(t, jsonBuf.String(), `"job_id": "job_1"`)

	var yamlBuf bytes.Buffer
	require.NoError(t, formatOrchListJobsResponse(resp, "yaml", &yamlBuf))
	assert.Contains(t, yamlBuf.String(), "job_1")

	var rawBuf bytes.Buffer
	require.NoError(t, formatOrchListJobsResponse(resp, "raw", &rawBuf))
	assert.Contains(t, rawBuf.String(), "Job:         job_1")
	assert.Contains(t, rawBuf.String(), "State:       running")
	assert.Contains(t, rawBuf.String(), "Error:       boom")

	assert.Error(t, formatOrchListJobsResponse(resp, "bogus", &bytes.Buffer{}))
}

func TestFormatOrchListJobsResponse_EmptyJobs(t *testing.T) {
	resp := &orchestratorSDK.ListJobsResponse{}

	var rawBuf bytes.Buffer
	require.NoError(t, formatOrchListJobsResponse(resp, "raw", &rawBuf))
	assert.Contains(t, rawBuf.String(), "No jobs found.")
}

func TestFormatOrchJobStatusResponse_JSONYAMLRawAndInvalid(t *testing.T) {
	status := &orchestratorSDK.JobStatus{
		JobID:       "job_2",
		State:       "completed",
		Iteration:   4,
		TaskCount:   8,
		Completed:   8,
		Failed:      0,
		Description: "build pipeline",
		Error:       "transient",
		Events: []orchestratorSDK.JobAuditEvent{
			{Timestamp: 1, Type: "start", Details: "began"},
		},
	}

	var jsonBuf bytes.Buffer
	require.NoError(t, formatOrchJobStatusResponse(status, "json", &jsonBuf))
	assert.Contains(t, jsonBuf.String(), `"job_id": "job_2"`)

	var yamlBuf bytes.Buffer
	require.NoError(t, formatOrchJobStatusResponse(status, "yaml", &yamlBuf))
	assert.Contains(t, yamlBuf.String(), "job_2")

	var rawBuf bytes.Buffer
	require.NoError(t, formatOrchJobStatusResponse(status, "raw", &rawBuf))
	assert.Contains(t, rawBuf.String(), "Job:         job_2")
	assert.Contains(t, rawBuf.String(), "Error:       transient")
	assert.Contains(t, rawBuf.String(), "Events:      1")
	assert.Contains(t, rawBuf.String(), "orchestrator job events job_2")

	assert.Error(t, formatOrchJobStatusResponse(status, "bogus", &bytes.Buffer{}))
}

func TestFormatOrchListJobsResponse_TableWithData(t *testing.T) {
	resp := &orchestratorSDK.ListJobsResponse{
		Jobs: []orchestratorSDK.JobSummary{
			{JobID: "job_1", State: "running", Description: strings.Repeat("y", 60)},
			{JobID: "job_2", State: "done", Description: "short"},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, formatOrchListJobsResponse(resp, "table", &buf))
	got := buf.String()
	assert.Contains(t, got, "job_1")
	assert.Contains(t, got, "...") // long description is truncated
}

func TestFormatOrchListJobsResponse_TableEmpty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, formatOrchListJobsResponse(&orchestratorSDK.ListJobsResponse{}, "table", &buf))
	assert.Contains(t, buf.String(), "No jobs found.")
}

func TestFormatBarrierStatusResponse_TableNoInitializedAt(t *testing.T) {
	resp := &qrdbtransport.BarrierStatusResponse{Sealed: false, Strategy: "none"}
	var buf bytes.Buffer
	require.NoError(t, formatBarrierStatusResponse(resp, "table", &buf))
	got := buf.String()
	assert.Contains(t, got, "unsealed")
	assert.NotContains(t, got, "Initialized At")
}
