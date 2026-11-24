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
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/opencontainers/go-digest"
	imgspec "github.com/opencontainers/image-spec/specs-go"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// layoutFile is a regular file of a layer tar in an image layout.
type layoutFile struct {
	content string
	mode    os.FileMode
}

// readLayout reads the image layout dir's one manifest, its configuration
// and the regular files of each layer blob dir holds, checking each blob it
// reads against its digest. Layers named but not held, a base image's, are
// skipped.
func readLayout(t *testing.T, dir string) (imgspecv1.Manifest, imgspecv1.Image, map[string]layoutFile) {
	t.Helper()
	blob := func(d digest.Digest) ([]byte, bool) {
		data, err := os.ReadFile(filepath.Join(dir, "blobs", d.Algorithm().String(), d.Encoded()))
		if errors.Is(err, os.ErrNotExist) {
			return nil, false
		}
		require.NoError(t, err)
		require.Equal(t, d, digest.FromBytes(data), "blob %s", d)
		return data, true
	}

	_, err := os.Stat(filepath.Join(dir, imgspecv1.ImageLayoutFile))
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(dir, imgspecv1.ImageIndexFile))
	require.NoError(t, err)
	var index imgspecv1.Index
	require.NoError(t, json.Unmarshal(data, &index))
	require.Len(t, index.Manifests, 1)

	var m imgspecv1.Manifest
	data, ok := blob(index.Manifests[0].Digest)
	require.True(t, ok, "manifest blob")
	require.NoError(t, json.Unmarshal(data, &m))

	var config imgspecv1.Image
	data, ok = blob(m.Config.Digest)
	require.True(t, ok, "config blob")
	require.NoError(t, json.Unmarshal(data, &config))

	files := map[string]layoutFile{}
	for _, layer := range m.Layers {
		data, ok := blob(layer.Digest)
		if !ok {
			continue
		}
		require.Equal(t, int64(len(data)), layer.Size)
		r := tar.NewReader(bytes.NewReader(data))
		for {
			header, err := r.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
			if header.Typeflag != tar.TypeReg {
				continue
			}
			content, err := io.ReadAll(r)
			require.NoError(t, err)
			files[path.Clean(header.Name)] = layoutFile{content: string(content), mode: header.FileInfo().Mode().Perm()}
		}
	}
	return m, config, files
}

// configOf returns the image configuration fields of oci.
func configOf(oci imgspecv1.Image) imageConfig {
	return imageConfig{
		Entrypoint: oci.Config.Entrypoint,
		Cmd:        oci.Config.Cmd,
		Env:        oci.Config.Env,
		WorkingDir: oci.Config.WorkingDir,
		User:       oci.Config.User,
		Labels:     oci.Config.Labels,
	}
}

func TestStorageLayerCommitter_LayoutFromScratch(t *testing.T) {
	ctx := context.Background()
	b := newCommitTestBackends(t)
	committer := b.commit.(*storageLayerCommitter)

	mnt, err := b.rootfs.Create(ctx, "c1", "")
	require.NoError(t, err)
	// An empty layer's root is 0555; the test user owns it, so it opens it
	// for writing as a build running as container root could.
	require.NoError(t, os.Chmod(mnt, 0o755))
	writeTestFile(t, mnt, "etc/hello", "hello\n", 0o644)
	writeTestFile(t, mnt, "bin/tool", "#!/bin/sh\n", 0o755)
	container, err := committer.store.Container("c1")
	require.NoError(t, err)

	dir := t.TempDir()
	config := scratchConfig()
	configDigest, err := committer.writeLayout(ctx, container, dir, config, "COPY hello /etc/hello")
	require.NoError(t, err)
	require.NoError(t, b.rootfs.Remove("c1"))

	m, oci, files := readLayout(t, dir)
	assert.Equal(t, configDigest, m.Config.Digest)
	assert.Equal(t, imgspecv1.MediaTypeImageConfig, m.Config.MediaType)
	require.Len(t, m.Layers, 1)
	assert.Equal(t, imgspecv1.MediaTypeImageLayer, m.Layers[0].MediaType)
	assert.Equal(t, "layers", oci.RootFS.Type)
	require.Len(t, oci.RootFS.DiffIDs, 1)
	assert.Equal(t, oci.RootFS.DiffIDs[0], m.Layers[0].Digest)

	assert.Equal(t, "linux", oci.OS)
	assert.Equal(t, runtime.GOARCH, oci.Architecture)
	assert.Equal(t, config, configOf(oci))
	require.Len(t, oci.History, 1)
	assert.Equal(t, "COPY hello /etc/hello", oci.History[0].CreatedBy)
	assert.NotNil(t, oci.History[0].Created)

	assert.Equal(t, "hello\n", files["etc/hello"].content)
	require.Contains(t, files, "bin/tool")
	assert.Equal(t, os.FileMode(0o755), files["bin/tool"].mode)
}

