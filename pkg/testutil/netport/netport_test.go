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

//go:build unix

package netport

import (
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// Reserve
// -----------------------------------------------------------------------------

// TestReserve_PortsAreDistinctAndOutsideTheEphemeralRange asserts the two
// properties the reservation rests on: the caller gets distinct ports, and none
// of them is a port the kernel could hand to somebody else's ":0" bind.
func TestReserve_PortsAreDistinctAndOutsideTheEphemeralRange(t *testing.T) {
	t.Parallel()

	ports, err := Reserve(16)
	require.NoError(t, err, "reserving ports must succeed on a host with a normal ephemeral range")
	require.Len(t, ports, 16)

	ephemeralLow, ephemeralHigh := ephemeralRange()
	seen := make(map[int]bool, len(ports))
	for _, port := range ports {
		assert.False(t, seen[port], "port %d handed out twice in one call", port)
		seen[port] = true
		assert.False(t, port >= ephemeralLow && port <= ephemeralHigh,
			"port %d lies inside the kernel ephemeral range %d-%d, so a ':0' bind elsewhere could take it",
			port, ephemeralLow, ephemeralHigh)
		assert.GreaterOrEqual(t, port, lowestReservablePort, "port %d is privileged", port)
		assert.LessOrEqual(t, port, 65535, "port %d is not a port", port)
	}
}

// TestReserve_RejectsNonPositiveCount is the bad-input half: asking for no
// ports is a caller bug and must be reported, not silently answered with an
// empty slice.
func TestReserve_RejectsNonPositiveCount(t *testing.T) {
	t.Parallel()

	for _, count := range []int{0, -1, -1024} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			t.Parallel()

			ports, err := Reserve(count)
			assert.Nil(t, ports)

			var invalid *InvalidCountError
			require.ErrorAs(t, err, &invalid, "a non-positive count must yield InvalidCountError")
			assert.Equal(t, count, invalid.Count)
		})
	}
}

// TestReserve_ParallelDrawsNeverHandOutTheSamePort is the go-quicbaas#706
// assertion. The helper it replaced drew each port by listening on ":0" and
// closing the listener, so two concurrent draws could be handed the same number
// and the second server to bind it lost. Every port drawn concurrently here
// must be unique across every drawer.
func TestReserve_ParallelDrawsNeverHandOutTheSamePort(t *testing.T) {
	t.Parallel()

	const (
		drawers        = 12
		portsPerDrawer = 8
	)

	var wg sync.WaitGroup
	results := make([][]int, drawers)
	errs := make([]error, drawers)
	for i := range drawers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = Reserve(portsPerDrawer)
		}()
	}
	wg.Wait()

	owner := make(map[int]int, drawers*portsPerDrawer)
	for i, ports := range results {
		require.NoError(t, errs[i], "drawer %d", i)
		require.Len(t, ports, portsPerDrawer, "drawer %d", i)
		for _, port := range ports {
			previous, clash := owner[port]
			require.False(t, clash,
				"port %d handed to drawer %d and drawer %d at once — this is the #706 collision",
				port, previous, i)
			owner[port] = i
		}
	}
	require.Len(t, owner, drawers*portsPerDrawer)
}

// TestReserve_PortsBindOnBothTransports proves the reservation is usable for
// what pkg/server actually does with it: a qrdb REST listener takes the TCP
// port and its HTTP/3 sibling takes the same number on UDP.
func TestReserve_PortsBindOnBothTransports(t *testing.T) {
	t.Parallel()

	ports, err := Reserve(4)
	require.NoError(t, err)

	for _, port := range ports {
		addr := LoopbackAddr(port)

		listener, listenErr := net.Listen("tcp", addr)
		require.NoError(t, listenErr, "reserved port %d must be bindable on TCP", port)
		packet, packetErr := net.ListenPacket("udp", addr)
		require.NoError(t, packetErr, "reserved port %d must be bindable on UDP", port)

		require.NoError(t, listener.Close())
		require.NoError(t, packet.Close())
	}
}

