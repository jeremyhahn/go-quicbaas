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
)

// Request fields a bind-mount source can arrive on. They name the offending
// field in a MountNotAllowedError so an operator reading the denial knows
// which part of the spawn request to fix.
const (
	// MountFieldWorkDir is the resolved work directory, which the sandbox
	// spawn paths bind-mount into the container.
	MountFieldWorkDir = "work_dir"

	// MountFieldExtraMounts is SpawnSessionRequest.ExtraMounts.
	MountFieldExtraMounts = "extra_mounts"

	// MountFieldContainerConfig is SpawnSessionRequest.ContainerConfig.Mounts,
	// the caller-supplied full container spec.
	MountFieldContainerConfig = "container_config.mounts"
)

// MountNotAllowedError reports a host bind-mount source that is outside every
// permitted root, or that cannot be proven to stay inside one. Bind mounts are
// deny-by-default: only sources under the requesting tenant's subtree of the
// managed projects root or under an operator-configured allowlist root are
// permitted.
type MountNotAllowedError struct {
	// Field names the spawn-request field the source arrived on. One of the
	// MountField* constants.
	Field string

	// TenantID is the tenant the mount was authorized for. Empty when the
	// denial did not depend on the tenant.
	TenantID string

	// Source is the rejected host path.
	Source string

	// Reason states why the mount was rejected.
	Reason string
}

// Error implements the error interface.
func (e *MountNotAllowedError) Error() string {
	msg := "mount not allowed"
	if e.Field != "" {
		msg += " (" + e.Field + ")"
	}
	if e.TenantID != "" {
		msg += " for tenant " + strconv.Quote(e.TenantID)
	}
	return msg + ": " + strconv.Quote(e.Source) + ": " + e.Reason
}

// MountPolicy decides which host paths a spawn request may bind-mount into an
// agent sandbox. A source is permitted only when it lies under the requesting
// tenant's own subtree of the managed projects root
// (<projects_root>/<tenant_id>) or under one of the operator-configured
// allowlist roots (orchestration.sandbox.allowed_mount_roots). Everything else
// — including every mount when no policy is configured at all — is denied.
//
// Two properties matter as much as the roots themselves:
//
//   - Authorization is TENANT-SCOPED. The projects root is shared by every
//     tenant (<root>/<tenant_id>/<project_id>/workspace), so granting the
//     whole root would let one tenant name another tenant's workspace as a
//     mount source. Directory permissions do not help: the sandbox container
//     runs as the daemon's own uid, which owns every tenant's workspace.
//
//   - Authorization is resolved, not lexical. A purely textual prefix test
//     passes for <workspace>/escape even when that entry is a symlink to /,
//     and an agent with write access to its own workspace can create exactly
//     that. Every source is therefore resolved with filepath.EvalSymlinks and
//     re-tested, and a source that does not exist is refused.
type MountPolicy struct {
	// ProjectsRoot is the managed-workspace root
	// (orchestration.projects_root, resolved). Empty disables the
	// projects-root grant.
	ProjectsRoot string

	// AllowedRoots is the operator allowlist of absolute host directories.
	// Unlike the projects root these are not tenant-scoped: the operator
	// named them explicitly and they are shared deployment-wide.
	AllowedRoots []string

	// SocketsRoot is the daemon's own per-session agent-socket root
	// (orchestration.sockets_root, resolved). It is admitted BY
	// CONSTRUCTION rather than by an operator allowlist entry: the daemon
	// binds the signing agent's socket there because a unix socket address
	// does not fit under a managed workspace, so a deployment that had to
	// remember to allowlist it would be a deployment whose signing breaks on
	// the first delivery.
	//
	// The grant is the narrowest one that admits that socket and nothing
	// else — see socketsRootPermits. Empty disables it.
	SocketsRoot string
}

// Authorize validates every requested mount for tenantID and returns the first
// violation as a MountNotAllowedError. A nil policy denies every mount, which
// is what an unwired orchestrator must do.
func (p *MountPolicy) Authorize(tenantID, field string, mounts []ContainerMount) error {
	for i := range mounts {
		if err := p.authorizeMount(tenantID, field, mounts[i]); err != nil {
			return err
		}
	}
	return nil
}

// AuthorizeSource validates one host bind-mount source for tenantID. It is the
// single decision point for every path this package hands to a container
// runtime as a bind-mount source.
func (p *MountPolicy) AuthorizeSource(tenantID, field, source string) error {
	return p.authorizeMount(tenantID, field, ContainerMount{Source: source})
}

// authorizeMount is the decision itself. It takes the whole mount rather than
// just its source because one grant — the daemon's sockets root — is
// conditioned on the mount being read-only.
func (p *MountPolicy) authorizeMount(tenantID, field string, mount ContainerMount) error {
	source := mount.Source
	if p == nil {
		return &MountNotAllowedError{
			Field:  field,
			Source: source,
			Reason: "no mount policy is configured",
		}
	}
	if source == "" {
		return &MountNotAllowedError{
			Field:  field,
			Source: source,
			Reason: "mount source is required",
		}
	}
	if !filepath.IsAbs(source) {
		return &MountNotAllowedError{
			Field:  field,
			Source: source,
			Reason: "mount source must be an absolute path",
		}
	}

	cleaned := filepath.Clean(source)
	if !p.sourcePermitted(tenantID, cleaned, mount.ReadOnly) {
		return &MountNotAllowedError{
			Field:    field,
			TenantID: tenantID,
			Source:   source,
			Reason:   "mount source is outside the tenant's projects root and every allowed mount root",
		}
	}

	// A lexically-permitted source can still resolve outside every root
	// through a symlink the agent itself planted inside its workspace.
	// Refusing an unresolvable source is deliberate: a mount source that does
	// not exist yet is created by the runtime as a root-owned directory, and
	// its identity cannot be checked at all.
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return &MountNotAllowedError{
			Field:    field,
			TenantID: tenantID,
			Source:   source,
			Reason:   "mount source cannot be resolved: " + err.Error(),
		}
	}
	if resolved != cleaned && !p.sourcePermitted(tenantID, resolved, mount.ReadOnly) {
		return &MountNotAllowedError{
			Field:    field,
			TenantID: tenantID,
			Source:   source,
			Reason: "mount source resolves to " + strconv.Quote(resolved) +
				", outside the tenant's projects root and every allowed mount root",
		}
	}
	return nil
}

