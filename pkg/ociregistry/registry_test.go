// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package ociregistry

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testLogger returns a logger for tests.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Only log errors during tests
	}))
}

// validTestConfig returns a valid test configuration.
func validTestConfig() RegistryConfig {
	cfg := DefaultRegistryConfig()
	cfg.TLS.Enabled = false // Disable TLS for tests
	cfg.Storage.Encrypted = false
	// Tests opt in to encryption; the encrypt-by-default path has its own test.
	cfg.Encryption.EncryptByDefault = false
	return cfg
}

// newTestRegistry constructs a Registry backed by an in-memory mock
// QuicBaaSStorageClient so Push/Pull/Delete/List exercise the real
// storage code path instead of returning StorageNotConfiguredError.
// Used by the post-Phase-5 tests; the older tests that intentionally
// run without storage continue to call NewRegistry directly to
// exercise the unconfigured branch.
func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	storage, err := NewQuicBaaSStorage(newMockStorageClient())
	if err != nil {
		t.Fatalf("NewQuicBaaSStorage: %v", err)
	}
	reg, err := NewRegistry(validTestConfig(), testLogger(), WithStorage(storage))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })
	return reg
}

func TestNewRegistry(t *testing.T) {
	tests := []struct {
		name        string
		config      RegistryConfig
		logger      *slog.Logger
		expectError bool
	}{
		{
			name:        "valid config",
			config:      validTestConfig(),
			logger:      testLogger(),
			expectError: false,
		},
		{
			name:        "valid config nil logger",
			config:      validTestConfig(),
			logger:      nil, // Should use default logger
			expectError: false,
		},
		{
			name: "invalid config empty listen addr",
			config: func() RegistryConfig {
				cfg := validTestConfig()
				cfg.ListenAddr = ""
				return cfg
			}(),
			logger:      testLogger(),
			expectError: true,
		},
		{
			name: "invalid config zero timeout",
			config: func() RegistryConfig {
				cfg := validTestConfig()
				cfg.ReadTimeout = 0
				return cfg
			}(),
			logger:      testLogger(),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg, err := NewRegistry(tt.config, tt.logger)
			if tt.expectError {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if reg == nil {
				t.Error("expected non-nil registry")
				return
			}

			// Verify registry is initialized
			if !reg.initialized.Load() {
				t.Error("registry should be initialized")
			}

			// Cleanup
			if err := reg.Close(); err != nil {
				t.Errorf("failed to close registry: %v", err)
			}
		})
	}
}

func TestRegistryPush(t *testing.T) {
	reg := newTestRegistry(t)

	tests := []struct {
		name        string
		ref         string
		image       *Image
		opts        PushOptions
		expectError error
	}{
		{
			name: "valid push",
			ref:  "myrepo/myimage:v1",
			image: &Image{
				Manifest: &ImageManifest{Layers: []Descriptor{{Digest: sha256Digest([]byte("layer")), Size: 5}}},
				Layers:   []Layer{{Digest: sha256Digest([]byte("layer")), Size: 5, Content: []byte("layer")}},
			},
			opts:        PushOptions{},
			expectError: nil,
		},
		{
			name:        "no layers",
			ref:         "myrepo/myimage:v2",
			image:       &Image{Manifest: &ImageManifest{}},
			opts:        PushOptions{},
			expectError: ImageEmptyError{},
		},
		{
			name:        "empty reference",
			ref:         "",
			image:       &Image{},
			opts:        PushOptions{},
			expectError: InvalidImageRefError{},
		},
		{
			name:        "invalid reference uppercase",
			ref:         "MyRepo/MyImage:v1",
			image:       &Image{},
			opts:        PushOptions{},
			expectError: InvalidImageRefError{},
		},
		{
			name:        "nil image",
			ref:         "myrepo/myimage:v1",
			image:       nil,
			opts:        PushOptions{},
			expectError: InvalidManifestError{},
		},
		{
			name:        "reference too long",
			ref:         string(make([]byte, 300)),
			image:       &Image{},
			opts:        PushOptions{},
			expectError: InvalidImageRefError{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			err := reg.Push(ctx, tt.ref, tt.image, tt.opts)

			if tt.expectError == nil && err != nil {
				t.Fatalf("expected success, got: %v", err)
			}
			if tt.expectError != nil {
				if err == nil {
					t.Error("expected error but got nil")
					return
				}
				// Check error type matches
				switch tt.expectError.(type) {
				case InvalidImageRefError:
					var target InvalidImageRefError
					if !errors.As(err, &target) {
						t.Errorf("expected InvalidImageRefError, got: %T", err)
					}
				case InvalidManifestError:
					var target InvalidManifestError
					if !errors.As(err, &target) {
						t.Errorf("expected InvalidManifestError, got: %T", err)
					}
				case ImageEmptyError:
					var target ImageEmptyError
					if !errors.As(err, &target) {
						t.Errorf("expected ImageEmptyError, got: %T", err)
					}
				}
			}
		})
	}
}