// TestReserve_NeverRepeatsWhileHeld asserts a reservation outlives the call
// that made it. Handing a live reservation to a later caller would put back
// exactly the window this package removes.
func TestReserve_NeverRepeatsWhileHeld(t *testing.T) {
	t.Parallel()

	first, err := Reserve(8)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, Release(first...)) })

	second, err := Reserve(8)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, Release(second...)) })

	drawn := make(map[int]bool, len(first))
	for _, port := range first {
		drawn[port] = true
	}
	for _, port := range second {
		assert.False(t, drawn[port], "port %d was handed out while still reserved", port)
	}
}

// -----------------------------------------------------------------------------
// Release
// -----------------------------------------------------------------------------

// TestRelease_ReturnsThePortToTheWindow covers the success path. Recycling is
// what keeps the window a workable size for a package that draws at hundreds of
// call sites, so a released port must genuinely become drawable again.
func TestRelease_ReturnsThePortToTheWindow(t *testing.T) {
	t.Parallel()

	ports, err := Reserve(1)
	require.NoError(t, err)
	port := ports[0]

	dir := t.TempDir()
	locked, result, err := acquire(dir, port)
	require.NoError(t, err)
	require.Equal(t, outcomeReserved, result, "premise: a private lock dir sees the port as free")
	require.NoError(t, locked.Close())

	require.NoError(t, Release(port))

	// With the reservation gone the shared lock is free again, which is what a
	// later draw needs.
	shared, err := lockDir()
	require.NoError(t, err)
	reclaimed, result, err := acquire(shared, port)
	require.NoError(t, err)
	require.Equal(t, outcomeReserved, result, "a released port must be acquirable again")
	require.NoError(t, reclaimed.Close())
}

// TestRelease_IsHarmlessForPortsItDoesNotHold is the bad-input half: releasing
// a port that was never reserved, or releasing twice, must not error and must
// not touch some other holder's reservation.
func TestRelease_IsHarmlessForPortsItDoesNotHold(t *testing.T) {
	t.Parallel()

	ports, err := Reserve(1)
	require.NoError(t, err)
	port := ports[0]

	require.NoError(t, Release(port))
	require.NoError(t, Release(port), "a second release is a no-op")
	require.NoError(t, Release(0, -1, 70000), "ports that were never reserved are skipped")
}

// TestReleaseError_WrapsItsCause pins the typed error callers match on when a
// lock file will not close.
func TestReleaseError_WrapsItsCause(t *testing.T) {
	t.Parallel()

	err := error(&ReleaseError{Port: 30000, Cause: os.ErrClosed})

	var releaseErr *ReleaseError
	require.ErrorAs(t, err, &releaseErr)
	assert.Equal(t, 30000, releaseErr.Port)
	assert.True(t, errors.Is(err, os.ErrClosed))
	assert.Contains(t, err.Error(), "30000")
}

// -----------------------------------------------------------------------------
// Must* wrappers
// -----------------------------------------------------------------------------
//
// The failure path of these wrappers is Reserve's, which
// TestReserve_RejectsNonPositiveCount covers directly. It cannot be exercised
// through the wrappers themselves: they report failure with t.Fatalf, and
// testing.TB is sealed against a recording substitute.

// TestMustReserve_ReturnsTheRequestedCount covers the wrapper's success path.
func TestMustReserve_ReturnsTheRequestedCount(t *testing.T) {
	t.Parallel()

	ports := MustReserve(t, 3)
	require.Len(t, ports, 3)
	assert.NotEqual(t, ports[0], ports[1])
	assert.NotEqual(t, ports[1], ports[2])
}

// TestMustReserveOne_ReturnsAUsablePort covers the single-port wrapper.
func TestMustReserveOne_ReturnsAUsablePort(t *testing.T) {
	t.Parallel()

	port := MustReserveOne(t)
	listener, err := net.Listen("tcp", LoopbackAddr(port))
	require.NoError(t, err)
	require.NoError(t, listener.Close())
}

