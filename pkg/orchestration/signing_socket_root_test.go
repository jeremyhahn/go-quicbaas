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

package orchestration

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testSigningSocketsRoot is a short private directory to prepare a sockets
// root under. It is NOT t.TempDir: that path carries the test's name and runs
// past the socket budget on its own.
func testSigningSocketsRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "qbs")
	if err != nil {
		t.Fatalf("sockets root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// =============================================================================
// The sockets root — measured at startup, never at the first delivery
// =============================================================================

// TestEnsureSigningSocketRoot_PreparesARootASocketFitsUnder is the good case:
// a short absolute root is created 0700 and accepted, and the arithmetic the
// acceptance rests on is exact — a real session directory plus the socket's
// name is exactly what LongestSigningSocketPath said it would be.
func TestEnsureSigningSocketRoot_PreparesARootASocketFitsUnder(t *testing.T) {
	root := filepath.Join(testSigningSocketsRoot(t), "sockets")
	if _, err := EnsureSigningSocketRoot(root); err != nil {
		t.Fatalf("EnsureSigningSocketRoot: %v", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("the root was not created: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("root mode = %o, want 0700; the socket's pathname is protected by its parent", perm)
	}

	name, err := NewSigningSocketDirName()
	if err != nil {
		t.Fatalf("signingSocketDirName: %v", err)
	}
	if len(name) != SigningSocketDirNameLen {
		t.Fatalf("session directory name %q is %d characters, want %d", name, len(name), SigningSocketDirNameLen)
	}
	socket := filepath.Join(root, name, WorkspaceSigningAgentSocketFile)
	if got := LongestSigningSocketPath(root); got != len(socket) {
		t.Errorf("LongestSigningSocketPath = %d, but a real socket path is %d bytes (%q)", got, len(socket), socket)
	}
	if len(socket) > MaxSigningSocketPath {
		t.Errorf("socket path is %d bytes, over the %d-byte limit", len(socket), MaxSigningSocketPath)
	}

	// Preparing it again is how a restart behaves, and it must not refuse.
	if _, err := EnsureSigningSocketRoot(root); err != nil {
		t.Errorf("a second preparation of an existing root: %v", err)
	}
}

// TestEnsureSigningSocketRoot_RefusesARootNoSocketCouldBind is the failure
// go-quicbaas#628 was filed for: the socket path is decided by the root plus
// two constant widths, so a root past the budget can never work, and it is
// refused HERE — with the root, the length and the limit — rather than at the
// first delivery's initializing state.
func TestEnsureSigningSocketRoot_RefusesARootNoSocketCouldBind(t *testing.T) {
	base := testSigningSocketsRoot(t)
	// Exactly one byte past the budget: the refusal and the acceptance below
	// straddle the boundary, which is what pins it against an off-by-one.
	pad := MaxSigningSocketRoot + 1 - len(base) - 1
	if pad < 1 {
		t.Fatalf("the test's own base %q is already %d bytes", base, len(base))
	}
	long := filepath.Join(base, strings.Repeat("d", pad))
	if len(long) != MaxSigningSocketRoot+1 {
		t.Fatalf("the over-long root is %d bytes, want %d", len(long), MaxSigningSocketRoot+1)
	}
	_, err := EnsureSigningSocketRoot(long)
	var overLong *SigningSocketRootError
	if !errors.As(err, &overLong) {
		t.Fatalf("err = %v, want *SigningSocketRootError", err)
	}
	if !errors.Is(err, ErrSigningSocketRoot) {
		t.Errorf("err does not unwrap to ErrSigningSocketRoot: %v", err)
	}
	if overLong.Root != long || overLong.Limit != MaxSigningSocketPath {
		t.Errorf("the refusal names root %q limit %d, want %q and %d",
			overLong.Root, overLong.Limit, long, MaxSigningSocketPath)
	}
	if overLong.Longest != LongestSigningSocketPath(long) {
		t.Errorf("Longest = %d, want %d", overLong.Longest, LongestSigningSocketPath(long))
	}
	if !strings.Contains(err.Error(), long) || !strings.Contains(err.Error(), "sockets root") {
		t.Errorf("the message names neither the root nor the key to fix: %q", err.Error())
	}
	if _, statErr := os.Stat(long); !os.IsNotExist(statErr) {
		t.Errorf("a refused root was created anyway: %v", statErr)
	}

	// One byte shorter is the longest root that works.
	fits := long[:len(long)-1]
	if _, err := EnsureSigningSocketRoot(fits); err != nil {
		t.Errorf("a root of exactly the budget was refused: %v", err)
	}
	if got := LongestSigningSocketPath(fits); got != MaxSigningSocketPath {
		t.Errorf("the longest root yields %d-byte sockets, want exactly the %d-byte limit", got, MaxSigningSocketPath)
	}
}

// TestEnsureSigningSocketRoot_RefusesARootThatCannotHoldASocketSafely covers
// the shapes go-xkms refuses at bind time — a world-writable parent (which is
// what the OS temporary directory is, and why a socket is never bound there),
// a symlink, and a path that is not a directory at all — plus the inputs that
// are not a root at all.
func TestEnsureSigningSocketRoot_RefusesARootThatCannotHoldASocketSafely(t *testing.T) {
	base := testSigningSocketsRoot(t)

	shared := filepath.Join(base, "shared")
	if err := os.Mkdir(shared, 0o777); err != nil { // #nosec G301 -- the point of the case
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o777); err != nil { // #nosec G302 -- umask would otherwise hide the case
		t.Fatal(err)
	}

	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(base, "link")
	if err := os.Symlink(shared, link); err != nil {
		t.Fatal(err)
	}

	for name, root := range map[string]string{
		"writable by everybody":    shared,
		"a symlink to a directory": link,
		"a regular file":           file,
		"under a regular file":     filepath.Join(file, "sockets"),
	} {
		_, err := EnsureSigningSocketRoot(root)
		var unusable *SigningSocketRootUnusableError
		if !errors.As(err, &unusable) {
			t.Errorf("%s: err = %v, want *SigningSocketRootUnusableError", name, err)
			continue
		}
		if unusable.Root != root || !errors.Is(err, ErrSigningSocketRoot) {
			t.Errorf("%s: the refusal names %q (%v)", name, unusable.Root, err)
		}
	}

	for name, root := range map[string]string{
		"empty":    "",
		"blank":    "   ",
		"relative": "sockets/here",
		"dot":      ".",
	} {
		_, err := EnsureSigningSocketRoot(root)
		var unusable *SigningSocketRootUnusableError
		if !errors.As(err, &unusable) || !errors.Is(err, ErrSigningSocketRoot) {
			t.Errorf("%s: err = %v, want *SigningSocketRootUnusableError", name, err)
		}
	}
}

// TestSigningSocketRootUnusableError_CarriesItsCause: the filesystem error is
// reachable, because "could not be created" without the errno is a message an
// operator cannot act on.
func TestSigningSocketRootUnusableError_CarriesItsCause(t *testing.T) {
	cause := os.ErrPermission
	err := &SigningSocketRootUnusableError{Root: "/run/quicbaas", Reason: "could not be created", Cause: cause}
	if !errors.Is(err, cause) || !errors.Is(err, ErrSigningSocketRoot) {
		t.Fatalf("the cause and the sentinel are not both reachable: %v", err)
	}
	if !strings.Contains(err.Error(), "/run/quicbaas") || !strings.Contains(err.Error(), cause.Error()) {
		t.Errorf("message = %q", err.Error())
	}
	bare := &SigningSocketRootUnusableError{Root: "/run/quicbaas", Reason: "is a symlink"}
	if !errors.Is(bare, ErrSigningSocketRoot) || strings.HasSuffix(bare.Error(), ": ") {
		t.Errorf("a causeless refusal reads badly: %q", bare.Error())
	}
}

// TestSigningSocketDirName_IsFreshEveryTime: the name is the only thing
// standing between one session's live socket and another spawn naming it as
// a mount source, so a repeat would be a real defect and not a cosmetic one.
func TestSigningSocketDirName_IsFreshEveryTime(t *testing.T) {
	seen := make(map[string]bool, 64)
	for range 64 {
		name, err := NewSigningSocketDirName()
		if err != nil {
			t.Fatalf("signingSocketDirName: %v", err)
		}
		if seen[name] {
			t.Fatalf("session directory name %q repeated", name)
		}
		seen[name] = true
		if strings.Trim(name, "0123456789abcdef") != "" {
			t.Fatalf("session directory name %q is not hexadecimal", name)
		}
	}
}

// =============================================================================
// The startup sweep — go-quicbaas#633
// =============================================================================

// signingSessionDir creates one session directory under root, named exactly
// the way the runner names one, and answers its path.
func signingSessionDir(t *testing.T, root string) string {
	t.Helper()
	name, err := NewSigningSocketDirName()
	if err != nil {
		t.Fatalf("signingSocketDirName: %v", err)
	}
	dir := filepath.Join(root, name)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("session directory: %v", err)
	}
	return dir
}

// listenSigningSocket binds an agent socket inside dir. The listener is
// answered as-is so a caller can decide whether the session is alive.
func listenSigningSocket(t *testing.T, dir string) *net.UnixListener {
	t.Helper()
	path := filepath.Join(dir, WorkspaceSigningAgentSocketFile)
	if len(path) > MaxSigningSocketPath {
		t.Fatalf("the test's own socket path is %d bytes, over the %d-byte budget: %s",
			len(path), MaxSigningSocketPath, path)
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("bind %s: %v", path, err)
	}
	return ln
}

// deadSigningSession is what a process killed mid-attempt leaves: a session
// directory holding a socket whose listener died with the process. The
// listener is closed WITHOUT unlinking, which is precisely the difference
// between a kill and a release.
func deadSigningSession(t *testing.T, root string) string {
	t.Helper()
	dir := signingSessionDir(t, root)
	ln := listenSigningSocket(t, dir)
	ln.SetUnlinkOnClose(false)
	if err := ln.Close(); err != nil {
		t.Fatalf("close the listener: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, WorkspaceSigningAgentSocketFile)); err != nil {
		t.Fatalf("the dead socket did not survive its listener: %v", err)
	}
	return dir
}

// liveSigningSession is an attempt that is still running: a bound socket with
// its listener held open for the length of the test.
func liveSigningSession(t *testing.T, root string) string {
	t.Helper()
	dir := signingSessionDir(t, root)
	ln := listenSigningSocket(t, dir)
	t.Cleanup(func() { _ = ln.Close() })
	return dir
}

// sweptNames answers the sweep's removals as a set.
func sweptNames(sweep SigningSocketSweep) map[string]bool {
	set := make(map[string]bool, len(sweep.Removed))
	for _, name := range sweep.Removed {
		set[name] = true
	}
	return set
}

// TestEnsureSigningSocketRoot_SweepsTheSessionsAKilledProcessLeft is
// go-quicbaas#633.
//
// An attempt that ends releases its agent — the runner's teardown runs on a
// context detached from the attempt's — so the only session directory that
// outlives its attempt is one whose PROCESS DIED HOLDING IT. The sockets root
// outlives the daemon, so without this sweep every hard kill costs the node
// one dead socket for ever.
//
// Both shapes a kill can leave are swept: the directory with a socket nobody
// answers, and the directory with no socket at all (killed between the
// agent's mkdir and its bind). A session whose agent is STILL LISTENING is
// not swept, which is what makes the sweep safe under the one deployment
// where a starting daemon is not alone under the root — two daemons of the
// same user share $XDG_RUNTIME_DIR/quicbaas.
func TestEnsureSigningSocketRoot_SweepsTheSessionsAKilledProcessLeft(t *testing.T) {
	root := testSigningSocketsRoot(t)

	dead := deadSigningSession(t, root)
	unbound := signingSessionDir(t, root)
	live := liveSigningSession(t, root)

	sweep, err := EnsureSigningSocketRoot(root)
	if err != nil {
		t.Fatalf("EnsureSigningSocketRoot: %v", err)
	}
	if sweep.Root != filepath.Clean(root) {
		t.Errorf("sweep.Root = %q, want %q", sweep.Root, filepath.Clean(root))
	}

	removed := sweptNames(sweep)
	for _, orphan := range []string{dead, unbound} {
		if _, statErr := os.Stat(orphan); !os.IsNotExist(statErr) {
			t.Errorf("the orphan %s survived the sweep: %v", orphan, statErr)
		}
		if !removed[filepath.Base(orphan)] {
			t.Errorf("the sweep did not report removing %s: %v", filepath.Base(orphan), sweep.Removed)
		}
	}
	if len(sweep.Removed) != 2 {
		t.Errorf("the sweep removed %v, want exactly the two orphans", sweep.Removed)
	}

	if _, statErr := os.Stat(filepath.Join(live, WorkspaceSigningAgentSocketFile)); statErr != nil {
		t.Errorf("a session whose agent is still listening was swept: %v", statErr)
	}
	if removed[filepath.Base(live)] {
		t.Errorf("the sweep reported removing a live session: %v", sweep.Removed)
	}

	// A second start finds nothing left to do, and does not report a sweep
	// it did not perform.
	again, err := EnsureSigningSocketRoot(root)
	if err != nil {
		t.Fatalf("a second start: %v", err)
	}
	if len(again.Removed) != 0 {
		t.Errorf("the second start swept %v, but the first left nothing behind", again.Removed)
	}
}

// TestEnsureSigningSocketRoot_SweepLeavesWhatIsNotItsOwn: the sweep unlinks,
// so it removes ONLY what this package mints — a name of exactly
// SigningSocketDirNameLen hexadecimal characters holding nothing but the
// agent's socket. Everything else under the root belongs to somebody else and
// is left exactly where it is, whatever it is named.
func TestEnsureSigningSocketRoot_SweepLeavesWhatIsNotItsOwn(t *testing.T) {
	root := testSigningSocketsRoot(t)

	// A name this package would never mint, holding a dead socket.
	foreign := filepath.Join(root, "not-a-session")
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	foreignSocket := listenSigningSocket(t, foreign)
	foreignSocket.SetUnlinkOnClose(false)
	if err := foreignSocket.Close(); err != nil {
		t.Fatal(err)
	}

	// The right name, but the directory holds something that is not ours.
	occupied := deadSigningSession(t, root)
	if err := os.WriteFile(filepath.Join(occupied, "notes.txt"), []byte("someone else's"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The right name and the right filename, but a regular file rather than
	// a socket: no agent ever bound here.
	impostor := signingSessionDir(t, root)
	if err := os.WriteFile(filepath.Join(impostor, WorkspaceSigningAgentSocketFile), []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A session-shaped name that is not a directory.
	fileName, err := NewSigningSocketDirName()
	if err != nil {
		t.Fatal(err)
	}
	plainFile := filepath.Join(root, fileName)
	if err := os.WriteFile(plainFile, []byte("a file, not a session"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A session-shaped name that is a SYMLINK to a real orphan elsewhere.
	// Following it would delete something outside the root.
	elsewhere := t.TempDir()
	target := filepath.Join(elsewhere, "victim")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	linkName, err := NewSigningSocketDirName()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, linkName)
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	sweep, err := EnsureSigningSocketRoot(root)
	if err != nil {
		t.Fatalf("EnsureSigningSocketRoot: %v", err)
	}
	if len(sweep.Removed) != 0 {
		t.Fatalf("the sweep removed %v, but nothing under the root was one of its own", sweep.Removed)
	}
	for _, kept := range []string{foreign, occupied, impostor, plainFile, link, target} {
		if _, statErr := os.Lstat(kept); statErr != nil {
			t.Errorf("the sweep removed %s, which is not one of its own: %v", kept, statErr)
		}
	}
}

// TestEnsureSigningSocketRoot_RefusesAnUnusableRootBeforeRemovingAnything:
// the sweep unlinks, so it runs only under a root the refusals have just
// vouched for. A root that is a symlink, or one every user can write to, is
// not the configured root in any sense that lets the daemon delete under it —
// and the orphan it holds is still there afterwards.
func TestEnsureSigningSocketRoot_RefusesAnUnusableRootBeforeRemovingAnything(t *testing.T) {
	base := testSigningSocketsRoot(t)

	shared := filepath.Join(base, "shared")
	if err := os.Mkdir(shared, 0o777); err != nil { // #nosec G301 -- the point of the case
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o777); err != nil { // #nosec G302 -- umask would otherwise hide the case
		t.Fatal(err)
	}
	sharedOrphan := deadSigningSession(t, shared)

	private := filepath.Join(base, "private")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	privateOrphan := deadSigningSession(t, private)
	link := filepath.Join(base, "link")
	if err := os.Symlink(private, link); err != nil {
		t.Fatal(err)
	}

	for name, root := range map[string]string{
		"writable by everybody":    shared,
		"a symlink to a directory": link,
	} {
		sweep, err := EnsureSigningSocketRoot(root)
		var unusable *SigningSocketRootUnusableError
		if !errors.As(err, &unusable) {
			t.Errorf("%s: err = %v, want *SigningSocketRootUnusableError", name, err)
		}
		if len(sweep.Removed) != 0 {
			t.Errorf("%s: a refused root reported the sweep %v", name, sweep.Removed)
		}
	}
	for _, orphan := range []string{sharedOrphan, privateOrphan} {
		if _, statErr := os.Stat(filepath.Join(orphan, WorkspaceSigningAgentSocketFile)); statErr != nil {
			t.Errorf("a refused root was swept anyway: %s is gone (%v)", orphan, statErr)
		}
	}
}

// TestEnsureSigningSocketRoot_SweepingAnEmptyRootIsANoOp: the ordinary start.
// The root is created 0700 and the sweep reports nothing, because reporting
// a sweep of a root that has never held a session would put a line in the
// operator's log that means nothing.
func TestEnsureSigningSocketRoot_SweepingAnEmptyRootIsANoOp(t *testing.T) {
	root := filepath.Join(testSigningSocketsRoot(t), "fresh")
	sweep, err := EnsureSigningSocketRoot(root)
	if err != nil {
		t.Fatalf("EnsureSigningSocketRoot: %v", err)
	}
	if sweep.Removed != nil {
		t.Errorf("an empty root reported the sweep %v", sweep.Removed)
	}
	if sweep.Root != filepath.Clean(root) {
		t.Errorf("sweep.Root = %q, want %q", sweep.Root, filepath.Clean(root))
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("the root was not created: %v", err)
	}
	// A prepared root holds nothing at all: the daemon's signing-root marker
	// is the daemon's own addition (sdlc.EnsureSigningSocketRoot), and a
	// fleet node's relay root never carries one.
	if len(entries) != 0 {
		t.Errorf("the root holds %v after a no-op sweep, want nothing", entries)
	}

	// A marker already there is a file, not a session directory, so a later
	// sweep leaves it exactly where it is.
	if _, err := EnsureSigningRootID(root); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureSigningSocketRoot(root); err != nil {
		t.Fatalf("re-prepare: %v", err)
	}
	if _, err := ReadSigningRootID(root); err != nil {
		t.Errorf("the sweep removed the signing-root marker: %v", err)
	}
}

// TestEnsureSigningSocketRoot_ReportsASweepItCouldNotComplete: a sweep that
// could not finish leaves the daemon unable to say what is bound under the
// root it is about to bind under, and that is a startup failure carrying the
// root and the errno rather than a silent success.
func TestEnsureSigningSocketRoot_ReportsASweepItCouldNotComplete(t *testing.T) {
	// The root itself refusing to be enumerated. It is staged against the
	// sweep directly because the refusals above would catch a missing root
	// first; what is under test here is the sweep's own failure path, which
	// a root removed between the refusals and the enumeration reaches.
	missing := filepath.Join(testSigningSocketsRoot(t), "gone")
	_, err := sweepSigningSocketRoot(missing)
	var rootErr *SigningSocketSweepError
	if !errors.As(err, &rootErr) {
		t.Fatalf("err = %v, want *SigningSocketSweepError", err)
	}
	if rootErr.Root != missing || rootErr.Entry != "" {
		t.Errorf("the failure names root %q entry %q, want %q and no entry", rootErr.Root, rootErr.Entry, missing)
	}
	if !errors.Is(err, ErrSigningSocketRoot) || !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the sentinel and the errno are not both reachable: %v", err)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("the message does not name the root: %q", err.Error())
	}

	// The other half: an orphan the sweep identified and could not remove.
	// A directory its parent will not let it unlink from is the only way to
	// stage that, and uid 0 is exempt from the permission check by design —
	// so the staging is asserted where the kernel enforces it, and the root
	// case above covers every user.
	locked := filepath.Join(testSigningSocketsRoot(t), "locked")
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	orphan := deadSigningSession(t, locked)
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	if os.Getuid() == 0 {
		t.Logf("running as uid 0, which unlinks from a read-only directory: "+
			"the per-session failure cannot be staged, and %s stands as the enumeration case", missing)
		return
	}
	_, err = EnsureSigningSocketRoot(locked)
	var entryErr *SigningSocketSweepError
	if !errors.As(err, &entryErr) {
		t.Fatalf("err = %v, want *SigningSocketSweepError", err)
	}
	if entryErr.Entry != filepath.Base(orphan) {
		t.Errorf("the failure names entry %q, want %q", entryErr.Entry, filepath.Base(orphan))
	}
	if !strings.Contains(err.Error(), filepath.Base(orphan)) || !strings.Contains(err.Error(), locked) {
		t.Errorf("the message names neither the session nor the root: %q", err.Error())
	}
}

// TestSigningSocketSweepError_NamesItsSubject: the root-level failure and the
// session-level failure read differently, and both reach their cause.
func TestSigningSocketSweepError_NamesItsSubject(t *testing.T) {
	cause := os.ErrPermission
	root := &SigningSocketSweepError{Root: "/run/quicbaas", Reason: "could not be enumerated", Cause: cause}
	if !errors.Is(root, cause) || !errors.Is(root, ErrSigningSocketRoot) {
		t.Fatalf("the cause and the sentinel are not both reachable: %v", root)
	}
	if !strings.Contains(root.Error(), "/run/quicbaas") || strings.Contains(root.Error(), "session  under") {
		t.Errorf("message = %q", root.Error())
	}
	entry := &SigningSocketSweepError{Root: "/run/quicbaas", Entry: "df769f4a7dd5", Reason: "could not be removed"}
	if !errors.Is(entry, ErrSigningSocketRoot) || !strings.Contains(entry.Error(), "df769f4a7dd5") {
		t.Errorf("message = %q", entry.Error())
	}
	if strings.HasSuffix(entry.Error(), ": ") {
		t.Errorf("a causeless failure reads badly: %q", entry.Error())
	}
}

// TestSigningSocketBudget_IsUnchangedBySweeping pins go-quicbaas#628's
// arithmetic. The sweep reads the same fixed widths the budget is computed
// from, and a change to either would silently move the other.
func TestSigningSocketBudget_IsUnchangedBySweeping(t *testing.T) {
	if MaxSigningSocketPath != 107 {
		t.Errorf("MaxSigningSocketPath = %d, want the kernel's 107", MaxSigningSocketPath)
	}
	if SigningSocketDirNameLen != 12 {
		t.Errorf("SigningSocketDirNameLen = %d, want 12", SigningSocketDirNameLen)
	}
	want := MaxSigningSocketPath - SigningSocketDirNameLen - len(WorkspaceSigningAgentSocketFile) - 2
	if MaxSigningSocketRoot != want || MaxSigningSocketRoot != 83 {
		t.Errorf("MaxSigningSocketRoot = %d, want %d (83 on Linux)", MaxSigningSocketRoot, want)
	}
	if got := LongestSigningSocketPath("/run/q"); got != len("/run/q")+SigningSocketDirNameLen+len(WorkspaceSigningAgentSocketFile)+2 {
		t.Errorf("LongestSigningSocketPath = %d", got)
	}
}

// TestIsSigningSessionDirName_AcceptsOnlyWhatTheRunnerMints: the sweep's
// whole safety rests on this predicate, so it is pinned directly.
func TestIsSigningSessionDirName_AcceptsOnlyWhatTheRunnerMints(t *testing.T) {
	minted, err := NewSigningSocketDirName()
	if err != nil {
		t.Fatal(err)
	}
	if !IsSigningSocketDirName(minted) {
		t.Errorf("a name the runner minted was not recognised: %q", minted)
	}
	for _, name := range []string{
		"", "df769f4a7dd", "df769f4a7dd5a", "DF769F4A7DD5", "df769f4a7dd-",
		"sa-df769f4a", "............", "df769f4a7dé", "../../../etc",
	} {
		if len(name) == SigningSocketDirNameLen && len([]rune(name)) != SigningSocketDirNameLen {
			t.Logf("%q is %d bytes over %d runes, which is the case the byte loop must not skip",
				name, len(name), len([]rune(name)))
		}
		if IsSigningSocketDirName(name) {
			t.Errorf("%q was recognised as a session directory", name)
		}
	}
}