func TestRegistryPushContextCancellation(t *testing.T) {
	reg, err := NewRegistry(validTestConfig(), testLogger())
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}
	defer func() { _ = reg.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err = reg.Push(ctx, "myrepo/myimage:v1", &Image{Manifest: &ImageManifest{}}, PushOptions{})
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestRegistryPull(t *testing.T) {
	reg := newTestRegistry(t)

	tests := []struct {
		name        string
		ref         string
		opts        PullOptions
		expectError error
	}{
		{
			name:        "image not found",
			ref:         "myrepo/myimage:v1",
			opts:        PullOptions{},
			expectError: ImageNotFoundError{},
		},
		{
			name:        "empty reference",
			ref:         "",
			opts:        PullOptions{},
			expectError: InvalidImageRefError{},
		},
		{
			name:        "invalid reference",
			ref:         "INVALID:tag",
			opts:        PullOptions{},
			expectError: InvalidImageRefError{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			_, err := reg.Pull(ctx, tt.ref, tt.opts)

			if err == nil {
				t.Error("expected error but got nil")
				return
			}

			switch tt.expectError.(type) {
			case InvalidImageRefError:
				var target InvalidImageRefError
				if !errors.As(err, &target) {
					t.Errorf("expected InvalidImageRefError, got: %T", err)
				}
			case ImageNotFoundError:
				var target ImageNotFoundError
				if !errors.As(err, &target) {
					t.Errorf("expected ImageNotFoundError, got: %T", err)
				}
			}
		})
	}
}

func TestRegistryPullContextCancellation(t *testing.T) {
	reg, err := NewRegistry(validTestConfig(), testLogger())
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}
	defer func() { _ = reg.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = reg.Pull(ctx, "myrepo/myimage:v1", PullOptions{})
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestRegistryDelete(t *testing.T) {
	reg := newTestRegistry(t)

	tests := []struct {
		name        string
		ref         string
		expectError error
	}{
		{
			name:        "image not found",
			ref:         "myrepo/myimage:v1",
			expectError: ImageNotFoundError{},
		},
		{
			name:        "empty reference",
			ref:         "",
			expectError: InvalidImageRefError{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			err := reg.Delete(ctx, tt.ref)

			if err == nil {
				t.Error("expected error but got nil")
				return
			}

			switch tt.expectError.(type) {
			case InvalidImageRefError:
				var target InvalidImageRefError
				if !errors.As(err, &target) {
					t.Errorf("expected InvalidImageRefError, got: %T", err)
				}
			case ImageNotFoundError:
				var target ImageNotFoundError
				if !errors.As(err, &target) {
					t.Errorf("expected ImageNotFoundError, got: %T", err)
				}
			}
		})
	}
}

func TestRegistryList(t *testing.T) {
	reg := newTestRegistry(t)

	ctx := context.Background()
	manifests, err := reg.List(ctx, RegistryListOptions{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(manifests) != 0 {
		t.Errorf("expected empty list, got %d items", len(manifests))
	}
}

// TestRegistryPushPullRoundTrip exercises the full Push -> List ->
// Pull -> Delete lifecycle against the in-memory storage. This is the
// production-path test that replaces the pre-Phase-5 stub-as-spec
// tests.
func TestRegistryPushPullRoundTrip(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()

	const ref = "myrepo/myimage:v1"
	layer := Layer{
		Digest:    sha256Digest([]byte("hello world")),
		MediaType: "application/vnd.oci.image.layer.v1.tar",
		Size:      11,
		Content:   []byte("hello world"),
	}
	configBlob := []byte(`{"architecture":"amd64"}`)
	manifest := &ImageManifest{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.manifest.v1+json",
		Config: Descriptor{
			MediaType: "application/vnd.oci.image.config.v1+json",
			Digest:    sha256Digest(configBlob),
			Size:      int64(len(configBlob)),
		},
		Layers: []Descriptor{{
			MediaType: layer.MediaType,
			Digest:    layer.Digest,
			Size:      layer.Size,
		}},
	}
	image := &Image{Manifest: manifest, Config: configBlob, Layers: []Layer{layer}}

	if err := reg.Push(ctx, ref, image, PushOptions{}); err != nil {
		t.Fatalf("Push: %v", err)
	}

	// Second push without AllowOverwrite must fail.
	if err := reg.Push(ctx, ref, image, PushOptions{}); err == nil {
		t.Fatal("expected ImageAlreadyExistsError on second push without overwrite")
	}

	// List should include the just-pushed manifest.
	list, err := reg.List(ctx, RegistryListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Reference != ref {
		t.Fatalf("List = %+v, want one manifest at %q", list, ref)
	}

	// Pull should round-trip the blob bytes.
	got, err := reg.Pull(ctx, ref, PullOptions{})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if string(got.Config) != string(configBlob) {
		t.Errorf("config mismatch: got %q want %q", got.Config, configBlob)
	}
	if len(got.Layers) != 1 || string(got.Layers[0].Content) != string(layer.Content) {
		t.Errorf("layer mismatch: got %+v", got.Layers)
	}

	// Delete + verify gone.
	if err := reg.Delete(ctx, ref); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := reg.Pull(ctx, ref, PullOptions{}); err == nil {
		t.Error("expected error after delete")
	}
	if err := reg.Delete(ctx, ref); err == nil {
		t.Error("expected ImageNotFoundError on second delete")
	}
}

// gatedManifestClient parks the first manifest Get until release is
// closed, so a test can hold one push inside its existence check.
type gatedManifestClient struct {
	*mockStorageClient
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (c *gatedManifestClient) Get(ctx context.Context, entityType, key string) ([]byte, bool, error) {
	if entityType == entityTypeManifest {
		first := false
		c.once.Do(func() { first = true })
		if first {
			close(c.entered)
			<-c.release
		}
	}
	return c.mockStorageClient.Get(ctx, entityType, key)
}

func competingPushImage(content string) *Image {
	layer := Layer{
		Digest:    sha256Digest([]byte(content)),
		MediaType: "application/vnd.oci.image.layer.v1.tar",
		Size:      int64(len(content)),
		Content:   []byte(content),
	}
	return &Image{
		Manifest: &ImageManifest{
			SchemaVersion: 2,
			MediaType:     "application/vnd.oci.image.manifest.v1+json",
			Layers:        []Descriptor{{MediaType: layer.MediaType, Digest: layer.Digest, Size: layer.Size}},
		},
		Layers: []Layer{layer},
	}
}

// TestRegistryPushNoOverwriteIsAtomic holds push A inside its existence
// check for an absent reference, starts push B (and a distribution PUT)
// of the same reference, and lets A finish only once both are parked on
// the reference: A succeeds, B is refused, and A's image stays.
func TestRegistryPushNoOverwriteIsAtomic(t *testing.T) {
	client := &gatedManifestClient{
		mockStorageClient: newMockStorageClient(),
		entered:           make(chan struct{}),
		release:           make(chan struct{}),
	}
	storage, err := NewQuicBaaSStorage(client)
	if err != nil {
		t.Fatalf("NewQuicBaaSStorage: %v", err)
	}
	reg, err := NewRegistry(validTestConfig(), testLogger(), WithStorage(storage))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })
	ctx := context.Background()
	const ref = "myrepo/race:v1"

	errA := make(chan error, 1)
	go func() { errA <- reg.Push(ctx, ref, competingPushImage("image A"), PushOptions{}) }()
	<-client.entered

	errB := make(chan error, 1)
	go func() { errB <- reg.Push(ctx, ref, competingPushImage("image B"), PushOptions{}) }()
	const ociManifest = "application/vnd.oci.image.manifest.v1+json"
	errPut := make(chan error, 1)
	go func() {
		_, err := reg.PutManifest(ctx, "myrepo/race", "v1", ociManifest,
			[]byte(`{"schemaVersion":2,"mediaType":"`+ociManifest+`","layers":[]}`))
		errPut <- err
	}()
	deadline := time.Now().Add(10 * time.Second)
	for reg.refLocks.waiting.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("competing writers did not wait on the reference (waiting=%d)", reg.refLocks.waiting.Load())
		}
		runtime.Gosched()
	}
	close(client.release)

	if err := <-errA; err != nil {
		t.Fatalf("push A: %v", err)
	}
	var exists ImageAlreadyExistsError
	if err := <-errB; !errors.As(err, &exists) {
		t.Fatalf("push B: want ImageAlreadyExistsError, got %v", err)
	}
	// The distribution PUT may overwrite a tag by design, but only after
	// A's check and store, never between them.
	if err := <-errPut; err != nil {
		t.Fatalf("PutManifest: %v", err)
	}
	if n := reg.refLocks.waiting.Load(); n != 0 {
		t.Errorf("waiting = %d after all writers finished", n)
	}
	reg.refLocks.mu.Lock()
	left := len(reg.refLocks.locks)
	reg.refLocks.mu.Unlock()
	if left != 0 {
		t.Errorf("%d reference locks left after all writers finished", left)
	}
}

// TestRegistryPush_Encrypted asserts the Encrypt opt is refused while
// encryption is deferred. This prevents the surface from silently
// landing unencrypted blobs when the caller asked for encryption.
func TestRegistryPush_Encrypted(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()
	err := reg.Push(ctx, "myrepo/x:v1", &Image{Manifest: &ImageManifest{}}, PushOptions{Encrypt: true})
	if err == nil {
		t.Fatal("expected EncryptionNotConfiguredError")
	}
	var enc EncryptionNotConfiguredError
	if !errors.As(err, &enc) {
		t.Errorf("expected EncryptionNotConfiguredError, got %T: %v", err, err)
	}
}

// TestRegistry_UnconfiguredStorage exercises the failure path when
// no storage was wired at construction. The Registry is a real
// boot-time misconfiguration sentinel here, not a silent no-op.
func TestRegistry_UnconfiguredStorage(t *testing.T) {
	reg, err := NewRegistry(validTestConfig(), testLogger())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	defer func() { _ = reg.Close() }()
	ctx := context.Background()
	if err := reg.Push(ctx, "x/y:1", &Image{Manifest: &ImageManifest{}}, PushOptions{}); err == nil {
		t.Error("expected StorageNotConfiguredError from Push")
	}
	if _, err := reg.Pull(ctx, "x/y:1", PullOptions{}); err == nil {
		t.Error("expected StorageNotConfiguredError from Pull")
	}
	if err := reg.Delete(ctx, "x/y:1"); err == nil {
		t.Error("expected StorageNotConfiguredError from Delete")
	}
	if _, err := reg.List(ctx, RegistryListOptions{}); err == nil {
		t.Error("expected StorageNotConfiguredError from List")
	}
}

func repeat64(c string) string {
	out := make([]byte, 64)
	for i := range out {
		out[i] = c[0]
	}
	return string(out)
}

// TestRegistry_EncryptedRoundTrip exercises the full lifecycle of an
// encrypted push: encrypted layer bytes land in storage, plaintext is
// never visible, and Pull transparently decrypts. The unencrypted
// content must come back byte-for-byte.
func TestRegistry_EncryptedRoundTrip(t *testing.T) {
	storage, err := NewQuicBaaSStorage(newMockStorageClient())
	if err != nil {
		t.Fatalf("NewQuicBaaSStorage: %v", err)
	}
	kmsClient, _ := newTestSDKKMSClient(t)
	enc, err := NewImageEncryptor(kmsClient)
	if err != nil {
		t.Fatalf("NewImageEncryptor: %v", err)
	}
	reg, err := NewRegistry(validTestConfig(), testLogger(), WithStorage(storage), WithEncryptor(enc))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })

	ctx := context.Background()
	plaintext := []byte("secret payload that must survive a round trip")
	digest := sha256Digest(plaintext)
	image := &Image{
		Manifest: &ImageManifest{
			SchemaVersion: 2,
			MediaType:     "application/vnd.oci.image.manifest.v1+json",
			Config: Descriptor{
				MediaType: "application/vnd.oci.image.config.v1+json",
				Digest:    sha256Digest([]byte("cfg!")),
				Size:      4,
			},
			Layers: []Descriptor{{
				MediaType: "application/vnd.oci.image.layer.v1.tar",
				Digest:    digest,
				Size:      int64(len(plaintext)),
			}},
		},
		Config: []byte("cfg!"),
		Layers: []Layer{{
			Digest:    digest,
			MediaType: "application/vnd.oci.image.layer.v1.tar",
			Size:      int64(len(plaintext)),
			Content:   plaintext,
		}},
	}

	ref := "encrypted/img:v1"
	if err := reg.Push(ctx, ref, image, PushOptions{Encrypt: true}); err != nil {
		t.Fatalf("Push: %v", err)
	}

	// The envelope is stored under the digest the layer descriptor
	// records, never the plaintext under the layer digest.
	stored, err := storage.GetManifest(ctx, ref)
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	envDigest := stored.Layers[0].Annotations[LayerAnnotationEncryptedBlob]
	if envDigest == "" || envDigest == digest {
		t.Fatalf("layer descriptor envelope digest = %q, want a digest other than %q", envDigest, digest)
	}
	storedBlob, err := storage.GetBlob(ctx, envDigest)
	if err != nil {
		t.Fatalf("GetBlob envelope: %v", err)
	}
	if got := sha256Digest(storedBlob); got != envDigest {
		t.Fatalf("stored envelope hashes to %s, stored under %s", got, envDigest)
	}
	if string(storedBlob) == string(plaintext) {
		t.Fatal("encrypted push left plaintext in storage")
	}
	if exists, err := storage.BlobExists(ctx, digest); err != nil || exists {
		t.Fatalf("blob under the plaintext layer digest: exists=%v err=%v", exists, err)
	}

	got, err := reg.Pull(ctx, ref, PullOptions{})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(got.Layers) != 1 || string(got.Layers[0].Content) != string(plaintext) {
		t.Errorf("encrypted round-trip lost plaintext: got %q want %q",
			got.Layers[0].Content, plaintext)
	}

	// Manifest annotations must record the encryption choice.
	manifest, err := storage.GetManifest(ctx, ref)
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if manifest.Annotations[ManifestAnnotationEncrypted] != "true" {
		t.Errorf("expected encrypted annotation, got %v", manifest.Annotations)
	}
}

