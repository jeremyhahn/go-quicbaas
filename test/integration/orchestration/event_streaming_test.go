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

//go:build integration && rag

package orchestration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Event Streaming (requires Ollama)
// =============================================================================

// TestEventStreaming_SubscribeJobEvents verifies that subscribing to job events
// returns a channel that delivers real-time events for a running job. The test
// starts a job via the Ollama backend, subscribes to events, collects them until
// the channel closes or a timeout is reached, and verifies that at least one
// event was received with the correct job ID.
func TestEventStreaming_SubscribeJobEvents(t *testing.T) {
	requireOllama(t)

	svc := newAgentService(t)
	tenantID := "event-streaming-subscribe"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	// Start a job that uses the Ollama backend.
	resp, err := svc.StartJob(ctx, &orchestration.StartJobRequest{
		TenantID:      tenantID,
		Description:   "Write a function that adds two integers",
		LeadBackend:   "ollama",
		LeadModel:     env.ollamaModel,
		MaxIterations: 2,
		PollInterval:  "2s",
	})
	require.NoError(t, err, "StartJob should succeed")
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.JobID, "job ID must not be empty")

	jobID := resp.JobID
	t.Logf("started job %s for event streaming test", jobID)

	// Subscribe to events for the running job.
	eventCh, subErr := svc.SubscribeJobEvents(ctx, tenantID, jobID)
	require.NoError(t, subErr, "SubscribeJobEvents should succeed for a running job")
	require.NotNil(t, eventCh, "event channel must not be nil")

	// Collect events until the channel closes or the timeout expires.
	collectCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	var events []*orchestration.JobEvent
	collecting := true
	for collecting {
		select {
		case evt, ok := <-eventCh:
			if !ok {
				// Channel closed, job has completed or been cleaned up.
				t.Logf("event channel closed after receiving %d events", len(events))
				collecting = false
				continue
			}
			t.Logf("received event: type=%s state=%s message=%s", evt.Type, evt.State, evt.Message)
			events = append(events, evt)

			// Stop collecting once a terminal state is reached.
			if orchestration.IsTerminalJobState(evt.State) {
				t.Logf("received terminal event (state=%s), stopping collection", evt.State)
				collecting = false
			}
		case <-collectCtx.Done():
			t.Logf("collection timeout reached after receiving %d events", len(events))
			collecting = false
		}
	}

	// Verify at least one event was received.
	require.NotEmpty(t, events, "should have received at least one job event")

	// Verify all events carry the correct job ID.
	for i, evt := range events {
		assert.Equal(t, jobID, evt.JobID,
			"event %d should have the correct job ID", i)
	}
}

// TestEventStreaming_SubscribeJobEvents_InvalidJob verifies that subscribing to
// events for a non-existent job returns an error.
func TestEventStreaming_SubscribeJobEvents_InvalidJob(t *testing.T) {
	requireOllama(t)

	svc := newAgentService(t)
	tenantID := "event-streaming-invalid-job"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	_, subErr := svc.SubscribeJobEvents(ctx, tenantID, "nonexistent-job-id")
	require.Error(t, subErr, "SubscribeJobEvents for a non-existent job should fail")
}

// TestEventStreaming_SubscribeJobEvents_ContextCancellation verifies that
// cancelling the context closes the event channel without blocking.
func TestEventStreaming_SubscribeJobEvents_ContextCancellation(t *testing.T) {
	requireOllama(t)

	svc := newAgentService(t)
	tenantID := "event-streaming-ctx-cancel"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	// Start a job so we have a valid subscription target.
	resp, err := svc.StartJob(ctx, &orchestration.StartJobRequest{
		TenantID:      tenantID,
		Description:   "Write a function that multiplies two integers",
		LeadBackend:   "ollama",
		LeadModel:     env.ollamaModel,
		MaxIterations: 3,
		PollInterval:  "2s",
	})
	require.NoError(t, err, "StartJob should succeed")
	require.NotNil(t, resp)

	// Subscribe with a cancellable context.
	subCtx, subCancel := context.WithCancel(ctx)
	eventCh, subErr := svc.SubscribeJobEvents(subCtx, tenantID, resp.JobID)
	require.NoError(t, subErr, "SubscribeJobEvents should succeed")
	require.NotNil(t, eventCh, "event channel must not be nil")

	// Cancel the subscription context.
	subCancel()

	// The channel should close promptly after cancellation. Drain any remaining
	// events and verify the channel is closed within a reasonable timeframe.
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer drainCancel()

	closed := false
	for !closed {
		select {
		case _, ok := <-eventCh:
			if !ok {
				closed = true
			}
		case <-drainCtx.Done():
			t.Fatal("event channel did not close within 10 seconds after context cancellation")
		}
	}

	assert.True(t, closed, "event channel should be closed after context cancellation")
}
