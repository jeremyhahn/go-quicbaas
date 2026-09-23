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
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// =============================================================================
// The signing agent is placed, not assumed
// =============================================================================
//
// A spawn that signs commits — the delivery worker — or clones an ssh remote
// mounts the KMS-backed ssh agent's socket, which the daemon binds under its
// OWN sockets root. Only an agent that can see that directory can mount it; any
// other refuses the spawn at its mount check, and the job is parked rather than
// re-placed (go-quicbaas#1013 is the relay that would lift this).
//
// SO THE DISPATCHER ONLY PLACES SUCH A SPAWN WHERE THE SOCKET IS LOCAL, and
// "local" is proved, not configured. The daemon writes a random identifier into
// its sockets root (EnsureSigningRootID). An agent looks for that marker under
// the directories its mount policy admits (DiscoverSigningRootIDs) and reports
// every identifier it finds, as the trait signing-agent:<id>, on registration
// and on every heartbeat. A spawn that needs the socket requires the trait
// naming THIS daemon's identifier. A path would not do: two daemons with the
// same data directory on two hosts derive the same sockets root.

// SigningRootMarkerFile is the file, inside a daemon's sockets root, holding
// that daemon's signing-root identifier. It is not a socket and not a session
// directory, so neither the mount policy nor the sweep treats it as either.
const SigningRootMarkerFile = ".quicbaas-signing-root"

// signingRootMarkerMode is the marker's mode: readable by the daemon's own uid
// alone, which is the uid a co-located agent runs as.
const signingRootMarkerMode os.FileMode = 0o400

// signingRootIDBytes is the identifier's entropy.
const signingRootIDBytes = 16

// AgentTraitSigningAgentPrefix prefixes the trait an agent advertises for each
// daemon sockets root it can mount from. Like AgentTraitGPU it rides in
// Capabilities and names a property of the HOST, never a backend.
const AgentTraitSigningAgentPrefix = "signing-agent:"

// SigningAgentTrait is the capability an agent advertises for the sockets root
// identified by id.
func SigningAgentTrait(id string) string { return AgentTraitSigningAgentPrefix + id }

// isSigningAgentTrait reports whether capability is a signing-agent trait.
func isSigningAgentTrait(capability string) bool {
	return strings.HasPrefix(capability, AgentTraitSigningAgentPrefix)
}

// EnsureSigningRootID returns the identifier in root's marker, writing a fresh
// one when there is none. root must already exist; the caller that prepares the
// sockets root calls this once it has.
func EnsureSigningRootID(root string) (string, error) {
	if id, err := ReadSigningRootID(root); err == nil {
		return id, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	raw := make([]byte, signingRootIDBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", &SigningRootMarkerError{Root: root, Reason: "no randomness for the identifier", Cause: err}
	}
	id := hex.EncodeToString(raw)
	path := filepath.Join(root, SigningRootMarkerFile)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, signingRootMarkerMode)
	if errors.Is(err, os.ErrExist) {
		// Another process wrote it first; theirs is the identifier.
		return ReadSigningRootID(root)
	}
	if err != nil {
		return "", &SigningRootMarkerError{Root: root, Reason: "could not be created", Cause: err}
	}
	_, writeErr := file.WriteString(id + "\n")
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return "", &SigningRootMarkerError{Root: root, Reason: "could not be written", Cause: errors.Join(writeErr, closeErr)}
	}
	return id, nil
}

// ReadSigningRootID reads the identifier in root's marker. A root with no
// marker answers an error satisfying errors.Is(err, os.ErrNotExist).
func ReadSigningRootID(root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, SigningRootMarkerFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		return "", &SigningRootMarkerError{Root: root, Reason: "could not be read", Cause: err}
	}
	id := strings.TrimSpace(string(raw))
	if !validSigningRootID(id) {
		return "", &SigningRootMarkerError{Root: root, Reason: "holds no identifier"}
	}
	return id, nil
}

