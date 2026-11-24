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
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	istorage "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/reexec"
)

// TestMain serves go.podman.io/storage's reexec helpers, which apply a layer
// in a re-executed copy of the test binary, as RunContainerInit does for the
// agent's own binary.
func TestMain(m *testing.M) {
	if reexec.Init() {
		return
	}
	os.Exit(m.Run())
}

// newCommitTestBackends opens an image store, a rootfs store and a layer
// committer over one empty vfs store that an unprivileged user can write:
// container root maps to the test user and chown failures are ignored. The
// image store pulls from the insecure hosts without TLS verification.
func newCommitTestBackends(t *testing.T, insecure ...string) *backends {
	t.Helper()
	dir := t.TempDir()
	policy := filepath.Join(dir, "policy.json")
	require.NoError(t, os.WriteFile(policy, []byte(`{"default":[{"type":"insecureAcceptAnything"}]}`), 0o600))
	tmp := filepath.Join(dir, "tmp")
	require.NoError(t, os.MkdirAll(tmp, 0o755))

	store, err := storage.GetStore(storage.StoreOptions{
		RunRoot:            filepath.Join(dir, "run"),
		GraphRoot:          filepath.Join(dir, "root"),
		GraphDriverName:    "vfs",
		GraphDriverOptions: []string{"vfs.ignore_chown_errors=true"},
		UIDMap:             []idtools.IDMap{{ContainerID: 0, HostID: os.Geteuid(), Size: 1}},
		GIDMap:             []idtools.IDMap{{ContainerID: 0, HostID: os.Getegid(), Size: 1}},
	})
	require.NoError(t, err)

	system := types.SystemContext{
		BigFilesTemporaryDir: tmp,
		BlobInfoCacheDir:     filepath.Join(dir, "cache"),
		SignaturePolicyPath:  policy,
	}
	b := &backends{
		images: &storageImageStore{store: store, system: system, insecureRegistries: insecure},
		rootfs: &storageRootfsStore{store: store},
		commit: &storageLayerCommitter{store: store, system: system},
		close: func() error {
			_, err := store.Shutdown(true)
			return err
		},
	}
	t.Cleanup(func() { require.NoError(t, b.close()) })
	return b
}

// writeTestFile writes content to rel under root, creating its directories.
func writeTestFile(t *testing.T, root, rel, content string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), mode))
	require.NoError(t, os.Chmod(path, mode))
}

// readTestFile reads rel under root.
func readTestFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, rel))
	require.NoError(t, err)
	return string(data)
}

// storedOCIConfig reads the stored image id's OCI configuration through the
// containers-storage transport.
func storedOCIConfig(t *testing.T, b *backends, id string) *imgspecv1.Image {
	t.Helper()
	images := b.images.(*storageImageStore)
	ref, err := istorage.Transport.NewStoreReference(images.store, nil, id)
	require.NoError(t, err)
	image, err := ref.NewImage(context.Background(), &images.system)
	require.NoError(t, err)
	defer func() { require.NoError(t, image.Close()) }()
	oci, err := image.OCIConfig(context.Background())
	require.NoError(t, err)
	return oci
}

// commitScratch commits a container on an empty layer holding etc/hello and
// bin/tool, and returns the stored image.
func commitScratch(t *testing.T, b *backends, config imageConfig) imageRecord {
	t.Helper()
	ctx := context.Background()
	mnt, err := b.rootfs.Create(ctx, "c1", "")
	require.NoError(t, err)
	// An empty layer's root is 0555; the test user owns it, so it opens it
	// for writing as a build running as container root could.
	require.NoError(t, os.Chmod(mnt, 0o755))
	writeTestFile(t, mnt, "etc/hello", "hello\n", 0o644)
	writeTestFile(t, mnt, "bin/tool", "#!/bin/sh\n", 0o755)

	record, err := b.commit.Commit(ctx, "c1", config, "COPY hello /etc/hello")
	require.NoError(t, err)
	require.NoError(t, b.rootfs.Remove("c1"))
	return record
}

func scratchConfig() imageConfig {
	return imageConfig{
		Entrypoint: []string{"/bin/tool"},
		Cmd:        []string{"--help"},
		Env:        []string{"PATH=/bin", "A=1"},
		WorkingDir: "/etc",
		User:       "1000:1000",
		Labels:     map[string]string{"stage": "one"},
	}
}

