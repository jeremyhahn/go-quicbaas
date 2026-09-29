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
	"testing"
)

// TestGoBuildCache_ApplyMountsTheTenantsOwnCaches: each tenant gets its own
// module and build cache, created on first use, and the toolchain is pointed
// at them.
func TestGoBuildCache_ApplyMountsTheTenantsOwnCaches(t *testing.T) {
	root := t.TempDir()
	cache := GoBuildCache{Root: root}
	spec := &ContainerSpec{}
	env := map[string]string{}

	if err := cache.Apply(spec, env, "acme"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if env["GOMODCACHE"] != ContainerGoModCache || env["GOCACHE"] != ContainerGoBuildCache {
		t.Fatalf("env = %v, want both caches pointed at their mounts", env)
	}
	want := map[string]string{
		filepath.Join(root, "acme", "mod"):   ContainerGoModCache,
		filepath.Join(root, "acme", "build"): ContainerGoBuildCache,
	}
	if len(spec.Mounts) != len(want) {
		t.Fatalf("mounts = %+v, want exactly the two caches", spec.Mounts)
	}
	for _, m := range spec.Mounts {
		if want[m.Source] != m.Target || m.Type != MountTypeBind || m.ReadOnly {
			t.Errorf("unexpected mount %+v", m)
		}
		info, err := os.Stat(m.Source)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Errorf("cache dir %s: %v %v", m.Source, info, err)
		}
		if err := m.Validate(); err != nil {
			t.Errorf("mount %+v does not validate: %v", m, err)
		}
	}
}

// TestGoBuildCache_ApplyRefusesATenantThatCannotKeyADirectory: a tenant ID
// that is a path would let one tenant's containers mount another's cache.
func TestGoBuildCache_ApplyRefusesATenantThatCannotKeyADirectory(t *testing.T) {
	cache := GoBuildCache{Root: t.TempDir()}
	for _, tenant := range []string{"", "..", "a/b"} {
		spec := &ContainerSpec{}
		if err := cache.Apply(spec, map[string]string{}, tenant); !errors.Is(err, ErrGoBuildCache) {
			t.Fatalf("Apply(%q) = %v, want ErrGoBuildCache", tenant, err)
		}
		if len(spec.Mounts) != 0 {
			t.Fatalf("a refused Apply still mounted %+v", spec.Mounts)
		}
	}
}

// TestGoBuildCache_DisabledIsANoOp: the zero value leaves the spec alone.
func TestGoBuildCache_DisabledIsANoOp(t *testing.T) {
	spec := &ContainerSpec{}
	env := map[string]string{}
	if err := (GoBuildCache{}).Apply(spec, env, "acme"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(spec.Mounts) != 0 || len(env) != 0 {
		t.Fatalf("a disabled cache changed the spawn: %+v %v", spec.Mounts, env)
	}
	if err := (GoBuildCache{}).Validate(nil); err != nil {
		t.Fatalf("Validate(disabled) = %v", err)
	}
}

// TestGoBuildCache_ValidateRefusesARootAPolicyGrantReaches is the mount-policy
// half: no request may be able to name a cache path as its own bind mount.
func TestGoBuildCache_ValidateRefusesARootAPolicyGrantReaches(t *testing.T) {
	policy := &MountPolicy{
		ProjectsRoot: "/srv/quicbaas/projects",
		AllowedRoots: []string{"/srv/shared"},
		SocketsRoot:  "/run/quicbaas",
	}
	for _, root := range []string{
		"relative/cache",
		"/srv/quicbaas/projects/cache",
		"/srv/quicbaas",
		"/srv/shared/go",
	} {
		if err := (GoBuildCache{Root: root}).Validate(policy); !errors.Is(err, ErrGoBuildCache) {
			t.Errorf("Validate(%q) = %v, want ErrGoBuildCache", root, err)
		}
	}
	if err := (GoBuildCache{Root: "/srv/quicbaas/cache/go"}).Validate(policy); err != nil {
		t.Fatalf("Validate(disjoint root) = %v", err)
	}
}

// TestGoBuildCache_PolicyAdmitsNoRequestMountOfTheCache proves the property
// Validate exists for: with the cache beside the projects root, a spawn that
// names a cache directory as an extra mount is denied for every tenant.
func TestGoBuildCache_PolicyAdmitsNoRequestMountOfTheCache(t *testing.T) {
	base := t.TempDir()
	projects := filepath.Join(base, "projects")
	cache := GoBuildCache{Root: filepath.Join(base, "cache", "go")}
	policy := &MountPolicy{ProjectsRoot: projects}
	if err := cache.Validate(policy); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	spec := &ContainerSpec{}
	if err := cache.Apply(spec, map[string]string{}, "acme"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, tenant := range []string{"acme", "other"} {
		err := policy.Authorize(tenant, MountFieldExtraMounts, spec.Mounts)
		var denied *MountNotAllowedError
		if !errors.As(err, &denied) {
			t.Fatalf("tenant %s could mount the cache as an extra mount: %v", tenant, err)
		}
	}
}

// TestSandboxSpec_MountsTheSharedCacheForTheContainersTenant: the backend
// applies the cache keyed by the tenant the container runs for.
func TestSandboxSpec_MountsTheSharedCacheForTheContainersTenant(t *testing.T) {
	root := t.TempDir()
	backend := NewACPBackend(ACPBackendConfig{Command: "claude", CLIMode: true})
	backend.SetGoBuildCache(GoBuildCache{Root: root})

	spec, err := backend.sandboxSpec(&SpawnRequest{WorkDir: WorkDirCurrent}, testContainerIdentity, "worker:1", []string{"claude"})
	if err != nil {
		t.Fatalf("sandboxSpec: %v", err)
	}
	mounted := map[string]bool{}
	for _, m := range spec.Mounts {
		mounted[m.Source] = true
	}
	if !mounted[filepath.Join(root, testContainerIdentity.TenantID, "mod")] ||
		!mounted[filepath.Join(root, testContainerIdentity.TenantID, "build")] {
		t.Fatalf("mounts = %+v, want the tenant's two caches", spec.Mounts)
	}
	if spec.Env["GOCACHE"] != ContainerGoBuildCache {
		t.Fatalf("GOCACHE = %q", spec.Env["GOCACHE"])
	}

	bad := containerIdentity{SessionID: "ses-x", TenantID: "../escape"}
	if _, err := backend.sandboxSpec(&SpawnRequest{WorkDir: WorkDirCurrent}, bad, "worker:1", []string{"claude"}); !errors.Is(err, ErrGoBuildCache) {
		t.Fatalf("sandboxSpec(bad tenant) = %v, want ErrGoBuildCache", err)
	}
}
