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
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// The sockets root — where a per-session signing socket is bound
// =============================================================================
//
// Two processes bind per-session ssh-agent sockets for a container to mount:
// the daemon, which serves the session's KMS-backed agent (pkg/orchestration/sdlc), and a
// fleet node on another host, which serves the SAME agent relayed over its
// FleetRuntime link (go-quicbaas#1013). Both bind under a sockets root of
// their own, and both need the same two things from it: a root a socket can
// actually be bound under, refused at startup when it cannot, and a sweep of
// the session directories a killed process left behind. So the root's rules
// live here, once, and each host adds only what is its own — the daemon's
// signing-root marker (EnsureSigningRootID) stays with the daemon.
//
// The socket lives under a SHORT, DEDICATED root, not under a workspace and
// not in the OS temporary directory. A unix socket address is a fixed-size
// kernel field, and a managed workspace is
// <projects_root>/<tenant_id>/<project_id>/workspace — 88 bytes of tenant and
// project before a socket name — so a socket placed there does not fit on any
// realistic projects root. The temporary directory is world-writable, so
// nothing can vouch for a pathname bound inside it.

// MaxSigningSocketPath bounds the socket's absolute path.
//
// A unix socket address is a fixed-size field in the kernel — sun_path is 108
// bytes on Linux, including its terminator, so 107 is the longest path that
// can be bound. Checking it turns "the agent failed to listen" into a message
// that names the path and the limit.
const MaxSigningSocketPath = 107

// SigningSocketDirNameLen is the length of one session's directory name: 12
// hexadecimal characters over 6 random bytes. It is a fixed width so the
// worst-case socket path for a root is a computation and not a guess.
const SigningSocketDirNameLen = 12

// MaxSigningSocketRoot is the longest sockets root that can hold a session
// socket: the kernel's limit less the session directory, the socket's name
// and the two separators between them. On Linux that is 83 bytes.
const MaxSigningSocketRoot = MaxSigningSocketPath - SigningSocketDirNameLen - len(WorkspaceSigningAgentSocketFile) - 2

// signingSocketRootMode is the sockets root's own mode: private to the owning
// user, because it is the parent every session directory is created in and a
// parent nobody else can write to is what makes the socket's pathname safe.
const signingSocketRootMode = 0o700

// signingSocketProbeTimeout bounds the sweep's liveness probe. Connecting to a
// unix socket is answered by the kernel without a round trip — a live listener
// takes the connection onto its backlog and a dead one is refused at once — so
// this is a ceiling on a pathology, not a wait anything reaches.
const signingSocketProbeTimeout = 2 * time.Second

// LongestSigningSocketPath returns the length of the longest socket path root
// can yield. Every session directory is the same width, so this is exact
// rather than an estimate.
func LongestSigningSocketPath(root string) int {
	return len(filepath.Clean(root)) + SigningSocketDirNameLen + len(WorkspaceSigningAgentSocketFile) + 2
}

// NewSigningSocketDirName returns a fresh directory name for one session's
// socket: SigningSocketDirNameLen hexadecimal characters over random bytes,
// with no prefix, because every byte of it is spent out of the socket path's
// budget.
//
// It is random rather than derived from the session for two reasons. A resumed
// attempt never collides with a directory a previous one owned — the go-xkms
// agent refuses an existing path by design, and adopting one would be adopting
// its contents. And a sockets root is shared by every tenant, so an
// unguessable name is what stops one spawn naming another session's live
// socket as a mount source.
func NewSigningSocketDirName() (string, error) {
	buf := make([]byte, SigningSocketDirNameLen/2)
	if _, err := rand.Read(buf); err != nil {
		return "", &SigningSocketNameError{Cause: err}
	}
	return hex.EncodeToString(buf), nil
}

// SigningSocketSweep reports what preparing a sockets root removed.
//
// It is a value rather than a log line because the caller owns the logger and
// reports the sweep next to the component it started.
type SigningSocketSweep struct {
	// Root is the sockets root the sweep ran under, cleaned.
	Root string

	// Removed names the orphaned session directories it removed, in the
	// order the root enumerated them. It is nil when there were none.
	Removed []string
}

