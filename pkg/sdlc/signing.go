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

package sdlc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The signing agent — a socket in, a key never
// =============================================================================
//
// The worker's commit is signed with the project's KMS-held key, and the
// worker runs in a container. THE KEY DOES NOT TRAVEL. What travels is a unix
// socket to an agent the daemon holds, bound for the lifetime of one attempt
// and removed with it, so a compromised worker can ask for signatures while
// its container lives and holds nothing at all afterwards.
//
// The socket lives under the DAEMON'S OWN SOCKETS ROOT
// (orchestration.sockets_root), not under the project's workspace, and not in
// the OS temporary directory. A unix socket address is a fixed-size kernel
// field, and a managed workspace is
// <projects_root>/<tenant_id>/<project_id>/workspace — 88 bytes of tenant and
// project before a socket name — so a socket placed there does not fit on any
// realistic projects root. The sockets root is short, dedicated, created 0700
// at startup, and admitted by the mount policy by construction.

// SigningSocketFile is the socket's name inside the directory the agent
// creates. It is fixed by the go-xkms SDK, which is why a caller can compute
// the mount source before the session exists, and why the mount policy can
// recognise one by its shape.
const SigningSocketFile = orchestration.WorkspaceSigningAgentSocketFile

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
const MaxSigningSocketRoot = MaxSigningSocketPath - SigningSocketDirNameLen - len(SigningSocketFile) - 2

// LongestSigningSocketPath returns the length of the longest socket path root
// can yield. Every session directory is the same width, so this is exact
// rather than an estimate.
func LongestSigningSocketPath(root string) int {
	return len(filepath.Clean(root)) + SigningSocketDirNameLen + len(SigningSocketFile) + 2
}