// TestRegistry_EncryptedPushWithoutEncryptor verifies the refusal
// branch still works when the encryptor option is omitted.
func TestRegistry_EncryptedPushWithoutEncryptor(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()
	err := reg.Push(ctx, "no-enc/img:v1", &Image{Manifest: &ImageManifest{}}, PushOptions{Encrypt: true})
	if err == nil {
		t.Fatal("expected EncryptionNotConfiguredError")
	}
	var enc EncryptionNotConfiguredError
	if !errors.As(err, &enc) {
		t.Errorf("expected EncryptionNotConfiguredError, got %T: %v", err, err)
	}
}

// TestRegistry_DeleteGCsOrphanBlobs verifies that Delete reclaims
// blob storage for layers no longer referenced by any surviving
// manifest, and that layers shared between images survive deletion
// of one of the sharing images.
func TestRegistry_DeleteLeavesBlobs(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()

	a := digestImage("shared layer", "cfg-a")
	b := digestImage("shared layer", "cfg-b")
	require.NoError(t, reg.Push(ctx, "img-a:v1", a, PushOptions{}))
	require.NoError(t, reg.Push(ctx, "img-b:v1", b, PushOptions{}))

	require.NoError(t, reg.Delete(ctx, "img-a:v1"))

	for _, img := range []*Image{a, b} {
		for _, d := range []string{img.Layers[0].Digest, img.Manifest.Config.Digest} {
			exists, err := reg.storage.BlobExists(ctx, d)
			require.NoError(t, err)
			assert.True(t, exists, "blob %s was deleted", d)
		}
	}

	_, err := reg.Pull(ctx, "img-a:v1", PullOptions{})
	var notFound ImageNotFoundError
	assert.ErrorAs(t, err, &notFound)

	pulled, err := reg.Pull(ctx, "img-b:v1", PullOptions{})
	require.NoError(t, err)
	require.Len(t, pulled.Layers, 1)
	assert.Equal(t, b.Layers[0].Content, pulled.Layers[0].Content)
	assert.Equal(t, b.Config, pulled.Config)
}