// TestMustReserveLoopbackAddr_ReturnsABindableAddress covers the address
// wrapper end to end.
func TestMustReserveLoopbackAddr_ReturnsABindableAddress(t *testing.T) {
	t.Parallel()

	addr := MustReserveLoopbackAddr(t)
	host, port, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	assert.Equal(t, LoopbackHost, host)

	listener, err := net.Listen("tcp", addr)
	require.NoError(t, err, "MustReserveLoopbackAddr must return a bindable address, got %q", addr)
	require.NoError(t, listener.Close())

	number, err := strconv.Atoi(port)
	require.NoError(t, err)
	assert.Positive(t, number)
}

// TestLoopbackAddr_RendersHostAndPort covers rendering, including the zero port
// a caller must never be handed but which must still render rather than panic.
func TestLoopbackAddr_RendersHostAndPort(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "127.0.0.1:8443", LoopbackAddr(8443))
	assert.Equal(t, "127.0.0.1:0", LoopbackAddr(0))
}

// -----------------------------------------------------------------------------
// acquire
// -----------------------------------------------------------------------------

// TestAcquire_TakesAFreePort covers the success path against a private lock
// directory, so the assertion does not depend on what the rest of the suite has
// already reserved.
func TestAcquire_TakesAFreePort(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	port := MustReserveOne(t)

	// The port is already this process's, so a fresh lock directory sees it as
	// untaken while the kernel still agrees it is free.
	file, result, err := acquire(dir, port)
	require.NoError(t, err)
	require.Equal(t, outcomeReserved, result, "a free, unlocked port must be acquirable")
	require.NotNil(t, file, "the open lock file IS the reservation")
	assert.FileExists(t, lockPath(dir, port))
	require.NoError(t, file.Close())
}

// TestAcquire_SkipsAPortAlreadyLocked asserts the cross-process exclusion. A
// second acquire of the same port through a second open file description must
// be refused — that is exactly what a sibling test binary's attempt looks like.
func TestAcquire_SkipsAPortAlreadyLocked(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	port := MustReserveOne(t)

	first, result, err := acquire(dir, port)
	require.NoError(t, err)
	require.Equal(t, outcomeReserved, result)
	t.Cleanup(func() { require.NoError(t, first.Close()) })

	second, result, err := acquire(dir, port)
	require.NoError(t, err, "a locked port is a skip, not a failure")
	assert.Equal(t, outcomeLockedOut, result, "a port already locked must not be handed out again")
	assert.Nil(t, second)
}

// TestAcquire_SkipsAPortSomethingIsListeningOn asserts the liveness probe: a
// port held by a process that never took our lock — a service that happens to
// sit in the reserved window — must be rejected.
func TestAcquire_SkipsAPortSomethingIsListeningOn(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	port := MustReserveOne(t)

	holder, err := net.Listen("tcp", LoopbackAddr(port))
	require.NoError(t, err)
	t.Cleanup(func() {
		if closeErr := holder.Close(); closeErr != nil {
			t.Logf("closing the holding listener: %v", closeErr)
		}
	})

	file, result, err := acquire(dir, port)
	require.NoError(t, err)
	assert.Equal(t, outcomeOccupied, result, "a port with a live TCP listener must not be handed out")
	assert.Nil(t, file)
}

// TestAcquire_SkipsAPortHeldOnUDPOnly is the dual-stack half. TCP and UDP are
// independent namespaces, and a qrdb REST listener needs both, so a port free
// on TCP but held on UDP is unusable.
func TestAcquire_SkipsAPortHeldOnUDPOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	port := MustReserveOne(t)

	holder, err := net.ListenPacket("udp", LoopbackAddr(port))
	require.NoError(t, err)
	t.Cleanup(func() {
		if closeErr := holder.Close(); closeErr != nil {
			t.Logf("closing the holding packet conn: %v", closeErr)
		}
	})

	// Premise: TCP really is free on that port.
	probe, err := net.Listen("tcp", LoopbackAddr(port))
	require.NoError(t, err, "premise failed: TCP %d is not free", port)
	require.NoError(t, probe.Close())

	file, result, err := acquire(dir, port)
	require.NoError(t, err)
	assert.Equal(t, outcomeOccupied, result, "a port held on UDP must not be handed out")
	assert.Nil(t, file)
}

