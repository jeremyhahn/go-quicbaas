// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-trusted-platform.
//
// go-trusted-platform is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

//go:build linux && cgo && containers_image_openpgp && exclude_graphdriver_btrfs

package containerruntime

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// newStorageBackends opens the production backends over an empty vfs store in
// a temporary directory.
func newStorageBackends(t *testing.T) *backends {
	t.Helper()
	dir := t.TempDir()
	b, err := newDefaultBackends(&RuntimeConfig{
		Root:                filepath.Join(dir, "root"),
		RunRoot:             filepath.Join(dir, "run"),
		TmpDir:              filepath.Join(dir, "tmp"),
		GraphDriverName:     "vfs",
		SignaturePolicyPath: filepath.Join(dir, "missing-policy.json"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, b.close()) })
	return b
}

func TestNewDefaultBackends_UnknownGraphDriver(t *testing.T) {
	dir := t.TempDir()
	_, err := newDefaultBackends(&RuntimeConfig{
		Root:            filepath.Join(dir, "root"),
		RunRoot:         filepath.Join(dir, "run"),
		GraphDriverName: "no-such-driver",
	})
	require.Error(t, err)
}

func TestStorageImageStore_InspectOfAMissingImage(t *testing.T) {
	b := newStorageBackends(t)

	_, err := b.images.Inspect(context.Background(), "alpine:latest")
	require.ErrorIs(t, err, ErrImageNotFound)
}

func TestStorageImageStore_InspectOfAnInvalidReference(t *testing.T) {
	b := newStorageBackends(t)

	_, err := b.images.Inspect(context.Background(), "Not/A/Valid:Reference:")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrImageNotFound)
}

func TestStorageImageStore_RemoveOfAMissingImage(t *testing.T) {
	b := newStorageBackends(t)

	require.NoError(t, b.images.Remove(context.Background(), "alpine:latest", true))
	err := b.images.Remove(context.Background(), "alpine:latest", false)
	require.ErrorIs(t, err, ErrImageNotFound)
}

func TestStorageImageStore_Tag(t *testing.T) {
	ctx := context.Background()
	b := newStorageBackends(t)
	images, ok := b.images.(*storageImageStore)
	require.True(t, ok)
	stored, err := images.store.CreateImage("", []string{"docker.io/library/alpine:latest"}, "", "", nil)
	require.NoError(t, err)

	require.NoError(t, images.Tag(ctx, "alpine:latest", "reg.example/o/w:1"))
	require.NoError(t, images.Tag(ctx, "sha256:"+stored.ID, "reg.example/o/w:2"))
	got, err := images.store.Image(stored.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{
		"docker.io/library/alpine:latest", "reg.example/o/w:1", "reg.example/o/w:2",
	}, got.Names)

	require.ErrorIs(t, images.Tag(ctx, "missing:latest", "reg.example/o/w:3"), ErrImageNotFound)
	require.ErrorIs(t, images.Tag(ctx, "sha256:"+strings.Repeat("e", 64), "reg.example/o/w:3"), ErrImageNotFound)
	require.Error(t, images.Tag(ctx, "alpine:latest", "Not A Reference"))
}

