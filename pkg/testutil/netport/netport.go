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

// Package netport reserves TCP/UDP port numbers for tests that hand the number
// to a server which binds it later.
//
// # The window this removes
//
// The usual way to find a free port is to listen on ":0", read the kernel's
// choice, close the listener, and pass the number on. That is check-then-act on
// a shared resource where the check RELEASES the resource: between the close and
// the server's bind the port is genuinely free, so anything on the host may take
// it. Nothing in the caller can make that interval smaller than zero, and under
// load — this repository boots roughly seventy-five parallel qrdb nodes in
// pkg/server alone, alongside other worktrees' test binaries — it is crossed
// often enough to redden unrelated gates (go-quicbaas#706).
//
// A reservation here is not a released bind. It is:
//
//  1. A port OUTSIDE the kernel's ephemeral range, so no ":0" bind and no
//     outbound connection anywhere on the host can ever be assigned it.
//  2. An exclusive flock(2) on a per-port lock file, held for the life of the
//     process, so no other cooperating test binary on the host draws it. The
//     kernel drops the lock when the process dies, so a killed run leaves
//     nothing stale behind.
//  3. Proven free on BOTH TCP and UDP at draw time, because a qrdb REST
//     listener takes TCP P and its HTTP/3 sibling takes UDP P — the two port
//     namespaces are independent and a free TCP port says nothing about UDP.
//
// What remains is a process that deliberately binds a specific port in the
// reserved window while a test run is in flight. That is not a race the caller
// loses to chance.
//
// # Lifetime
//
// A reservation lasts until Release, and MustReserve releases on test cleanup.
// Recycling ports is what makes the window a workable size — pkg/server alone
// builds a test config at some seven hundred call sites — and it does not put
// the interval back: a port is only returned once the test that drew it has
// finished, so no server is still on its way to binding it, and the next
// drawer re-proves the port free before taking it.
//
// The lock directory is keyed by user id, so concurrent runs by ONE user
// coordinate. Two different users' runs on one host do not, which is the same
// exposure any temp-dir-based coordination has and is out of scope for CI,
// where a runner is a single user.
//
// Unix only: the cross-process reservation is flock(2). pkg/server, the primary
// consumer, does not build on Windows either.
package netport

import (
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
)

// LoopbackHost is the address every reserved port is proven free on and the
// address callers are expected to bind.
const LoopbackHost = "127.0.0.1"

// procPortRange is where Linux publishes the ephemeral port range the kernel
// draws ":0" binds and outbound source ports from.
const procPortRange = "/proc/sys/net/ipv4/ip_local_port_range"

// Conservative stand-ins for the ephemeral range on a kernel that does not
// publish one (anything but Linux). The window is chosen BELOW the low bound
// and 32768 is the lowest low bound mainstream kernels ship — Darwin's is
// 49152 — so assuming the Linux default keeps the window clear of the real
// range on every supported host.
const (
	defaultEphemeralLow  = 32768
	defaultEphemeralHigh = 60999
)

// windowSize is how many ports the reserved window spans, and minWindowSize the
// smallest window worth using. 8192 leaves room for every test binary a host
// runs at once; a run that exhausts it is reporting a leak, not bad luck.
const (
	windowSize    = 8192
	minWindowSize = 256
)

// lowestReservablePort keeps the window clear of the privileged range.
const lowestReservablePort = 1024

// InvalidCountError reports a request for a non-positive number of ports.
type InvalidCountError struct {
	Count int
}

func (e *InvalidCountError) Error() string {
	return "netport: port count must be positive, got " + strconv.Itoa(e.Count)
}

// WindowUnavailableError reports a host whose ephemeral port range leaves no
// room for a reserved window on either side of it.
type WindowUnavailableError struct {
	EphemeralLow  int
	EphemeralHigh int
}

func (e *WindowUnavailableError) Error() string {
	return "netport: ephemeral port range " + strconv.Itoa(e.EphemeralLow) + "-" +
		strconv.Itoa(e.EphemeralHigh) + " leaves no room for a reserved window"
}

