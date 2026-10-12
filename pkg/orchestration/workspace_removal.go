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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// =============================================================================
// Session workspace removal — the checkout goes when its task is over
// =============================================================================
//
// A session workspace persists across the job's sessions, so nothing removes it
// when a session ends. It is removed when the TASK is done with it: by the sdlc
// runner's discard stage, on readmission and deletion of the task, and by the
// sweep for whatever those missed (go-quicbaas#777).
//
// REMOVAL IS CONFINED THE WAY PROVISIONING IS. The path is checked against the
// mount policy exactly as the bind-mount source it once was, and every level
// is walked through an os.Root opened at <root>/<tenant>, refusing a link at
// any of them — a tenant can plant links through its own read-write mount, and
// a removal that followed one would delete whatever it points at.
//
// A HELD WORKSPACE IS NEVER REMOVED. The manager counts the live sessions it
// provisioned into each directory, and a removal of one still mounted is a
// typed refusal: the container would lose its checkout mid-session.

// SetMountPolicy installs the policy every removal is checked against. Without
// one, removal is confined by the tenant's root alone. Call it before the host
// accepts a spawn; it is not safe against a concurrent removal.
func (m *WorkspaceManager) SetMountPolicy(policy *MountPolicy) { m.policy = policy }

// RemoveSessionWorkspace removes the named session workspace and the checkout
// in it, reporting whether there was one to remove. An absent workspace is not
// an error: removal is idempotent, and the discard stage, readmission,
// deletion and the sweep may each reach the same one.
//
// Every failure is a WorkspaceRemovalError.
func (m *WorkspaceManager) RemoveSessionWorkspace(tenantID, projectID, key string) (bool, error) {
	dir, err := m.SessionWorkspacePath(tenantID, projectID, key)
	if err != nil {
		return false, &WorkspaceRemovalError{Key: key, Cause: err}
	}
	if strings.HasPrefix(key, workspaceSpecDirName) {
		return false, &WorkspaceRemovalError{Key: key, Path: dir, Cause: &SessionWorkspaceInvalidError{
			Field:  "key",
			Reason: "names a spec directory, not a workspace",
		}}
	}
	if m.policy != nil {
		if err := m.policy.AuthorizeSource(tenantID, MountFieldWorkDir, dir); err != nil {
			return false, &WorkspaceRemovalError{Key: key, Path: dir, Cause: err}
		}
	}
	// The held check and the delete are one section against provisioning:
	// a session provisioned between them would lose its checkout.
	unlock := m.lockWorkspace(dir)
	defer unlock()
	if m.isHeld(dir) {
		return false, &WorkspaceRemovalError{Key: key, Path: dir, Cause: &WorkspaceHeldError{Path: dir}}
	}
	if m.hooks != nil && m.hooks.beforeRemove != nil {
		m.hooks.beforeRemove(dir)
	}
	removed, err := m.removeWorkspace(tenantID, projectID, key)
	if err != nil {
		return false, &WorkspaceRemovalError{Key: key, Path: dir, Cause: err}
	}
	return removed, nil
}

// removeWorkspace is RemoveSessionWorkspace's filesystem half. The tenant
// directory and each level below it must be a real directory or absent; an
// absent level means there is nothing to remove.
func (m *WorkspaceManager) removeWorkspace(tenantID, projectID, key string) (bool, error) {
	tenantDir := filepath.Join(m.root, tenantID)
	present, err := realDirPresent(tenantDir, tenantDir, os.Lstat)
	if err != nil || !present {
		return false, err
	}
	root, err := os.OpenRoot(tenantDir)
	if err != nil {
		return false, &WorkspaceIOError{Op: "remove", Path: tenantDir, Err: err}
	}
	defer root.Close()

	parent := filepath.Join(projectID, workspaceDirName)
	rel := filepath.Join(parent, key)
	for _, level := range []string{projectID, parent, rel} {
		present, err := realDirPresent(level, filepath.Join(tenantDir, level), root.Lstat)
		if err != nil || !present {
			return false, err
		}
	}
	if err := removeConfined(root, rel); err != nil {
		return false, &WorkspaceIOError{Op: "remove", Path: filepath.Join(tenantDir, rel), Err: err}
	}
	return true, nil
}