// digestImage builds an image whose layer and config digests are the
// sha256 of their content.
func digestImage(layer, config string) *Image {
	const layerType = "application/vnd.oci.image.layer.v1.tar"
	ld := sha256Digest([]byte(layer))
	return &Image{
		Manifest: &ImageManifest{
			SchemaVersion: 2,
			MediaType:     "application/vnd.oci.image.manifest.v1+json",
			Config:        Descriptor{Digest: sha256Digest([]byte(config)), Size: int64(len(config))},
			Layers:        []Descriptor{{MediaType: layerType, Digest: ld, Size: int64(len(layer))}},
		},
		Config: []byte(config),
		Layers: []Layer{{Digest: ld, MediaType: layerType, Size: int64(len(layer)), Content: []byte(layer)}},
	}
}

func TestRegistryPush_RejectsDigestMismatch(t *testing.T) {
	victim := []byte("victim layer")
	victimDigest := sha256Digest(victim)

	tests := []struct {
		name   string
		mutate func(img *Image)
		check  func(t *testing.T, err error)
	}{
		{
			name: "layer digest is not the content hash",
			mutate: func(img *Image) {
				img.Layers[0].Digest = victimDigest
				img.Manifest.Layers[0].Digest = victimDigest
			},
			check: func(t *testing.T, err error) {
				var target DigestMismatchError
				assert.ErrorAs(t, err, &target)
			},
		},
		{
			name: "config digest is not the config hash",
			mutate: func(img *Image) {
				img.Manifest.Config.Digest = victimDigest
			},
			check: func(t *testing.T, err error) {
				var target DigestMismatchError
				assert.ErrorAs(t, err, &target)
			},
		},
		{
			name: "manifest descriptor names another layer",
			mutate: func(img *Image) {
				img.Manifest.Layers[0].Digest = victimDigest
			},
			check: func(t *testing.T, err error) {
				var target InvalidManifestError
				assert.ErrorAs(t, err, &target)
			},
		},
		{
			name: "non-sha256 layer digest",
			mutate: func(img *Image) {
				img.Layers[0].Digest = "sha512:" + repeat64("a")
				img.Manifest.Layers[0].Digest = img.Layers[0].Digest
			},
			check: func(t *testing.T, err error) {
				var target InvalidDigestError
				assert.ErrorAs(t, err, &target)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := newTestRegistry(t)
			ctx := context.Background()
			require.NoError(t, reg.storage.StoreBlob(ctx, victimDigest, victim))

			img := digestImage("attacker bytes", "cfg")
			tt.mutate(img)
			err := reg.Push(ctx, "evil/img:v1", img, PushOptions{})
			require.Error(t, err)
			tt.check(t, err)

			got, err := reg.storage.GetBlob(ctx, victimDigest)
			require.NoError(t, err)
			assert.Equal(t, victim, got, "the blob under the targeted digest changed")
			exists, err := reg.storage.ManifestExists(ctx, "evil/img:v1")
			require.NoError(t, err)
			assert.False(t, exists)
		})
	}
}

