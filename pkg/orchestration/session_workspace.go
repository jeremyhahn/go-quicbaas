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
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// =============================================================================
// Session workspaces — the spawn names the workspace, the executing host makes it
// =============================================================================
//
// A SPAWN THAT CARRIES A HOST PATH CAN ONLY RUN ON THE HOST THAT MADE IT. The
// sdlc runners used to provision their checkout directory under the DAEMON's
// projects root and bind-mount that absolute path, so a fleet agent on any
// other machine refused the mount — it has no such directory, and its mount
// policy admits nothing of the daemon's (go-quicbaas#981). Git already stays
// agent-side: the clone, the commit and the push all run inside the session's
// container. The directory the container clones into is the one piece that did
// not, and this is where it moves.
//
// So a spawn NAMES its workspace — tenant, project, key — and the host that
// executes the session provisions it under its OWN projects root:
//
//	<projects_root>/<tenant_id>/<project_id>/workspace/<key>
//
// The daemon's in-process backend provisions under the daemon's root; a fleet
// agent provisions under the root it was started with. Either way the host
// reports the directory back, and the daemon records it on the session.
//
// THE CHECKOUT PERSISTS ON ONE HOST, SO THE JOB STAYS ON THAT HOST. A job's
// later sessions — the next review round, a rolled-over worker, the respawn
// after a restart — reuse the clone the first one made, which is only possible
// where the clone is. They carry the first session's placement as a pin
// (SessionWorkspace.Pin), and a pin that cannot be honoured is a typed refusal,
// never a fresh clone somewhere else: WorkspaceAgentUnavailableError when the
// agent is gone, WorkspaceStaleError when it is there but the checkout is not.

// sessionWorkspaceSpecFiles is the closed set of file names a spec directory
// may hold. A host writing a spec directory from a wire request writes these
// names and nothing else, so a caller cannot use the spec to place arbitrary
// files under a tenant's workspace.
var sessionWorkspaceSpecFiles = map[string]struct{}{
	WorkspaceSpecFile:       {},
	WorkspaceForgeTokenFile: {},
	WorkspaceKnownHostsFile: {},
	// The allowed_signers a delivery's signing key is verified against.
	WorkspaceAllowedSignersFile: {},
}

// SessionWorkspace names the workspace a spawn works in. It replaces a host
// path on the spawn: whichever host executes the session provisions the named
// workspace under its own projects root and mounts it at the container's
// checkout root.
type SessionWorkspace struct {
	// TenantID owns the workspace. It must be the tenant the spawn runs
	// under; a host refuses to provision one tenant's workspace for another.
	TenantID string `json:"tenant_id"`

	// ProjectID is the project the workspace belongs to.
	ProjectID string `json:"project_id"`

	// Key names this workspace within the project: an attempt, a review of
	// one head, a triage checkout. It is a single path segment.
	Key string `json:"key"`

	// Pin is where an earlier session of the same job provisioned this
	// workspace, nil for the job's first spawn. A pinned spawn lands on the
	// pinned host or is refused; see the package note above.
	Pin *WorkspacePlacement `json:"pin,omitempty"`

	// Spec is the spec directory the container reads its checkout from,
	// written beside the workspace by the host that provisions it.
	//
	// IT NEVER SERIALIZES. It carries the forge token, and a spawn request
	// is copied into durable records and logs; the token travels to the
	// executing host on the authenticated spawn RPC and nowhere else.
	Spec *WorkspaceSpecFiles `json:"-"`
}

