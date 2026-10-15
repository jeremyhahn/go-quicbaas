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

//go:build integration && linux && cgo && containers_image_openpgp && exclude_graphdriver_btrfs

package containerruntime_test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"go.podman.io/image/v5/copy"
	"go.podman.io/image/v5/docker"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/oci/layout"
	"go.podman.io/image/v5/signature"
	"go.podman.io/image/v5/types"
)

// registryEnv names the registry the compose file runs, as host:port.
const registryEnv = "CONTAINERRUNTIME_REGISTRY"

// busyboxEnv names a statically linked busybox, the only file the test image
// needs besides its /bin/sh link.
const busyboxEnv = "CONTAINERRUNTIME_BUSYBOX"

// registryHost returns the plain-HTTP registry the test images are pushed to.
func registryHost(t *testing.T) string {
	t.Helper()
	host := os.Getenv(registryEnv)
	require.NotEmpty(t, host, "%s must name the test registry", registryEnv)
	return host
}

// pushShellImage builds a one-layer image holding a static busybox as
// /bin/sh, pushes it to the test registry as repository:latest and returns
// the reference and the digest of the pushed manifest.
func pushShellImage(t *testing.T, repository string) (string, digest.Digest) {
	t.Helper()

	dir := t.TempDir()
	writeShellLayout(t, dir)

	ref := registryHost(t) + "/" + repository + ":latest"
	source, err := layout.NewReference(dir, "latest")
	require.NoError(t, err)
	destination, err := docker.ParseReference("//" + ref)
	require.NoError(t, err)

	policy, err := signature.NewPolicyContext(&signature.Policy{
		Default: signature.PolicyRequirements{signature.NewPRInsecureAcceptAnything()},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, policy.Destroy()) })

	pushed, err := copy.Image(context.Background(), policy, destination, source, &copy.Options{
		DestinationCtx: &types.SystemContext{DockerInsecureSkipTLSVerify: types.NewOptionalBool(true)},
	})
	require.NoError(t, err)

	manifestDigest, err := manifest.Digest(pushed)
	require.NoError(t, err)
	return ref, manifestDigest
}

// writeShellLayout writes an OCI image layout to dir whose "latest" image is
// the busybox shell layer.
func writeShellLayout(t *testing.T, dir string) {
	t.Helper()

	layer := writeBlob(t, dir, imgspecv1.MediaTypeImageLayer, shellLayer(t))

	config, err := json.Marshal(imgspecv1.Image{
		Platform: imgspecv1.Platform{Architecture: runtime.GOARCH, OS: "linux"},
		RootFS:   imgspecv1.RootFS{Type: "layers", DiffIDs: []digest.Digest{layer.Digest}},
	})
	require.NoError(t, err)

	imageManifest, err := json.Marshal(imgspecv1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: imgspecv1.MediaTypeImageManifest,
		Config:    writeBlob(t, dir, imgspecv1.MediaTypeImageConfig, config),
		Layers:    []imgspecv1.Descriptor{layer},
	})
	require.NoError(t, err)

	manifestDescriptor := writeBlob(t, dir, imgspecv1.MediaTypeImageManifest, imageManifest)
	manifestDescriptor.Annotations = map[string]string{imgspecv1.AnnotationRefName: "latest"}

	index, err := json.Marshal(imgspecv1.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: imgspecv1.MediaTypeImageIndex,
		Manifests: []imgspecv1.Descriptor{manifestDescriptor},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.json"), index, 0o600))

	layoutFile, err := json.Marshal(imgspecv1.ImageLayout{Version: imgspecv1.ImageLayoutVersion})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, imgspecv1.ImageLayoutFile), layoutFile, 0o600))
}

// writeBlob stores data in the layout's blob directory and describes it.
func writeBlob(t *testing.T, dir, mediaType string, data []byte) imgspecv1.Descriptor {
	t.Helper()

	blobDigest := digest.FromBytes(data)
	blobs := filepath.Join(dir, "blobs", blobDigest.Algorithm().String())
	require.NoError(t, os.MkdirAll(blobs, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(blobs, blobDigest.Encoded()), data, 0o600))

	return imgspecv1.Descriptor{MediaType: mediaType, Digest: blobDigest, Size: int64(len(data))}
}

// shellLayer returns an uncompressed layer tar holding /bin/busybox and a
// /bin/sh link to it.
func shellLayer(t *testing.T) []byte {
	t.Helper()

	busyboxPath := os.Getenv(busyboxEnv)
	require.NotEmpty(t, busyboxPath, "%s must name a static busybox", busyboxEnv)
	busybox, err := os.ReadFile(busyboxPath)
	require.NoError(t, err)

	var layer bytes.Buffer
	archive := tar.NewWriter(&layer)
	require.NoError(t, archive.WriteHeader(&tar.Header{Name: "bin/", Typeflag: tar.TypeDir, Mode: 0o755}))
	require.NoError(t, archive.WriteHeader(&tar.Header{
		Name: "bin/busybox", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(busybox)),
	}))
	_, err = archive.Write(busybox)
	require.NoError(t, err)
	require.NoError(t, archive.WriteHeader(&tar.Header{
		Name: "bin/sh", Typeflag: tar.TypeSymlink, Linkname: "busybox", Mode: 0o777,
	}))
	require.NoError(t, archive.Close())
	return layer.Bytes()
}
