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
	"path/filepath"
	"slices"
	"strings"

	"go.podman.io/image/v5/copy"
	"go.podman.io/image/v5/docker"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/signature"
	istorage "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// newDefaultBackends builds the production backends: libcontainer state
// under RunRoot/libcontainer, and image and container layers in a
// go.podman.io/storage store rooted at ImageStorePath (Root/storage when
// empty) with its run state under RunRoot/storage.
func newDefaultBackends(config *RuntimeConfig) (*backends, error) {
	graphRoot := config.ImageStorePath
	if graphRoot == "" {
		graphRoot = filepath.Join(config.Root, "storage")
	}

	store, err := storage.GetStore(storage.StoreOptions{
		RunRoot:         filepath.Join(config.RunRoot, "storage"),
		GraphRoot:       graphRoot,
		GraphDriverName: config.GraphDriverName,
	})
	if err != nil {
		return nil, err
	}

	images := &storageImageStore{
		store: store,
		system: types.SystemContext{
			BigFilesTemporaryDir: config.TmpDir,
			BlobInfoCacheDir:     filepath.Join(graphRoot, "cache"),
			SignaturePolicyPath:  config.SignaturePolicyPath,
		},
		insecureRegistries: config.InsecureRegistries,
	}

	return &backends{
		engine: newLibcontainerEngine(filepath.Join(config.RunRoot, "libcontainer")),
		images: images,
		rootfs: &storageRootfsStore{store: store},
		close: func() error {
			_, err := store.Shutdown(true)
			return err
		},
	}, nil
}

// storageImageStore is the imageStore backed by go.podman.io/image/v5
// copying from registries into a go.podman.io/storage store.
type storageImageStore struct {
	store  storage.Store
	system types.SystemContext

	// insecureRegistries are registry hosts pulled from without TLS
	// verification.
	insecureRegistries []string
}

// Pull copies ref from its registry into the store. The signature policy
// (SignaturePolicyPath, else the host's /etc/containers/policy.json) decides
// whether the image is admitted.
func (s *storageImageStore) Pull(ctx context.Context, ref string) (record imageRecord, err error) {
	named, err := normalizeImageReference(ref)
	if err != nil {
		return imageRecord{}, err
	}

	source, err := docker.ParseReference("//" + named.String())
	if err != nil {
		return imageRecord{}, err
	}
	destination, err := istorage.Transport.ParseStoreReference(s.store, named.String())
	if err != nil {
		return imageRecord{}, err
	}

	if _, err := s.copyImage(ctx, destination, source, s.systemFor(named)); err != nil {
		return imageRecord{}, err
	}

	return s.Inspect(ctx, ref)
}

// Push copies the stored image ref to the registry it names, signing in
// with auth when it is set, and returns the digest of the manifest the
// registry now holds. An image that is not stored is an error matching
// ErrImageNotFound.
func (s *storageImageStore) Push(ctx context.Context, ref string, auth *orchestration.RegistryAuth) (string, error) {
	named, err := normalizeImageReference(ref)
	if err != nil {
		return "", err
	}
	if _, err := s.resolve(ref); err != nil {
		return "", err
	}

	source, err := istorage.Transport.ParseStoreReference(s.store, named.String())
	if err != nil {
		return "", err
	}
	destination, err := docker.ParseReference("//" + named.String())
	if err != nil {
		return "", err
	}

	system := s.systemFor(named)
	if auth != nil {
		system.DockerAuthConfig = &types.DockerAuthConfig{
			Username: auth.Username,
			Password: string(auth.Secret),
		}
	}
	written, err := s.copyImage(ctx, destination, source, system)
	if err != nil {
		return "", err
	}
	digest, err := manifest.Digest(written)
	if err != nil {
		return "", err
	}
	return digest.String(), nil
}

// systemFor returns the system context for talking to the registry named
// carries, without TLS verification when that registry is insecure.
func (s *storageImageStore) systemFor(named reference.Named) types.SystemContext {
	system := s.system
	if slices.Contains(s.insecureRegistries, reference.Domain(named)) {
		system.DockerInsecureSkipTLSVerify = types.NewOptionalBool(true)
	}
	return system
}

// copyImage copies source to destination under the signature policy and
// returns the manifest written to destination.
func (s *storageImageStore) copyImage(ctx context.Context, destination, source types.ImageReference, system types.SystemContext) (written []byte, err error) {
	policy, err := signature.DefaultPolicy(&system)
	if err != nil {
		return nil, err
	}
	policyContext, err := signature.NewPolicyContext(policy)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, policyContext.Destroy())
	}()

	return copy.Image(ctx, policyContext, destination, source, &copy.Options{
		SourceCtx:      &system,
		DestinationCtx: &system,
	})
}