// Validate refuses a workspace no host could provision: a missing or
// non-segment identifier, or a spec naming a file outside the closed set.
func (w *SessionWorkspace) Validate() error {
	if w == nil {
		return &SessionWorkspaceInvalidError{Field: "workspace", Reason: "is required"}
	}
	for _, seg := range []struct{ field, value string }{
		{"tenant_id", w.TenantID},
		{"project_id", w.ProjectID},
		{"key", w.Key},
	} {
		if err := validateWorkspaceSegment(seg.field, seg.value); err != nil {
			return &SessionWorkspaceInvalidError{Field: seg.field, Reason: err.Error()}
		}
	}
	// A key shaped like a spec directory would be removed by the startup
	// sweep (SweepSessionSpecs) together with the checkout in it.
	if strings.HasPrefix(w.Key, workspaceSpecDirName) {
		return &SessionWorkspaceInvalidError{
			Field:  "key",
			Reason: "may not begin with " + strconv.Quote(workspaceSpecDirName) + ", the spec directories' prefix",
		}
	}
	if w.Spec != nil {
		for name := range w.Spec.Files {
			if _, ok := sessionWorkspaceSpecFiles[name]; !ok {
				return &SessionWorkspaceInvalidError{
					Field:  "spec",
					Reason: "names file " + strconv.Quote(name) + ", which no workspace spec holds",
				}
			}
		}
	}
	return nil
}

// Pinned reports whether the workspace was already placed by an earlier
// session of the job.
func (w *SessionWorkspace) Pinned() bool {
	return w != nil && w.Pin != nil
}

// WorkspacePlacement is where a session's workspace was provisioned: the host
// that holds it and the directory on that host.
type WorkspacePlacement struct {
	// AgentHost is the fleet agent the workspace lives on — its registration
	// Host, the value the dispatcher resolves host affinity against. Empty
	// means the daemon's own in-process backend.
	AgentHost string `json:"agent_host,omitempty"`

	// Key is the workspace key the placement is for.
	Key string `json:"key"`

	// Dir is the workspace directory as that host sees it. It names nothing
	// on any other host.
	Dir string `json:"dir"`
}

// OnDaemon reports whether the workspace lives on the daemon itself rather
// than on a fleet agent.
func (p WorkspacePlacement) OnDaemon() bool { return p.AgentHost == "" }

// ProvisionedWorkspace is a session workspace made on this host: the
// directory mounted at the container's checkout root, the spec directory
// beside it, and the release that removes the spec directory again.
type ProvisionedWorkspace struct {
	// Dir is the workspace directory. It persists across the job's sessions;
	// only the spec directory is released.
	Dir string

	// SpecDir is the spec directory, empty when the spawn carried no spec.
	SpecDir string

	release     func()
	releaseOnce sync.Once
}

// Release removes the spec directory — and with it the forge token — leaving
// the workspace and its checkout in place for the job's next session. It is
// idempotent and safe on a nil receiver.
func (p *ProvisionedWorkspace) Release() {
	if p == nil || p.release == nil {
		return
	}
	p.releaseOnce.Do(p.release)
}

// ApplyTo puts the workspace on a backend spawn: the directory as the work
// directory, and the spec directory as a read-only mount at WorkspaceSpecDir.
func (p *ProvisionedWorkspace) ApplyTo(req *SpawnRequest) {
	if p == nil || req == nil {
		return
	}
	req.WorkDir = p.Dir
	if p.SpecDir != "" {
		req.ExtraMounts = append(req.ExtraMounts, WorkspaceSpecMounts(p.SpecDir, "")...)
	}
}

// WorkspaceProvisioner provisions named session workspaces on the host it
// runs on. *WorkspaceManager is the implementation; the daemon and every
// fleet agent use it over their own projects root.
type WorkspaceProvisioner interface {
	// ProvisionSession makes the named workspace and writes its spec.
	ProvisionSession(ws *SessionWorkspace) (*ProvisionedWorkspace, error)

	// SweepSessionSpecs removes every spec directory under the projects
	// root. It is called at startup, while the host holds no session.
	SweepSessionSpecs() ([]string, error)
}

var _ WorkspaceProvisioner = (*WorkspaceManager)(nil)

// SessionWorkspacePath is the directory a named session workspace occupies,
// without touching the filesystem:
// <root>/<tenant_id>/<project_id>/workspace/<key>.
func (m *WorkspaceManager) SessionWorkspacePath(tenantID, projectID, key string) (string, error) {
	workspace, err := m.WorkspacePath(tenantID, projectID)
	if err != nil {
		return "", err
	}
	if err := validateWorkspaceSegment("key", key); err != nil {
		return "", err
	}
	return filepath.Join(workspace, key), nil
}

