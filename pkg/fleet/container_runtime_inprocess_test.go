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
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// inProcessRuntime is the runtime a registered in-process constructor builds
// in these tests.
type inProcessRuntime struct {
	fakeContainerRuntime
	root string
}

// registerInProcess registers a constructor for the life of the test that
// records the root it was asked for and fails with buildErr when it is set.
// The registration is process-wide, so these tests do not run in parallel.
func registerInProcess(t *testing.T, buildErr error) *[]string {
	t.Helper()
	var roots []string
	SetInProcessContainerRuntime(func(root string, _ interfaces.Logger) (orchestration.ContainerRuntime, error) {
		roots = append(roots, root)
		if buildErr != nil {
			return nil, buildErr
		}
		return &inProcessRuntime{fakeContainerRuntime: fakeContainerRuntime{available: true}, root: root}, nil
	})
	t.Cleanup(func() { SetInProcessContainerRuntime(nil) })
	return &roots
}

func TestResolveContainerRuntime_InProcessWithoutTheRuntimeIsRefused(t *testing.T) {
	SetInProcessContainerRuntime(nil)

	rt, err := resolveContainerRuntime(containerRuntimeInProcess, newSignalLogger(t, ""), execContainerRuntimeFactory.withInProcess(t.TempDir()))
	if !errors.Is(err, ErrInProcessRuntimeUnavailable) {
		t.Fatalf("expected ErrInProcessRuntimeUnavailable, got %v", err)
	}
	if rt != nil {
		t.Fatalf("expected no runtime, got %T", rt)
	}
}

func TestResolveContainerRuntime_InProcessBuildsUnderTheAgentsRoot(t *testing.T) {
	roots := registerInProcess(t, nil)
	root := t.TempDir()

	rt, err := resolveContainerRuntime(containerRuntimeInProcess, newSignalLogger(t, ""), execContainerRuntimeFactory.withInProcess(root))
	if err != nil {
		t.Fatalf("resolveContainerRuntime(libcontainer): %v", err)
	}
	got, ok := rt.(*inProcessRuntime)
	if !ok {
		t.Fatalf("expected the in-process runtime, got %T", rt)
	}
	if got.root != root || len(*roots) != 1 {
		t.Fatalf("expected one build under %q, got %q (builds %v)", root, got.root, *roots)
	}
}

func TestResolveContainerRuntime_InProcessThatCannotStartIsAnError(t *testing.T) {
	errNoStart := errors.New("cannot start here")
	registerInProcess(t, errNoStart)

	if _, err := resolveContainerRuntime(containerRuntimeInProcess, newSignalLogger(t, ""), execContainerRuntimeFactory.withInProcess(t.TempDir())); !errors.Is(err, errNoStart) {
		t.Fatalf("expected the start failure, got %v", err)
	}
}

func TestResolveContainerRuntime_InProcessNeedsARoot(t *testing.T) {
	roots := registerInProcess(t, nil)

	if _, err := resolveContainerRuntime(containerRuntimeInProcess, newSignalLogger(t, ""), execContainerRuntimeFactory.withInProcess("")); !errors.Is(err, ErrInProcessRuntimeNoRoot) {
		t.Fatalf("expected ErrInProcessRuntimeNoRoot, got %v", err)
	}
	if len(*roots) != 0 {
		t.Fatalf("no runtime may be built without a root, built under %v", *roots)
	}
}

// No exported resolver takes the in-process runtime's key, even with the
// runtime registered: they refuse it as Config.Validate does.
func TestResolveContainerRuntime_ExportedCallsRefuseTheLibcontainerSelector(t *testing.T) {
	roots := registerInProcess(t, nil)
	assertRefused := func(name string, rt orchestration.ContainerRuntime, err error) {
		t.Helper()
		if rt != nil {
			t.Fatalf("%s: expected no runtime, got %T", name, rt)
		}
		var detail *SentinelDetailError
		if !errors.Is(err, ErrConfigUnknownRuntime) || !errors.As(err, &detail) || detail.Detail != `"libcontainer"` {
			t.Fatalf("%s: expected ErrConfigUnknownRuntime for \"libcontainer\", got %v", name, err)
		}
	}

	rt, err := ResolveContainerRuntimeIn(containerRuntimeInProcess, t.TempDir(), newSignalLogger(t, ""))
	assertRefused("ResolveContainerRuntimeIn", rt, err)
	rt, err = ResolveContainerRuntime(containerRuntimeInProcess, newSignalLogger(t, ""))
	assertRefused("ResolveContainerRuntime", rt, err)
	if len(*roots) != 0 {
		t.Fatalf("built an in-process runtime under %v", *roots)
	}
}

// The in-process runtime is opt-in by name: it has no container networking
// and no seccomp or AppArmor confinement yet, so "auto" keeps choosing an
// engine that has them even when this binary carries the in-process runtime.
func TestResolveContainerRuntime_AutoNeverTakesTheInProcessRuntime(t *testing.T) {
	registerInProcess(t, nil)
	recorder := &recordingRuntimeFactory{available: []string{"docker"}}

	rt, err := resolveContainerRuntime(ContainerRuntimeAuto, newSignalLogger(t, ""), recorder.factory().withInProcess(t.TempDir()))
	if err != nil {
		t.Fatalf("resolve auto: %v", err)
	}
	if _, ok := rt.(*inProcessRuntime); ok {
		t.Fatal("auto took the in-process runtime; it must be named to be used")
	}
	named, ok := rt.(*namedContainerRuntime)
	if !ok || named.command != "docker" {
		t.Fatalf("auto must take the detected engine, got %T %+v", rt, rt)
	}
}

func TestResolveContainerRuntime_AutoWithTheInProcessRuntimeUnstartable(t *testing.T) {
	registerInProcess(t, errors.New("not root"))
	recorder := &recordingRuntimeFactory{available: []string{"podman"}}

	rt, err := resolveContainerRuntime(ContainerRuntimeAuto, newSignalLogger(t, ""), recorder.factory().withInProcess(t.TempDir()))
	if err != nil {
		t.Fatalf("resolve auto: %v", err)
	}
	named, ok := rt.(*namedContainerRuntime)
	if !ok || named.command != "podman" {
		t.Fatalf("auto must fall back to the detected engine, got %T %+v", rt, rt)
	}
}

func TestConfigValidate_RejectsTheLibcontainerSelector(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.ContainerRuntime = ContainerRuntimeSelector("libcontainer")
	if err := cfg.Validate(); !errors.Is(err, ErrConfigUnknownRuntime) {
		t.Fatalf("Validate(libcontainer) = %v, want ErrConfigUnknownRuntime: no --container-runtime value selects the in-process runtime", err)
	}
}
