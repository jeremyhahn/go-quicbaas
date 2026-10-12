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

package orchestration

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// workspaceDirName is the leaf directory holding the project's working tree:
// <projects_root>/<tenant_id>/<project_id>/workspace.
const workspaceDirName = "workspace"

// workspaceDirPerm is the permission every directory the manager creates is
// held at. Workspaces may hold cloned source and resolved credentials in
// flight, so nothing outside the daemon's user may traverse them.
const workspaceDirPerm = os.FileMode(0o700)

// =============================================================================
// Typed errors
// =============================================================================

// WorkspaceRootError reports a projects root the manager cannot operate on.
type WorkspaceRootError struct {
	// Root is the rejected projects root.
	Root string

	// Reason states why the root was rejected.
	Reason string
}

// Error implements the error interface.
func (e *WorkspaceRootError) Error() string {
	return "workspace: invalid projects root " + strconv.Quote(e.Root) + ": " + e.Reason
}

// WorkspaceSegmentError reports a tenant or project identifier that cannot be
// used as a path segment under the projects root. Identifiers are
// server-generated UUIDs and validated tenant IDs, so this error firing means
// a caller bypassed that validation — the manager refuses rather than letting
// the path escape the root.
type WorkspaceSegmentError struct {
	// Field names the offending identifier ("tenant_id" or "project_id").
	Field string

	// Value is the rejected identifier.
	Value string

	// Reason states why the identifier was rejected.
	Reason string
}

// Error implements the error interface.
func (e *WorkspaceSegmentError) Error() string {
	return "workspace: invalid " + e.Field + " " + strconv.Quote(e.Value) + ": " + e.Reason
}

// WorkspaceIOError reports a filesystem operation failure while provisioning
// or removing a workspace.
type WorkspaceIOError struct {
	// Op is the failed operation ("provision" or "remove").
	Op string

	// Path is the filesystem path the operation targeted.
	Path string

	// Err is the underlying filesystem error.
	Err error
}

// Error implements the error interface.
func (e *WorkspaceIOError) Error() string {
	return "workspace: " + e.Op + " " + strconv.Quote(e.Path) + ": " + e.Err.Error()
}

// Unwrap exposes the underlying filesystem error to errors.Is / errors.As.
func (e *WorkspaceIOError) Unwrap() error { return e.Err }

// =============================================================================
// WorkspaceManager
// =============================================================================

// WorkspaceManager provisions and removes server-managed project workspaces
// under a single absolute projects root. Layout:
//
//	<root>/<tenant_id>/<project_id>/workspace
//
// Every directory it creates is 0700. Both operations are idempotent, and
// every path it touches is validated to stay under the root.
type WorkspaceManager struct {
	root string

	// conclusions, when set, lets the startup sweep remove the workspace of a
	// concluded task (SetConclusions).
	conclusions WorkspaceConclusions

	// policy, when set, is the mount policy every removal is checked against
	// before anything is removed (SetMountPolicy).
	policy *MountPolicy

	// held counts the live sessions provisioned into each workspace directory,
	// so a removal never takes a checkout a running container still mounts.
	heldMu sync.Mutex
	held   map[string]int

	// locks serialises, per workspace directory, provisioning with taking
	// its hold and a removal's held check with its delete (lockWorkspace).
	// Guarded by heldMu.
	locks map[string]*workspaceLock

	// hooks lets a test stop at the points two of those sections can meet.
	// Nil in production.
	hooks *workspaceHooks
}

// NewWorkspaceManager returns a manager rooted at root, which must be an
// absolute path. The root directory itself is created lazily on the first
// Provision call.
func NewWorkspaceManager(root string) (*WorkspaceManager, error) {
	if root == "" {
		return nil, &WorkspaceRootError{Root: root, Reason: "projects root is required"}
	}
	if !filepath.IsAbs(root) {
		return nil, &WorkspaceRootError{Root: root, Reason: "projects root must be an absolute path"}
	}
	return &WorkspaceManager{root: filepath.Clean(root)}, nil
}