// ProvisionSession makes the named workspace on this host and writes its spec
// directory beside it.
//
// EVERY FILESYSTEM OPERATION IS CONFINED TO THE TENANT'S SUBTREE. A tenant may
// mount its own workspaces read-write, so anything under
// <root>/<tenant> may be a symlink it planted — a workspace key pointing at
// /etc, a workspace directory pointing at another tenant's. Every create,
// chmod and write below goes through an os.Root opened at <root>/<tenant>,
// which refuses any path that resolves outside it, race-free; and each level
// the workspace is made of must itself be a directory, not a link.
//
// A PINNED WORKSPACE IS FOUND, NEVER MADE. The pin says an earlier session of
// the job provisioned it here, at Pin.Dir, and cloned into it. A directory that
// resolves elsewhere — the host's projects root moved — or no longer exists —
// somebody removed it — is WorkspaceStaleError: re-creating it empty would hand
// the session a fresh clone while every record of the job says it continues the
// old one.
//
// EVERY FAILURE IS A WORKSPACE FAILURE. A projects root that is read-only, full
// or not ours is an operator's condition exactly as a stale pin is, so a
// filesystem error is reported as SessionWorkspaceProvisionError — within the
// ErrSessionWorkspace family, which crosses the agent wire as such and parks
// the job uncharged — rather than as a bare I/O error a runner would charge.
func (m *WorkspaceManager) ProvisionSession(ws *SessionWorkspace) (*ProvisionedWorkspace, error) {
	provisioned, err := m.provisionSession(ws)
	if err != nil && !errors.Is(err, ErrSessionWorkspace) {
		key := ""
		if ws != nil {
			key = ws.Key
		}
		return nil, &SessionWorkspaceProvisionError{Key: key, Cause: err}
	}
	return provisioned, err
}

// provisionSession is ProvisionSession's body; see it for the contract.
func (m *WorkspaceManager) provisionSession(ws *SessionWorkspace) (*ProvisionedWorkspace, error) {
	if err := ws.Validate(); err != nil {
		return nil, err
	}
	dir, err := m.SessionWorkspacePath(ws.TenantID, ws.ProjectID, ws.Key)
	if err != nil {
		return nil, err
	}
	tenant, err := m.tenantRoot(ws.TenantID)
	if err != nil {
		return nil, err
	}
	defer tenant.Close()

	parent := filepath.Join(ws.ProjectID, workspaceDirName)
	rel := filepath.Join(parent, ws.Key)
	if pin := ws.Pin; pin != nil && pin.Key == ws.Key {
		if err := requirePinnedWorkspace(tenant, ws, rel, dir); err != nil {
			return nil, err
		}
	} else if err := makeConfinedDirs(tenant, dir, ws.ProjectID, parent, rel); err != nil {
		return nil, err
	}
	provisioned := &ProvisionedWorkspace{Dir: dir}
	if ws.Spec == nil {
		return provisioned, nil
	}
	// The spec directory is written BESIDE the workspace, never inside it:
	// the container mounts the workspace read-write, and a token under it
	// would be readable — and rewritable — through the checkout mount.
	specName, err := writeSpecDirIn(tenant, parent, ws.Spec.Files)
	// THE IN-MEMORY COPY GOES THE MOMENT IT IS WRITTEN, or the moment it
	// could not be: the file is now the one copy on this host, and it lives
	// exactly as long as the session.
	ws.Spec.Wipe()
	if err != nil {
		return nil, err
	}
	specRel := filepath.Join(parent, specName)
	provisioned.SpecDir = filepath.Join(m.root, ws.TenantID, specRel)
	provisioned.release = func() {
		// Removed through the tenant's root too, so a directory swapped for
		// a link since it was written cannot aim the removal elsewhere.
		if r, openErr := m.tenantRoot(ws.TenantID); openErr == nil {
			_ = r.RemoveAll(specRel)
			_ = r.Close()
		}
	}
	return provisioned, nil
}