// TestAcquire_FailsWhenTheLockFileCannotBeOpened is the bad-input half: an
// unusable lock directory is fatal, because every candidate would fail the same
// way and searching the whole window would only slow the failure down.
func TestAcquire_FailsWhenTheLockFileCannotBeOpened(t *testing.T) {
	t.Parallel()

	notADir := filepath.Join(t.TempDir(), "occupied")
	require.NoError(t, os.WriteFile(notADir, []byte("not a directory"), 0o600))

	file, _, err := acquire(notADir, 30000)
	assert.Nil(t, file)

	var lockErr *LockFileError
	require.ErrorAs(t, err, &lockErr)
	assert.Equal(t, 30000, lockErr.Port)
	assert.Error(t, lockErr.Unwrap())
}

// TestLockPath_NamesTheFileAfterThePort covers both a normal and an
// out-of-range port number, since lockPath must not silently normalise either.
func TestLockPath_NamesTheFileAfterThePort(t *testing.T) {
	t.Parallel()

	assert.Equal(t, filepath.Join("/locks", "30000.lock"), lockPath("/locks", 30000))
	assert.Equal(t, filepath.Join("/locks", "0.lock"), lockPath("/locks", 0))
}

// -----------------------------------------------------------------------------
// probes
// -----------------------------------------------------------------------------

// TestDualStackFree_TrueWhenBothTransportsAreFree covers the success path.
func TestDualStackFree_TrueWhenBothTransportsAreFree(t *testing.T) {
	t.Parallel()

	assert.True(t, dualStackFree(MustReserveOne(t)))
}

// TestDualStackFree_FalseWhenEitherTransportIsHeld covers both failure halves
// in one table so neither can regress unnoticed.
func TestDualStackFree_FalseWhenEitherTransportIsHeld(t *testing.T) {
	t.Parallel()

	tests := map[string]func(t *testing.T, addr string){
		"tcp held": func(t *testing.T, addr string) {
			listener, err := net.Listen("tcp", addr)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, listener.Close()) })
		},
		"udp held": func(t *testing.T, addr string) {
			packet, err := net.ListenPacket("udp", addr)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, packet.Close()) })
		},
	}

	for name, hold := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			port := MustReserveOne(t)
			hold(t, LoopbackAddr(port))
			assert.False(t, dualStackFree(port))
		})
	}
}

// TestCloseProbe_TrueWhenBothProbesClose covers the success path, including the
// nil packet conn dualStackFree passes when the UDP bind never happened.
func TestCloseProbe_TrueWhenBothProbesClose(t *testing.T) {
	t.Parallel()

	addr := LoopbackAddr(MustReserveOne(t))
	listener, err := net.Listen("tcp", addr)
	require.NoError(t, err)
	packet, err := net.ListenPacket("udp", addr)
	require.NoError(t, err)

	assert.True(t, closeProbe(listener, packet))

	other, err := net.Listen("tcp", LoopbackAddr(MustReserveOne(t)))
	require.NoError(t, err)
	assert.True(t, closeProbe(other, nil), "a nil packet conn is the TCP-only probe path")
}

// TestCloseProbe_FalseWhenAProbeWillNotClose is the bad-input half: a socket
// that refuses to close leaves the port in an unknown state, so the candidate
// must be rejected.
func TestCloseProbe_FalseWhenAProbeWillNotClose(t *testing.T) {
	t.Parallel()

	addr := LoopbackAddr(MustReserveOne(t))
	listener, err := net.Listen("tcp", addr)
	require.NoError(t, err)
	require.NoError(t, listener.Close())

	// A second close fails, which is how a probe that cannot be released looks.
	assert.False(t, closeProbe(listener, nil))

	// The same must hold when it is the UDP half that will not release.
	other := LoopbackAddr(MustReserveOne(t))
	open, err := net.Listen("tcp", other)
	require.NoError(t, err)
	packet, err := net.ListenPacket("udp", other)
	require.NoError(t, err)
	require.NoError(t, packet.Close())

	assert.False(t, closeProbe(open, packet))
}