// Root returns the absolute projects root the manager operates under.
func (m *WorkspaceManager) Root() string { return m.root }

// WorkspacePath returns the workspace directory for the tenant/project pair
// without touching the filesystem.
func (m *WorkspaceManager) WorkspacePath(tenantID, projectID string) (string, error) {
	projectDir, err := m.projectDir(tenantID, projectID)
	if err != nil {
		return "", err
	}
	return filepath.Join(projectDir, workspaceDirName), nil
}

// Provision creates <root>/<tenant_id>/<project_id>/workspace with 0700
// permissions on every created level and returns the workspace path. It is
// idempotent: an already-provisioned workspace is returned as-is.
func (m *WorkspaceManager) Provision(tenantID, projectID string) (string, error) {
	workspace, err := m.WorkspacePath(tenantID, projectID)
	if err != nil {
		return "", err
	}
	if mkErr := os.MkdirAll(workspace, workspaceDirPerm); mkErr != nil {
		return "", &WorkspaceIOError{Op: "provision", Path: workspace, Err: mkErr}
	}
	// MkdirAll applies the umask to the mode it creates with, so pin each
	// managed level to 0700 explicitly. The root itself is shared across
	// tenants and stays at whatever the operator set it to.
	projectDir := filepath.Dir(workspace)
	tenantDir := filepath.Dir(projectDir)
	for _, dir := range []string{tenantDir, projectDir, workspace} {
		if chErr := os.Chmod(dir, workspaceDirPerm); chErr != nil {
			return "", &WorkspaceIOError{Op: "provision", Path: dir, Err: chErr}
		}
	}
	return workspace, nil
}

// Remove deletes the project's entire directory tree
// (<root>/<tenant_id>/<project_id>). It is idempotent: removing an absent
// tree succeeds.
func (m *WorkspaceManager) Remove(tenantID, projectID string) error {
	projectDir, err := m.projectDir(tenantID, projectID)
	if err != nil {
		return err
	}
	if rmErr := os.RemoveAll(projectDir); rmErr != nil {
		return &WorkspaceIOError{Op: "remove", Path: projectDir, Err: rmErr}
	}
	return nil
}

// projectDir validates both identifiers and returns
// <root>/<tenant_id>/<project_id>, asserting the result stays under the root.
func (m *WorkspaceManager) projectDir(tenantID, projectID string) (string, error) {
	if err := validateWorkspaceSegment("tenant_id", tenantID); err != nil {
		return "", err
	}
	if err := validateWorkspaceSegment("project_id", projectID); err != nil {
		return "", err
	}
	dir := filepath.Join(m.root, tenantID, projectID)
	// Defence in depth: the segment validation above already forbids every
	// construct that could redirect the join, so a path outside the root here
	// means that validation was broken — refuse rather than operate on it.
	if !pathWithinRoot(m.root, dir) {
		return "", &WorkspaceSegmentError{
			Field:  "project_id",
			Value:  projectID,
			Reason: "resolved path escapes the projects root",
		}
	}
	return dir, nil
}

// validateWorkspaceSegment rejects any identifier that is not usable as a
// single path element: empty strings, path separators, traversal elements and
// hidden-file prefixes.
func validateWorkspaceSegment(field, value string) error {
	switch {
	case value == "":
		return &WorkspaceSegmentError{Field: field, Value: value, Reason: "must not be empty"}
	case value == "." || value == "..":
		return &WorkspaceSegmentError{Field: field, Value: value, Reason: "must not be a path traversal element"}
	case strings.ContainsAny(value, `/\`):
		return &WorkspaceSegmentError{Field: field, Value: value, Reason: "must not contain path separators"}
	case strings.ContainsRune(value, 0):
		return &WorkspaceSegmentError{Field: field, Value: value, Reason: "must not contain NUL bytes"}
	}
	return nil
}

// pathWithinRoot reports whether path is root itself or a descendant of root.
// Both inputs must already be cleaned absolute paths.
func pathWithinRoot(root, path string) bool {
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}
