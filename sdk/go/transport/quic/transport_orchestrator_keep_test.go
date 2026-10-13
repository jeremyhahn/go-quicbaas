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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestRequest_SpawnSessionCarriesKeepOverHTTP3 asserts the keep marker reaches
// the spawn endpoint, and that a spawn which does not set it sends none.
func TestRequest_SpawnSessionCarriesKeepOverHTTP3(t *testing.T) {
	bodies := make(chan map[string]interface{}, 2)
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		bodies <- body
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{"session_id": "s1", "backend": "claude"}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(addr))
	require.NoError(t, err)
	defer tr.Close()

	for _, keep := range []bool{true, false} {
		_, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "SpawnSession",
			&orchestratorclient.SpawnSessionRequest{Backend: "claude", Keep: keep})
		require.NoError(t, err)

		body := <-bodies
		got, present := body["keep"]
		if keep {
			assert.Equal(t, true, got, "a kept spawn lost its keep marker")
		} else {
			assert.False(t, present, "an unkept spawn sent keep = %v", got)
		}
	}
}