// ExhaustedError reports a reserved window with no free port left in it.
//
// The breakdown separates the two ways a window empties, because they have
// different fixes: LockedOut counts ports another reservation holds (a leak, or
// a window too small for the number of tests in flight), Occupied counts ports
// something outside this mechanism is listening on (the window overlaps a
// service range on this host).
type ExhaustedError struct {
	Low       int
	High      int
	Requested int
	Reserved  int
	LockedOut int
	Occupied  int
}

func (e *ExhaustedError) Error() string {
	return "netport: reserved window " + strconv.Itoa(e.Low) + "-" + strconv.Itoa(e.High-1) +
		" yielded " + strconv.Itoa(e.Reserved) + " of " + strconv.Itoa(e.Requested) +
		" ports (" + strconv.Itoa(e.LockedOut) + " already reserved, " +
		strconv.Itoa(e.Occupied) + " occupied)"
}

// LockDirError reports a lock directory that could not be created.
type LockDirError struct {
	Dir   string
	Cause error
}

func (e *LockDirError) Error() string {
	return "netport: creating lock directory " + e.Dir + ": " + e.Cause.Error()
}

func (e *LockDirError) Unwrap() error { return e.Cause }

// LockFileError reports a per-port lock file that could not be opened.
type LockFileError struct {
	Port  int
	Cause error
}

func (e *LockFileError) Error() string {
	return "netport: opening the lock file for port " + strconv.Itoa(e.Port) + ": " + e.Cause.Error()
}

func (e *LockFileError) Unwrap() error { return e.Cause }

// ReleaseError reports a reservation whose lock file would not close, which
// leaves the port reserved for the rest of the process.
type ReleaseError struct {
	Port  int
	Cause error
}

func (e *ReleaseError) Error() string {
	return "netport: releasing port " + strconv.Itoa(e.Port) + ": " + e.Cause.Error()
}

func (e *ReleaseError) Unwrap() error { return e.Cause }

// MalformedPortRangeError reports an ip_local_port_range that did not parse.
type MalformedPortRangeError struct {
	Content string
}

func (e *MalformedPortRangeError) Error() string {
	return "netport: malformed ip_local_port_range " + strconv.Quote(e.Content)
}

// outcome is why a candidate port was or was not taken.
type outcome int

const (
	outcomeReserved outcome = iota
	outcomeLockedOut
	outcomeOccupied
)

// held retains the *os.File behind every reservation. The flock lives on the
// open file description, and os.File closes its descriptor from a finalizer, so
// dropping the last reference would silently release the lock.
var held sync.Map // int -> *os.File

// memoized holds the two per-process facts every reservation needs: the
// reserved window, and the directory its lock files live in. Both are computed
// once and both can fail, so each keeps its verdict alongside its value.
type portWindow struct {
	low  int
	high int
	err  error
}

type lockDirectory struct {
	path string
	err  error
}

var (
	windowOnce   sync.Once
	windowResult portWindow

	lockDirOnce   sync.Once
	lockDirResult lockDirectory
)

// Reserve returns n distinct ports that no other cooperating process on the
// host will draw and that the kernel will never assign on its own.
//
// The caller binds them whenever it likes; the reservation lasts until Release.
func Reserve(n int) ([]int, error) {
	if n <= 0 {
		return nil, &InvalidCountError{Count: n}
	}
	low, high, err := window()
	if err != nil {
		return nil, err
	}
	dir, err := lockDir()
	if err != nil {
		return nil, err
	}
	return reserveFrom(dir, low, high, n)
}