func TestRegistryPush_EncryptByDefault(t *testing.T) {
	cfg := DefaultRegistryConfig()
	cfg.TLS.Enabled = false
	cfg.Storage.Encrypted = false
	require.True(t, cfg.Encryption.EncryptByDefault)
	ctx := context.Background()

	storage, err := NewQuicBaaSStorage(newMockStorageClient())
	require.NoError(t, err)
	kmsClient, _ := newTestSDKKMSClient(t)
	enc, err := NewImageEncryptor(kmsClient)
	require.NoError(t, err)
	reg, err := NewRegistry(cfg, testLogger(), WithStorage(storage), WithEncryptor(enc))
	require.NoError(t, err)
	t.Cleanup(func() { _ = reg.Close() })

	img := digestImage("plain layer", "cfg")
	require.NoError(t, reg.Push(ctx, "enc/img:v1", img, PushOptions{}))
	m, err := storage.GetManifest(ctx, "enc/img:v1")
	require.NoError(t, err)
	assert.Equal(t, "true", m.Annotations[ManifestAnnotationEncrypted])
	envDigest := m.Layers[0].Annotations[LayerAnnotationEncryptedBlob]
	require.NotEmpty(t, envDigest)
	blob, err := storage.GetBlob(ctx, envDigest)
	require.NoError(t, err)
	assert.NotEqual(t, []byte("plain layer"), blob, "default push stored the plaintext")
	exists, err := storage.BlobExists(ctx, img.Layers[0].Digest)
	require.NoError(t, err)
	assert.False(t, exists, "default push stored a blob under the plaintext layer digest")

	plainStorage, err := NewQuicBaaSStorage(newMockStorageClient())
	require.NoError(t, err)
	noEnc, err := NewRegistry(cfg, testLogger(), WithStorage(plainStorage))
	require.NoError(t, err)
	t.Cleanup(func() { _ = noEnc.Close() })
	err = noEnc.Push(ctx, "enc/img:v1", digestImage("plain layer", "cfg"), PushOptions{})
	var target EncryptionNotConfiguredError
	assert.ErrorAs(t, err, &target)
}

func TestRegistry_DeleteKeepsSharedBlobWhenScanFails(t *testing.T) {
	client := newMockStorageClient()
	storage, err := NewQuicBaaSStorage(client)
	require.NoError(t, err)
	reg, err := NewRegistry(validTestConfig(), testLogger(), WithStorage(storage))
	require.NoError(t, err)
	t.Cleanup(func() { _ = reg.Close() })
	ctx := context.Background()

	a := digestImage("shared layer", "cfg")
	b := digestImage("shared layer", "cfg")
	require.NoError(t, reg.Push(ctx, "img-a:v1", a, PushOptions{}))
	require.NoError(t, reg.Push(ctx, "img-b:v1", b, PushOptions{}))
	shared := a.Layers[0].Digest

	// img-b's manifest still exists but can no longer be read.
	client.data[entityTypeManifest+":"+storage.manifestKey("img-b:v1")] = []byte("corrupt{")

	require.NoError(t, reg.Delete(ctx, "img-a:v1"))
	exists, err := storage.BlobExists(ctx, shared)
	require.NoError(t, err)
	assert.True(t, exists, "a blob an unreadable manifest may reference was collected")
}

func TestRegistryList_FiltersByRepository(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()
	for _, ref := range []string{"apps/web:v1", "apps/web:v2", "apps/web:latest", "apps/db:v1"} {
		require.NoError(t, reg.Push(ctx, ref, digestImage("layer "+ref, "cfg"), PushOptions{}))
	}

	refs := func(opts RegistryListOptions) []string {
		t.Helper()
		list, err := reg.List(ctx, opts)
		require.NoError(t, err)
		out := make([]string, 0, len(list))
		for _, m := range list {
			out = append(out, m.Reference)
		}
		return out
	}
	assert.ElementsMatch(t, []string{"apps/web:v1", "apps/web:v2", "apps/web:latest"},
		refs(RegistryListOptions{Repository: "apps/web"}))
	assert.ElementsMatch(t, []string{"apps/web:v1", "apps/web:v2"},
		refs(RegistryListOptions{Repository: "apps/web", TagPrefix: "v"}))
	assert.ElementsMatch(t, []string{"apps/db:v1"}, refs(RegistryListOptions{Repository: "apps/db"}))
}

func TestRegistryClose_PushBlockedOnSemaphore(t *testing.T) {
	cfg := validTestConfig()
	cfg.MaxConcurrentPushes = 1
	cfg.MaxConcurrentPulls = 1
	storage, err := NewQuicBaaSStorage(newMockStorageClient())
	require.NoError(t, err)
	reg, err := NewRegistry(cfg, testLogger(), WithStorage(storage))
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, reg.Push(ctx, "apps/web:v1", digestImage("layer", "cfg"), PushOptions{}))

	// Hold the only slot of each semaphore so Push and Pull block on it.
	reg.pushSemaphore <- struct{}{}
	reg.pullSemaphore <- struct{}{}

	pushErr := make(chan error, 1)
	pullErr := make(chan error, 1)
	go func() {
		pushErr <- reg.Push(ctx, "apps/web:v2", digestImage("layer 2", "cfg"), PushOptions{})
	}()
	go func() {
		_, err := reg.Pull(ctx, "apps/web:v1", PullOptions{})
		pullErr <- err
	}()
	// Give both goroutines time to reach the semaphore.
	time.Sleep(50 * time.Millisecond)

	require.NoError(t, reg.Close())
	<-reg.pushSemaphore
	<-reg.pullSemaphore

	var closed RegistryClosedError
	select {
	case err := <-pushErr:
		assert.ErrorAs(t, err, &closed)
	case <-time.After(5 * time.Second):
		t.Fatal("Push did not return after Close")
	}
	select {
	case err := <-pullErr:
		assert.ErrorAs(t, err, &closed)
	case <-time.After(5 * time.Second):
		t.Fatal("Pull did not return after Close")
	}
}