// -----------------------------------------------------------------------------
// window arithmetic
// -----------------------------------------------------------------------------

// TestComputeWindow_PlacesTheWindowClearOfTheEphemeralRange tables the three
// shapes of host: the ordinary one with room below, one whose ephemeral range
// reaches down into the registered ports so only the space above is usable, and
// one that leaves no room at all.
func TestComputeWindow_PlacesTheWindowClearOfTheEphemeralRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		low, high    int
		wantLow      int
		wantHigh     int
		wantUnusable bool
	}{
		{
			name:     "linux default leaves room below",
			low:      32768,
			high:     60999,
			wantLow:  32768 - windowSize,
			wantHigh: 32768,
		},
		{
			name:     "darwin default leaves room below",
			low:      49152,
			high:     65535,
			wantLow:  49152 - windowSize,
			wantHigh: 49152,
		},
		{
			name:     "range starting low forces the window above it",
			low:      2048,
			high:     60999,
			wantLow:  61000,
			wantHigh: min(61000+windowSize, 65536),
		},
		{
			name:         "range covering everything leaves no window",
			low:          1024,
			high:         65535,
			wantUnusable: true,
		},
		{
			name:         "range ending just short of the top leaves too little",
			low:          1024,
			high:         65400,
			wantUnusable: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			low, high, err := computeWindow(tc.low, tc.high)
			if tc.wantUnusable {
				var unusable *WindowUnavailableError
				require.ErrorAs(t, err, &unusable)
				assert.Equal(t, tc.low, unusable.EphemeralLow)
				assert.Equal(t, tc.high, unusable.EphemeralHigh)
				assert.NotEmpty(t, unusable.Error())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantLow, low)
			assert.Equal(t, tc.wantHigh, high)
			assert.GreaterOrEqual(t, high-low, minWindowSize)
			// The whole point: no reserved port may fall in the ephemeral range.
			assert.True(t, high <= tc.low || low > tc.high,
				"window %d-%d overlaps the ephemeral range %d-%d", low, high, tc.low, tc.high)
		})
	}
}

// TestWindow_IsStableAcrossCalls asserts the process-wide window is computed
// once. A window that moved between calls would let a later call hand out a
// port an earlier one had already reserved under a different range.
func TestWindow_IsStableAcrossCalls(t *testing.T) {
	t.Parallel()

	firstLow, firstHigh, err := window()
	require.NoError(t, err)
	secondLow, secondHigh, err := window()
	require.NoError(t, err)

	assert.Equal(t, firstLow, secondLow)
	assert.Equal(t, firstHigh, secondHigh)
	assert.GreaterOrEqual(t, firstHigh-firstLow, minWindowSize)
}

// TestParsePortRange_ReadsBothBounds covers the shape Linux actually writes,
// including the trailing newline and the tab separator.
func TestParsePortRange_ReadsBothBounds(t *testing.T) {
	t.Parallel()

	tests := map[string]struct{ low, high int }{
		"32768\t60999\n":      {32768, 60999},
		"1024 65535":          {1024, 65535},
		"  15000   20000  \n": {15000, 20000},
	}

	for content, want := range tests {
		t.Run(strconv.Quote(content), func(t *testing.T) {
			t.Parallel()

			low, high, err := parsePortRange(content)
			require.NoError(t, err)
			assert.Equal(t, want.low, low)
			assert.Equal(t, want.high, high)
		})
	}
}

// TestParsePortRange_RejectsMalformedContent is the bad-input half. A kernel
// file that does not parse must not be silently believed: the caller falls back
// to documented conservative bounds only because this reports the failure.
func TestParsePortRange_RejectsMalformedContent(t *testing.T) {
	t.Parallel()

	for _, content := range []string{
		"",
		"32768",
		"32768 60999 61000",
		"low high",
		"32768 sixty",
		"0 60999",
		"32768 70000",
		"60999 32768",
	} {
		t.Run(strconv.Quote(content), func(t *testing.T) {
			t.Parallel()

			low, high, err := parsePortRange(content)
			assert.Zero(t, low)
			assert.Zero(t, high)

			var malformed *MalformedPortRangeError
			require.ErrorAs(t, err, &malformed)
			assert.Equal(t, content, malformed.Content)
			assert.NotEmpty(t, malformed.Error())
		})
	}
}

