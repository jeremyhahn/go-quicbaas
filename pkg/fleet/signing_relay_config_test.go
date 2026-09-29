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
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestConfig_EffectiveSocketsRootDerivesLikeTheServer: an explicit root is
// used as given; an unset one is derived exactly as the server derives its own
// — under $XDG_RUNTIME_DIR per instance, or <work_dir>/s — and is absolute, so
// the mount policy compares it against absolute mount sources.
func TestConfig_EffectiveSocketsRootDerivesLikeTheServer(t *testing.T) {
	workDir := t.TempDir()

	explicit := &Config{WorkDir: workDir, SocketsRoot: "/run/qa"}
	if got, err := explicit.EffectiveSocketsRoot(); err != nil || got != "/run/qa" {
		t.Fatalf("explicit = %q, %v", got, err)
	}

	t.Setenv("XDG_RUNTIME_DIR", "")
	derived := &Config{WorkDir: workDir}
	got, err := derived.EffectiveSocketsRoot()
	if err != nil || got != filepath.Join(workDir, config.SocketsRootDataDirName) {
		t.Fatalf("derived without a runtime dir = %q, %v", got, err)
	}
	if policy := derived.MountPolicy(); policy.SocketsRoot != got {
		t.Fatalf("the mount policy admits %q, want the sockets root %q", policy.SocketsRoot, got)
	}

	runtimeDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	got, err = derived.EffectiveSocketsRoot()
	if err != nil || !strings.HasPrefix(got, filepath.Join(runtimeDir, config.SocketsRootRuntimeDirName)+string(filepath.Separator)) {
		t.Fatalf("derived under a runtime dir = %q, %v", got, err)
	}
}

// TestConfig_EffectiveSocketsRootRefusesNothingToDeriveFrom: with no work
// directory there is nothing to derive a root from, and the refusal is typed;
// the mount policy then grants no socket at all.
func TestConfig_EffectiveSocketsRootRefusesNothingToDeriveFrom(t *testing.T) {
	empty := &Config{}
	if _, err := empty.EffectiveSocketsRoot(); !errors.Is(err, ErrConfigSocketsRoot) {
		t.Fatalf("err = %v, want ErrConfigSocketsRoot", err)
	}
	if policy := empty.MountPolicy(); policy.SocketsRoot != "" {
		t.Fatalf("a root that could not be resolved was granted: %q", policy.SocketsRoot)
	}
}

// TestHeartbeatBody_CarriesTheSigningRelay: the relay is re-advertised on every
// heartbeat, so the control plane's trait follows the daemon.
func TestHeartbeatBody_CarriesTheSigningRelay(t *testing.T) {
	on := heartbeatRequestBody(HeartbeatStatus{Backends: []string{"claude"}, SigningRelay: true})
	if !strings.Contains(on, `"signing_relay":true`) {
		t.Fatalf("body = %s, want signing_relay true", on)
	}
	off := heartbeatRequestBody(HeartbeatStatus{Backends: []string{"claude"}})
	if !strings.Contains(off, `"signing_relay":false`) {
		t.Fatalf("body = %s, want signing_relay false", off)
	}
}

// TestEnroll_AdvertisesTheSigningRelay: a daemon whose runtime serves relayed
// signing sockets says so on its register payload, so a signing spawn can be
// placed on it from the first spawn; one whose runtime does not, does not.
func TestEnroll_AdvertisesTheSigningRelay(t *testing.T) {
	withProbe(t, &stubDetector{})
	for name, tc := range map[string]struct {
		relay func() bool
		want  bool
	}{
		"relay served":     {func() bool { return true }, true},
		"relay not served": {func() bool { return false }, false},
		"nothing wired":    {nil, false},
	} {
		capture := &registerPayloadCapture{t: t}
		srv := httptest.NewTLSServer(capture.handler())
		cfg := newEnrollerConfig(t, srv.URL)
		require.NoError(t, os.MkdirAll(cfg.WorkDir, 0o700))
		enroller := NewProductionEnroller(ProductionEnrollerDeps{
			HTTPClient:     srv.Client(),
			Logger:         &testLogger{t: t},
			ServedBackends: func() []string { return []string{"claude"} },
			SigningRelay:   tc.relay,
		})
		_, err := enroller.Enroll(context.Background(), cfg)
		srv.Close()
		require.NoError(t, err, name)

		metadata, ok := capture.body["metadata"].(map[string]any)
		require.True(t, ok, name)
		backends, ok := metadata["backends"].(string)
		require.True(t, ok, name)
		got := strings.Contains(","+backends+",", ","+orchestration.FleetNodeTraitSigningRelay+",")
		require.Equal(t, tc.want, got, "%s: backends = %q", name, backends)
	}
}

