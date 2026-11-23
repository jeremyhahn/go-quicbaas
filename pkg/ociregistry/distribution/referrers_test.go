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

package distribution

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	sigType  = "application/vnd.example.signature"
	sbomType = "application/vnd.example.sbom"
)

// referrerManifest is an artifact manifest of artifactType whose
// subject is subject.
func referrerManifest(artifactType, subject string) []byte {
	return []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json",` +
		`"artifactType":"` + artifactType + `",` +
		`"config":{"mediaType":"application/vnd.oci.empty.v1+json","digest":"sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a","size":2},` +
		`"layers":[],"subject":{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"` + subject + `","size":10}}`)
}

func decodeIndex(t *testing.T, body []byte) referrersIndex {
	t.Helper()
	var idx referrersIndex
	require.NoError(t, json.Unmarshal(body, &idx))
	return idx
}

// TestOCIDist_Referrers pushes an image and two artifacts that refer
// to it, then lists the referrers, unfiltered and by artifactType.
func TestOCIDist_Referrers(t *testing.T) {
	backend := newFakeOCIBackend()
	router := newTestRouter(t, backend, openGates(), OCIDistLimits{})

	image := []byte(`{"schemaVersion":2,"config":{"mediaType":"application/vnd.oci.image.config.v1+json"}}`)
	rec := do(router, http.MethodPut, "/v2/apps/web/manifests/v1", image, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	subject := sha256Of(image)

	sig := referrerManifest(sigType, subject)
	sbom := referrerManifest(sbomType, subject)
	for _, m := range [][]byte{sig, sbom} {
		rec = do(router, http.MethodPut, "/v2/apps/web/manifests/"+sha256Of(m), m, nil)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		assert.Equal(t, subject, rec.Header().Get("OCI-Subject"))
	}

	t.Run("all referrers", func(t *testing.T) {
		rec := do(router, http.MethodGet, "/v2/apps/web/referrers/"+subject, nil, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, imageIndexMediaType, rec.Header().Get("Content-Type"))
		assert.Empty(t, rec.Header().Get("OCI-Filters-Applied"))
		idx := decodeIndex(t, rec.Body.Bytes())
		assert.Equal(t, 2, idx.SchemaVersion)
		assert.Equal(t, imageIndexMediaType, idx.MediaType)
		got := map[string]string{}
		for _, d := range idx.Manifests {
			got[d.Digest] = d.ArtifactType
		}
		assert.Equal(t, map[string]string{sha256Of(sig): sigType, sha256Of(sbom): sbomType}, got)
	})

	t.Run("filtered by artifactType", func(t *testing.T) {
		rec := do(router, http.MethodGet, "/v2/apps/web/referrers/"+subject+"?artifactType="+sigType, nil, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, "artifactType", rec.Header().Get("OCI-Filters-Applied"))
		idx := decodeIndex(t, rec.Body.Bytes())
		require.Len(t, idx.Manifests, 1)
		assert.Equal(t, sha256Of(sig), idx.Manifests[0].Digest)
		assert.Equal(t, int64(len(sig)), idx.Manifests[0].Size)
	})

	t.Run("no referrers is an empty index", func(t *testing.T) {
		rec := do(router, http.MethodGet, "/v2/apps/web/referrers/"+sha256Of([]byte("nothing")), nil, nil)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"schemaVersion":2,"mediaType":"`+imageIndexMediaType+`","manifests":[]}`, rec.Body.String())
	})

	t.Run("other repository sees none", func(t *testing.T) {
		rec := do(router, http.MethodGet, "/v2/apps/other/referrers/"+subject, nil, nil)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Empty(t, decodeIndex(t, rec.Body.Bytes()).Manifests)
	})

	t.Run("invalid digest", func(t *testing.T) {
		rec := do(router, http.MethodGet, "/v2/apps/web/referrers/latest", nil, nil)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "DIGEST_INVALID")
	})
}

// TestOCIDist_PutManifestReturnsOCISubject checks that a manifest PUT
// with a subject answers with OCI-Subject, and one without does not.
func TestOCIDist_PutManifestReturnsOCISubject(t *testing.T) {
	router := newTestRouter(t, newFakeOCIBackend(), openGates(), OCIDistLimits{})

	image := []byte(`{"schemaVersion":2,"config":{"mediaType":"application/vnd.oci.image.config.v1+json"}}`)
	rec := do(router, http.MethodPut, "/v2/apps/web/manifests/v1", image, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Empty(t, rec.Header().Get("OCI-Subject"))

	subject := sha256Of(image)
	sig := referrerManifest(sigType, subject)
	rec = do(router, http.MethodPut, "/v2/apps/web/manifests/"+sha256Of(sig), sig, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, subject, rec.Header().Get("OCI-Subject"))
}

// TestOCIDist_ReferrersBackendError maps a backend failure to a 500.
func TestOCIDist_ReferrersBackendError(t *testing.T) {
	backend := newFakeOCIBackend()
	backend.failList = errors.New("storage down")
	router := newTestRouter(t, backend, openGates(), OCIDistLimits{})
	rec := do(router, http.MethodGet, "/v2/apps/web/referrers/"+sha256Of([]byte("x")), nil, nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestOCIDist_ReferrersGated runs the referrers route behind the read
// gate.
func TestOCIDist_ReferrersGated(t *testing.T) {
	router := newTestRouter(t, newFakeOCIBackend(), OCIDistGates{Read: denyGate, Write: passGate}, OCIDistLimits{})
	rec := do(router, http.MethodGet, "/v2/apps/web/referrers/"+sha256Of([]byte("x")), nil, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