// TestEphemeralRange_ReportsUsableBounds covers the reader. On Linux it reads
// the kernel's own file; elsewhere it reports the documented stand-ins. Either
// way the bounds must be a usable range.
func TestEphemeralRange_ReportsUsableBounds(t *testing.T) {
	t.Parallel()

	low, high := ephemeralRange()
	assert.Positive(t, low)
	assert.Less(t, low, high)
	assert.LessOrEqual(t, high, 65535)

	if content, err := os.ReadFile(procPortRange); err == nil {
		kernelLow, kernelHigh, parseErr := parsePortRange(string(content))
		require.NoError(t, parseErr, "this kernel publishes %q", string(content))
		assert.Equal(t, kernelLow, low, "the kernel's own bounds must be used when published")
		assert.Equal(t, kernelHigh, high)
	}
}

// -----------------------------------------------------------------------------
// lock directory
// -----------------------------------------------------------------------------

// TestLockDir_IsAPerUserDirectoryUnderTemp covers the success path and the
// naming that keeps two users' runs from fighting over each other's lock files.
func TestLockDir_IsAPerUserDirectoryUnderTemp(t *testing.T) {
	t.Parallel()

	dir, err := lockDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(os.TempDir(), "go-quicbaas-netport-"+strconv.Itoa(os.Getuid())), dir)

	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

// TestLockDirError_WrapsItsCause is the bad-input half for the directory
// failure: callers match on the type and unwrap to the syscall error.
func TestLockDirError_WrapsItsCause(t *testing.T) {
	t.Parallel()

	cause := os.ErrPermission
	err := error(&LockDirError{Dir: "/nope", Cause: cause})

	var dirErr *LockDirError
	require.ErrorAs(t, err, &dirErr)
	assert.Equal(t, "/nope", dirErr.Dir)
	assert.True(t, errors.Is(err, os.ErrPermission))
	assert.Contains(t, err.Error(), "/nope")
}

// TestExhaustedError_DescribesTheShortfall covers the message a window with
// nothing left produces, which is the only signal a leak would give.
func TestExhaustedError_DescribesTheShortfall(t *testing.T) {
	t.Parallel()

	err := &ExhaustedError{Low: 24576, High: 32768, Requested: 3, Reserved: 1, LockedOut: 8000, Occupied: 191}
	assert.Contains(t, err.Error(), "24576-32767")
	assert.Contains(t, err.Error(), "1 of 3")
	assert.Contains(t, err.Error(), "8000 already reserved")
	assert.Contains(t, err.Error(), "191 occupied")
}

// -----------------------------------------------------------------------------
// window search
// -----------------------------------------------------------------------------

// TestReserveFrom_FindsPortsInASmallWindow covers the search against a window
// narrow enough to reason about exactly.
func TestReserveFrom_FindsPortsInASmallWindow(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	candidates := MustReserve(t, 4)
	low, high := boundsOf(t, candidates)

	ports, err := reserveFrom(dir, low, high, 2)
	require.NoError(t, err)
	require.Len(t, ports, 2)
	t.Cleanup(func() { require.NoError(t, Release(ports...)) })

	for _, port := range ports {
		assert.GreaterOrEqual(t, port, low)
		assert.Less(t, port, high)
	}
}

