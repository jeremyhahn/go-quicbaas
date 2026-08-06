//go:build rest

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

// sealed_window_sdk_compat_test.go is the go-trusted-platform M6-gate
// compatibility proof: the quicbaas SDK client (the exact client TP's gate
// uses) drives BarrierStatus + BarrierInitialize against a SEALED quicbaasd,
// hermetically in-process, and the unseal completes the full boot.
//
// It lives in the EXTERNAL test package because the quicbaas SDK imports
// pkg/server (sdk/go/server.go), which would be an import cycle from the
// internal test package. Everything here goes through the exported Server
// surface + the wire.
package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	quicbaassdk "github.com/jeremyhahn/go-quicbaas/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/server"
)

// sdkSealedEnvelope is qrdb's documented REST sealed-window deny body.
const sdkSealedEnvelope = `{"error":"barrier sealed","code":"SEALED"}`

// sdkWaitFor polls fn every 100ms until true or the deadline elapses.
func sdkWaitFor(t *testing.T, timeout time.Duration, what string, fn func() bool) {
	t.Helper()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(timeout)
	for {
		select {
		case <-ticker.C:
			if fn() {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// TestSealedBoot_SDKDrivesBarrierAgainstSealedDaemon is the end-to-end
// sealed-window contract test:
//
//  1. seal-enabled boot with no auto-unseal material -> sealed window (no
//     death, no panic);
//  2. sealed-phase allowlist over the real REST listener: barrier status
//     reachable, off-allowlist path receives the sealed envelope;
//  3. the quicbaas SDK client drives BarrierStatus + BarrierInitialize (the
//     TP M6-gate verbs) against the sealed daemon pre-auth;
//  4. the unseal completes the full boot: the node SDK client comes online
//     and the full transports replace the sealed listeners.
func TestSealedBoot_SDKDrivesBarrierAgainstSealedDaemon(t *testing.T) {
	cfg := server.NewSealedBootTestConfig(t)

	srv, err := server.NewServer(cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		// Shutdown runs the full teardown; the Start goroutine stays parked
		// on the shutdown channel (Shutdown and TriggerShutdown share one
		// sync.Once by design — the daemon closes the channel from its
		// signal handler), which is harmless for the test binary.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	startErr := make(chan error, 1)
	go func() { startErr <- srv.Start() }()

	// Phase 1: the sealed-window REST listener comes up and serves the
	// barrier status verb.
	var baseURL string
	sdkWaitFor(t, 60*time.Second, "sealed-window REST listener", func() bool {
		addr := srv.RESTAddr()
		if addr == nil {
			return false
		}
		baseURL = "http://" + addr.String()
		resp, getErr := http.Get(baseURL + "/v1/barrier/status")
		if getErr != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode == http.StatusOK
	})
	require.Nil(t, srv.GetQRDBNodeClient(), "sealed node has no SDK client yet")

	// Phase 2: sealed-window admit/deny over the real listener.
	resp, err := http.Get(baseURL + "/v1/barrier/status")
	require.NoError(t, err)
	statusBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var status struct {
		Sealed bool `json:"sealed"`
	}
	require.NoError(t, json.Unmarshal(statusBody, &status))
	assert.True(t, status.Sealed, "barrier status must report sealed: %s", statusBody)

	resp, err = http.Get(baseURL + "/api/v1/kms/keys")
	require.NoError(t, err)
	deniedBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.Equal(t, sdkSealedEnvelope, string(deniedBody), "off-allowlist path must receive the sealed envelope")

	// Phase 3 (TP M6-gate compatibility): the quicbaas SDK drives the
	// barrier verbs against the sealed daemon.
	sdkClient, err := quicbaassdk.NewClient(baseURL, quicbaassdk.WithREST())
	require.NoError(t, err)
	t.Cleanup(func() { _ = sdkClient.Close() })

	barrier, err := sdkClient.Security().Barrier()
	require.NoError(t, err, "SDK Security().Barrier() must be reachable against a sealed daemon")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st, err := barrier.BarrierStatus(ctx)
	require.NoError(t, err, "SDK BarrierStatus must work against a sealed daemon")
	assert.True(t, st.Sealed)

	// First boot: the barrier is uninitialized; BarrierInitialize seeds the
	// root key and unseals in one step. (BarrierUnseal is the restart-path
	// verb over the same allowlisted wire route; on an uninitialized
	// barrier only initialize is valid.)
	require.NoError(t, barrier.BarrierInitialize(ctx, &qrdbtransport.BarrierInitializeRequest{
		Secret: "sealed-window-e2e-passphrase",
	}), "SDK BarrierInitialize must drive the sealed daemon's barrier")

	// Phase 4: the daemon notices the unseal, completes the node boot, and
	// replaces the sealed listeners with the full transports.
	sdkWaitFor(t, 120*time.Second, "full boot after unseal", func() bool {
		if srv.GetQRDBNodeClient() == nil {
			return false
		}
		addr := srv.RESTAddr()
		if addr == nil {
			return false
		}
		fullResp, getErr := http.Get("http://" + addr.String() + "/api/v1/kms/keys")
		if getErr != nil {
			return false
		}
		defer func() { _ = fullResp.Body.Close() }()
		body, _ := io.ReadAll(fullResp.Body)
		// Off-allowlist path must no longer receive the sealed envelope.
		return string(body) != sdkSealedEnvelope
	})

	// Post-unseal, the SDK sees the unsealed barrier through the full
	// transport (fresh client — the sealed listener's ephemeral port is
	// gone with the sealed phase).
	fullAddr := srv.RESTAddr()
	require.NotNil(t, fullAddr)
	fullClient, err := quicbaassdk.NewClient("http://"+fullAddr.String(), quicbaassdk.WithREST())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fullClient.Close() })

	fullBarrier, err := fullClient.Security().Barrier()
	require.NoError(t, err)
	fullStatus, err := fullBarrier.BarrierStatus(ctx)
	require.NoError(t, err)
	assert.False(t, fullStatus.Sealed, "barrier must report unsealed after the window closes")

	select {
	case bootErr := <-startErr:
		t.Fatalf("Start returned before shutdown: %v", bootErr)
	default:
	}
}
