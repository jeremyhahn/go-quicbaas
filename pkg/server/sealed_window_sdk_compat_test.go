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

// sdkWaitFor polls fn every 100ms until it reports true, bounded by
// t.Deadline(). (This file is in the external server_test package and so
// cannot reuse the in-package awaitCond helper directly.)
//
// BE PRECISE ABOUT WHAT THAT BOUND IS, because the comment this replaces was
// wrong about it: it claimed the deadline anchor meant "a busy machine cannot
// fail a boot that is merely slow". It does not. t.Deadline() is the test
// BINARY's deadline -- `go test -timeout`, 10m for `make test` -- so the
// budget this wait gets is that timeout MINUS everything every preceding test
// in pkg/server already spent. A busy machine slows those too, which shrinks
// this test's budget by the very mechanism the old comment offered as
// protection. Anchoring to t.Deadline() did not remove the fixed-wall-clock
// flake; it made the budget depend on unrelated tests.
//
// It is still the right anchor -- a wait that outlives the harness's own
// timeout reports nothing useful, because the harness panics the process
// first -- but it buys a graceful report, NOT immunity from slowness. Do not
// reintroduce a guarantee here that the code does not have.
//
// (Worth recording so the next reader does not re-derive it: in the go#497
// occurrence of 2026-09-04, job 45519, the budget was NOT the binding
// constraint. The test began 71.6s into a 600s timeout with 88% of it still
// unspent, and the boot then emitted nothing at all for 525s. It had failed,
// not slowed -- which is what the startErr case below exists to say out loud.)
//
// It also watches startErr, the channel carrying srv.Start's return value, and
// that is not belt-and-braces: every predicate here is a property the boot
// establishes, so a boot that has ALREADY FAILED makes fn unsatisfiable
// forever. Without this case the wait polls a dead server until the deadline
// and then reports "timed out waiting for X" -- naming a slow boot when what
// actually happened was an immediate, fully-explained failure sitting unread
// in a channel. That is go#497: a bind conflict on the post-unseal listener
// was reported 1601 SECONDS later as a timeout, because the only read of
// startErr was placed after the wait it would have explained.
//
// The deadline bound is the test BINARY's, not this test's, so an
// unsatisfiable predicate burns every second the rest of the package had
// left. Failing on startErr is what keeps that cost off the runner.
// sealedWindowBarrierStatusPath is qrdb's wire barrier-status route. go-qrdb
// v0.50 makes it ListenerOnly: only a boot-time sealed-window listener serves
// it while sealed, and quicbaas's sealed REST listener is one (it installs
// go-qrdb's listener gate).
const sealedWindowBarrierStatusPath = "/v1/barrier/status"

func sdkWaitFor(t *testing.T, startErr <-chan error, what string, fn func() bool) {
	t.Helper()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	// A nil channel blocks forever, which is the right behaviour under
	// `go test -timeout 0`.
	var deadline <-chan time.Time
	if testDeadline, ok := t.Deadline(); ok {
		// Leave a margin for this test to report its own failure before
		// the harness panics the process with a stack dump.
		deadline = time.After(time.Until(testDeadline) - 5*time.Second)
	}
	for {
		select {
		case <-ticker.C:
			if fn() {
				return
			}
		case bootErr := <-startErr:
			t.Fatalf("Start returned while waiting for %s: %v", what, bootErr)
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
//  4. the unseal completes the full node boot (the node's own SDK client
//     comes online).
//
// Phases 1-3 run over server.NewSealedBootTestConfig's plain-HTTP
// sealed-window listener (sealed_window_rest.go, which honors
// Security.TLSEnabled — kept off here; see that config's doc comment for why
// TLS is not simply flipped on). Phase 4 verifies the node itself completed
// its Unseal via GetQRDBNodeClient rather than by round-tripping the
// post-unseal REST surface: that surface now runs on the embedded qrdb
// node's own unified-server listener, which is TLS-only (see
// (*config.Config).NetworkServing), so it is unreachable in this
// TLS-disabled configuration — a real, deliberate consequence of "network
// serving requires TLS", not a gap in this test.
func TestSealedBoot_SDKDrivesBarrierAgainstSealedDaemon(t *testing.T) {
	cfg := server.NewSealedBootTestConfig(t)
	server.InitSealedBootDataDir(t, cfg)

	srv, err := server.NewServer(cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		// Shutdown signals the shutdown channel first (releasing the Start
		// goroutine) and then runs the full teardown (#139).
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	startErr := make(chan error, 1)
	go func() { startErr <- srv.Start() }()

	// Phase 1: the sealed-window REST listener comes up and serves the
	// barrier status verb.
	var baseURL string
	sdkWaitFor(t, startErr, "the sealed-window REST listener to serve barrier status", func() bool {
		addr := srv.RESTAddr()
		if addr == nil {
			return false
		}
		baseURL = "http://" + addr.String()
		resp, getErr := http.Get(baseURL + sealedWindowBarrierStatusPath)
		if getErr != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode == http.StatusOK
	})
	require.Nil(t, srv.GetQRDBNodeClient(), "sealed node has no SDK client yet")

	// Phase 2: sealed-window admit/deny over the real listener.
	resp, err := http.Get(baseURL + sealedWindowBarrierStatusPath)
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

	// The barrier was initialized by first boot (InitSealedBootDataDir):
	// under go-qrdb v0.47 `qrdb init` is the only system bootstrap, so the
	// restart-path verb BarrierUnseal is what brings the barrier up.
	require.NoError(t, barrier.BarrierUnseal(ctx, &qrdbtransport.BarrierUnsealRequest{
		Secret: []byte("sealed-window-e2e-passphrase"),
	}), "SDK BarrierUnseal must drive the sealed daemon's barrier")

	// Phase 4: the daemon notices the unseal and completes the node boot —
	// verified at the node level (GetQRDBNodeClient), since the post-unseal
	// REST surface is TLS-only and unreachable under this TLS-disabled
	// config (see the doc comment above).
	sdkWaitFor(t, startErr, "the full node boot to complete after unseal", func() bool {
		return srv.GetQRDBNodeClient() != nil
	})
	assert.False(t, srv.GetQRDBNodeClient() == nil, "node SDK client must be online after unseal completes the boot")

	select {
	case bootErr := <-startErr:
		t.Fatalf("Start returned before shutdown: %v", bootErr)
	default:
	}
}