// EnsureSigningSocketRoot prepares the daemon's sockets root and refuses,
// AT STARTUP, every root a session could not bind a socket in.
//
// It is called before the runner exists precisely so the two failures this
// subsystem actually has are reported by the thing that can name them:
//
//   - a root so long that the socket would exceed the kernel's sun_path field.
//     Discovered at initializing, it is one failed job per delivery on a
//     deployment that looks configured; discovered here, it is a refusal
//     naming the root and the limit.
//   - a root that cannot hold an agent socket safely. The socket's own 0600
//     mode does not protect its PATHNAME — the containing directory decides
//     who may unlink and re-bind it — so the session's parent must be private
//     to this user. go-xkms refuses a world-writable parent (which is why the
//     OS temporary directory is not a candidate) and it refuses it at the
//     same late moment; this refuses it now.
//
// The root itself is created 0700 and outlives every session. The session's
// own directory under it is created by the agent and removed with it — unless
// the process died holding it, which is what the sweep this returns is for.
//
// The refusals are ALL EVALUATED BEFORE THE SWEEP. The sweep unlinks
// directories, so it runs only under a root this function has just vouched
// for; a root it refuses is left exactly as it was found.
func EnsureSigningSocketRoot(root string) (SigningSocketSweep, error) {
	cleaned := filepath.Clean(strings.TrimSpace(root))
	if cleaned == "" || cleaned == "." || !filepath.IsAbs(cleaned) {
		return SigningSocketSweep{}, &InvalidInputError{
			Field:   "orchestration.sockets_root",
			Message: "the daemon binds its per-session agent sockets under an absolute root",
		}
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
	sweep, err := sweepSigningSocketRoot(cleaned)
	if err != nil {
		return sweep, err
	}
	// THE ROOT CARRIES THIS DAEMON'S SIGNING IDENTIFIER, which is how a fleet
	// agent proves it can mount the sockets bound here and how the dispatcher
	// places a signing spawn only on such an agent (go-quicbaas#981).
	if _, err := orchestration.EnsureSigningRootID(cleaned); err != nil {
		return sweep, &SigningSocketRootUnusableError{
			Root: cleaned, Reason: "could not carry its signing-root marker", Cause: err,
		}
	}
	return sweep, nil
}

// =============================================================================
// The startup sweep — what a process that died mid-attempt left behind
// =============================================================================
//
// An attempt that ENDS releases its agent: the runner's teardown stops the
// session on a context detached from the attempt's, so a cancelled or failed
// attempt still unlinks its socket and its directory. What no teardown can
// survive is the process not running any more. A daemon killed between the
// agent's bind and its release leaves a 0700 directory holding a dead socket,
// and the sockets root OUTLIVES THE DAEMON — it is $XDG_RUNTIME_DIR/quicbaas
// or <data_dir>/s — so on a node that kills jobs those accumulate without
// bound. go-quicbaas#633.
//
// WHAT "NOT LIVE" MEANS. An agent's listener dies with the process that held
// it, so a socket that refuses a connection is answered by nobody and the
// attempt that bound it is over. The sweep asks the socket rather than
// leaning on the simpler invariant "this daemon has only just started, so
// nothing under the root can be live": that invariant holds for ONE daemon
// per root and the root can be shared. $XDG_RUNTIME_DIR/quicbaas is reached
// by every daemon of the same user, and a second one starting must not unlink
// the first one's live sockets. Asking costs one connect that is immediately
// released, and it is correct under both deployments.
//
// WHAT IS SWEPT is only what this package mints: a directory named exactly
// SigningSocketDirNameLen hexadecimal characters, holding nothing but
// SigningSocketFile, and that socket answering nobody. An empty one is a
// process killed between the agent's mkdir and its bind, and there is nothing
// to ask. Anything else under the root — a foreign name, a file, a symlink,
// a directory with other contents in it, a directory this daemon may not read
// — is somebody else's and is left exactly where it is.

// signingSocketProbeTimeout bounds the liveness probe. Connecting to a unix
// socket is answered by the kernel without a round trip — a live listener
// takes the connection onto its backlog and a dead one is refused at once —
// so this is a ceiling on a pathology, not a wait anything reaches.
const signingSocketProbeTimeout = 2 * time.Second

// SigningSocketSweep reports what preparing the sockets root removed.
//
// It is a value rather than a log line because the module holds no logger:
// the host owns the sockets root's configuration and its logger, and reports
// the sweep next to the module it started.
type SigningSocketSweep struct {
	// Root is the sockets root the sweep ran under, cleaned.
	Root string

	// Removed names the orphaned session directories it removed, in the
	// order the root enumerated them. It is nil when there were none.
	Removed []string
}

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
		if !entry.IsDir() || !isSigningSessionDirName(entry.Name()) {
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

// isSigningSessionDirName reports whether name has the shape
// signingSocketDirName mints: exactly SigningSocketDirNameLen lowercase
// hexadecimal characters, and nothing else. A multi-byte rune fails on its
// leading byte, so the check is safe over any name the filesystem returns.
func isSigningSessionDirName(name string) bool {
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

// signingSessionSocket reports whether dir is one of this package's session
// directories, and the socket inside it when there is one.
//
// A directory holding anything other than the agent's socket is not one of
// ours whatever it is named, and one this daemon cannot read is not
// positively identified as ours either — both are declined rather than
// reported as failures, because the sweep removes only what it recognises.
func signingSessionSocket(dir string) (socket string, ours bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if entry.Name() != SigningSocketFile || entry.Type()&os.ModeSocket == 0 {
			return "", false
		}
	}
	if len(entries) == 0 {
		// Killed between the agent's mkdir and its bind: ours, and there is
		// no socket to ask.
		return "", true
	}
	return filepath.Join(dir, SigningSocketFile), true
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

// signingSocketRootMode is the sockets root's own mode: private to the daemon
// user, because it is the parent every session directory is created in and a
// parent nobody else can write to is what makes the socket's pathname safe.
const signingSocketRootMode = 0o700

// SigningScope says which of the project's keys an agent serves, and it is
// the ONE place the difference between a reader's container and a deliverer's
// is stated.
//
// THERE ARE TWO KEYS, NOT ONE. A project's git identity is an ACCESS key —
// what ssh authenticates a clone, a fetch and a push with — and a COMMIT
// SIGNING key. ProjectGit.AccessKey has always known that; what did not was
// the agent, which served both or neither, so a runner that must not sign
// could not clone either. That is go-quicbaas#929: triage was handed no agent
// on purpose, and therefore no way to authenticate an ssh remote, and burned
// one job per sweep discovering it inside a container.
type SigningScope uint8

const (
	// SigningScopeCheckout serves the project's ACCESS KEY ONLY: enough for
	// git to authenticate a clone or a fetch over ssh, and no commit-signing
	// key, so nothing in the container can produce a commit signature under
	// the project's provenance identity.
	//
	// It is what a READER asks for — triage and the reviewer — and its agent
	// is stopped the moment the checkout is made, before anything is
	// prompted: the socket the container holds from then on answers nothing
	// at all. A project whose access key IS its signing key (the common
	// shape, see ProjectGit.AccessKey) narrows that window rather than
	// removing it; an operator who wants it removed configures a distinct
	// git.ssh_key, which is the field that exists for exactly this.
	SigningScopeCheckout SigningScope = iota

	// SigningScopeDelivery serves the access key AND the commit signing key:
	// the delivery runner's container clones, signs its commit and pushes it,
	// and an agent serving only one of the two would produce a signed commit
	// nothing could push.
	SigningScopeDelivery
)

// signingScopeNames render a scope for a person reading a log or an error.
var signingScopeNames = map[SigningScope]string{
	SigningScopeCheckout: "checkout",
	SigningScopeDelivery: "delivery",
}

// String implements fmt.Stringer.
func (s SigningScope) String() string {
	if name, ok := signingScopeNames[s]; ok {
		return name
	}
	return "unknown"
}

// SignsCommits reports whether an agent of this scope serves a commit-signing
// key. It is the question the publisher's git configuration turns on, asked of
// the scope rather than inferred from a session's fields.
func (s SigningScope) SignsCommits() bool { return s == SigningScopeDelivery }

// signingSocketDirName is a fresh directory name per session:
// SigningSocketDirNameLen hexadecimal characters over random bytes, with no
// prefix, because every byte of it is spent out of the socket path's budget.
//
// It is random rather than derived from the job for two reasons. A resumed
// attempt never collides with a directory a previous one owned — the agent
// refuses an existing path by design, and adopting one would be adopting its
// contents. And the sockets root is shared by every tenant, so an unguessable
// name is what stops one spawn naming another session's live socket as a mount
// source.
func signingSocketDirName() (string, error) {
	buf := make([]byte, SigningSocketDirNameLen/2)
	if _, err := rand.Read(buf); err != nil {
		return "", &SigningSessionError{Message: "could not name the agent's socket directory: " + err.Error()}
	}
	return hex.EncodeToString(buf), nil
}

// SigningSession is one running signing agent.
type SigningSession struct {
	// SocketPath is the agent's socket on the DAEMON's filesystem. It is
	// what the spawn bind-mounts into the container.
	SocketPath string

	// PublicKey is the signing key's public half in authorized_keys form.
	// git is given it by value ("user.signingkey=key::<line>"), so nothing
	// has to write a key file into the container and delete it again.
	PublicKey string

	// Stop tears the agent down and removes the socket and its directory.
	// It is always non-nil and always safe to call more than once.
	Stop func(context.Context)
}

// SigningAgent starts an ephemeral, KMS-backed SSH agent for one session.
//
// The module holds no keys and no KMS client: the host supplies this over the
// same git provisioner that resolves every other project credential.
type SigningAgent interface {
	// StartSigningAgent binds an agent serving the keys scope names, on a
	// socket inside socketDir.
	//
	// socketDir MUST NOT EXIST — the session creates it 0700 itself, which is
	// what makes a 0600 socket inside it safe — and its parent must be a
	// directory private to the daemon's own user. Both refusals are typed and
	// neither is worked around.
	//
	// A delivery scope answers with the commit signing key's public half on
	// the session; a checkout scope answers with none, because it serves
	// none.
	StartSigningAgent(
		ctx context.Context,
		project *orchestration.Project,
		socketDir string,
		scope SigningScope,
	) (*SigningSession, error)
}
