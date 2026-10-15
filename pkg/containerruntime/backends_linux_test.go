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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestStorageRootfsStore_RemoveOfAMissingContainer(t *testing.T) {
	b := newStorageBackends(t)

	require.Error(t, b.rootfs.Remove("no-such-container"))
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