// storeEmptyBase stores an image with no layers, PATH=/usr/bin in its
// environment and one history entry, as a pull would store it, and returns
// its ID. Storing it applies no layer, so an unprivileged user can.
func storeEmptyBase(t *testing.T, store storage.Store) string {
	t.Helper()
	config, err := json.Marshal(imgspecv1.Image{
		Platform: imgspecv1.Platform{OS: "linux", Architecture: runtime.GOARCH},
		Config:   imgspecv1.ImageConfig{Env: []string{"PATH=/usr/bin"}},
		RootFS:   imgspecv1.RootFS{Type: "layers", DiffIDs: []digest.Digest{}},
		History:  []imgspecv1.History{{CreatedBy: "FROM base"}},
	})
	require.NoError(t, err)
	configDigest := digest.FromBytes(config)
	manifestData, err := json.Marshal(imgspecv1.Manifest{
		Versioned: imgspec.Versioned{SchemaVersion: 2},
		MediaType: imgspecv1.MediaTypeImageManifest,
		Config:    imgspecv1.Descriptor{MediaType: imgspecv1.MediaTypeImageConfig, Digest: configDigest, Size: int64(len(config))},
		Layers:    []imgspecv1.Descriptor{},
	})
	require.NoError(t, err)
	manifestDigest := digest.FromBytes(manifestData)

	image, err := store.CreateImage(configDigest.Encoded(), nil, "", "", &storage.ImageOptions{
		Digest: manifestDigest,
		BigData: []storage.ImageBigDataOption{
			{Key: configDigest.String(), Data: config, Digest: configDigest},
			{Key: storage.ImageDigestBigDataKey, Data: manifestData, Digest: manifestDigest},
		},
	})
	require.NoError(t, err)
	return image.ID
}

func TestStorageLayerCommitter_LayoutOnABase(t *testing.T) {
	ctx := context.Background()
	b := newCommitTestBackends(t)
	committer := b.commit.(*storageLayerCommitter)
	base := storeEmptyBase(t, committer.store)

	// A container on a stored image takes the image's ID mapping, which a
	// layerless image lacks, so its layer would be chowned to host root. It
	// is created with the store's test-user mapping instead, as an
	// unprivileged build would map it.
	_, err := committer.store.CreateContainer("c2", nil, base, "", "", &storage.ContainerOptions{
		IDMappingOptions: storage.IDMappingOptions{
			UIDMap: []idtools.IDMap{{ContainerID: 0, HostID: os.Geteuid(), Size: 1}},
			GIDMap: []idtools.IDMap{{ContainerID: 0, HostID: os.Getegid(), Size: 1}},
		},
	})
	require.NoError(t, err)
	mnt, err := committer.store.Mount("c2", "")
	require.NoError(t, err)
	require.NoError(t, os.Chmod(mnt, 0o755))
	writeTestFile(t, mnt, "etc/second", "second\n", 0o644)
	container, err := committer.store.Container("c2")
	require.NoError(t, err)

	dir := t.TempDir()
	config := imageConfig{Cmd: []string{"/bin/tool", "run"}, Env: []string{"PATH=/bin"}}
	configDigest, err := committer.writeLayout(ctx, container, dir, config, "RUN echo second")
	require.NoError(t, err)
	require.NoError(t, b.rootfs.Remove("c2"))

	m, oci, files := readLayout(t, dir)
	assert.Equal(t, configDigest, m.Config.Digest)
	require.Len(t, m.Layers, 1)
	require.Len(t, oci.RootFS.DiffIDs, 1)
	assert.Equal(t, oci.RootFS.DiffIDs[0], m.Layers[0].Digest)
	require.Len(t, oci.History, 2)
	assert.Equal(t, "FROM base", oci.History[0].CreatedBy)
	assert.Equal(t, "RUN echo second", oci.History[1].CreatedBy)
	assert.Equal(t, []string{"PATH=/bin"}, oci.Config.Env)
	assert.Equal(t, config, configOf(oci))
	assert.Equal(t, "second\n", files["etc/second"].content)
}

func TestStorageLayerCommitter_Errors(t *testing.T) {
	ctx := context.Background()
	b := newCommitTestBackends(t)

	_, err := b.commit.Commit(ctx, "missing", imageConfig{}, "RUN true")
	require.ErrorIs(t, err, storage.ErrContainerUnknown)
	require.NoError(t, b.rootfs.Remove("missing"))
}

func TestNewDefaultBackends_HasACommitter(t *testing.T) {
	assert.NotNil(t, newStorageBackends(t).commit)
}
