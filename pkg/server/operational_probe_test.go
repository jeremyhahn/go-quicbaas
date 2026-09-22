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

package server

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// stubCeremonyStateReader returns a fixed ceremony status or error.
type stubCeremonyStateReader struct {
	state string
	err   error
	calls int
}

func (s *stubCeremonyStateReader) CeremonyStatus(context.Context) (*qrdbsdk.CeremonyStatusInfo, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	if s.state == "" {
		// Models a servicer that returns neither a status nor an error.
		return nil, nil
	}
	return &qrdbsdk.CeremonyStatusInfo{State: s.state}, nil
}

// stubBarrierStatusReader returns a fixed barrier status or error.
type stubBarrierStatusReader struct {
	initializedAt time.Time
	nilStatus     bool
	err           error
}

func (s *stubBarrierStatusReader) BarrierStatus(context.Context) (*qrdbsdk.BarrierStatusInfo, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.nilStatus {
		return nil, nil
	}
	return &qrdbsdk.BarrierStatusInfo{InitializedAt: s.initializedAt}, nil
}

// initializedBarrier is a barrier that already carries a persisted
// initialization timestamp (the daemon-restart case).
func initializedBarrier() *stubBarrierStatusReader {
	return &stubBarrierStatusReader{initializedAt: time.Unix(1700000000, 0).UTC()}
}

// virginBarrier is a barrier that has never been initialized (first boot).
func virginBarrier() *stubBarrierStatusReader {
	return &stubBarrierStatusReader{}
}

// TestServerOperationalProbe_StateMatrix pins the full decision matrix of the
// two signals the probe combines. Every ambiguous combination must report
// OPERATIONAL (fail closed) so the pre-operational authentication exemption is
// withheld; only a positively-unfinished ceremony reports pre-operational.
func TestServerOperationalProbe_StateMatrix(t *testing.T) {
	cases := []struct {
		name    string
		probe   *serverOperationalProbe
		wantOps bool
	}{
		{
			name: "enrolling is pre-operational even with an initialized barrier",
			// M-of-N enrollment in progress: Initialize ran in THIS process, so
			// the officer share enclaves exist and the claim handshakes are
			// live. The barrier is necessarily initialized by then, which is
			// why the barrier signal alone cannot decide this case.
			probe: &serverOperationalProbe{
				ceremony: &stubCeremonyStateReader{state: string(qrdbsdk.StateEnrolling)},
				barrier:  initializedBarrier(),
			},
			wantOps: false,
		},
		{
			name: "awaiting-init on a never-initialized barrier is pre-operational",
			probe: &serverOperationalProbe{
				ceremony: &stubCeremonyStateReader{state: string(qrdbsdk.StateAwaitingInit)},
				barrier:  virginBarrier(),
			},
			wantOps: false,
		},
		{
			name: "awaiting-init on an INITIALIZED barrier is operational",
			// THE DEFECT: qrdb's ceremony state is in-memory and resets to
			// awaiting_init on every process start, so a restarted
			// already-initialized daemon reports awaiting_init while sealed.
			// The persisted barrier timestamp is what closes the window.
			probe: &serverOperationalProbe{
				ceremony: &stubCeremonyStateReader{state: string(qrdbsdk.StateAwaitingInit)},
				barrier:  initializedBarrier(),
			},
			wantOps: true,
		},
		{
			name: "operational state is operational",
			probe: &serverOperationalProbe{
				ceremony: &stubCeremonyStateReader{state: string(qrdbsdk.StateOperational)},
				barrier:  virginBarrier(),
			},
			wantOps: true,
		},
		{
			name: "unrecognized ceremony state fails closed",
			probe: &serverOperationalProbe{
				ceremony: &stubCeremonyStateReader{state: "half_way_through_something"},
				barrier:  virginBarrier(),
			},
			wantOps: true,
		},
		{
			name: "ceremony status error fails closed",
			probe: &serverOperationalProbe{
				ceremony: &stubCeremonyStateReader{err: errors.New("sealed")},
				barrier:  virginBarrier(),
			},
			wantOps: true,
		},
		{
			name: "ceremony status returning no info fails closed",
			probe: &serverOperationalProbe{
				ceremony: &stubCeremonyStateReader{},
				barrier:  virginBarrier(),
			},
			wantOps: true,
		},
		{
			name:    "absent ceremony service fails closed",
			probe:   &serverOperationalProbe{barrier: virginBarrier()},
			wantOps: true,
		},
		{
			name: "awaiting-init with an absent barrier service fails closed",
			probe: &serverOperationalProbe{
				ceremony: &stubCeremonyStateReader{state: string(qrdbsdk.StateAwaitingInit)},
			},
			wantOps: true,
		},
		{
			name: "awaiting-init with a barrier status error fails closed",
			probe: &serverOperationalProbe{
				ceremony: &stubCeremonyStateReader{state: string(qrdbsdk.StateAwaitingInit)},
				barrier:  &stubBarrierStatusReader{err: errors.New("backend down")},
			},
			wantOps: true,
		},
		{
			name: "awaiting-init with a nil barrier status fails closed",
			probe: &serverOperationalProbe{
				ceremony: &stubCeremonyStateReader{state: string(qrdbsdk.StateAwaitingInit)},
				barrier:  &stubBarrierStatusReader{nilStatus: true},
			},
			wantOps: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantOps, tc.probe.IsOperational())
		})
	}
}