// relayDaemon builds a daemon whose runtime binds relays under socketsRoot.
func relayDaemon(t *testing.T, socketsRoot string) (*Daemon, *fakeSpawnServer) {
	t.Helper()
	cfg := newTestConfig(t)
	runtime, err := NewRuntime(RuntimeConfig{
		Backends:    map[string]orchestration.AgentBackend{"mock": &mockBackend{}},
		Logger:      &testLogger{t: t},
		WorkDir:     cfg.WorkDir,
		SocketsRoot: socketsRoot,
	})
	require.NoError(t, err)
	spawnServer := &fakeSpawnServer{addr: "127.0.0.1:0"}
	d, err := NewDaemon(cfg, DaemonDeps{
		Runtime:     runtime,
		Enroller:    &fakeEnroller{identity: &orchestration.AgentIdentity{AgentID: "agent-test", TenantID: cfg.TenantID}},
		SpawnServer: spawnServer,
		Logger:      &testLogger{t: t},
	})
	require.NoError(t, err)
	return d, spawnServer
}

// TestDaemon_StartSweepsTheRelaySocketsAKilledPredecessorLeft: the relay
// sockets a killed daemon left bound are swept before the spawn server accepts
// a spawn that would bind a new one.
func TestDaemon_StartSweepsTheRelaySocketsAKilledPredecessorLeft(t *testing.T) {
	root := shortDir(t, "qbr")
	name, err := orchestration.NewSigningSocketDirName()
	require.NoError(t, err)
	orphan := filepath.Join(root, name)
	require.NoError(t, os.Mkdir(orphan, 0o700))

	d, spawnServer := relayDaemon(t, root)
	require.NoError(t, d.Start(context.Background()))
	t.Cleanup(func() {
		if err := d.Stop(context.Background()); err != nil {
			t.Errorf("stop: %v", err)
		}
	})
	_, statErr := os.Stat(orphan)
	require.True(t, os.IsNotExist(statErr), "the orphaned relay directory survived startup: %v", statErr)
	require.True(t, spawnServer.started.Load())
}

// TestDaemon_StartRefusesARelayRootItCannotVouchFor: a sockets root another
// user can write to cannot hold a relay socket safely, so the daemon does not
// start — the same call the control plane makes about its own root.
func TestDaemon_StartRefusesARelayRootItCannotVouchFor(t *testing.T) {
	shared := filepath.Join(shortDir(t, "qbr"), "shared")
	require.NoError(t, os.Mkdir(shared, 0o777)) // #nosec G301 -- the point of the case
	require.NoError(t, os.Chmod(shared, 0o777)) // #nosec G302 -- umask would otherwise hide it

	d, spawnServer := relayDaemon(t, shared)
	err := d.Start(context.Background())
	require.True(t, errors.Is(err, orchestration.ErrSigningSocketRoot), "Start err = %v", err)
	require.Equal(t, "failed", d.State())
	require.False(t, spawnServer.started.Load(), "the spawn server started behind an unusable relay root")
}

// TestHeartbeatRequestBody_ImagesOnlyWhenListed proves the images field is
// sent when the host listed them and absent otherwise, so a failed listing
// never reads as a host with no images.
func TestHeartbeatRequestBody_ImagesOnlyWhenListed(t *testing.T) {
	with := heartbeatRequestBody(HeartbeatStatus{Backends: []string{"claude"}, Images: []string{"worker:0.11.0"}})
	if !strings.Contains(with, `"images":["worker:0.11.0"]`) {
		t.Fatalf("body = %s, want the listed images", with)
	}
	without := heartbeatRequestBody(HeartbeatStatus{Backends: []string{"claude"}})
	if strings.Contains(without, `"images"`) {
		t.Fatalf("body = %s, want no images field when none were listed", without)
	}
}
