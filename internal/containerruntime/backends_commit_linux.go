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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/opencontainers/go-digest"
	imgspec "github.com/opencontainers/image-spec/specs-go"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/image/v5/copy"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/oci/layout"
	"go.podman.io/image/v5/signature"
	istorage "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/archive"
)

// storageLayerCommitter stores build containers' read-write layers as images
// in a go.podman.io/storage store.
type storageLayerCommitter struct {
	store storage.Store

	// system is the image store's system context; its BigFilesTemporaryDir
	// holds the image layout a commit writes before copying it into store.
	system types.SystemContext
}

// Commit implements layerCommitter. It writes the new image as an OCI image
// layout holding the new layer, config and manifest, and copies that layout
// into the store through the containers-storage destination a pull uses. The
// base image's layers are not in the layout: the copy reuses them from the
// store by digest.
func (c *storageLayerCommitter) Commit(ctx context.Context, containerID string, config imageConfig, createdBy string) (record imageRecord, err error) {
	container, err := c.store.Container(containerID)
	if err != nil {
		return imageRecord{}, fmt.Errorf("commit container %s: %w", containerID, err)
	}

	oci, layers, err := c.baseImage(ctx, container.ImageID)
	if err != nil {
		return imageRecord{}, fmt.Errorf("commit container %s: base image %s: %w", containerID, container.ImageID, err)
	}

	dir, err := os.MkdirTemp(c.system.BigFilesTemporaryDir, "commit-")
	if err != nil {
		return imageRecord{}, err
	}
	defer func() {
		err = errors.Join(err, os.RemoveAll(dir))
	}()

	layer, err := c.writeDiff(dir, container.LayerID)
	if err != nil {
		return imageRecord{}, fmt.Errorf("commit container %s: %w", containerID, err)
	}

	now := time.Now().UTC()
	oci.Created = &now
	oci.Config.Entrypoint = config.Entrypoint
	oci.Config.Cmd = config.Cmd
	oci.Config.Env = config.Env
	oci.Config.WorkingDir = config.WorkingDir
	oci.Config.User = config.User
	oci.Config.Labels = config.Labels
	oci.RootFS.Type = "layers"
	oci.RootFS.DiffIDs = append(oci.RootFS.DiffIDs, layer.Digest)
	oci.History = append(oci.History, imgspecv1.History{Created: &now, CreatedBy: createdBy})

	configDesc, err := writeLayoutJSON(dir, imgspecv1.MediaTypeImageConfig, oci)
	if err != nil {
		return imageRecord{}, err
	}
	manifestDesc, err := writeLayoutJSON(dir, imgspecv1.MediaTypeImageManifest, imgspecv1.Manifest{
		Versioned: imgspec.Versioned{SchemaVersion: 2},
		MediaType: imgspecv1.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    append(layers, layer),
	})
	if err != nil {
		return imageRecord{}, err
	}
	if err := writeLayoutIndex(dir, manifestDesc); err != nil {
		return imageRecord{}, err
	}

	id := configDesc.Digest.Encoded()
	if err := c.copyLayout(ctx, dir, id); err != nil {
		return imageRecord{}, fmt.Errorf("commit container %s: %w", containerID, err)
	}
	images := &storageImageStore{store: c.store, system: c.system}
	return images.Inspect(ctx, "sha256:"+id)
}

// baseImage returns the OCI configuration and manifest layer descriptors of
// the stored image id, or an empty linux configuration for no image.
func (c *storageLayerCommitter) baseImage(ctx context.Context, id string) (oci *imgspecv1.Image, layers []imgspecv1.Descriptor, err error) {
	if id == "" {
		return &imgspecv1.Image{
			Platform: imgspecv1.Platform{OS: "linux", Architecture: runtime.GOARCH},
		}, nil, nil
	}

	ref, err := istorage.Transport.NewStoreReference(c.store, nil, id)
	if err != nil {
		return nil, nil, err
	}
	image, err := ref.NewImage(ctx, &c.system)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		err = errors.Join(err, image.Close())
	}()

	oci, err = image.OCIConfig(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, info := range image.LayerInfos() {
		layers = append(layers, imgspecv1.Descriptor{
			MediaType:   ociLayerMediaType(info.MediaType),
			Digest:      info.Digest,
			Size:        info.Size,
			Annotations: info.Annotations,
		})
	}
	return oci, layers, nil
}

