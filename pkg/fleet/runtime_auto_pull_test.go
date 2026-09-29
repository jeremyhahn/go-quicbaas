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
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// promotedImage is a digest-pinned registry reference, the shape the worker
// image refresh rebinds projects to.
const promotedImage = "reg.example:3001/o/software-factory-worker:0.11.0-alpha-cc2.1.280-cx0.156.0@sha256:" +
	"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// pullingContainer is a sandbox engine that knows which images it holds.
type pullingContainer struct {
	*streamJSONContainer
	mu         sync.Mutex
	held       map[string]bool
	inspectErr error
	pullErr    error
	pulled     []string
}

func (p *pullingContainer) InspectImage(_ context.Context, image string) (*orchestration.ImageInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.inspectErr != nil {
		return nil, p.inspectErr
	}
	if !p.held[image] {
		return nil, &orchestration.ImageNotFoundError{Image: image}
	}
	return &orchestration.ImageInfo{ID: "sha256:held"}, nil
}

func (p *pullingContainer) Pull(_ context.Context, image string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pulled = append(p.pulled, image)
	if p.pullErr != nil {
		return &orchestration.ContainerImagePullError{Image: image, Cause: p.pullErr}
	}
	return nil
}

func newAutoPullRuntime(t *testing.T, engine *pullingContainer, autoPull bool) (*Runtime, string) {
	t.Helper()
	root := t.TempDir()
	workDir := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		t.Fatal(err)
	}
	backend := orchestration.NewACPBackend(orchestration.ACPBackendConfig{
		Command: "claude", Args: []string{"--print"}, DefaultModel: "claude-opus-5-5", CLIMode: true,
	})
	runtime, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"claude": backend},
		SandboxPolicy:    orchestration.SandboxPolicyRequired,
		ContainerRuntime: engine,
		AutoPull:         autoPull,
		Logger:           &testLogger{t: t},
		MountPolicy:      &orchestration.MountPolicy{AllowedRoots: []string{root}},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return runtime, workDir
}

func spawnNamedImage(t *testing.T, runtime *Runtime, workDir string) (*orchestration.Session, error) {
	t.Helper()
	sess, err := runtime.Spawn(t.Context(), "tenant-1", nil, "claude", &orchestration.SpawnRequest{
		ProjectID: "proj-1", WorkDir: workDir, Persistent: true, ContainerImage: promotedImage,
	})
	if err == nil {
		t.Cleanup(func() {
			if closeErr := runtime.Close(context.Background(), "claude", sess.ID); closeErr != nil {
				t.Logf("close: %v", closeErr)
			}
		})
	}
	return sess, err
}

func TestRuntime_AutoPullFetchesANamedImageThisHostLacks(t *testing.T) {
	engine := &pullingContainer{streamJSONContainer: &streamJSONContainer{containerID: "c1"}}
	runtime, workDir := newAutoPullRuntime(t, engine, true)

	if _, err := spawnNamedImage(t, runtime, workDir); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if len(engine.pulled) != 1 || engine.pulled[0] != promotedImage {
		t.Fatalf("pulled = %v, want the named image pulled once", engine.pulled)
	}
	if specs := engine.launchedSpecs(); len(specs) != 1 || specs[0].Image != promotedImage {
		t.Fatalf("launched = %+v, want the named image launched after the pull", specs)
	}
}

func TestRuntime_AutoPullLeavesAHeldImageAlone(t *testing.T) {
	engine := &pullingContainer{streamJSONContainer: &streamJSONContainer{containerID: "c1"},
		held: map[string]bool{promotedImage: true}}
	runtime, workDir := newAutoPullRuntime(t, engine, true)

	if _, err := spawnNamedImage(t, runtime, workDir); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if len(engine.pulled) != 0 {
		t.Fatalf("pulled %v, but the host already held the image", engine.pulled)
	}
}

func TestRuntime_WithoutAutoPullNothingIsPulled(t *testing.T) {
	engine := &pullingContainer{streamJSONContainer: &streamJSONContainer{containerID: "c1"}}
	runtime, workDir := newAutoPullRuntime(t, engine, false)

	if _, err := spawnNamedImage(t, runtime, workDir); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if len(engine.pulled) != 0 {
		t.Fatalf("pulled %v with --auto-pull off", engine.pulled)
	}
}

func TestRuntime_AutoPullFailureRefusesTheSpawn(t *testing.T) {
	cases := map[string]struct {
		engine *pullingContainer
		want   error
	}{
		"pull refused": {
			engine: &pullingContainer{streamJSONContainer: &streamJSONContainer{containerID: "c1"}, pullErr: errors.New("unauthorized")},
			want:   orchestration.ErrContainerImagePull,
		},
		"engine cannot inspect": {
			engine: &pullingContainer{streamJSONContainer: &streamJSONContainer{containerID: "c1"},
				inspectErr: &orchestration.ImageInspectError{Image: promotedImage}},
			want: orchestration.ErrImageInspect,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			runtime, workDir := newAutoPullRuntime(t, tc.engine, true)
			if _, err := spawnNamedImage(t, runtime, workDir); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if specs := tc.engine.launchedSpecs(); len(specs) != 0 {
				t.Fatalf("a container was launched from an image that could not be ensured: %+v", specs)
			}
		})
	}
}