// tenantRoot opens <root>/<tenant> as an os.Root, creating it (and the
// projects root) when absent. The tenant directory itself must be a directory,
// not a link: it is the one level a tenant cannot replace — that would take
// write access to the projects root, which no tenant is granted — and the
// confinement of everything below rests on it.
func (m *WorkspaceManager) tenantRoot(tenantID string) (*os.Root, error) {
	if err := validateWorkspaceSegment("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(m.root, workspaceDirPerm); err != nil {
		return nil, &WorkspaceIOError{Op: "provision", Path: m.root, Err: err}
	}
	dir := filepath.Join(m.root, tenantID)
	if err := os.Mkdir(dir, workspaceDirPerm); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, &WorkspaceIOError{Op: "provision", Path: dir, Err: err}
	}
	if err := requireRealDir(dir, os.Lstat); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, &WorkspaceIOError{Op: "provision", Path: dir, Err: err}
	}
	return root, nil
}

// makeConfinedDirs creates each level of a workspace inside the tenant's root
// and pins it 0700, refusing a level that is a link.
func makeConfinedDirs(tenant *os.Root, dir string, levels ...string) error {
	if err := tenant.MkdirAll(levels[len(levels)-1], workspaceDirPerm); err != nil {
		return &WorkspaceIOError{Op: "provision", Path: dir, Err: err}
	}
	// MkdirAll applies the umask and leaves existing levels as they were, so
	// each level is pinned — through the handle, never the name.
	for _, level := range levels {
		if err := chmodConfinedDir(tenant, level, workspaceDirPerm); err != nil {
			return err
		}
	}
	return nil
}

// chmodConfinedDir sets a directory's mode through a handle opened inside
// root, and only when that handle IS the directory at path — not a link to
// one.
//
// NOT root.Chmod, AND NOT LSTAT-THEN-CHMOD. Both act by name, and a tenant can
// swap the name for a link between any check and the act (Go documents the
// race for Root.Chmod on Unix). The handle is opened through root, so it can
// only ever be an object inside the tenant's subtree; then the object at the
// name is compared with the handle, and a link — whose Lstat is the link, not
// the directory — is refused. The mode is set on the handle, which no later
// swap of the name can redirect.
func chmodConfinedDir(root *os.Root, path string, mode os.FileMode) error {
	handle, err := root.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return &WorkspaceIOError{Op: "provision", Path: path, Err: err}
	}
	defer handle.Close()
	held, err := handle.Stat()
	if err != nil {
		return &WorkspaceIOError{Op: "provision", Path: path, Err: err}
	}
	named, err := root.Lstat(path)
	if err != nil {
		return &WorkspaceIOError{Op: "provision", Path: path, Err: err}
	}
	if named.Mode()&os.ModeSymlink != 0 || !os.SameFile(held, named) {
		return &WorkspaceLinkError{Path: path}
	}
	if !held.IsDir() {
		return &WorkspaceIOError{Op: "provision", Path: path, Err: errNotADirectory}
	}
	if err := handle.Chmod(mode); err != nil {
		return &WorkspaceIOError{Op: "provision", Path: path, Err: err}
	}
	return nil
}

// requireRealDir refuses a path that is a symbolic link or not a directory.
func requireRealDir(path string, lstat func(string) (os.FileInfo, error)) error {
	info, err := lstat(path)
	switch {
	case err != nil:
		return &WorkspaceIOError{Op: "provision", Path: path, Err: err}
	case info.Mode()&os.ModeSymlink != 0:
		return &WorkspaceLinkError{Path: path}
	case !info.IsDir():
		return &WorkspaceIOError{Op: "provision", Path: path, Err: errNotADirectory}
	}
	return nil
}

