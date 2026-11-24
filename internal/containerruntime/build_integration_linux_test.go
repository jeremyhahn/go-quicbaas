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

//go:build integration && linux && cgo && containers_image_openpgp && exclude_graphdriver_btrfs

// These tests store layers, which go.podman.io/storage applies in a chroot,
// so they need root; `make integration-test-containerruntime` runs them in
// its privileged container.

package containerruntime

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// busyboxRoundTrip is built on a base holding only busybox.
const busyboxRoundTrip = `FROM localhost/busybox-base:test AS one
RUN echo hi > /hello
COPY <<EOF /etc/greeting
hey
EOF
FROM scratch
COPY --from=one /hello /etc/greeting /out/
CMD ["/out/hello"]
`

// commitBusyboxBase stores an image holding the statically linked busybox
// $CONTAINERRUNTIME_BUSYBOX names as bin/busybox, with bin/sh a link to it,
// tagged localhost/busybox-base:test.
func commitBusyboxBase(t *testing.T, b *backends) {
	t.Helper()
	ctx := context.Background()
	busybox := os.Getenv("CONTAINERRUNTIME_BUSYBOX")
	require.NotEmpty(t, busybox, "CONTAINERRUNTIME_BUSYBOX names a statically linked busybox")
	program, err := fs.ReadFile(os.DirFS(filepath.Dir(busybox)), filepath.Base(busybox))
	require.NoError(t, err)

	mnt, err := b.rootfs.Create(ctx, "busybox-base", "")
	require.NoError(t, err)
	require.NoError(t, os.Chmod(mnt, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(mnt, "bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mnt, "bin", "busybox"), program, 0o600))
	require.NoError(t, os.Chmod(filepath.Join(mnt, "bin", "busybox"), 0o755))
	require.NoError(t, os.Symlink("busybox", filepath.Join(mnt, "bin", "sh")))

	base, err := b.commit.Commit(ctx, "busybox-base", imageConfig{}, "COPY busybox /bin/busybox")
	require.NoError(t, err)
	require.NoError(t, b.rootfs.Remove("busybox-base"))
	require.NoError(t, b.images.Tag(ctx, "sha256:"+base.ID, "localhost/busybox-base:test"))
}

func TestBuildImage_BusyboxRoundTrip(t *testing.T) {
	ctx := context.Background()
	b := newCommitTestBackends(t)
	commitBusyboxBase(t, b)

	dir := t.TempDir()
	config := &RuntimeConfig{
		Root:            filepath.Join(dir, "root"),
		RunRoot:         filepath.Join(dir, "run"),
		TmpDir:          filepath.Join(dir, "tmp"),
		GraphDriverName: "vfs",
		StaticDir:       filepath.Join(dir, "static"),
		VolumePath:      filepath.Join(dir, "volumes"),
		ImageStorePath:  filepath.Join(dir, "images"),
	}
	// The store is b's, which closes it; the runtime closes only its engine.
	rt, err := NewRuntime(ctx, config, withBackends(&backends{
		engine: newLibcontainerEngine(filepath.Join(dir, "libcontainer")),
		images: b.images,
		rootfs: b.rootfs,
		commit: b.commit,
	}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rt.Close()) })

	built, err := rt.buildImage(ctx, []byte(busyboxRoundTrip), buildOptions{Tags: []string{"localhost/built:test"}})
	require.NoError(t, err)
	tagged, err := b.images.Inspect(ctx, "localhost/built:test")
	require.NoError(t, err)
	assert.Equal(t, built.ID, tagged.ID)
	assert.Equal(t, []string{"/out/hello"}, built.Config.Cmd)

	// The build removed stage one's images, so the store holds the base
	// and the built image, each with its one layer. This is checked
	// before the root filesystem below adds a container layer.
	base, err := b.images.Inspect(ctx, "localhost/busybox-base:test")
	require.NoError(t, err)
	store := b.images.(*storageImageStore).store
	storedImages, err := store.Images()
	require.NoError(t, err)
	var imageIDs []string
	for _, image := range storedImages {
		imageIDs = append(imageIDs, image.ID)
	}
	assert.ElementsMatch(t, []string{base.ID, built.ID}, imageIDs, "the intermediate images are removed")
	layers, err := store.Layers()
	require.NoError(t, err)
	assert.Len(t, layers, 2, "the intermediate images' layers are freed")

	mnt, err := b.rootfs.Create(ctx, "built-check", built.ID)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, b.rootfs.Remove("built-check")) })
	assert.Equal(t, "hi\n", readTestFile(t, mnt, "out/hello"))
	assert.Equal(t, "hey\n", readTestFile(t, mnt, "out/greeting"))
	_, err = os.Lstat(filepath.Join(mnt, "bin"))
	require.ErrorIs(t, err, os.ErrNotExist, "the scratch stage holds only what it copied")

	var history []string
	for _, entry := range storedOCIConfig(t, b, built.ID).History {
		history = append(history, entry.CreatedBy)
	}
	assert.Equal(t, []string{"COPY --from=one /hello /etc/greeting /out/"}, history)

	containers, err := rt.ListContainers(ctx, true)
	require.NoError(t, err)
	assert.Empty(t, containers, "the build's containers are removed")
}