// The image goes out of local storage to a registry in-process, with no
// docker or podman binary, and the digest returned is the one the registry
// serves for the pushed name.
func TestStorageImageStore_PushRoundTrip(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(registry.New())
	t.Cleanup(server.Close)
	host := strings.TrimPrefix(server.URL, "http://")

	// No layers: unpacking one chowns its files, which needs root.
	seed, err := random.Image(256, 0)
	require.NoError(t, err)
	seedRef, err := name.ParseReference(host + "/seed/app:1")
	require.NoError(t, err)
	require.NoError(t, remote.Write(seedRef, seed))

	dir := t.TempDir()
	policy := filepath.Join(dir, "policy.json")
	require.NoError(t, os.WriteFile(policy, []byte(`{"default":[{"type":"insecureAcceptAnything"}]}`), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tmp"), 0o755))
	b, err := newDefaultBackends(&RuntimeConfig{
		Root:                filepath.Join(dir, "root"),
		RunRoot:             filepath.Join(dir, "run"),
		TmpDir:              filepath.Join(dir, "tmp"),
		GraphDriverName:     "vfs",
		SignaturePolicyPath: policy,
		InsecureRegistries:  []string{host},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, b.close()) })

	_, err = b.images.Pull(ctx, host+"/seed/app:1")
	require.NoError(t, err)
	require.NoError(t, b.images.Tag(ctx, host+"/seed/app:1", host+"/o/worker:1"))

	digest, err := b.images.Push(ctx, host+"/o/worker:1", nil)
	require.NoError(t, err)
	pushedRef, err := name.ParseReference(host + "/o/worker:1")
	require.NoError(t, err)
	served, err := remote.Head(pushedRef)
	require.NoError(t, err)
	assert.Equal(t, served.Digest.String(), digest)

	// The tag@digest form the worker-image refresher smokes pulls by its digest.
	_, err = b.images.Pull(ctx, host+"/o/worker:1@"+digest)
	require.NoError(t, err)

	_, err = b.images.Push(ctx, host+"/o/missing:1", nil)
	require.ErrorIs(t, err, ErrImageNotFound)
}

func TestStorageImageStore_ResolvesAndRunsByImageID(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(registry.New())
	t.Cleanup(server.Close)
	host := strings.TrimPrefix(server.URL, "http://")

	// No layers: unpacking one chowns its files, which needs root.
	seed, err := random.Image(256, 0)
	require.NoError(t, err)
	seedRef, err := name.ParseReference(host + "/seed/app:1")
	require.NoError(t, err)
	require.NoError(t, remote.Write(seedRef, seed))

	dir := t.TempDir()
	policy := filepath.Join(dir, "policy.json")
	require.NoError(t, os.WriteFile(policy, []byte(`{"default":[{"type":"insecureAcceptAnything"}]}`), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tmp"), 0o755))
	b, err := newDefaultBackends(&RuntimeConfig{
		Root:                filepath.Join(dir, "root"),
		RunRoot:             filepath.Join(dir, "run"),
		TmpDir:              filepath.Join(dir, "tmp"),
		GraphDriverName:     "vfs",
		SignaturePolicyPath: policy,
		InsecureRegistries:  []string{host},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, b.close()) })

	rec, err := b.images.Pull(ctx, host+"/seed/app:1")
	require.NoError(t, err)

	for _, ref := range []string{"sha256:" + rec.ID, rec.ID} {
		got, err := b.images.Inspect(ctx, ref)
		require.NoError(t, err, ref)
		assert.Equal(t, rec.ID, got.ID, ref)
	}
	missing := "sha256:" + strings.Repeat("e", 64)
	_, err = b.images.Inspect(ctx, missing)
	require.ErrorIs(t, err, ErrImageNotFound)
	require.NoError(t, b.images.Remove(ctx, missing, true))

	engine := newFakeEngine()
	engine.initResult = &fakeResult{stdout: "ok\n"}
	config := &RuntimeConfig{
		Root:            filepath.Join(dir, "rt-root"),
		RunRoot:         filepath.Join(dir, "rt-run"),
		TmpDir:          filepath.Join(dir, "tmp"),
		GraphDriverName: "vfs",
		StaticDir:       filepath.Join(dir, "static"),
		VolumePath:      filepath.Join(dir, "volumes"),
		ImageStorePath:  filepath.Join(dir, "images"),
	}
	rt, err := NewRuntime(ctx, config, withBackends(&backends{
		engine: engine,
		images: b.images,
		rootfs: &fakeRootfs{root: filepath.Join(dir, "rootfs")},
	}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rt.Close()) })
	jr := NewJobRuntime(rt)

	info, err := jr.InspectImage(ctx, "sha256:"+rec.ID)
	require.NoError(t, err)
	assert.Equal(t, "sha256:"+rec.ID, info.ID)

	// Without the registry a pull attempt fails differently.
	server.Close()
	result, err := jr.RunImage(ctx, &orchestration.ImageRunSpec{
		Image:       "sha256:" + rec.ID,
		Command:     []string{"true"},
		NetworkNone: true,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "ok\n", result.Stdout)

	_, err = jr.RunImage(ctx, &orchestration.ImageRunSpec{
		Image:       missing,
		Command:     []string{"true"},
		NetworkNone: true,
	})
	var notFound *orchestration.ImageNotFoundError
	require.ErrorAs(t, err, &notFound)
}

func TestStorageImageStore_PullHonoursTheSignaturePolicy(t *testing.T) {
	b := newStorageBackends(t)

	// The policy file does not exist, so the pull stops before any network
	// access: nothing is copied without a policy to judge it by.
	_, err := b.images.Pull(context.Background(), "registry.invalid/app:1")
	require.Error(t, err)
	_, err = b.images.Inspect(context.Background(), "registry.invalid/app:1")
	require.ErrorIs(t, err, ErrImageNotFound)
}

func TestStorageImageStore_PullOfAnInvalidReference(t *testing.T) {
	b := newStorageBackends(t)

	_, err := b.images.Pull(context.Background(), "Not/A/Valid:Reference:")
	require.Error(t, err)
}

func TestStorageRootfsStore_CreateFromAMissingImage(t *testing.T) {
	b := newStorageBackends(t)

	_, err := b.rootfs.Create(context.Background(), "container", "no-such-image")
	require.Error(t, err)
}

// A reclaim that removed the rootfs and then died before deleting its bundle
// retries the removal on the next start. The production store reports the
// container's absence on unmount as an unknown layer, and the retry must read
// that as done, or the bundle stays forever and every start fails its sweep.
func TestStorageRootfsStore_RemoveIsRetrySafe(t *testing.T) {
	ctx := context.Background()
	b := newStorageBackends(t)
	require.NoError(t, b.rootfs.Remove("never-created"))
	if os.Geteuid() != 0 {
		t.Skip("creating a read-write layer chowns it, which needs root")
	}
	images, ok := b.images.(*storageImageStore)
	require.True(t, ok)
	stored, err := images.store.CreateImage("", []string{"docker.io/library/empty:latest"}, "", "", nil)
	require.NoError(t, err)

	_, err = b.rootfs.Create(ctx, "reclaimed", stored.ID)
	require.NoError(t, err)
	require.NoError(t, b.rootfs.Remove("reclaimed"))
	require.NoError(t, b.rootfs.Remove("reclaimed"), "the retried removal")
}

func TestNormalizeImageReference(t *testing.T) {
	tests := map[string]string{
		"alpine":                                "docker.io/library/alpine:latest",
		"alpine:3.20":                           "docker.io/library/alpine:3.20",
		"registry.local:5000/team/app":          "registry.local:5000/team/app:latest",
		"registry.local/app@" + testImageDigest: "registry.local/app@" + testImageDigest,
	}
	for ref, want := range tests {
		t.Run(ref, func(t *testing.T) {
			named, err := normalizeImageReference(ref)
			require.NoError(t, err)
			assert.Equal(t, want, named.String())
		})
	}
}

// storageRuntimeConfig is a valid runtime configuration over a vfs store in a
// temporary directory.
func storageRuntimeConfig(t *testing.T) *RuntimeConfig {
	t.Helper()
	dir := t.TempDir()
	return &RuntimeConfig{
		Root:            filepath.Join(dir, "root"),
		RunRoot:         filepath.Join(dir, "run"),
		TmpDir:          filepath.Join(dir, "tmp"),
		GraphDriverName: "vfs",
		StaticDir:       filepath.Join(dir, "static"),
		VolumePath:      filepath.Join(dir, "volumes"),
		ImageStorePath:  filepath.Join(dir, "images"),
	}
}

func TestNewRuntime_OpensTheProductionBackends(t *testing.T) {
	rt, err := NewRuntime(context.Background(), storageRuntimeConfig(t))
	require.NoError(t, err)
	assert.True(t, rt.IsInitialized())

	_, err = rt.CreateContainer(context.Background(), &ContainerConfig{Name: "none", Image: "alpine:latest"})
	require.ErrorIs(t, err, ErrImageNotFound)
	require.NoError(t, rt.Close())
}

// blockStorageRunRoot puts a regular file where the store keeps its run
// state, so opening the store fails after the configuration is accepted.
func blockStorageRunRoot(t *testing.T, config *RuntimeConfig) {
	t.Helper()
	require.NoError(t, os.MkdirAll(config.RunRoot, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(config.RunRoot, "storage"), nil, 0o600))
}

func TestNewRuntime_BackendFailure(t *testing.T) {
	config := storageRuntimeConfig(t)
	blockStorageRunRoot(t, config)

	_, err := NewRuntime(context.Background(), config)
	var runtimeErr *RuntimeError
	require.ErrorAs(t, err, &runtimeErr)
	assert.Equal(t, "init backends", runtimeErr.Operation)
}

func TestManagerInitialize_OpensTheRuntime(t *testing.T) {
	config := DefaultManagerConfig()
	config.Runtime = storageRuntimeConfig(t)
	manager, err := NewPodmanManager(config)
	require.NoError(t, err)

	require.NoError(t, manager.Initialize(context.Background()))
	assert.True(t, manager.IsInitialized())
	require.NoError(t, manager.Initialize(context.Background()))
	require.NoError(t, manager.Close())
}

func TestManagerInitialize_RuntimeFailure(t *testing.T) {
	config := DefaultManagerConfig()
	config.Runtime = storageRuntimeConfig(t)
	blockStorageRunRoot(t, config.Runtime)
	manager, err := NewPodmanManager(config)
	require.NoError(t, err)

	var runtimeErr *RuntimeError
	require.ErrorAs(t, manager.Initialize(context.Background()), &runtimeErr)
	assert.False(t, manager.IsInitialized())
}

func TestNormalizeImageReference_Invalid(t *testing.T) {
	_, err := normalizeImageReference("Not/A/Valid:Reference:")
	require.Error(t, err)
}