// realDirPresent reports whether path is a directory, false when it does not
// exist, and refuses a link or a non-directory. shown is the path as errors
// report it.
func realDirPresent(path, shown string, lstat func(string) (os.FileInfo, error)) (bool, error) {
	info, err := lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	case err != nil:
		return false, &WorkspaceIOError{Op: "remove", Path: shown, Err: err}
	case info.Mode()&os.ModeSymlink != 0:
		return false, &WorkspaceLinkError{Path: shown}
	case !info.IsDir():
		return false, &WorkspaceIOError{Op: "remove", Path: shown, Err: errNotADirectory}
	}
	return true, nil
}

// workspaceLock is one workspace directory's lock. users counts the callers
// holding or waiting for it, so the entry goes when the last one leaves.
type workspaceLock struct {
	mu    sync.Mutex
	users int
}

// workspaceHooks are the points a test stops at to order provisioning
// against removal without sleeping.
type workspaceHooks struct {
	// provisioned runs once provisioning has placed the directory, before it
	// takes the hold.
	provisioned func(dir string)
	// beforeRemove runs once a removal has found the workspace unheld,
	// before it deletes it.
	beforeRemove func(dir string)
	// contended runs when a caller is about to wait for a workspace lock
	// another caller holds.
	contended func(dir string)
}

// lockWorkspace takes dir's lock and returns its release. Directory
// provisioning with its hold, and a removal's held check with its delete,
// each run under it, so neither section interleaves with the other. The
// lock is per directory: work on one workspace never waits for another.
func (m *WorkspaceManager) lockWorkspace(dir string) (unlock func()) {
	m.heldMu.Lock()
	if m.locks == nil {
		m.locks = make(map[string]*workspaceLock)
	}
	lock := m.locks[dir]
	if lock == nil {
		lock = &workspaceLock{}
		m.locks[dir] = lock
	}
	lock.users++
	contended := lock.users > 1
	m.heldMu.Unlock()

	if contended && m.hooks != nil && m.hooks.contended != nil {
		m.hooks.contended(dir)
	}
	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		m.heldMu.Lock()
		defer m.heldMu.Unlock()
		lock.users--
		if lock.users == 0 {
			delete(m.locks, dir)
		}
	}
}

// hold records one more live session provisioned into dir.
func (m *WorkspaceManager) hold(dir string) {
	m.heldMu.Lock()
	defer m.heldMu.Unlock()
	if m.held == nil {
		m.held = make(map[string]int)
	}
	m.held[dir]++
}

// unhold records the end of one live session provisioned into dir.
func (m *WorkspaceManager) unhold(dir string) {
	m.heldMu.Lock()
	defer m.heldMu.Unlock()
	if m.held[dir] <= 1 {
		delete(m.held, dir)
		return
	}
	m.held[dir]--
}

// isHeld reports whether a live session is provisioned into dir.
func (m *WorkspaceManager) isHeld(dir string) bool {
	m.heldMu.Lock()
	defer m.heldMu.Unlock()
	return m.held[dir] > 0
}

// =============================================================================
// Typed errors
// =============================================================================

// ErrWorkspaceRemoval is the sentinel every failed session-workspace removal
// wraps. A removal failure is logged by its caller and never fails the
// conclusion it follows: the sweep retries it.
var ErrWorkspaceRemoval = errors.New("session workspace: removal failed")

// WorkspaceRemovalError reports a session workspace that could not be removed.
type WorkspaceRemovalError struct {
	// JobID is the job whose workspace it is, when the removal was asked for
	// by job; empty otherwise.
	JobID string

	// Key is the workspace.
	Key string

	// Path is the workspace directory, empty when the name never resolved to
	// one.
	Path string

	// Cause is why it was not removed.
	Cause error
}

// Error implements the error interface.
func (e *WorkspaceRemovalError) Error() string {
	msg := "session workspace: " + strconv.Quote(e.Key)
	if e.JobID != "" {
		msg += " of job " + e.JobID
	}
	if e.Path != "" {
		msg += " at " + strconv.Quote(e.Path)
	}
	return msg + " was not removed: " + e.Cause.Error()
}

// Unwrap returns ErrWorkspaceRemoval and the cause.
func (e *WorkspaceRemovalError) Unwrap() []error { return []error{ErrWorkspaceRemoval, e.Cause} }

// WorkspaceHeldError reports a removal refused because a live session on this
// host is provisioned into the workspace.
type WorkspaceHeldError struct {
	// Path is the workspace directory.
	Path string
}

// Error implements the error interface.
func (e *WorkspaceHeldError) Error() string {
	return "session workspace: " + strconv.Quote(e.Path) + " is held by a live session"
}