// ociLayerMediaType returns the OCI media type of a layer a Docker schema 2
// manifest gives mediaType; an OCI media type is returned unchanged.
func ociLayerMediaType(mediaType string) string {
	switch mediaType {
	case manifest.DockerV2Schema2LayerMediaType:
		return imgspecv1.MediaTypeImageLayerGzip
	case manifest.DockerV2SchemaLayerMediaTypeUncompressed:
		return imgspecv1.MediaTypeImageLayer
	case manifest.DockerV2SchemaLayerMediaTypeZstd:
		return imgspecv1.MediaTypeImageLayerZstd
	}
	return mediaType
}

// writeDiff writes layerID's changes against its parent, uncompressed, as a
// blob in the image layout dir and returns its layer descriptor. The layer is
// uncompressed, so its digest is also its diff ID.
func (c *storageLayerCommitter) writeDiff(dir, layerID string) (desc imgspecv1.Descriptor, err error) {
	uncompressed := archive.Uncompressed
	diff, err := c.store.Diff("", layerID, &storage.DiffOptions{Compression: &uncompressed})
	if err != nil {
		return imgspecv1.Descriptor{}, err
	}
	defer func() {
		err = errors.Join(err, diff.Close())
	}()

	blobs := filepath.Join(dir, "blobs", "sha256")
	if err := os.MkdirAll(blobs, 0o755); err != nil {
		return imgspecv1.Descriptor{}, err
	}
	file, err := os.CreateTemp(blobs, "layer-")
	if err != nil {
		return imgspecv1.Descriptor{}, err
	}
	digester := digest.Canonical.Digester()
	size, err := io.Copy(io.MultiWriter(file, digester.Hash()), diff)
	err = errors.Join(err, file.Close())
	if err != nil {
		return imgspecv1.Descriptor{}, err
	}
	if err := os.Rename(file.Name(), filepath.Join(blobs, digester.Digest().Encoded())); err != nil {
		return imgspecv1.Descriptor{}, err
	}
	return imgspecv1.Descriptor{
		MediaType: imgspecv1.MediaTypeImageLayer,
		Digest:    digester.Digest(),
		Size:      size,
	}, nil
}

// writeLayoutJSON writes value as a JSON blob in the image layout dir and
// returns its descriptor.
func writeLayoutJSON(dir, mediaType string, value any) (imgspecv1.Descriptor, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return imgspecv1.Descriptor{}, err
	}
	d := digest.FromBytes(data)
	blobs := filepath.Join(dir, "blobs", "sha256")
	if err := os.MkdirAll(blobs, 0o755); err != nil {
		return imgspecv1.Descriptor{}, err
	}
	if err := os.WriteFile(filepath.Join(blobs, d.Encoded()), data, 0o644); err != nil {
		return imgspecv1.Descriptor{}, err
	}
	return imgspecv1.Descriptor{MediaType: mediaType, Digest: d, Size: int64(len(data))}, nil
}

// writeLayoutIndex makes dir an OCI image layout whose one image is the
// manifest desc.
func writeLayoutIndex(dir string, desc imgspecv1.Descriptor) error {
	layoutFile, err := json.Marshal(imgspecv1.ImageLayout{Version: imgspecv1.ImageLayoutVersion})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, imgspecv1.ImageLayoutFile), layoutFile, 0o644); err != nil {
		return err
	}
	index, err := json.Marshal(imgspecv1.Index{
		Versioned: imgspec.Versioned{SchemaVersion: 2},
		MediaType: imgspecv1.MediaTypeImageIndex,
		Manifests: []imgspecv1.Descriptor{desc},
	})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, imgspecv1.ImageIndexFile), index, 0o644)
}

// copyLayout copies the image layout dir into the store as the image id.
// The image is this build's own content, so the copy accepts it without the
// configured signature policy, which governs what is pulled from registries.
func (c *storageLayerCommitter) copyLayout(ctx context.Context, dir, id string) (err error) {
	source, err := layout.NewReference(dir, "")
	if err != nil {
		return err
	}
	destination, err := istorage.Transport.NewStoreReference(c.store, nil, id)
	if err != nil {
		return err
	}
	policyContext, err := signature.NewPolicyContext(&signature.Policy{
		Default: signature.PolicyRequirements{signature.NewPRInsecureAcceptAnything()},
	})
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, policyContext.Destroy())
	}()

	system := c.system
	_, err = copy.Image(ctx, policyContext, destination, source, &copy.Options{
		SourceCtx:      &system,
		DestinationCtx: &system,
	})
	return err
}