// sourcePermitted reports whether the cleaned absolute source lies under any
// root this tenant may mount from. readOnly is the mount's own flag; only the
// sockets-root grant depends on it.
func (p *MountPolicy) sourcePermitted(tenantID, source string, readOnly bool) bool {
	if p.tenantRootPermits(tenantID, source) {
		return true
	}
	if p.socketsRootPermits(source, readOnly) {
		return true
	}
	for _, root := range p.AllowedRoots {
		if root == "" || !filepath.IsAbs(root) {
			continue
		}
		if rootPermits(filepath.Clean(root), source) {
			return true
		}
	}
	return false
}

// socketsRootPermits reports whether source is a signing-agent socket the
// daemon bound under SocketsRoot, and may therefore be mounted.
//
// THE GRANT IS THE SOCKET, NOT THE ROOT. A root-wide grant would be a
// deployment-wide one — SocketsRoot is shared by every tenant, exactly as the
// projects root is — so what is admitted is only:
//
//   - the socket file itself, one directory below the root
//     (<sockets_root>/<session>/agent.sock) and nothing above or beside it,
//     which is also what stops a caller mounting the root and reading the
//     session names inside it;
//   - that is actually a unix socket on disk, so a regular file planted at
//     that shape is not a mount source;
//   - mounted READ-ONLY, because that is all a container signing against the
//     agent ever needs.
//
// What remains is a caller naming another live session's socket, which means
// guessing that session's random directory name with one attempt per spawn.
func (p *MountPolicy) socketsRootPermits(source string, readOnly bool) bool {
	if !readOnly || p.SocketsRoot == "" || !filepath.IsAbs(p.SocketsRoot) {
		return false
	}
	if filepath.Base(source) != WorkspaceSigningAgentSocketFile {
		return false
	}
	sessionDir := filepath.Dir(source)
	root := filepath.Clean(p.SocketsRoot)
	if filepath.Dir(sessionDir) != root && !resolvedSocketsRootParent(root, sessionDir) {
		return false
	}
	info, err := os.Lstat(source)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSocket != 0
}

// resolvedSocketsRootParent reports whether sessionDir sits directly under
// the sockets root as that root resolves on disk. A deployment whose runtime
// or data directory is reached through a symlinked parent would otherwise
// have its own socket denied — the same reason rootPermits resolves its root.
func resolvedSocketsRootParent(root, sessionDir string) bool {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil || resolvedRoot == root {
		return false
	}
	return filepath.Dir(sessionDir) == resolvedRoot
}

// tenantRootPermits reports whether source lies under the requesting tenant's
// own subtree of the managed projects root. An empty, relative, or otherwise
// unusable tenant identifier grants nothing, so a caller with no tenant can
// never reach the projects root.
func (p *MountPolicy) tenantRootPermits(tenantID, source string) bool {
	if p.ProjectsRoot == "" || !filepath.IsAbs(p.ProjectsRoot) {
		return false
	}
	if err := validateWorkspaceSegment("tenant_id", tenantID); err != nil {
		return false
	}
	tenantRoot := filepath.Join(filepath.Clean(p.ProjectsRoot), tenantID)
	return rootPermits(tenantRoot, source)
}

// rootPermits reports whether source lies under root, comparing root both as
// the operator wrote it and as it resolves on disk. Resolving the root matters
// because AuthorizeSource compares a symlink-resolved source: a deployment
// whose data directory is reached through a symlinked parent would otherwise
// have every one of its own workspaces denied.
func rootPermits(root, source string) bool {
	if pathWithinRoot(root, source) {
		return true
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil || resolvedRoot == root {
		return false
	}
	return pathWithinRoot(resolvedRoot, source)
}

// grantReaching names the first grant of this policy that could admit a path
// at or under root as a bind-mount source, or returns "" when none can. A root
// that contains a grant's directory is reached too, because the grant admits
// that part of it. The sockets-root grant admits only live signing sockets and
// never reaches a directory tree. A nil policy grants nothing.
func (p *MountPolicy) grantReaching(root string) string {
	if p == nil {
		return ""
	}
	overlaps := func(grant string) bool {
		if grant == "" || !filepath.IsAbs(grant) {
			return false
		}
		grant = filepath.Clean(grant)
		return pathWithinRoot(grant, root) || pathWithinRoot(root, grant)
	}
	if overlaps(p.ProjectsRoot) {
		return "projects root " + strconv.Quote(p.ProjectsRoot)
	}
	for _, allowed := range p.AllowedRoots {
		if overlaps(allowed) {
			return "allowed mount root " + strconv.Quote(allowed)
		}
	}
	return ""
}
