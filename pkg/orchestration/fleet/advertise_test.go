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

package fleet

import (
	"context"
	"errors"
	"net"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestConfig_Validate_AdvertiseAddr (go-quicbaas#795): an advertised address
// is optional, and when set must be a host:port the control plane can dial.
func TestConfig_Validate_AdvertiseAddr(t *testing.T) {
	t.Parallel()
	for _, addr := range []string{"", "agent01.example.test:9091", "198.51.100.7:9091", "[2001:db8::7]:9091"} {
		c := newTestConfig(t)
		c.AdvertiseAddr = addr
		assert.NoError(t, c.Validate(), "advertise_addr %q", addr)
	}
	for _, addr := range []string{"agent01", ":9091", "0.0.0.0:9091", "[::]:9091", "agent01:0", "agent01:65536", "agent01:http"} {
		c := newTestConfig(t)
		c.AdvertiseAddr = addr
		err := c.Validate()
		assert.ErrorIs(t, err, ErrConfigInvalidAdvertiseAddr, "advertise_addr %q", addr)
	}
}

// TestEnroll_SendsTheAdvertisedAddress: the register payload carries the
// advertised address when one is configured, and omits the key otherwise.
func TestEnroll_SendsTheAdvertisedAddress(t *testing.T) {
	withProbe(t, &stubDetector{})

	for _, advertise := range []string{"agent01.example.test:9091", ""} {
		capture := &registerPayloadCapture{t: t}
		srv := httptest.NewTLSServer(capture.handler())
		t.Cleanup(srv.Close)

		cfg := newEnrollerConfig(t, srv.URL)
		cfg.AdvertiseAddr = advertise
		require.NoError(t, os.MkdirAll(cfg.WorkDir, 0o700))
		enroller := NewProductionEnroller(ProductionEnrollerDeps{HTTPClient: srv.Client(), Logger: &testLogger{t: t}})

		_, err := enroller.Enroll(context.Background(), cfg)
		require.NoError(t, err)

		got, present := capture.body["advertise_addr"]
		if advertise == "" {
			assert.False(t, present, "no advertise_addr is sent when none is configured")
			continue
		}
		assert.Equal(t, advertise, got)
	}
}

// dialingEnroller enrolls only after dialing the spawn server's address, the
// way the control plane probes a registering node.
type dialingEnroller struct {
	server  SpawnServer
	dialErr error
}

func (e *dialingEnroller) Enroll(ctx context.Context, cfg *Config) (*orchestration.AgentIdentity, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", e.server.Addr())
	if err != nil {
		e.dialErr = err
		return nil, err
	}
	e.dialErr = conn.Close()
	return &orchestration.AgentIdentity{AgentID: "agent-01", TenantID: cfg.TenantID}, nil
}

// TestDaemon_BindsTheSpawnServerBeforeEnrolling: the spawn server listens
// before enrollment runs, so the registration probe reaches it.
func TestDaemon_BindsTheSpawnServerBeforeEnrolling(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	server, err := NewGRPCSpawnServer(GRPCSpawnServerDeps{
		Runtime:  newTestRuntime(t),
		BindAddr: "127.0.0.1:0",
		Logger:   &testLogger{t: t},
	})
	require.NoError(t, err)
	enroller := &dialingEnroller{server: server}
	d, err := NewDaemon(cfg, DaemonDeps{
		Runtime:     newTestRuntime(t),
		Enroller:    enroller,
		SpawnServer: server,
		Logger:      &testLogger{t: t},
	})
	require.NoError(t, err)

	require.NoError(t, d.Start(context.Background()))
	t.Cleanup(func() { _ = d.Stop(context.Background()) })

	assert.NoError(t, enroller.dialErr, "the spawn server must accept connections during enrollment")
}

// failingBinder is a spawn server whose listener cannot bind.
type failingBinder struct {
	fakeSpawnServer
	err error
}

func (f *failingBinder) Bind(context.Context) error { return f.err }

// TestDaemon_BindFailureFailsStartBeforeEnrolling: a node that cannot bind
// does not register an address nothing listens on.
func TestDaemon_BindFailureFailsStartBeforeEnrolling(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	bindErr := errors.New("address in use")
	enroller := &fakeEnroller{identity: &orchestration.AgentIdentity{AgentID: "a", TenantID: cfg.TenantID}}
	d, err := NewDaemon(cfg, DaemonDeps{
		Runtime:     newTestRuntime(t),
		Enroller:    enroller,
		SpawnServer: &failingBinder{err: bindErr},
		Logger:      &testLogger{t: t},
	})
	require.NoError(t, err)

	startErr := d.Start(context.Background())

	require.ErrorIs(t, startErr, bindErr)
	assert.Equal(t, "failed", d.State())
	assert.Zero(t, enroller.calls.Load(), "enrollment must not run without a listener")
}

// TestGRPCSpawnServer_StopClosesABoundListener: a listener bound but never
// served is released by Stop, and a stopped server does not bind again.
func TestGRPCSpawnServer_StopClosesABoundListener(t *testing.T) {
	t.Parallel()
	server, err := NewGRPCSpawnServer(GRPCSpawnServerDeps{
		Runtime:  newTestRuntime(t),
		BindAddr: "127.0.0.1:0",
		Logger:   &testLogger{t: t},
	})
	require.NoError(t, err)
	require.NoError(t, server.Bind(context.Background()))
	addr := server.Addr()
	require.NotEmpty(t, addr)

	require.NoError(t, server.Stop(context.Background()))

	conn, dialErr := net.Dial("tcp", addr)
	if dialErr == nil {
		_ = conn.Close()
	}
	assert.Error(t, dialErr, "the listener must be closed")
	assert.ErrorIs(t, server.Bind(context.Background()), ErrSpawnServerStopped)
	assert.ErrorIs(t, server.Start(context.Background()), ErrSpawnServerStopped)
}

// statusRecordingHeartbeat keeps the last status it was sent.
type statusRecordingHeartbeat struct {
	status HeartbeatStatus
}

func (h *statusRecordingHeartbeat) SendHeartbeat(_ context.Context, _ *orchestration.AgentIdentity, status HeartbeatStatus) error {
	h.status = status
	return nil
}

// TestDaemon_HeartbeatCarriesTheDialAddress: a heartbeat names the advertised
// address when one is set and the spawn server's address otherwise, so a
// re-registration from a heartbeat keeps the address enrollment proved.
func TestDaemon_HeartbeatCarriesTheDialAddress(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ advertise, want string }{
		{"agent01.example.test:9091", "agent01.example.test:9091"},
		{"", "198.51.100.7:9091"},
	} {
		cfg := newTestConfig(t)
		cfg.AdvertiseAddr = tc.advertise
		hb := &statusRecordingHeartbeat{}
		d, err := NewDaemon(cfg, DaemonDeps{
			Runtime:     newTestRuntime(t),
			Enroller:    &fakeEnroller{},
			Heartbeat:   hb,
			SpawnServer: &fakeSpawnServer{addr: "198.51.100.7:9091"},
			Logger:      &testLogger{t: t},
		})
		require.NoError(t, err)
		d.identity.Store(&orchestration.AgentIdentity{AgentID: "a", TenantID: cfg.TenantID})

		d.sendHeartbeat(context.Background())

		assert.Equal(t, tc.want, hb.status.BindAddr)
	}
}