func TestStorageLayerCommitter_FromScratch(t *testing.T) {
	ctx := context.Background()
	b := newCommitTestBackends(t)
	config := scratchConfig()

	record := commitScratch(t, b, config)
	require.NotEmpty(t, record.ID)

	inspected, err := b.images.Inspect(ctx, "sha256:"+record.ID)
	require.NoError(t, err)
	assert.Equal(t, record.ID, inspected.ID)
	assert.Equal(t, config, inspected.Config)
	assert.Equal(t, config, record.Config)

	oci := storedOCIConfig(t, b, record.ID)
	require.Len(t, oci.RootFS.DiffIDs, 1)
	require.Len(t, oci.History, 1)
	assert.Equal(t, "COPY hello /etc/hello", oci.History[0].CreatedBy)
	assert.Equal(t, "linux", oci.OS)

	mnt, err := b.rootfs.Create(ctx, "c2", record.ID)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, b.rootfs.Remove("c2")) })
	assert.Equal(t, "hello\n", readTestFile(t, mnt, "etc/hello"))
	info, err := os.Stat(filepath.Join(mnt, "bin/tool"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
}

// commitStacked commits a second layer on base that adds etc/second and
// overwrites etc/hello, and returns the stored image.
func commitStacked(t *testing.T, b *backends, base string) imageRecord {
	t.Helper()
	ctx := context.Background()
	mnt, err := b.rootfs.Create(ctx, "c3", base)
	require.NoError(t, err)
	writeTestFile(t, mnt, "etc/second", "second\n", 0o644)
	writeTestFile(t, mnt, "etc/hello", "hello again\n", 0o644)

	record, err := b.commit.Commit(ctx, "c3", imageConfig{
		Cmd:    []string{"/bin/tool", "run"},
		Labels: map[string]string{"stage": "two"},
	}, "RUN echo second")
	require.NoError(t, err)
	require.NoError(t, b.rootfs.Remove("c3"))
	return record
}

func TestStorageLayerCommitter_Stacked(t *testing.T) {
	ctx := context.Background()
	b := newCommitTestBackends(t)
	first := commitScratch(t, b, scratchConfig())
	second := commitStacked(t, b, first.ID)
	require.NotEqual(t, first.ID, second.ID)

	inspected, err := b.images.Inspect(ctx, "sha256:"+second.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"/bin/tool", "run"}, inspected.Config.Cmd)
	assert.Equal(t, map[string]string{"stage": "two"}, inspected.Config.Labels)

	oci := storedOCIConfig(t, b, second.ID)
	require.Len(t, oci.RootFS.DiffIDs, 2)
	assert.Equal(t, storedOCIConfig(t, b, first.ID).RootFS.DiffIDs[0], oci.RootFS.DiffIDs[0])
	require.Len(t, oci.History, 2)
	assert.Equal(t, "COPY hello /etc/hello", oci.History[0].CreatedBy)
	assert.Equal(t, "RUN echo second", oci.History[1].CreatedBy)

	mnt, err := b.rootfs.Create(ctx, "c4", second.ID)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, b.rootfs.Remove("c4")) })
	assert.Equal(t, "hello again\n", readTestFile(t, mnt, "etc/hello"))
	assert.Equal(t, "second\n", readTestFile(t, mnt, "etc/second"))

	// The first image is unchanged.
	mnt, err = b.rootfs.Create(ctx, "c5", first.ID)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, b.rootfs.Remove("c5")) })
	assert.Equal(t, "hello\n", readTestFile(t, mnt, "etc/hello"))
	_, err = os.Stat(filepath.Join(mnt, "etc/second"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestStorageLayerCommitter_PushAndRebuild(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(registry.New())
	t.Cleanup(server.Close)
	host := strings.TrimPrefix(server.URL, "http://")

	b := newCommitTestBackends(t, host)
	first := commitScratch(t, b, scratchConfig())
	second := commitStacked(t, b, first.ID)

	require.NoError(t, b.images.Tag(ctx, "sha256:"+second.ID, host+"/build/app:1"))
	digest, err := b.images.Push(ctx, host+"/build/app:1", nil)
	require.NoError(t, err)
	pushedRef, err := name.ParseReference(host + "/build/app:1")
	require.NoError(t, err)
	served, err := remote.Head(pushedRef)
	require.NoError(t, err)
	assert.Equal(t, served.Digest.String(), digest)
	pushed := storedOCIConfig(t, b, second.ID).RootFS.DiffIDs

	// A commit on the pulled image adds one layer and reuses the pulled ones.
	b2 := newCommitTestBackends(t, host)
	pulled, err := b2.images.Pull(ctx, host+"/build/app:1")
	require.NoError(t, err)
	mnt, err := b2.rootfs.Create(ctx, "p1", pulled.ID)
	require.NoError(t, err)
	writeTestFile(t, mnt, "etc/third", "third\n", 0o644)
	rebuilt, err := b2.commit.Commit(ctx, "p1", pulled.Config, "RUN echo third")
	require.NoError(t, err)
	require.NoError(t, b2.rootfs.Remove("p1"))

	oci := storedOCIConfig(t, b2, rebuilt.ID)
	require.Len(t, oci.RootFS.DiffIDs, 3)
	assert.Equal(t, pushed, oci.RootFS.DiffIDs[:2])

	mnt, err = b2.rootfs.Create(ctx, "p2", rebuilt.ID)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, b2.rootfs.Remove("p2")) })
	assert.Equal(t, "hello again\n", readTestFile(t, mnt, "etc/hello"))
	assert.Equal(t, "third\n", readTestFile(t, mnt, "etc/third"))
}

func TestStorageLayerCommitter_Errors(t *testing.T) {
	ctx := context.Background()
	b := newCommitTestBackends(t)

	_, err := b.commit.Commit(ctx, "missing", imageConfig{}, "RUN true")
	require.ErrorIs(t, err, storage.ErrContainerUnknown)
	require.NoError(t, b.rootfs.Remove("missing"))

	// The store keeps an image while a container uses it, so a build
	// container's base cannot go missing before its commit.
	base := commitScratch(t, b, scratchConfig())
	_, err = b.rootfs.Create(ctx, "c6", base.ID)
	require.NoError(t, err)
	err = b.images.Remove(ctx, "sha256:"+base.ID, false)
	require.True(t, errors.Is(err, storage.ErrImageUsedByContainer), "got %v", err)

	_, err = b.commit.Commit(ctx, "c6", imageConfig{}, "RUN true")
	require.NoError(t, err)
	require.NoError(t, b.rootfs.Remove("c6"))
}

func TestNewDefaultBackends_HasACommitter(t *testing.T) {
	assert.NotNil(t, newStorageBackends(t).commit)
}