// errNotADirectory is the cause a WorkspaceIOError carries for a workspace
// level that exists as something other than a directory.
var errNotADirectory = errors.New("not a directory")

// SweepSessionSpecs removes every spec directory under the projects root —
// <root>/<tenant>/<project>/workspace/quicbaas-workspace-* — and returns
// what it removed, leaving every checkout in place.
//
// A SPEC DIRECTORY NEVER OUTLIVES ITS SESSION, and no session outlives the
// process that spawned it: an agent sweeps its containers at startup, and the
// daemon's in-process sessions die with it. So at startup every spec directory
// is an orphan, holding a forge token in plaintext on a persistent disk for as
// long as nothing removes it. This is what removes it. It must run before the
// host accepts a spawn, or it would take a live session's spec with it.
//
// It walks each tenant through that tenant's own root and descends into
// directories only, never links, so a link a tenant planted cannot aim the
// removal outside its subtree.
func (m *WorkspaceManager) SweepSessionSpecs() ([]string, error) {
	tenants, err := os.ReadDir(m.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, &WorkspaceIOError{Op: "sweep", Path: m.root, Err: err}
	}
	var removed []string
	for _, tenant := range tenants {
		if !tenant.IsDir() || validateWorkspaceSegment("tenant_id", tenant.Name()) != nil {
			continue
		}
		swept, sweepErr := m.sweepTenantSpecs(tenant.Name())
		removed = append(removed, swept...)
		if sweepErr != nil {
			return removed, sweepErr
		}
	}
	return removed, nil
}

// sweepTenantSpecs removes one tenant's spec directories through its root.
func (m *WorkspaceManager) sweepTenantSpecs(tenantID string) ([]string, error) {
	tenantDir := filepath.Join(m.root, tenantID)
	root, err := os.OpenRoot(tenantDir)
	if err != nil {
		return nil, &WorkspaceIOError{Op: "sweep", Path: tenantDir, Err: err}
	}
	defer root.Close()
	projects, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, &WorkspaceIOError{Op: "sweep", Path: tenantDir, Err: err}
	}
	var removed []string
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		parent := filepath.Join(project.Name(), workspaceDirName)
		if requireRealDir(parent, root.Lstat) != nil {
			continue
		}
		entries, readErr := fs.ReadDir(root.FS(), parent)
		if readErr != nil {
			return removed, &WorkspaceIOError{Op: "sweep", Path: filepath.Join(tenantDir, parent), Err: readErr}
		}
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), workspaceSpecDirName) {
				continue
			}
			rel := filepath.Join(parent, entry.Name())
			if rmErr := root.RemoveAll(rel); rmErr != nil {
				return removed, &WorkspaceIOError{Op: "sweep", Path: filepath.Join(tenantDir, rel), Err: rmErr}
			}
			removed = append(removed, filepath.Join(tenantDir, rel))
		}
	}
	return removed, nil
}

// requirePinnedWorkspace refuses a pinned workspace that is not where the pin
// says, is not there at all, or is there only as a link.
//
// EVERY LEVEL, NOT ONLY THE LAST. A link at the project or workspace directory
// that stays inside the tenant's subtree passes the root's confinement and the
// mount check alike, and would hand the session another checkout while the job
// reports the pinned one. So each ancestor must be a real directory too.
func requirePinnedWorkspace(tenant *os.Root, ws *SessionWorkspace, rel, dir string) error {
	if filepath.Clean(ws.Pin.Dir) != dir {
		return &WorkspaceStaleError{
			Key:      ws.Key,
			Expected: ws.Pin.Dir,
			Reason:   "this host provisions the workspace at " + strconv.Quote(dir) + "; its projects root moved",
		}
	}
	for _, ancestor := range []string{ws.ProjectID, filepath.Join(ws.ProjectID, workspaceDirName)} {
		info, err := tenant.Lstat(ancestor)
		switch {
		case errors.Is(err, os.ErrNotExist):
			return &WorkspaceStaleError{Key: ws.Key, Expected: ws.Pin.Dir, Reason: "the checkout no longer exists"}
		case err != nil:
			return &WorkspaceIOError{Op: "provision", Path: dir, Err: err}
		case info.Mode()&os.ModeSymlink != 0:
			return &WorkspaceLinkError{Path: filepath.Join(filepath.Dir(filepath.Dir(dir)), ancestor)}
		case !info.IsDir():
			return &WorkspaceStaleError{Key: ws.Key, Expected: ws.Pin.Dir, Reason: "an ancestor is not a directory"}
		}
	}
	info, err := tenant.Lstat(rel)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return &WorkspaceStaleError{Key: ws.Key, Expected: ws.Pin.Dir, Reason: "the checkout no longer exists"}
	case err != nil:
		return &WorkspaceIOError{Op: "provision", Path: dir, Err: err}
	case info.Mode()&os.ModeSymlink != 0:
		return &WorkspaceLinkError{Path: dir}
	case !info.IsDir():
		return &WorkspaceStaleError{Key: ws.Key, Expected: ws.Pin.Dir, Reason: "the path is not a directory"}
	}
	return nil
}