// Inspect returns the stored image ref names.
func (s *storageImageStore) Inspect(ctx context.Context, ref string) (imageRecord, error) {
	image, err := s.resolve(ref)
	if err != nil {
		return imageRecord{}, err
	}

	size, err := s.store.ImageSize(image.ID)
	if err != nil {
		return imageRecord{}, err
	}

	digests := make([]string, 0, len(image.Digests)+1)
	for _, d := range image.Digests {
		digests = append(digests, d.String())
	}
	if image.Digest != "" && !slices.Contains(digests, image.Digest.String()) {
		digests = append(digests, image.Digest.String())
	}

	config, err := s.imageConfig(ctx, image.ID)
	if err != nil {
		return imageRecord{}, err
	}

	return imageRecord{
		ID:      image.ID,
		Names:   image.Names,
		Digests: digests,
		Size:    size,
		Created: image.Created,
		Config:  config,
	}, nil
}

// imageConfig reads the stored image's OCI configuration.
func (s *storageImageStore) imageConfig(ctx context.Context, id string) (config imageConfig, err error) {
	ref, err := istorage.Transport.NewStoreReference(s.store, nil, id)
	if err != nil {
		return imageConfig{}, err
	}
	image, err := ref.NewImage(ctx, &s.system)
	if err != nil {
		return imageConfig{}, err
	}
	defer func() {
		err = errors.Join(err, image.Close())
	}()

	oci, err := image.OCIConfig(ctx)
	if err != nil {
		return imageConfig{}, err
	}
	return imageConfig{
		Entrypoint: oci.Config.Entrypoint,
		Cmd:        oci.Config.Cmd,
		Env:        oci.Config.Env,
		WorkingDir: oci.Config.WorkingDir,
		User:       oci.Config.User,
		Labels:     oci.Config.Labels,
	}, nil
}

// Remove deletes the stored image ref names. With force, an image that is
// not stored is not an error.
func (s *storageImageStore) Remove(ctx context.Context, ref string, force bool) error {
	image, err := s.resolve(ref)
	if err != nil {
		if force && errors.Is(err, ErrImageNotFound) {
			return nil
		}
		return err
	}

	_, err = s.store.DeleteImage(image.ID, true)
	return err
}

func (s *storageImageStore) Tag(ctx context.Context, source, target string) error {
	named, err := normalizeImageReference(target)
	if err != nil {
		return err
	}
	var image *storage.Image
	if id, byID := strings.CutPrefix(source, "sha256:"); byID {
		image, err = s.store.Image(id)
		if errors.Is(err, storage.ErrImageUnknown) {
			err = errors.Join(ErrImageNotFound, err)
		}
	} else {
		image, err = s.resolve(source)
	}
	if err != nil {
		return err
	}
	return s.store.AddNames(image.ID, []string{named.String()})
}

// resolve finds the stored image ref names. A missing image is an error
// matching ErrImageNotFound.
func (s *storageImageStore) resolve(ref string) (*storage.Image, error) {
	named, err := normalizeImageReference(ref)
	if err != nil {
		return nil, err
	}
	storeRef, err := istorage.Transport.ParseStoreReference(s.store, named.String())
	if err != nil {
		return nil, err
	}

	_, image, err := istorage.ResolveReference(storeRef)
	if errors.Is(err, istorage.ErrNoSuchImage) {
		return nil, errors.Join(ErrImageNotFound, err)
	}
	return image, err
}

// normalizeImageReference parses a familiar image reference ("alpine",
// "registry:5000/app@sha256:...") into a fully qualified one, tagged latest
// when it names neither a tag nor a digest.
func normalizeImageReference(ref string) (reference.Named, error) {
	named, err := reference.ParseNormalizedNamed(ref)
	if err != nil {
		return nil, err
	}
	return reference.TagNameOnly(named), nil
}

// storageRootfsStore is the rootfsStore backed by go.podman.io/storage
// container layers.
type storageRootfsStore struct {
	store storage.Store
}

// Create makes a read-write container layer on imageID and mounts it.
func (s *storageRootfsStore) Create(ctx context.Context, containerID, imageID string) (string, error) {
	if _, err := s.store.CreateContainer(containerID, nil, imageID, "", "", nil); err != nil {
		return "", err
	}

	mountPoint, err := s.store.Mount(containerID, "")
	if err != nil {
		return "", errors.Join(err, s.store.DeleteContainer(containerID))
	}
	return mountPoint, nil
}

// Remove unmounts and deletes a container layer.
func (s *storageRootfsStore) Remove(containerID string) error {
	// A reclaim retried after a removal that got this far finds the layer or
	// the container already gone, and that counts as removed. A missing layer
	// does not end the removal: the container record may outlive it.
	if _, err := s.store.Unmount(containerID, true); err != nil &&
		!errors.Is(err, storage.ErrContainerUnknown) && !errors.Is(err, storage.ErrLayerUnknown) {
		return err
	}
	if err := s.store.DeleteContainer(containerID); err != nil &&
		!errors.Is(err, storage.ErrNotAContainer) && !errors.Is(err, storage.ErrContainerUnknown) {
		return err
	}
	return nil
}
