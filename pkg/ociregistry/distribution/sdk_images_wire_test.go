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

package distribution_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/ociregistry"
	"github.com/jeremyhahn/go-quicbaas/pkg/ociregistry/distribution"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/images"
)

// wireDescriptor is a content descriptor in a test manifest.
type wireDescriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int    `json:"size"`
}

// wireManifest is an OCI image manifest built by the test.
type wireManifest struct {
	SchemaVersion int              `json:"schemaVersion"`
	MediaType     string           `json:"mediaType"`
	ArtifactType  string           `json:"artifactType,omitempty"`
	Config        wireDescriptor   `json:"config"`
	Layers        []wireDescriptor `json:"layers"`
	Subject       *wireDescriptor  `json:"subject,omitempty"`
}

func wireRegistry(t *testing.T) http.Handler {
	t.Helper()
	files, err := ociregistry.NewFileStorageClient(t.TempDir())
	require.NoError(t, err)
	storage, err := ociregistry.NewQuicBaaSStorage(files)
	require.NoError(t, err)
	cfg := ociregistry.DefaultRegistryConfig()
	cfg.TLS.Enabled = false
	cfg.Storage.Encrypted = false
	cfg.Encryption.EncryptByDefault = false
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg, err := ociregistry.NewRegistry(cfg, logger, ociregistry.WithStorage(storage))
	require.NoError(t, err)
	t.Cleanup(func() { _ = reg.Close() })

	h, err := distribution.NewRegistryHandler(distribution.RegistryHandlerConfig{
		Logger:  logger,
		Backend: reg,
		Identity: func(*http.Request) (string, bool) {
			return "spiffe://example.test/workload/wire", true
		},
		Attestation: ociregistry.RegistryAttestationConfig{RequireAttestation: false},
	})
	require.NoError(t, err)
	return h
}

// TestSDKImagesWire pushes and pulls an image and a referrer through the
// real /v2/ handler with the SDK image client.
func TestSDKImagesWire(t *testing.T) {
	srv := httptest.NewServer(wireRegistry(t))
	t.Cleanup(srv.Close)
	c, err := images.NewClient(srv.URL, images.WithHTTPClient(srv.Client()))
	require.NoError(t, err)
	ctx := context.Background()
	const repo = "team/app"

	require.NoError(t, c.Ping(ctx))

	configBlob := []byte(`{"architecture":"amd64","os":"linux"}`)
	layerBlob := []byte("layer contents for the wire test")
	configDigest, err := c.PushBlob(ctx, repo, configBlob)
	require.NoError(t, err)
	layerDigest, err := c.PushBlob(ctx, repo, layerBlob)
	require.NoError(t, err)
	ok, err := c.BlobExists(ctx, repo, layerDigest)
	require.NoError(t, err)
	require.True(t, ok)

	image, err := json.Marshal(wireManifest{
		SchemaVersion: 2,
		MediaType:     images.MediaTypeOCIManifest,
		Config:        wireDescriptor{MediaType: "application/vnd.oci.image.config.v1+json", Digest: configDigest, Size: len(configBlob)},
		Layers:        []wireDescriptor{{MediaType: "application/vnd.oci.image.layer.v1.tar", Digest: layerDigest, Size: len(layerBlob)}},
	})
	require.NoError(t, err)
	pushed, err := c.PutManifest(ctx, repo, "v1", images.MediaTypeOCIManifest, image)
	require.NoError(t, err)
	require.NotEmpty(t, pushed.Digest)
	require.Empty(t, pushed.Subject)

	byTag, err := c.GetManifest(ctx, repo, "v1")
	require.NoError(t, err)
	byDigest, err := c.GetManifest(ctx, repo, pushed.Digest)
	require.NoError(t, err)
	require.Equal(t, image, byTag.Body)
	require.Equal(t, byTag.Body, byDigest.Body)
	require.Equal(t, images.MediaTypeOCIManifest, byTag.MediaType)
	require.Equal(t, byTag.MediaType, byDigest.MediaType)
	require.Equal(t, pushed.Digest, byTag.Digest)

	tags, err := c.Tags(ctx, repo)
	require.NoError(t, err)
	require.Contains(t, tags, "v1")
	repos, err := c.Catalog(ctx)
	require.NoError(t, err)
	require.Contains(t, repos, repo)

	const sigType = "application/vnd.example.signature.v1+json"
	referrer, err := json.Marshal(wireManifest{
		SchemaVersion: 2,
		MediaType:     images.MediaTypeOCIManifest,
		ArtifactType:  sigType,
		Config:        wireDescriptor{MediaType: "application/vnd.oci.empty.v1+json", Digest: configDigest, Size: len(configBlob)},
		Layers:        []wireDescriptor{{MediaType: "application/octet-stream", Digest: layerDigest, Size: len(layerBlob)}},
		Subject:       &wireDescriptor{MediaType: images.MediaTypeOCIManifest, Digest: pushed.Digest, Size: len(image)},
	})
	require.NoError(t, err)
	sig, err := c.PutManifest(ctx, repo, "sig", images.MediaTypeOCIManifest, referrer)
	require.NoError(t, err)
	require.Equal(t, pushed.Digest, sig.Subject)

	list, err := c.Referrers(ctx, repo, pushed.Digest, "")
	require.NoError(t, err)
	require.False(t, list.FiltersApplied)
	require.True(t, hasReferrer(list.Descriptors, sig.Digest), "referrers %+v", list.Descriptors)
	filtered, err := c.Referrers(ctx, repo, pushed.Digest, sigType)
	require.NoError(t, err)
	require.True(t, filtered.FiltersApplied)
	require.True(t, hasReferrer(filtered.Descriptors, sig.Digest), "referrers %+v", filtered.Descriptors)

	require.NoError(t, c.DeleteManifest(ctx, repo, sig.Digest))

	got, err := c.GetBlob(ctx, repo, layerDigest)
	require.NoError(t, err)
	require.Equal(t, layerBlob, got)
}

func hasReferrer(ds []images.Descriptor, digest string) bool {
	for _, d := range ds {
		if d.Digest == digest {
			return true
		}
	}
	return false
}