// TestReserveFrom_ExhaustedWindowReportsTheBreakdown is the bad-input half: a
// window with nothing free must name what filled it, because "already
// reserved" and "occupied" have different fixes.
func TestReserveFrom_ExhaustedWindowReportsTheBreakdown(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	port := MustReserveOne(t)

	// Occupy the only port in the window on TCP, which is what a service
	// sitting inside the reserved range looks like.
	holder, err := net.Listen("tcp", LoopbackAddr(port))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, holder.Close()) })

	ports, err := reserveFrom(dir, port, port+1, 1)
	assert.Empty(t, ports)

	var exhausted *ExhaustedError
	require.ErrorAs(t, err, &exhausted)
	assert.Equal(t, 1, exhausted.Requested)
	assert.Equal(t, 0, exhausted.Reserved)
	assert.Equal(t, 1, exhausted.Occupied)
	assert.Equal(t, 0, exhausted.LockedOut)
}

// TestReserveFrom_CountsPortsLockedByAnotherReservation pins the other half of
// the breakdown, which is what a leaked reservation would show up as.
func TestReserveFrom_CountsPortsLockedByAnotherReservation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	port := MustReserveOne(t)

	locked, result, err := acquire(dir, port)
	require.NoError(t, err)
	require.Equal(t, outcomeReserved, result)
	t.Cleanup(func() { require.NoError(t, locked.Close()) })

	ports, err := reserveFrom(dir, port, port+1, 1)
	assert.Empty(t, ports)

	var exhausted *ExhaustedError
	require.ErrorAs(t, err, &exhausted)
	assert.Equal(t, 1, exhausted.LockedOut)
	assert.Equal(t, 0, exhausted.Occupied)
}

// TestReserveFrom_ReleasesAPartialDraw asserts a draw that cannot be completed
// hands back what it took. A partial draw the caller never sees would hold
// those ports for the life of the process.
func TestReserveFrom_ReleasesAPartialDraw(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	candidates := MustReserve(t, 2)
	low, high := boundsOf(t, candidates)

	// Ask for more ports than the window can hold.
	ports, err := reserveFrom(dir, low, high, high-low+1)
	assert.Empty(t, ports)

	var exhausted *ExhaustedError
	require.ErrorAs(t, err, &exhausted)

	// Everything the failed draw touched must be free again.
	for port := low; port < high; port++ {
		file, result, acquireErr := acquire(dir, port)
		require.NoError(t, acquireErr)
		if result != outcomeReserved {
			continue
		}
		require.NoError(t, file.Close())
	}
	recovered, err := reserveFrom(dir, low, high, 1)
	require.NoError(t, err, "a partial draw must not leave the window locked")
	require.NoError(t, Release(recovered...))
}

// boundsOf returns a window that spans the given reserved ports, so a test can
// search a range it knows the contents of.
func boundsOf(t *testing.T, ports []int) (int, int) {
	t.Helper()
	require.NotEmpty(t, ports)

	low, high := ports[0], ports[0]+1
	for _, port := range ports {
		low = min(low, port)
		high = max(high, port+1)
	}
	return low, high
}

// -----------------------------------------------------------------------------
// release failure
// -----------------------------------------------------------------------------

// TestRelease_ReportsALockFileThatWillNotClose is the failure half of Release.
// A lock file that will not close leaves the port reserved for the rest of the
// process, which the caller has to hear about.
func TestRelease_ReportsALockFileThatWillNotClose(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	port := MustReserveOne(t)

	file, result, err := acquire(dir, port)
	require.NoError(t, err)
	require.Equal(t, outcomeReserved, result)
	require.NoError(t, file.Close())

	// A closed descriptor is how a lock file that cannot be released behaves.
	held.Store(port, file)
	releaseErr := Release(port)

	var release *ReleaseError
	require.ErrorAs(t, releaseErr, &release)
	assert.Equal(t, port, release.Port)
	assert.True(t, errors.Is(releaseErr, os.ErrClosed))
}

// -----------------------------------------------------------------------------
// ephemeral range and lock directory seams
// -----------------------------------------------------------------------------

// TestEphemeralRangeFrom_ReadsAPublishedRange covers the path Linux takes.
func TestEphemeralRangeFrom_ReadsAPublishedRange(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ip_local_port_range")
	require.NoError(t, os.WriteFile(path, []byte("40000\t50000\n"), 0o600))

	low, high := ephemeralRangeFrom(path)
	assert.Equal(t, 40000, low)
	assert.Equal(t, 50000, high)
}