// =============================================================================
// Typed errors
// =============================================================================

// ErrSessionWorkspace is the sentinel every session-workspace refusal wraps,
// so a runner can ask "was this the workspace?" in one errors.Is.
var ErrSessionWorkspace = errors.New("session workspace: unavailable")

// SessionWorkspaceInvalidError reports a named workspace no host could
// provision.
type SessionWorkspaceInvalidError struct {
	// Field names the offending part of the workspace.
	Field string

	// Reason states what is wrong with it.
	Reason string
}

// Error implements the error interface.
func (e *SessionWorkspaceInvalidError) Error() string {
	return "session workspace: invalid " + e.Field + ": " + e.Reason
}

// Unwrap returns ErrSessionWorkspace.
func (e *SessionWorkspaceInvalidError) Unwrap() error { return ErrSessionWorkspace }

// SessionWorkspaceTenantError reports a spawn naming a workspace of a tenant
// other than the one it runs under.
type SessionWorkspaceTenantError struct {
	// SpawnTenant is the tenant the spawn runs under.
	SpawnTenant string

	// WorkspaceTenant is the tenant the workspace names.
	WorkspaceTenant string
}

// Error implements the error interface.
func (e *SessionWorkspaceTenantError) Error() string {
	return "session workspace: tenant " + strconv.Quote(e.SpawnTenant) +
		" may not provision a workspace of tenant " + strconv.Quote(e.WorkspaceTenant)
}

// Unwrap returns ErrSessionWorkspace.
func (e *SessionWorkspaceTenantError) Unwrap() error { return ErrSessionWorkspace }

// SessionWorkspaceUnsupportedError reports a host asked to provision a
// workspace that holds no projects root to provision it under.
type SessionWorkspaceUnsupportedError struct {
	// Host names the host that refused ("daemon" or the agent's work dir).
	Host string
}

// Error implements the error interface.
func (e *SessionWorkspaceUnsupportedError) Error() string {
	return "session workspace: " + e.Host + " has no projects root to provision a named workspace under"
}

// Unwrap returns ErrSessionWorkspace.
func (e *SessionWorkspaceUnsupportedError) Unwrap() error { return ErrSessionWorkspace }

// WorkspaceAgentUnavailableError reports a pinned spawn whose workspace lives
// on an agent the dispatcher cannot reach: deregistered, offline, or no longer
// serving the backend. The checkout is there and nowhere else, so the spawn is
// refused rather than sent to a host that would clone afresh.
type WorkspaceAgentUnavailableError struct {
	// AgentHost is the agent the workspace is pinned to; empty for the
	// daemon's own in-process backend.
	AgentHost string

	// Key is the pinned workspace.
	Key string

	// Backend is the backend the spawn asked for.
	Backend string

	// ResolvedHost is where the dispatcher would have sent the spawn instead,
	// empty for the daemon's in-process backend or when nothing resolved.
	ResolvedHost string

	// Cause is the failure that showed the agent gone, when there was one —
	// a spawn RPC it never answered. It is reported, not unwrapped: the
	// refusal is the workspace's, and it must not be classified as the
	// transient unreachability it was observed as.
	Cause error
}