// TestServerOperationalProbe_NilReceiver pins that a nil probe reports
// operational rather than panicking: an absent probe must never widen the
// unauthenticated surface.
func TestServerOperationalProbe_NilReceiver(t *testing.T) {
	var probe *serverOperationalProbe
	assert.True(t, probe.IsOperational())
}

// TestServerOperationalProbe_ReadsLiveState pins that the verdict is recomputed
// per call rather than cached at construction: the gate is installed once at
// boot and must observe the ceremony completing under it.
func TestServerOperationalProbe_ReadsLiveState(t *testing.T) {
	ceremony := &stubCeremonyStateReader{state: string(qrdbsdk.StateEnrolling)}
	probe := &serverOperationalProbe{ceremony: ceremony, barrier: initializedBarrier()}

	assert.False(t, probe.IsOperational())

	ceremony.state = string(qrdbsdk.StateOperational)
	assert.True(t, probe.IsOperational())
	assert.Equal(t, 2, ceremony.calls, "state must be read on every call")
}

// TestServerOperationalProbe wires the probe the way lifecycle does and asserts
// the snapshot: with no ceremony services the probe fails closed, and with them
// wired it reflects the live state.
func TestServerOperationalProbe(t *testing.T) {
	t.Run("no ceremony services fails closed", func(t *testing.T) {
		s := &Server{}
		probe := s.OperationalProbe()
		require.NotNil(t, probe)
		assert.True(t, probe.IsOperational())
	})

	t.Run("nil server fails closed", func(t *testing.T) {
		var s *Server
		assert.True(t, s.OperationalProbe().IsOperational())
	})

	t.Run("empty ceremony services fails closed", func(t *testing.T) {
		s := &Server{ceremonyServices: &CeremonyServices{}}
		assert.True(t, s.OperationalProbe().IsOperational())
	})

	// End-to-end against the REAL qrdb ceremony + barrier services (in-memory
	// backend, no host state): a fresh deployment is pre-operational, and the
	// SAME probe flips to operational once the barrier carries a persisted
	// initialization timestamp — the restart case the in-memory ceremony state
	// alone cannot distinguish.
	t.Run("real services: first boot is pre-operational", func(t *testing.T) {
		s := &Server{ceremonyServices: newRealCeremonyServices(t)}
		assert.False(t, s.OperationalProbe().IsOperational())
	})

	t.Run("real services: initialized barrier is operational", func(t *testing.T) {
		services := newRealCeremonyServices(t)
		s := &Server{ceremonyServices: services}
		probe := s.OperationalProbe()
		require.False(t, probe.IsOperational(), "precondition: uninitialized")

		require.NoError(t, services.Barrier.BarrierInitialize(context.Background(), []byte("unit-test-secret")))

		assert.True(t, probe.IsOperational(),
			"an initialized barrier must report operational even though the "+
				"in-memory ceremony state is still awaiting_init")
	})
}

// newRealCeremonyServices builds the production bootstrap ceremony stack over
// an in-memory backend: a real qrdb StorageBarrier, credential service and
// ceremony service. No host state is touched and nothing blocks.
func newRealCeremonyServices(t *testing.T) *CeremonyServices {
	t.Helper()
	services, err := buildBootstrapCeremonyServices(slog.Default(), t.TempDir())
	require.NoError(t, err)
	require.NotNil(t, services)
	return services
}
