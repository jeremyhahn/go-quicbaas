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
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sdlcSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

func (m *mockSDLCClient) WorkerImageStatus(context.Context) (*sdlcSDK.WorkerImageStatus, error) {
	if m.err != nil {
		return nil, m.err
	}
	if len(m.statuses) == 0 {
		return &sdlcSDK.WorkerImageStatus{}, nil
	}
	next := m.statuses[0]
	if len(m.statuses) > 1 {
		m.statuses = m.statuses[1:]
	}
	return next, nil
}

func (m *mockSDLCClient) RefreshWorkerImage(context.Context) (*sdlcSDK.WorkerImageStatus, error) {
	m.refreshes++
	if m.err != nil {
		return nil, m.err
	}
	return &sdlcSDK.WorkerImageStatus{Serving: true, Queued: true}, nil
}

// withWorkerImageWait makes --wait poll fast for the test.
func withWorkerImageWait(t *testing.T, wait bool, timeout time.Duration) {
	t.Helper()
	origWait, origTimeout, origEvery := workerImageWait, workerImageWaitTimeout, workerImagePollInterval
	workerImageWait, workerImageWaitTimeout, workerImagePollInterval = wait, timeout, time.Millisecond
	t.Cleanup(func() {
		workerImageWait, workerImageWaitTimeout, workerImagePollInterval = origWait, origTimeout, origEvery
	})
}

func finishedPass(outcome string) *sdlcSDK.WorkerImageStatus {
	return &sdlcSDK.WorkerImageStatus{Serving: true, Last: &sdlcSDK.WorkerImageReport{
		Outcome: outcome, StartedAt: time.Now().Add(time.Second), Error: "gate failed",
	}}
}

func TestSDLCWorkerImageRefresh_QueuesAndReturns(t *testing.T) {
	mc := &mockSDLCClient{}
	withSDLCMock(t, mc)
	withWorkerImageWait(t, false, time.Minute)
	if err := runSDLCWorkerImageRefresh(nil, nil); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if mc.refreshes != 1 {
		t.Fatalf("refreshes = %d", mc.refreshes)
	}
}

func TestSDLCWorkerImageRefresh_WaitFollowsThePassToItsOutcome(t *testing.T) {
	running := &sdlcSDK.WorkerImageStatus{Serving: true, Running: true}
	cases := map[string]struct {
		final   *sdlcSDK.WorkerImageStatus
		wantErr bool
	}{
		"promoted": {final: finishedPass("promoted")},
		"current":  {final: finishedPass("current")},
		"failed":   {final: finishedPass("failed"), wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			mc := &mockSDLCClient{statuses: []*sdlcSDK.WorkerImageStatus{running, tc.final}}
			withSDLCMock(t, mc)
			withWorkerImageWait(t, true, time.Minute)
			err := runSDLCWorkerImageRefresh(nil, nil)
			var failed *WorkerImageRefreshFailedError
			if tc.wantErr != errors.As(err, &failed) {
				t.Fatalf("err = %v, want failure %v", err, tc.wantErr)
			}
		})
	}
}

func TestSDLCWorkerImageRefresh_WaitGivesUpAtItsDeadline(t *testing.T) {
	mc := &mockSDLCClient{statuses: []*sdlcSDK.WorkerImageStatus{{Serving: true, Running: true}}}
	withSDLCMock(t, mc)
	withWorkerImageWait(t, true, 20*time.Millisecond)
	err := runSDLCWorkerImageRefresh(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("err = %v", err)
	}
}

func TestSDLCWorkerImage_ErrorsReachTheCaller(t *testing.T) {
	mc := &mockSDLCClient{err: errors.New("worker image: a refresh is already in progress")}
	withSDLCMock(t, mc)
	withWorkerImageWait(t, false, time.Minute)
	if err := runSDLCWorkerImageRefresh(nil, nil); err == nil {
		t.Fatal("a refused refresh reported success")
	}
	if err := runSDLCWorkerImageStatus(nil, nil); err == nil {
		t.Fatal("a failed status read reported success")
	}
}

func TestWorkerImagePassFinished(t *testing.T) {
	requested := time.Now()
	old := &sdlcSDK.WorkerImageStatus{Last: &sdlcSDK.WorkerImageReport{StartedAt: requested.Add(-time.Hour)}}
	if workerImagePassFinished(old, requested) {
		t.Fatal("a pass that started before the request is not the requested one")
	}
	if workerImagePassFinished(&sdlcSDK.WorkerImageStatus{Queued: true}, requested) {
		t.Fatal("a queued pass has not finished")
	}
	if !workerImagePassFinished(finishedPass("promoted"), requested) {
		t.Fatal("a pass that started after the request and finished is the one")
	}
}

func TestFormatWorkerImageStatus_TableNamesEverything(t *testing.T) {
	status := &sdlcSDK.WorkerImageStatus{Serving: true, Last: &sdlcSDK.WorkerImageReport{
		Outcome: "partial", Trigger: "schedule", Reference: "reg/o/w:1@sha256:abc",
		Latest: sdlcSDK.WorkerImageAgents{ClaudeCode: "2.1.280", Codex: "0.156.0"},
		Probes: []sdlcSDK.WorkerImageCheck{{Name: "lint", Passed: true}},
		Smokes: []sdlcSDK.WorkerImageCheck{{Name: "codex/gpt-5.5", Passed: false}},
		Promotions: []sdlcSDK.WorkerImagePromotion{
			{TenantID: "acme", AgentID: "claude-coder"}, {TenantID: "acme", AgentID: "codex-reviewer", Error: "sealed"},
		},
		Skipped: []string{"t2: sealed"}, Error: "1 of 2 bindings could not be rebound",
	}}
	var out bytes.Buffer
	if err := formatWorkerImageStatus(status, "table", &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"partial (schedule)", "claude-code 2.1.280", "reg/o/w:1@sha256:abc", "PASS", "FAIL",
		"acme/agent:codex-reviewer", "NOT repointed: sealed", "skipped", "could not be rebound"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("table lacks %q:\n%s", want, out.String())
		}
	}
	out.Reset()
	if err := formatWorkerImageStatus(&sdlcSDK.WorkerImageStatus{}, "table", &out); err != nil ||
		!strings.Contains(out.String(), "not serving") {
		t.Fatalf("an idle node: %q %v", out.String(), err)
	}
	if err := formatWorkerImageStatus(status, "nope", &out); err == nil {
		t.Fatal("an unknown format was accepted")
	}
	for _, format := range []string{"json", "yaml"} {
		out.Reset()
		if err := formatWorkerImageStatus(status, format, &out); err != nil || !strings.Contains(out.String(), "2.1.280") {
			t.Fatalf("%s: %q %v", format, out.String(), err)
		}
	}
}
