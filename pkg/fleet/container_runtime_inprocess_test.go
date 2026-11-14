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

func TestResolveContainerRuntimeIn_LibcontainerWithoutTheRuntimeIsRefused(t *testing.T) {
	SetInProcessContainerRuntime(nil)

	rt, err := ResolveContainerRuntimeIn(ContainerRuntimeInProcess, t.TempDir(), newSignalLogger(t, ""))
	if !errors.Is(err, ErrInProcessRuntimeUnavailable) {
		t.Fatalf("expected ErrInProcessRuntimeUnavailable, got %v", err)
	}
	if rt != nil {
		t.Fatalf("expected no runtime, got %T", rt)
	}
}

func TestResolveContainerRuntimeIn_LibcontainerBuildsUnderTheAgentsRoot(t *testing.T) {
	roots := registerInProcess(t, nil)
	root := t.TempDir()

	rt, err := ResolveContainerRuntimeIn(ContainerRuntimeInProcess, root, newSignalLogger(t, ""))
	if err != nil {
		t.Fatalf("ResolveContainerRuntimeIn(libcontainer): %v", err)
	}
	got, ok := rt.(*inProcessRuntime)
	if !ok {
		t.Fatalf("expected the in-process runtime, got %T", rt)
	}
	if got.root != root || len(*roots) != 1 {
		t.Fatalf("expected one build under %q, got %q (builds %v)", root, got.root, *roots)
	}
}

func TestResolveContainerRuntimeIn_LibcontainerThatCannotStartIsAnError(t *testing.T) {
	errNoStart := errors.New("cannot start here")
	registerInProcess(t, errNoStart)

	if _, err := ResolveContainerRuntimeIn(ContainerRuntimeInProcess, t.TempDir(), newSignalLogger(t, "")); !errors.Is(err, errNoStart) {
		t.Fatalf("expected the start failure, got %v", err)
	}
}

func TestResolveContainerRuntimeIn_LibcontainerNeedsARoot(t *testing.T) {
	roots := registerInProcess(t, nil)

	if _, err := ResolveContainerRuntimeIn(ContainerRuntimeInProcess, "", newSignalLogger(t, "")); !errors.Is(err, ErrInProcessRuntimeNoRoot) {
		t.Fatalf("expected ErrInProcessRuntimeNoRoot, got %v", err)
	}
	if len(*roots) != 0 {
		t.Fatalf("no runtime may be built without a root, built under %v", *roots)
	}
}

// ResolveContainerRuntime has no state directory to give the in-process
// runtime, so even with one registered it never builds one.
func TestResolveContainerRuntime_NeverBuildsTheInProcessRuntime(t *testing.T) {
	roots := registerInProcess(t, nil)

	if _, err := ResolveContainerRuntime(ContainerRuntimeInProcess, newSignalLogger(t, "")); !errors.Is(err, ErrInProcessRuntimeUnavailable) {
		t.Fatalf("expected ErrInProcessRuntimeUnavailable, got %v", err)
	}
	if len(*roots) != 0 {
		t.Fatalf("built an in-process runtime under %v", *roots)
	}
}

func TestResolveContainerRuntime_AutoPrefersTheInProcessRuntime(t *testing.T) {
	registerInProcess(t, nil)
	recorder := &recordingRuntimeFactory{available: []string{"docker"}}

	rt, err := resolveContainerRuntime(ContainerRuntimeAuto, newSignalLogger(t, ""), recorder.factory().withInProcess(t.TempDir()))
	if err != nil {
		t.Fatalf("resolve auto: %v", err)
	}
	if _, ok := rt.(*inProcessRuntime); !ok {
		t.Fatalf("auto must take the in-process runtime, got %T", rt)
	}
	if recorder.detectCalls != 0 {
		t.Fatalf("auto probed for an engine %d times with the in-process runtime available", recorder.detectCalls)
	}
}

func TestResolveContainerRuntime_AutoFallsBackWhenTheInProcessRuntimeCannotStart(t *testing.T) {
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

func TestConfigValidate_AcceptsTheLibcontainerSelector(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.ContainerRuntime = ContainerRuntimeInProcess
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate(libcontainer): %v", err)
	}
}