// reserveFrom walks the window [low, high) for n free ports, locking each
// against dir. It is separate from Reserve so the search can be exercised
// against a window small enough to fill, which is the only way to reach the
// exhaustion path.
func reserveFrom(dir string, low, high, n int) ([]int, error) {
	size := high - low
	ports := make([]int, 0, n)
	lockedOut, occupied := 0, 0
	// A random start spreads concurrent binaries over the window instead of
	// having them all contend on its first port. Nothing here is a secret: the
	// offset only picks where in a public range to begin looking, and the
	// reservation itself is what makes the result exclusive.
	start := rand.IntN(size) //nolint:gosec // G404: contention spreading, not a secret
	for offset := 0; offset < size && len(ports) < n; offset++ {
		port := low + (start+offset)%size
		file, result, err := acquire(dir, port)
		if err != nil {
			return nil, err
		}
		switch result {
		case outcomeReserved:
			held.Store(port, file)
			ports = append(ports, port)
		case outcomeLockedOut:
			lockedOut++
		case outcomeOccupied:
			occupied++
		}
	}
	if len(ports) < n {
		// Give back whatever was taken: a partial draw the caller never sees
		// would hold ports for the life of the process.
		if releaseErr := Release(ports...); releaseErr != nil {
			return nil, releaseErr
		}
		return nil, &ExhaustedError{
			Low: low, High: high, Requested: n, Reserved: len(ports),
			LockedOut: lockedOut, Occupied: occupied,
		}
	}
	return ports, nil
}

// Release returns ports to the window. A port that is not reserved by this
// process is skipped, so releasing twice is harmless.
//
// Callers that took their ports through MustReserve do not call this: the test
// cleanup does.
func Release(ports ...int) error {
	for _, port := range ports {
		file, loaded := held.LoadAndDelete(port)
		if !loaded {
			continue
		}
		// Closing the descriptor drops the flock with it.
		if err := file.(*os.File).Close(); err != nil {
			return &ReleaseError{Port: port, Cause: err}
		}
	}
	return nil
}

// MustReserve returns n reserved ports or fails the test, and releases them
// when the test finishes.
//
// Release is registered as a cleanup rather than a defer so it runs after the
// test's own cleanups — after the server holding the port has been shut down.
func MustReserve(t *testing.T, n int) []int {
	t.Helper()
	ports, err := Reserve(n)
	if err != nil {
		t.Fatalf("reserving %d test ports: %v", n, err)
	}
	t.Cleanup(func() {
		if releaseErr := Release(ports...); releaseErr != nil {
			t.Errorf("releasing test ports %v: %v", ports, releaseErr)
		}
	})
	return ports
}

// MustReserveOne returns a single reserved port or fails the test.
func MustReserveOne(t *testing.T) int {
	t.Helper()
	return MustReserve(t, 1)[0]
}

// MustReserveLoopbackAddr returns a reserved "127.0.0.1:port" address or fails
// the test.
func MustReserveLoopbackAddr(t *testing.T) string {
	t.Helper()
	return LoopbackAddr(MustReserveOne(t))
}

// LoopbackAddr renders port as the loopback address callers bind.
func LoopbackAddr(port int) string {
	return net.JoinHostPort(LoopbackHost, strconv.Itoa(port))
}

// acquire takes the host-wide lock on port and proves the port free on both
// transports. On success it returns the open lock file, whose descriptor IS the
// reservation: closing it releases the port.
//
// A port another process holds, or that something is already listening on, is
// not an error — the caller moves to the next candidate. Only a lock file that
// cannot be opened at all is fatal, because every subsequent candidate would
// fail the same way.
func acquire(dir string, port int) (*os.File, outcome, error) {
	file, err := os.OpenFile(lockPath(dir, port), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, outcomeOccupied, &LockFileError{Port: port, Cause: err}
	}
	// flock conflicts between distinct open file descriptions even within one
	// process, so this excludes sibling goroutines as well as sibling binaries.
	if lockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); lockErr != nil {
		if closeErr := file.Close(); closeErr != nil {
			return nil, outcomeOccupied, &LockFileError{Port: port, Cause: closeErr}
		}
		if lockErr == syscall.EWOULDBLOCK {
			return nil, outcomeLockedOut, nil
		}
		return nil, outcomeOccupied, &LockFileError{Port: port, Cause: lockErr}
	}
	if !dualStackFree(port) {
		if closeErr := file.Close(); closeErr != nil {
			return nil, outcomeOccupied, &LockFileError{Port: port, Cause: closeErr}
		}
		return nil, outcomeOccupied, nil
	}
	return file, outcomeReserved, nil
}

