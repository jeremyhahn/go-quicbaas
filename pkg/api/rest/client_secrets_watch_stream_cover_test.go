//go:build rest

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

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestWatchSecretsByPrefix_StreamsChange drives the SSE reader goroutine end to
// end: the server emits a single "change" event, the client decodes it onto the
// channel, and the channel is closed when the stream ends. This exercises the
// event-parsing, data-buffering, JSON-decode, and channel-send branches of the
// watch goroutine.
func TestWatchSecretsByPrefix_StreamsChange(t *testing.T) {
	change := service.SecretChange{
		Path:       "/apps/myapp/db-password",
		ChangeType: service.SecretChangeUpdated,
	}
	payload, err := json.Marshal(change)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		// A non-"change" event is ignored by the reader; include it first to
		// exercise the event-type filter branch.
		_, _ = w.Write([]byte("event: ping\ndata: {}\n\n"))
		// The real change event the client should surface.
		_, _ = w.Write([]byte("event: change\ndata: "))
		_, _ = w.Write(payload)
		_, _ = w.Write([]byte("\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		// Returning from the handler closes the response body, which makes the
		// client-side reader hit EOF and close the changes channel.
	}))
	defer srv.Close()

	c := newKMSTestClient(t, srv)

	ch, err := c.WatchSecretsByPrefix(context.Background(), &service.WatchSecretsRequest{
		Prefix: "/apps/myapp/",
	})
	require.NoError(t, err)
	require.NotNil(t, ch)

	// Receive the single emitted change, then drain until the channel closes.
	got, ok := <-ch
	require.True(t, ok, "expected a change before the channel closed")
	require.Equal(t, change.Path, got.Path)
	require.Equal(t, change.ChangeType, got.ChangeType)

	// Drain any remaining values and confirm the channel is closed once the
	// server-side stream ends.
	for range ch {
	}
}
