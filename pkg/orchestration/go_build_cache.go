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
	"os"
	"path/filepath"
)

// =============================================================================
// The shared Go build cache
// =============================================================================
//
// A worker container that runs `go build` or `go test` fills GOMODCACHE and
// GOCACHE inside its own writable layer. Measured on the burn-in node on
// 2026-09-24, that was 9-12 GB of every 10-15 GB container: 2 GB of modules
// and 7-10 GB of build cache, rebuilt from nothing by every container. Go's
// module and build caches are safe for concurrent use by several processes
// (both lock with file locks), so one host-side copy serves every container.
//
// THE CACHE IS KEYED BY TENANT. A worker has full control of its container,
// and both caches are TRUSTED by the toolchain once written: an extracted
// module directory is not re-verified against go.sum on each build, and a
// build-cache entry is reused by its action ID alone. A cache shared across
// tenants would let one tenant's worker plant code that another tenant's build
// then compiles and runs. Within one tenant the workers already share the
// tenant's forge identity and credentials, so sharing the cache adds nothing a
// worker could not already do. So each tenant gets <root>/<tenant>/{mod,build}
// and never sees another's.
//
// THE MOUNT IS THE DAEMON'S, NOT THE REQUEST'S. Like the credential directory,
// the cache is mounted by the backend itself after the request's own mounts
// were authorized, and the mount policy grants no request any path under the
// cache root: Validate refuses a root that a projects-root or allowlist grant
// could reach, so a spawn can never name another tenant's cache as an extra
// mount.

// Container paths the shared caches are mounted at, and the directory names
// under a tenant's cache directory on the host.
const (
	// ContainerGoModCache is GOMODCACHE inside a sandbox container.
	ContainerGoModCache = "/var/cache/quicbaas/go/mod"

	// ContainerGoBuildCache is GOCACHE inside a sandbox container.
	ContainerGoBuildCache = "/var/cache/quicbaas/go/build"

	goModCacheDirName   = "mod"
	goBuildCacheDirName = "build"
)

// GoBuildCache is a per-host, per-tenant Go module and build cache shared by
// every sandbox container a daemon launches. The zero value is disabled: each
// container keeps its own caches in its writable layer.
type GoBuildCache struct {
	// Root is the host directory holding one cache directory per tenant.
	Root string
}

// Enabled reports whether a cache root is configured.
func (c GoBuildCache) Enabled() bool {
	return c.Root != ""
}

// Validate refuses a root that is relative, or that any grant of policy could
// admit as a request's bind mount. A nil policy grants nothing, so any
// absolute root passes against it.
func (c GoBuildCache) Validate(policy *MountPolicy) error {
	if !c.Enabled() {
		return nil
	}
	if !filepath.IsAbs(c.Root) {
		return &GoBuildCacheError{Root: c.Root, Reason: "must be an absolute path"}
	}
	if grant := policy.grantReaching(filepath.Clean(c.Root)); grant != "" {
		return &GoBuildCacheError{
			Root: c.Root,
			Reason: "overlaps the mount policy's " + grant +
				"; a spawn could then name another tenant's cache as a bind mount",
		}
	}
	return nil
}

// Apply mounts tenantID's caches into spec and points the Go toolchain at them
// through env, creating the host directories on first use. env is the spawn's
// environment map, which spec.Env shares.
func (c GoBuildCache) Apply(spec *ContainerSpec, env map[string]string, tenantID string) error {
	if !c.Enabled() {
		return nil
	}
	if err := validateWorkspaceSegment("tenant_id", tenantID); err != nil {
		return &GoBuildCacheError{Root: c.Root, Reason: "tenant cannot key a cache directory", Cause: err}
	}
	tenantDir := filepath.Join(c.Root, tenantID)
	for _, cache := range []struct {
		dir, target, envKey string
	}{
		{goModCacheDirName, ContainerGoModCache, "GOMODCACHE"},
		{goBuildCacheDirName, ContainerGoBuildCache, "GOCACHE"},
	} {
		source := filepath.Join(tenantDir, cache.dir)
		// 0700: the container runs as the daemon's uid, which owns these,
		// and no other local user has any business in a tenant's cache.
		if err := os.MkdirAll(source, 0o700); err != nil {
			return &GoBuildCacheError{Root: c.Root, Reason: "cannot create " + source, Cause: err}
		}
		spec.Mounts = append(spec.Mounts, ContainerMount{
			Type:   MountTypeBind,
			Source: source,
			Target: cache.target,
		})
		env[cache.envKey] = cache.target
	}
	return nil
}