// EnsureSigningSocketRoot prepares a sockets root and refuses, AT STARTUP,
// every root a session could not bind a socket in; then it sweeps the session
// directories a killed process left behind.
//
// The two refusals are the failures this subsystem actually has:
//
//   - a root so long that the socket would exceed the kernel's sun_path field.
//     Discovered at the first session, it is one failed job per delivery on a
//     host that looks configured; discovered here, it is a refusal naming the
//     root and the limit.
//   - a root that cannot hold an agent socket safely. The socket's own 0600
//     mode does not protect its PATHNAME — the containing directory decides
//     who may unlink and re-bind it — so the session's parent must be private
//     to this user. go-xkms refuses a world-writable parent at bind time; this
//     refuses it now.
//
// The root itself is created 0700 and outlives every session. The refusals are
// ALL EVALUATED BEFORE THE SWEEP: the sweep unlinks directories, so it runs
// only under a root this function has just vouched for, and a root it refuses
// is left exactly as it was found.
func EnsureSigningSocketRoot(root string) (SigningSocketSweep, error) {
	cleaned := filepath.Clean(strings.TrimSpace(root))
	if strings.TrimSpace(root) == "" || !filepath.IsAbs(cleaned) {
		return SigningSocketSweep{}, &SigningSocketRootUnusableError{Root: root, Reason: "is not an absolute path"}
	}
	if longest := LongestSigningSocketPath(cleaned); longest > MaxSigningSocketPath {
		return SigningSocketSweep{}, &SigningSocketRootError{Root: cleaned, Longest: longest, Limit: MaxSigningSocketPath}
	}
	if err := os.MkdirAll(cleaned, signingSocketRootMode); err != nil {
		return SigningSocketSweep{}, &SigningSocketRootUnusableError{Root: cleaned, Reason: "could not be created", Cause: err}
	}
	info, err := os.Lstat(cleaned)
	if err != nil {
		return SigningSocketSweep{}, &SigningSocketRootUnusableError{Root: cleaned, Reason: "could not be inspected", Cause: err}
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		// A symlink can be re-pointed between this check and the bind.
		return SigningSocketSweep{}, &SigningSocketRootUnusableError{Root: cleaned, Reason: "is a symlink"}
	case !info.IsDir():
		return SigningSocketSweep{}, &SigningSocketRootUnusableError{Root: cleaned, Reason: "is not a directory"}
	case info.Mode().Perm()&0o022 != 0:
		// The OS temporary directory is exactly this shape, and it is why a
		// socket is never bound there: another user can unlink the session
		// directory's name and put their own agent at it.
		return SigningSocketSweep{}, &SigningSocketRootUnusableError{Root: cleaned, Reason: "is writable by group or other"}
	}
	return sweepSigningSocketRoot(cleaned)
}

// =============================================================================
// The startup sweep — what a process that died mid-session left behind
// =============================================================================
//
// A session that ENDS releases its socket. What no teardown can survive is the
// process not running any more: one killed between the agent's bind and its
// release leaves a 0700 directory holding a dead socket, and the sockets root
// OUTLIVES the process — it is $XDG_RUNTIME_DIR/quicbaas/<instance> or under a
// data directory — so on a host that kills jobs those accumulate without
// bound (go-quicbaas#633).
//
// WHAT "NOT LIVE" MEANS. A listener dies with the process that held it, so a
// socket that refuses a connection is answered by nobody. The sweep asks the
// socket rather than assuming "this process has only just started, so nothing
// under the root can be live": a root can be shared, and a second process
// starting must not unlink the first one's live sockets. Asking costs one
// connect that is immediately released.
//
// WHAT IS SWEPT is only what NewSigningSocketDirName mints: a directory named
// exactly SigningSocketDirNameLen hexadecimal characters, holding nothing but
// the socket, and that socket answering nobody. An empty one is a process
// killed between the agent's mkdir and its bind. Anything else under the root
// — a foreign name, a file, a symlink, a directory with other contents, one
// this process may not read — is somebody else's and is left where it is.