func TestRegistryVerifyClient(t *testing.T) {
	tests := []struct {
		name               string
		requireAttestation bool
		attestation        *AttestationEvidence
		allowedAIKs        []string
		requiredPCRs       map[int][]byte
		quoteMaxAge        time.Duration
		expectError        bool
		expectedErrorType  interface{}
	}{
		{
			name:               "attestation not required",
			requireAttestation: false,
			attestation:        nil,
			expectError:        false,
		},
		{
			name:               "attestation required but nil",
			requireAttestation: true,
			attestation:        nil,
			expectError:        true,
			expectedErrorType:  AttestationRequiredError{},
		},
		{
			name:               "attestation required and valid",
			requireAttestation: true,
			attestation: &AttestationEvidence{
				ClientID:  "client-1",
				Quote:     []byte("quote"),
				Signature: []byte("sig"),
				AIK:       []byte("aik-1-bytes-here"),
				Timestamp: time.Now(),
				PCRs:      map[int][]byte{},
			},
			quoteMaxAge: 10 * time.Minute,
			expectError: false,
		},
		{
			// A fresh-looking Timestamp does not make a quote fresh: only
			// a nonce the registry issued does.
			name:               "nonce never issued",
			requireAttestation: true,
			attestation: &AttestationEvidence{
				ClientID:  "client-1",
				Quote:     []byte("quote"),
				Signature: []byte("sig"),
				AIK:       []byte("aik-1-bytes"),
				Nonce:     []byte("never-issued"),
				Timestamp: time.Now().Add(time.Hour),
			},
			quoteMaxAge:       10 * time.Minute,
			expectError:       true,
			expectedErrorType: InvalidQuoteError{},
		},
		{
			name:               "PCR mismatch",
			requireAttestation: true,
			attestation: &AttestationEvidence{
				ClientID:  "client-1",
				Quote:     []byte("quote"),
				Signature: []byte("sig"),
				AIK:       []byte("aik-1-bytes"),
				Timestamp: time.Now(),
				PCRs:      map[int][]byte{7: {0x01, 0x02, 0x03}},
			},
			requiredPCRs:      map[int][]byte{7: {0x04, 0x05, 0x06}},
			quoteMaxAge:       10 * time.Minute,
			expectError:       true,
			expectedErrorType: PCRMismatchError{},
		},
		{
			name:               "PCR missing",
			requireAttestation: true,
			attestation: &AttestationEvidence{
				ClientID:  "client-1",
				Quote:     []byte("quote"),
				Signature: []byte("sig"),
				AIK:       []byte("aik-1-bytes"),
				Timestamp: time.Now(),
				PCRs:      map[int][]byte{}, // PCR 7 missing
			},
			requiredPCRs:      map[int][]byte{7: {0x01, 0x02, 0x03}},
			quoteMaxAge:       10 * time.Minute,
			expectError:       true,
			expectedErrorType: PCRMismatchError{},
		},
		{
			name:               "PCR match",
			requireAttestation: true,
			attestation: &AttestationEvidence{
				ClientID:  "client-1",
				Quote:     []byte("quote"),
				Signature: []byte("sig"),
				AIK:       []byte("aik-1-bytes"),
				Timestamp: time.Now(),
				PCRs:      map[int][]byte{7: {0x01, 0x02, 0x03}},
			},
			requiredPCRs: map[int][]byte{7: {0x01, 0x02, 0x03}},
			quoteMaxAge:  10 * time.Minute,
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validTestConfig()
			cfg.Attestation.RequireAttestation = tt.requireAttestation
			cfg.Attestation.AllowedAIKs = tt.allowedAIKs
			cfg.Attestation.RequiredPCRs = tt.requiredPCRs
			if tt.quoteMaxAge > 0 {
				cfg.Attestation.QuoteMaxAge = tt.quoteMaxAge
			}

			var verifiedPCRs map[int][]byte
			if tt.attestation != nil {
				verifiedPCRs = tt.attestation.PCRs
			}
			reg, err := NewRegistry(cfg, testLogger(), WithAttestor(pcrAttestor(verifiedPCRs)))
			if err != nil {
				t.Fatalf("failed to create registry: %v", err)
			}
			defer func() { _ = reg.Close() }()

			ctx := context.Background()
			if tt.attestation != nil && tt.attestation.Nonce == nil {
				issueNonce(t, reg, tt.attestation)
			}
			err = reg.VerifyClient(ctx, tt.attestation)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got nil")
					return
				}
				switch tt.expectedErrorType.(type) {
				case AttestationRequiredError:
					var target AttestationRequiredError
					if !errors.As(err, &target) {
						t.Errorf("expected AttestationRequiredError, got: %T (%v)", err, err)
					}
				case AIKNotAllowedError:
					var target AIKNotAllowedError
					if !errors.As(err, &target) {
						t.Errorf("expected AIKNotAllowedError, got: %T (%v)", err, err)
					}
				case InvalidQuoteError:
					var target InvalidQuoteError
					if !errors.As(err, &target) {
						t.Errorf("expected InvalidQuoteError, got: %T (%v)", err, err)
					}
				case PCRMismatchError:
					var target PCRMismatchError
					if !errors.As(err, &target) {
						t.Errorf("expected PCRMismatchError, got: %T (%v)", err, err)
					}
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// pcrAttestor is an attestor whose quote verification succeeds and
// reports pcrs as the verified PCR values.
func pcrAttestor(pcrs map[int][]byte) *mockAttestor {
	return &mockAttestor{verifyFunc: func(context.Context, []byte, []byte, []byte) (map[int][]byte, error) {
		return pcrs, nil
	}}
}

// issueNonce sets ev.Nonce to a challenge reg issued to ev.ClientID.
func issueNonce(t *testing.T, reg *Registry, ev *AttestationEvidence) {
	t.Helper()
	nonce, err := reg.IssueChallenge(context.Background(), ev.ClientID)
	require.NoError(t, err)
	ev.Nonce = nonce
}

// TestRegistryVerifyClient_ChallengeIsSingleUse proves freshness comes from
// the registry's challenge and not the caller's Timestamp: a verified quote
// replayed with a new Timestamp is refused, as are an expired challenge, one
// issued to another client, and one the quote does not carry.
func TestRegistryVerifyClient_ChallengeIsSingleUse(t *testing.T) {
	cfg := validTestConfig()
	cfg.Attestation.RequireAttestation = true
	cfg.Attestation.NonceValidityDuration = time.Minute

	attestor := &mockAttestor{verifyFunc: func(context.Context, []byte, []byte, []byte) (map[int][]byte, error) {
		return map[int][]byte{}, nil
	}}
	reg, err := NewRegistry(cfg, testLogger(), WithAttestor(attestor))
	require.NoError(t, err)
	defer func() { _ = reg.Close() }()
	current := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reg.now = func() time.Time { return current }
	ctx := context.Background()

	ev := &AttestationEvidence{
		ClientID: "client-1", Quote: []byte("quote"), Signature: []byte("sig"),
		AIK: []byte("aik-1-bytes"), Timestamp: current,
	}
	issueNonce(t, reg, ev)
	require.NoError(t, reg.VerifyClient(ctx, ev))

	var quoteErr InvalidQuoteError
	replay := *ev
	replay.Timestamp = current.Add(time.Hour)
	assert.ErrorAs(t, reg.VerifyClient(ctx, &replay), &quoteErr, "replayed quote")

	issueNonce(t, reg, ev)
	current = current.Add(time.Minute + time.Nanosecond)
	assert.ErrorAs(t, reg.VerifyClient(ctx, ev), &quoteErr, "expired challenge")

	other := *ev
	other.ClientID = "client-2"
	issueNonce(t, reg, &other)
	ev.Nonce = other.Nonce
	assert.ErrorAs(t, reg.VerifyClient(ctx, ev), &quoteErr, "another client's challenge")

	ev.Nonce = nil
	assert.ErrorAs(t, reg.VerifyClient(ctx, ev), &quoteErr, "no challenge")

	_, err = reg.IssueChallenge(ctx, "")
	var failed AttestationFailedError
	assert.ErrorAs(t, err, &failed)
}

func TestRegistryVerifyClient_FailsClosed(t *testing.T) {
	required := map[int][]byte{7: {0x01, 0x02, 0x03}}
	evidence := func() *AttestationEvidence {
		return &AttestationEvidence{
			ClientID:  "client-1",
			Quote:     []byte("quote"),
			Signature: []byte("sig"),
			AIK:       []byte("aik-1-bytes"),
			Timestamp: time.Now(),
			PCRs:      map[int][]byte{7: {0x01, 0x02, 0x03}},
		}
	}
	noQuote := evidence()
	noQuote.Quote = nil

	tests := []struct {
		name     string
		attestor Attestor
		evidence *AttestationEvidence
		check    func(t *testing.T, err error)
	}{
		{
			name:     "no attestor configured",
			evidence: evidence(),
			check: func(t *testing.T, err error) {
				var target AttestationFailedError
				assert.ErrorAs(t, err, &target)
			},
		},
		{
			name:     "empty quote",
			attestor: pcrAttestor(required),
			evidence: noQuote,
			check: func(t *testing.T, err error) {
				var target AttestationFailedError
				assert.ErrorAs(t, err, &target)
			},
		},
		{
			name: "attestor rejects the quote",
			attestor: &mockAttestor{verifyFunc: func(context.Context, []byte, []byte, []byte) (map[int][]byte, error) {
				return nil, errors.New("bad quote signature")
			}},
			evidence: evidence(),
			check: func(t *testing.T, err error) {
				var target InvalidQuoteError
				assert.ErrorAs(t, err, &target)
			},
		},
		{
			name:     "claimed PCR not in the verified quote",
			attestor: pcrAttestor(map[int][]byte{7: {0x09, 0x09, 0x09}}),
			evidence: evidence(),
			check: func(t *testing.T, err error) {
				var target PCRMismatchError
				assert.ErrorAs(t, err, &target)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validTestConfig()
			cfg.Attestation.RequireAttestation = true
			cfg.Attestation.RequiredPCRs = required
			var opts []RegistryOption
			if tt.attestor != nil {
				opts = append(opts, WithAttestor(tt.attestor))
			}
			reg, err := NewRegistry(cfg, testLogger(), opts...)
			require.NoError(t, err)
			defer func() { _ = reg.Close() }()

			issueNonce(t, reg, tt.evidence)
			err = reg.VerifyClient(context.Background(), tt.evidence)
			require.Error(t, err)
			tt.check(t, err)
			_, ok := reg.GetSession("client-1")
			assert.False(t, ok, "no session may be cached on failure")
		})
	}
}

func TestRegistrySessionManagement(t *testing.T) {
	cfg := validTestConfig()
	cfg.Attestation.RequireAttestation = true
	cfg.Attestation.CacheSessionDuration = 1 * time.Hour

	reg, err := NewRegistry(cfg, testLogger(), WithAttestor(pcrAttestor(map[int][]byte{})))
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}
	defer func() { _ = reg.Close() }()

	// Verify a client
	attestation := &AttestationEvidence{
		ClientID:  "client-1",
		Quote:     []byte("quote"),
		Signature: []byte("sig"),
		AIK:       []byte("aik-1-bytes-here-long"),
		Timestamp: time.Now(),
		PCRs:      map[int][]byte{},
	}

	ctx := context.Background()
	issueNonce(t, reg, attestation)
	if err := reg.VerifyClient(ctx, attestation); err != nil {
		t.Fatalf("failed to verify client: %v", err)
	}

	// Session should exist
	session, ok := reg.GetSession("client-1")
	if !ok {
		t.Error("expected session to exist")
	}
	if session.ClientID != "client-1" {
		t.Errorf("expected client ID 'client-1', got: %s", session.ClientID)
	}

	// Invalidate session
	reg.InvalidateSession("client-1")

	// Session should not exist
	_, ok = reg.GetSession("client-1")
	if ok {
		t.Error("expected session to be invalidated")
	}
}

func TestRegistrySessionExpiration(t *testing.T) {
	cfg := validTestConfig()
	cfg.Attestation.RequireAttestation = true
	cfg.Attestation.CacheSessionDuration = time.Minute

	reg, err := NewRegistry(cfg, testLogger(), WithAttestor(pcrAttestor(map[int][]byte{})))
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}
	defer func() { _ = reg.Close() }()

	current := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reg.now = func() time.Time { return current }

	// Verify a client
	attestation := &AttestationEvidence{
		ClientID:  "client-1",
		Quote:     []byte("quote"),
		Signature: []byte("sig"),
		AIK:       []byte("aik-1-bytes-here"),
		Timestamp: current,
		PCRs:      map[int][]byte{},
	}

	ctx := context.Background()
	issueNonce(t, reg, attestation)
	if err := reg.VerifyClient(ctx, attestation); err != nil {
		t.Fatalf("failed to verify client: %v", err)
	}

	// Still valid at exactly the session duration: expiry is strictly after
	// ValidUntil.
	current = current.Add(time.Minute)
	session, ok := reg.GetSession("client-1")
	if !ok {
		t.Fatal("expected session to be valid at exactly its duration")
	}
	if !session.ValidUntil.Equal(current) {
		t.Errorf("ValidUntil = %v, want %v", session.ValidUntil, current)
	}

	// Expired one tick later, and evicted on that read.
	current = current.Add(time.Nanosecond)
	if _, ok := reg.GetSession("client-1"); ok {
		t.Error("expected session to be expired")
	}
	if _, ok := reg.sessions.Load("client-1"); ok {
		t.Error("expected expired session to be evicted")
	}
}

func TestRegistryEncryptImage(t *testing.T) {
	reg, err := NewRegistry(validTestConfig(), testLogger())
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}
	defer func() { _ = reg.Close() }()

	tests := []struct {
		name        string
		ref         string
		keyID       string
		expectError bool
	}{
		{
			name:        "image not found",
			ref:         "myrepo/myimage:v1",
			keyID:       "my-key",
			expectError: true,
		},
		{
			name:        "empty reference",
			ref:         "",
			keyID:       "my-key",
			expectError: true,
		},
		{
			name:        "empty key ID",
			ref:         "myrepo/myimage:v1",
			keyID:       "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			err := reg.EncryptImage(ctx, tt.ref, tt.keyID)

			if tt.expectError && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestRegistryDecryptImage(t *testing.T) {
	reg, err := NewRegistry(validTestConfig(), testLogger())
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}
	defer func() { _ = reg.Close() }()

	tests := []struct {
		name        string
		ref         string
		expectError bool
	}{
		{
			name:        "image not found",
			ref:         "myrepo/myimage:v1",
			expectError: true,
		},
		{
			name:        "empty reference",
			ref:         "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			_, err := reg.DecryptImage(ctx, tt.ref)

			if tt.expectError && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestRegistryClose(t *testing.T) {
	reg, err := NewRegistry(validTestConfig(), testLogger())
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	// First close should succeed
	if err := reg.Close(); err != nil {
		t.Errorf("first close failed: %v", err)
	}

	// Second close should return error
	err = reg.Close()
	var target RegistryClosedError
	if !errors.As(err, &target) {
		t.Errorf("expected RegistryClosedError, got: %T (%v)", err, err)
	}
}

func TestRegistryOperationsAfterClose(t *testing.T) {
	reg, err := NewRegistry(validTestConfig(), testLogger())
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	if err := reg.Close(); err != nil {
		t.Fatalf("failed to close registry: %v", err)
	}

	ctx := context.Background()

	// All operations should fail
	var target RegistryClosedError

	err = reg.Push(ctx, "test:latest", &Image{}, PushOptions{})
	if !errors.As(err, &target) {
		t.Errorf("Push: expected RegistryClosedError, got: %T", err)
	}

	_, err = reg.Pull(ctx, "test:latest", PullOptions{})
	if !errors.As(err, &target) {
		t.Errorf("Pull: expected RegistryClosedError, got: %T", err)
	}

	err = reg.Delete(ctx, "test:latest")
	if !errors.As(err, &target) {
		t.Errorf("Delete: expected RegistryClosedError, got: %T", err)
	}

	_, err = reg.List(ctx, RegistryListOptions{})
	if !errors.As(err, &target) {
		t.Errorf("List: expected RegistryClosedError, got: %T", err)
	}

	err = reg.VerifyClient(ctx, &AttestationEvidence{})
	if !errors.As(err, &target) {
		t.Errorf("VerifyClient: expected RegistryClosedError, got: %T", err)
	}

	err = reg.EncryptImage(ctx, "test:latest", "key")
	if !errors.As(err, &target) {
		t.Errorf("EncryptImage: expected RegistryClosedError, got: %T", err)
	}

	_, err = reg.DecryptImage(ctx, "test:latest")
	if !errors.As(err, &target) {
		t.Errorf("DecryptImage: expected RegistryClosedError, got: %T", err)
	}
}

func TestValidateImageRef(t *testing.T) {
	tests := []struct {
		name        string
		ref         string
		expectError bool
	}{
		{"simple name", "alpine", false},
		{"with tag", "alpine:latest", false},
		{"with repository", "myrepo/myimage", false},
		{"with repository and tag", "myrepo/myimage:v1.2.3", false},
		{"with digest", "myrepo/myimage@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abcd", false},
		{"with registry", "registry.example.com/myrepo/myimage:v1", false},
		{"with port", "localhost:5000/myimage:v1", false},
		{"empty", "", true},
		{"too long", string(make([]byte, 300)), true},
		{"uppercase", "MyRepo/MyImage:v1", true},
		{"special chars", "my-repo/my_image:v1", false},
		{"dots in name", "my.repo/my.image:v1", false},
		// OCI distribution-spec repository and tag grammar.
		{"double underscore separator", "my__repo/image:v1", false},
		{"hyphen run separator", "my---repo/image:v1", false},
		{"triple underscore separator", "my___repo/image:v1", true},
		{"trailing separator", "myrepo-/image:v1", true},
		{"integration name", "grpc-1792717075-push-basic-20261023005754-333050821:v1", false},
		{"mixed-case integration name", "gRPC-1792717075-push-basic-20261023005754-333050821:v1", true},
		{"128-char tag", "alpine:" + strings.Repeat("a", 128), false},
		{"129-char tag", "alpine:" + strings.Repeat("a", 129), true},
		{"tag starting with dot", "alpine:.v1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateImageRef(tt.ref)
			if tt.expectError && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestRegistryConcurrentOperations(t *testing.T) {
	cfg := validTestConfig()
	cfg.MaxConcurrentPulls = 10
	cfg.MaxConcurrentPushes = 10

	reg, err := NewRegistry(cfg, testLogger())
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}
	defer func() { _ = reg.Close() }()

	var wg sync.WaitGroup
	ctx := context.Background()

	// Start multiple concurrent pulls and pushes
	for i := 0; i < 20; i++ {
		wg.Add(2)

		go func(idx int) {
			defer wg.Done()
			_, _ = reg.Pull(ctx, "test:latest", PullOptions{})
		}(i)

		go func(idx int) {
			defer wg.Done()
			_ = reg.Push(ctx, "test:latest", &Image{Manifest: &ImageManifest{}}, PushOptions{})
		}(i)
	}

	wg.Wait()
}

// Ensure TrustedRegistry interface is properly implemented.
func TestInterfaceImplementation(t *testing.T) {
	var _ TrustedRegistry = (*Registry)(nil)
}