// TestEphemeralRangeFrom_StandsInDefaults is the bad-input half: a kernel that
// publishes nothing, and one that publishes nonsense, both have to yield bounds
// conservative enough to keep the window clear of the real range.
func TestEphemeralRangeFrom_StandsInDefaults(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	malformed := filepath.Join(dir, "malformed")
	require.NoError(t, os.WriteFile(malformed, []byte("not a range"), 0o600))

	for name, path := range map[string]string{
		"absent":    filepath.Join(dir, "missing"),
		"malformed": malformed,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			low, high := ephemeralRangeFrom(path)
			assert.Equal(t, defaultEphemeralLow, low)
			assert.Equal(t, defaultEphemeralHigh, high)
		})
	}
}

// TestEnsureLockDir_CreatesTheDirectory covers the success path, including the
// second call that finds it already there.
func TestEnsureLockDir_CreatesTheDirectory(t *testing.T) {
	t.Parallel()

	want := filepath.Join(t.TempDir(), "locks")
	got, err := ensureLockDir(want)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	got, err = ensureLockDir(want)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// TestEnsureLockDir_ReportsAPathItCannotCreate is the bad-input half.
func TestEnsureLockDir_ReportsAPathItCannotCreate(t *testing.T) {
	t.Parallel()

	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("not a directory"), 0o600))

	got, err := ensureLockDir(filepath.Join(blocker, "locks"))
	assert.Empty(t, got)

	var dirErr *LockDirError
	require.ErrorAs(t, err, &dirErr)
	assert.Error(t, dirErr.Unwrap())
}

// TestDefaultLockDir_IsKeyedByUser pins the naming that keeps two users' runs
// out of each other's lock files.
func TestDefaultLockDir_IsKeyedByUser(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		filepath.Join(os.TempDir(), "go-quicbaas-netport-"+strconv.Itoa(os.Getuid())),
		defaultLockDir())
}

func TestRandomStart_StaysInsideTheWindow(t *testing.T) {
	for range 64 {
		start, err := randomStart(7, rand.Reader)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, start, 0)
		assert.Less(t, start, 7)
	}
}

func TestRandomStart_ReportsAFailingEntropySource(t *testing.T) {
	cause := errors.New("entropy exhausted")
	_, err := randomStart(7, iotest.ErrReader(cause))

	var entropyErr *EntropyError
	require.ErrorAs(t, err, &entropyErr)
	assert.ErrorIs(t, err, cause)
	assert.Contains(t, err.Error(), "choosing a start port")
}

// -----------------------------------------------------------------------------
// error messages
// -----------------------------------------------------------------------------

// TestErrorMessages_NameTheProblem covers the Error method of every typed error
// this package returns. They are what a failing test run shows, so an empty or
// contextless one costs a debugging session.
func TestErrorMessages_NameTheProblem(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err  error
		want []string
	}{
		"invalid count": {
			err:  &InvalidCountError{Count: -3},
			want: []string{"positive", "-3"},
		},
		"window unavailable": {
			err:  &WindowUnavailableError{EphemeralLow: 1024, EphemeralHigh: 65535},
			want: []string{"1024", "65535", "no room"},
		},
		"lock dir": {
			err:  &LockDirError{Dir: "/locks", Cause: os.ErrPermission},
			want: []string{"/locks", "permission"},
		},
		"lock file": {
			err:  &LockFileError{Port: 30001, Cause: os.ErrPermission},
			want: []string{"30001", "permission"},
		},
		"release": {
			err:  &ReleaseError{Port: 30002, Cause: os.ErrClosed},
			want: []string{"30002", "closed"},
		},
		"malformed range": {
			err:  &MalformedPortRangeError{Content: "junk"},
			want: []string{"ip_local_port_range", "junk"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			message := tc.err.Error()
			assert.True(t, strings.HasPrefix(message, "netport: "), "message %q must name the package", message)
			for _, want := range tc.want {
				assert.Contains(t, message, want)
			}
		})
	}
}