// lockPath is the lock file for one port.
func lockPath(dir string, port int) string {
	return filepath.Join(dir, strconv.Itoa(port)+".lock")
}

// dualStackFree reports whether port is bindable on TCP and UDP at once. Both
// sockets are opened before either closes, so the pair is proven simultaneously
// available rather than one and then the other.
func dualStackFree(port int) bool {
	addr := LoopbackAddr(port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	// The UDP bind is attempted while the TCP socket is still open so the pair
	// is proven simultaneously available, and both are released together
	// whichever way it went.
	packet, packetErr := net.ListenPacket("udp", addr)
	released := closeProbe(listener, packet)
	return packetErr == nil && released
}

// closeProbe shuts the probe sockets and reports whether both closed cleanly. A
// probe that will not close leaves the port in an unknown state, so the
// candidate is rejected rather than handed out.
func closeProbe(listener net.Listener, packet net.PacketConn) bool {
	ok := listener.Close() == nil
	if packet != nil && packet.Close() != nil {
		ok = false
	}
	return ok
}

// window computes the reserved range once per process.
func window() (int, int, error) {
	windowOnce.Do(func() {
		ephemeralLow, ephemeralHigh := ephemeralRange()
		low, high, err := computeWindow(ephemeralLow, ephemeralHigh)
		windowResult = portWindow{low: low, high: high, err: err}
	})
	return windowResult.low, windowResult.high, windowResult.err
}

// computeWindow places a reserved range clear of the kernel's ephemeral range,
// preferring the space below it and falling back to the space above only when
// the ephemeral range starts too close to the privileged ports.
func computeWindow(ephemeralLow, ephemeralHigh int) (int, int, error) {
	if low := ephemeralLow - windowSize; low >= lowestReservablePort {
		return low, ephemeralLow, nil
	}
	low := ephemeralHigh + 1
	high := min(low+windowSize, 65536)
	if high-low >= minWindowSize {
		return low, high, nil
	}
	return 0, 0, &WindowUnavailableError{EphemeralLow: ephemeralLow, EphemeralHigh: ephemeralHigh}
}

// ephemeralRange reports the range the kernel draws ephemeral ports from, using
// the documented conservative stand-ins when the kernel does not publish one.
func ephemeralRange() (int, int) {
	return ephemeralRangeFrom(procPortRange)
}

// ephemeralRangeFrom reads the range from path, standing in the conservative
// defaults when the file is absent (any kernel but Linux) or unreadable.
func ephemeralRangeFrom(path string) (int, int) {
	content, err := os.ReadFile(path)
	if err != nil {
		return defaultEphemeralLow, defaultEphemeralHigh
	}
	low, high, parseErr := parsePortRange(string(content))
	if parseErr != nil {
		return defaultEphemeralLow, defaultEphemeralHigh
	}
	return low, high
}

// parsePortRange reads the two whitespace-separated bounds Linux writes to
// ip_local_port_range.
func parsePortRange(content string) (int, int, error) {
	fields := strings.Fields(content)
	if len(fields) != 2 {
		return 0, 0, &MalformedPortRangeError{Content: content}
	}
	low, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, &MalformedPortRangeError{Content: content}
	}
	high, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, &MalformedPortRangeError{Content: content}
	}
	if low < 1 || high > 65535 || low > high {
		return 0, 0, &MalformedPortRangeError{Content: content}
	}
	return low, high, nil
}

// lockDir creates the per-user lock directory once per process.
func lockDir() (string, error) {
	lockDirOnce.Do(func() {
		path, err := ensureLockDir(defaultLockDir())
		lockDirResult = lockDirectory{path: path, err: err}
	})
	return lockDirResult.path, lockDirResult.err
}

// defaultLockDir names the lock directory. It is keyed by user id so two users
// on one host never collide over each other's lock files.
func defaultLockDir() string {
	return filepath.Join(os.TempDir(), "go-quicbaas-netport-"+strconv.Itoa(os.Getuid()))
}

// ensureLockDir creates dir if it is missing.
func ensureLockDir(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", &LockDirError{Dir: dir, Cause: err}
	}
	return dir, nil
}