// validSigningRootID reports whether id has the shape EnsureSigningRootID
// writes, so a planted or truncated marker is never advertised.
func validSigningRootID(id string) bool {
	if len(id) != 2*signingRootIDBytes {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

// DiscoverSigningRootIDs returns the identifiers of every daemon sockets root
// found directly in, or one level below, each of roots — the directories an
// agent's mount policy admits. The daemon's sockets root is
// <runtime dir>/quicbaas/<instance>, and an operator admits the <runtime
// dir>/quicbaas parent, hence the one level. Unreadable entries are skipped:
// an identifier that cannot be read is a socket the agent could not be proved
// to reach. The result is sorted and de-duplicated.
func DiscoverSigningRootIDs(roots []string) []string {
	found := map[string]struct{}{}
	for _, root := range roots {
		if root == "" || !filepath.IsAbs(root) {
			continue
		}
		candidates := []string{root}
		if entries, err := os.ReadDir(root); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					candidates = append(candidates, filepath.Join(root, entry.Name()))
				}
			}
		}
		for _, dir := range candidates {
			if id, err := ReadSigningRootID(dir); err == nil {
				found[id] = struct{}{}
			}
		}
	}
	ids := make([]string, 0, len(found))
	for id := range found {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// signingAgentRequiredKey is the context key marking a spawn that mounts the
// daemon's signing socket.
type signingAgentRequiredKey struct{}

// WithSigningAgentRequired marks ctx as resolving a spawn that mounts the
// daemon's signing socket, so the fleet resolver places it only on an agent
// advertising this daemon's signing-agent trait.
func WithSigningAgentRequired(ctx context.Context) context.Context {
	return context.WithValue(ctx, signingAgentRequiredKey{}, true)
}

// SigningAgentRequiredFromContext reports whether ctx carries the mark.
func SigningAgentRequiredFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(signingAgentRequiredKey{}).(bool)
	return v
}

// spawnNeedsSigningAgent reports whether a spawn mounts the daemon's signing
// socket. The mount is the fact — it is what an agent elsewhere would refuse —
// so it is read off the request rather than declared beside it.
func spawnNeedsSigningAgent(req *SpawnSessionRequest) bool {
	for _, mount := range req.ExtraMounts {
		if mount.Target == WorkspaceSigningAgentSocket {
			return true
		}
	}
	return false
}

// =============================================================================
// Typed errors
// =============================================================================

// ErrSigningAgentUnavailable is the sentinel for a spawn that mounts the
// daemon's signing socket and has nowhere it could be mounted.
var ErrSigningAgentUnavailable = errors.New("signing agent: no host can mount the daemon's signing socket")

// SigningAgentUnavailableError reports a spawn that needs the daemon's signing
// socket for which no fleet agent co-located with the daemon — and no
// in-process backend on the daemon itself — serves the backend.
type SigningAgentUnavailableError struct {
	// Backend is the backend the spawn asked for.
	Backend string

	// Cause is the resolution failure.
	Cause error
}

// Error implements the error interface.
func (e *SigningAgentUnavailableError) Error() string {
	msg := ErrSigningAgentUnavailable.Error() + " for a " + e.Backend + " spawn: no agent advertising this " +
		"daemon's " + AgentTraitSigningAgentPrefix + "<id> trait serves it (go-quicbaas#1013)"
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause.
func (e *SigningAgentUnavailableError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrSigningAgentUnavailable}
	}
	return []error{ErrSigningAgentUnavailable, e.Cause}
}

// SigningRootMarkerError reports a sockets-root marker that could not be
// written or read.
type SigningRootMarkerError struct {
	// Root is the sockets root.
	Root string

	// Reason states what failed.
	Reason string

	// Cause is the underlying error, nil when there is none.
	Cause error
}

// Error implements the error interface.
func (e *SigningRootMarkerError) Error() string {
	msg := "signing agent: marker in " + e.Root + " " + e.Reason
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the cause.
func (e *SigningRootMarkerError) Unwrap() error { return e.Cause }