// Error implements the error interface.
func (e *WorkspaceAgentUnavailableError) Error() string {
	holder := "the daemon's in-process backend"
	if e.AgentHost != "" {
		holder = "agent " + strconv.Quote(e.AgentHost)
	}
	msg := "session workspace: workspace " + strconv.Quote(e.Key) + " lives on " + holder +
		", which cannot take a " + e.Backend + " spawn"
	if e.ResolvedHost != "" {
		msg += " (the dispatcher resolved " + strconv.Quote(e.ResolvedHost) + ")"
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg + "; the job's checkout is not moved to another host"
}

// Unwrap returns ErrSessionWorkspace.
func (e *WorkspaceAgentUnavailableError) Unwrap() error { return ErrSessionWorkspace }

// SessionWorkspaceProvisionError reports a session workspace the host could not
// provision for a filesystem reason: a read-only or full disk, a permission, a
// spec that could not be written.
type SessionWorkspaceProvisionError struct {
	// Key is the workspace.
	Key string

	// Cause is the filesystem failure.
	Cause error
}

// Error implements the error interface.
func (e *SessionWorkspaceProvisionError) Error() string {
	return "session workspace: " + strconv.Quote(e.Key) + " could not be provisioned: " + e.Cause.Error()
}

// Unwrap returns ErrSessionWorkspace and the cause.
func (e *SessionWorkspaceProvisionError) Unwrap() []error {
	return []error{ErrSessionWorkspace, e.Cause}
}

// WorkspacePinPersistError reports a job whose workspace pin could not be
// stored. The session that produced the placement is closed rather than used:
// a checkout the durable record does not locate is one a recovery could
// abandon for a fresh clone elsewhere. It is a workspace refusal, parked for
// an operator.
type WorkspacePinPersistError struct {
	// JobID is the job.
	JobID string

	// Key is the workspace.
	Key string

	// Cause is the store's failure.
	Cause error
}

// Error implements the error interface.
func (e *WorkspacePinPersistError) Error() string {
	return "session workspace: job " + e.JobID + ": the pin for workspace " + strconv.Quote(e.Key) +
		" could not be stored, so its session is not used: " + e.Cause.Error()
}

// Unwrap returns ErrSessionWorkspace and the cause.
func (e *WorkspacePinPersistError) Unwrap() []error { return []error{ErrSessionWorkspace, e.Cause} }

// WorkspaceLinkError reports a level of a session workspace that is a symbolic
// link. The workspace is made of directories and nothing else: a link there
// was planted through a mount, and following it would let the provisioning
// act somewhere the tenant's grant never reached.
type WorkspaceLinkError struct {
	// Path is the link.
	Path string
}

// Error implements the error interface.
func (e *WorkspaceLinkError) Error() string {
	return "session workspace: " + strconv.Quote(e.Path) + " is a symbolic link, not a directory; it is not followed"
}

// Unwrap returns ErrSessionWorkspace.
func (e *WorkspaceLinkError) Unwrap() error { return ErrSessionWorkspace }

// WorkspaceStaleError reports a pinned workspace the pinned host no longer
// holds where the pin says.
type WorkspaceStaleError struct {
	// Key is the pinned workspace.
	Key string

	// Expected is the directory the pin recorded.
	Expected string

	// Reason states what the host found instead.
	Reason string
}

// Error implements the error interface.
func (e *WorkspaceStaleError) Error() string {
	return "session workspace: pinned workspace " + strconv.Quote(e.Key) + " at " +
		strconv.Quote(e.Expected) + " is stale: " + e.Reason
}

// Unwrap returns ErrSessionWorkspace.
func (e *WorkspaceStaleError) Unwrap() error { return ErrSessionWorkspace }
