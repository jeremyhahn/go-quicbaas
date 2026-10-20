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

package quic

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dekclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dek"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// dekProgressServer answers the DEK status and re-encryption progress routes
// over HTTP/3 with the service responses the quicbaas server encodes.
func dekProgressServer(t *testing.T, progress *service.ReencryptionProgress) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body interface{}
		switch {
		case strings.HasSuffix(r.URL.Path, "/status"):
			body = &service.GetDEKStatusResponse{ReencryptionProgress: progress}
		case strings.HasSuffix(r.URL.Path, "/reencrypt"):
			body = &service.GetReencryptionProgressResponse{Path: "app/key", Status: "running", Progress: progress}
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Errorf("encode reply: %v", err)
		}
	})
}

func TestDEKProgressOverHTTP3_CarriesStatusAndEstimatedCompletion(t *testing.T) {
	eta := time.Unix(1700003600, 0).UTC()
	progress := &service.ReencryptionProgress{
		Status: "running", InProgress: true, TotalKeys: 4, ProcessedKeys: 2, EstimatedCompletion: &eta,
	}
	addr, shutdown := startTestHTTP3Server(t, dekProgressServer(t, progress))
	t.Cleanup(shutdown)
	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	t.Cleanup(func() { tr.Close() })
	client := dekclient.NewClient(tr)
	ctx := context.Background()

	resp, err := client.GetReencryptionProgress(ctx, &dekclient.GetReencryptionProgressRequest{Path: "app/key"})
	require.NoError(t, err)
	require.NotNil(t, resp.Progress)
	assert.Equal(t, "running", resp.Progress.Status)
	assert.True(t, resp.Progress.EstimatedCompletion.Equal(eta), "EstimatedCompletion = %v", resp.Progress.EstimatedCompletion)

	status, err := client.GetDEKStatus(ctx, "app/key")
	require.NoError(t, err)
	require.NotNil(t, status.ReencryptionProgress)
	assert.Equal(t, "running", status.ReencryptionProgress.Status)
	assert.True(t, status.ReencryptionProgress.EstimatedCompletion.Equal(eta),
		"EstimatedCompletion = %v", status.ReencryptionProgress.EstimatedCompletion)
}