// sweepSigningSocketRoot removes the session directories left by a process
// that is no longer running. Its caller has already vouched for root.
func sweepSigningSocketRoot(root string) (SigningSocketSweep, error) {
	sweep := SigningSocketSweep{Root: root}
	entries, err := os.ReadDir(root)
	if err != nil {
		return sweep, &SigningSocketSweepError{Root: root, Reason: "could not be enumerated", Cause: err}
	}
	for _, entry := range entries {
		// ReadDir does not follow a symlink, so IsDir is false for one and a
		// link planted under the root is never descended into or removed.
		if !entry.IsDir() || !IsSigningSocketDirName(entry.Name()) {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		socket, ours := signingSessionSocket(dir)
		if !ours {
			continue
		}
		if socket != "" {
			live, err := signingSocketAnswers(socket)
			if err != nil {
				return sweep, &SigningSocketSweepError{
					Root: root, Entry: entry.Name(), Reason: "could not be probed for a live agent", Cause: err,
				}
			}
			if live {
				continue
			}
		}
		if err := os.RemoveAll(dir); err != nil {
			return sweep, &SigningSocketSweepError{
				Root: root, Entry: entry.Name(), Reason: "could not be removed", Cause: err,
			}
		}
		sweep.Removed = append(sweep.Removed, entry.Name())
	}
	return sweep, nil
}

// IsSigningSocketDirName reports whether name has the shape
// NewSigningSocketDirName mints: exactly SigningSocketDirNameLen lowercase
// hexadecimal characters, and nothing else. A multi-byte rune fails on its
// leading byte, so the check is safe over any name the filesystem returns.
func IsSigningSocketDirName(name string) bool {
	if len(name) != SigningSocketDirNameLen {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// signingSessionSocket reports whether dir is one of these session
// directories, and the socket inside it when there is one.
//
// A directory holding anything other than the agent's socket is not one of
// ours whatever it is named, and one this process cannot read is not
// positively identified as ours either — both are declined rather than
// reported as failures, because the sweep removes only what it recognises.
func signingSessionSocket(dir string) (socket string, ours bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if entry.Name() != WorkspaceSigningAgentSocketFile || entry.Type()&os.ModeSocket == 0 {
			return "", false
		}
	}
	if len(entries) == 0 {
		// Killed between the agent's mkdir and its bind: ours, and there is
		// no socket to ask.
		return "", true
	}
	return filepath.Join(dir, WorkspaceSigningAgentSocketFile), true
}

// signingSocketAnswers reports whether a live listener holds path.
//
// The connect is the whole question — the agent is asked nothing and told
// nothing — and the error returned is the probe's own release, so a
// connection this sweep opened is never dropped silently.
func signingSocketAnswers(path string) (bool, error) {
	conn, err := net.DialTimeout("unix", path, signingSocketProbeTimeout)
	if err != nil {
		return false, nil
	}
	return true, conn.Close()
}

// =============================================================================
// Typed errors
// =============================================================================

// ErrSigningSocketRoot is the sentinel every sockets-root refusal unwraps to.
var ErrSigningSocketRoot = errors.New("signing sockets root")

// SigningSocketRootError reports a sockets root under which no session could
// ever bind a socket, because the socket's absolute path would exceed the
// kernel's limit.
//
// It is raised AT STARTUP rather than at the first session: the arithmetic is
// fixed — the session directory and the socket's name are constant widths —
// so the root alone decides it, and an operator can fix a refusal that names
// the root and the limit.
type SigningSocketRootError struct {
	// Root is the effective sockets root.
	Root string

	// Longest is the length of the longest socket path Root can yield.
	Longest int

	// Limit is the longest path this platform accepts.
	Limit int
}

// Error implements the error interface.
func (e *SigningSocketRootError) Error() string {
	return ErrSigningSocketRoot.Error() + ": " + e.Root +
		" yields socket paths of up to " + strconv.Itoa(e.Longest) +
		" bytes and the limit is " + strconv.Itoa(e.Limit) +
		"; configure a shorter absolute sockets root"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *SigningSocketRootError) Unwrap() error { return ErrSigningSocketRoot }

// SigningSocketRootUnusableError reports a sockets root that is not an absolute
// path, exists but cannot hold an agent socket safely, or could not be created.
//
// A unix socket's own mode does not protect its pathname: the containing
// directory decides who may unlink and re-bind it, so the root every session
// directory is created in must be private to the owning user.
type SigningSocketRootUnusableError struct {
	// Root is the effective sockets root.
	Root string

	// Reason states what is wrong with it.
	Reason string

	// Cause is the underlying filesystem error, when there was one.
	Cause error
}

// Error implements the error interface.
func (e *SigningSocketRootUnusableError) Error() string {
	msg := ErrSigningSocketRoot.Error() + ": " + strconv.Quote(e.Root) + " " + e.Reason +
		"; an agent socket needs an absolute directory private to its owner"
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *SigningSocketRootUnusableError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrSigningSocketRoot}
	}
	return []error{ErrSigningSocketRoot, e.Cause}
}

// SigningSocketSweepError reports a startup sweep of a sockets root that could
// not be completed.
//
// The sweep DECLINES what it cannot positively identify as one of its own
// session directories, so this is never "something under the root looked
// odd": it is the root itself refusing to be enumerated, or a directory the
// sweep did identify as an orphan and could not probe or remove. Either leaves
// the root in a state its owner cannot vouch for before it binds a socket under
// it, which is a startup failure for the same reason an unusable root is.
type SigningSocketSweepError struct {
	// Root is the effective sockets root.
	Root string

	// Entry is the session directory that failed, when one did. It is empty
	// when the root itself is the subject.
	Entry string

	// Reason states what could not be done.
	Reason string

	// Cause is the underlying error.
	Cause error
}

// Error implements the error interface.
func (e *SigningSocketSweepError) Error() string {
	subject := e.Root
	if e.Entry != "" {
		subject = "the orphaned signing session " + e.Entry + " under " + e.Root
	}
	msg := ErrSigningSocketRoot.Error() + ": " + subject + " " + e.Reason +
		"; the sessions a killed process left are swept before a new one is bound"
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *SigningSocketSweepError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrSigningSocketRoot}
	}
	return []error{ErrSigningSocketRoot, e.Cause}
}

// SigningSocketNameError reports that no session directory name could be
// minted, because the system's randomness failed.
type SigningSocketNameError struct {
	// Cause is the randomness failure.
	Cause error
}

// Error implements the error interface.
func (e *SigningSocketNameError) Error() string {
	return ErrSigningSocketRoot.Error() + ": could not name a session's socket directory: " + e.Cause.Error()
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *SigningSocketNameError) Unwrap() []error {
	return []error{ErrSigningSocketRoot, e.Cause}
}
