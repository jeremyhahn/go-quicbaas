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

package fleet

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// listingContainerRuntime is a container runtime that can enumerate its
// owned containers, which is what makes it reapable.
type listingContainerRuntime struct {
	fakeContainerRuntime

	mu         sync.Mutex
	containers []orchestration.OwnedContainer
	askedOwner string
	removed    []string
}

func (l *listingContainerRuntime) ListOwned(_ context.Context, owner string) ([]orchestration.OwnedContainer, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.askedOwner = owner
	return append([]orchestration.OwnedContainer(nil), l.containers...), nil
}

func (l *listingContainerRuntime) Remove(_ context.Context, id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.removed = append(l.removed, id)
	return nil
}

// holdingBackend is a backend that holds exactly the sessions it names.
type holdingBackend struct {
	mockBackend
	held map[string]bool
}

func (b *holdingBackend) HoldsSession(id string) bool { return b.held[id] }

// TestRuntime_ContainerReaperSparesEverySessionAnyBackendHolds: the reaper
// claims by this daemon's work directory and asks EVERY backend — a session
// the second backend holds must not be reaped because the first does not.
func TestRuntime_ContainerReaperSparesEverySessionAnyBackendHolds(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	old := time.Now().Add(-time.Hour)
	rt := &listingContainerRuntime{containers: []orchestration.OwnedContainer{
		{ID: "held-by-b", CreatedAt: old, SessionID: "ses-b"},
		{ID: "orphan", CreatedAt: old, SessionID: "ses-gone"},
	}}
	rt.available = true
	runtime, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{
			"a": &holdingBackend{held: map[string]bool{"ses-a": true}},
			"b": &holdingBackend{held: map[string]bool{"ses-b": true}},
		},
		ContainerRuntime: rt,
		Logger:           &testLogger{t: t},
		WorkDir:          workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	reaper, err := runtime.ContainerReaper(time.Minute)
	if err != nil || reaper == nil {
		t.Fatalf("ContainerReaper = %v, %v", reaper, err)
	}
	sweep, err := reaper.Reap(context.Background())
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if len(sweep.Removed) != 1 || sweep.Removed[0] != "orphan" {
		t.Fatalf("removed %v, want only the orphan", sweep.Removed)
	}
	if rt.askedOwner != workDir {
		t.Fatalf("listed by owner %q, want the work directory", rt.askedOwner)
	}
}

// TestRuntime_ContainerReaperIsNilWithoutAnEnumerableRuntime: a daemon that
// launches no containers, or cannot list them, has nothing to reap.
func TestRuntime_ContainerReaperIsNilWithoutAnEnumerableRuntime(t *testing.T) {
	t.Parallel()
	plain := &fakeContainerRuntime{available: true}
	runtime, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"mock": &mockBackend{}},
		ContainerRuntime: plain,
		Logger:           &testLogger{t: t},
		WorkDir:          t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if reaper, err := runtime.ContainerReaper(time.Minute); reaper != nil || err != nil {
		t.Fatalf("ContainerReaper = %v, %v; want nil, nil", reaper, err)
	}
	if runtime.HoldsSession("anything") {
		t.Fatal("a backend that cannot hold sessions reported one held")
	}
}

// TestNewRuntime_RefusesAGoCacheTheMountPolicyReaches: a cache a request's
// grant could reach is one another tenant could mount.
func TestNewRuntime_RefusesAGoCacheTheMountPolicyReaches(t *testing.T) {
	t.Parallel()
	projects := filepath.Join(t.TempDir(), "projects")
	_, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"mock": &mockBackend{}},
		ContainerRuntime: &fakeContainerRuntime{available: true},
		Logger:           &testLogger{t: t},
		WorkDir:          t.TempDir(),
		MountPolicy:      &orchestration.MountPolicy{ProjectsRoot: projects},
		GoBuildCache:     orchestration.GoBuildCache{Root: filepath.Join(projects, "cache")},
	})
	if !errors.Is(err, orchestration.ErrGoBuildCache) {
		t.Fatalf("NewRuntime = %v, want ErrGoBuildCache", err)
	}
}

// TestConfig_GoBuildCacheDefaultsUnderTheWorkDir covers the default, the
// override, and the refusal of a cache the allowlist reaches.
func TestConfig_GoBuildCacheDefaultsUnderTheWorkDir(t *testing.T) {
	t.Parallel()
	cfg := &Config{WorkDir: "/var/lib/quicbaas-agent"}
	if got := cfg.GoBuildCache().Root; got != "/var/lib/quicbaas-agent/cache/go" {
		t.Fatalf("default root = %q", got)
	}
	cfg.GoCacheDir = " /srv/go-cache/ "
	if got := cfg.GoBuildCache().Root; got != "/srv/go-cache" {
		t.Fatalf("override root = %q", got)
	}
	if (&Config{}).GoBuildCache().Enabled() {
		t.Fatal("a config with no work dir enabled a cache")
	}

	reached := &Config{WorkDir: "/var/lib/quicbaas-agent", AllowedMountRoots: []string{"/var/lib/quicbaas-agent"}}
	if err := reached.GoBuildCache().Validate(reached.MountPolicy()); !errors.Is(err, orchestration.ErrGoBuildCache) {
		t.Fatalf("Validate = %v, want the allowlist overlap refused", err)
	}
}
