// Package provenancetest is an in-memory OCI registry with the referrers
// API, for tests of provenance publishing and verification.
package provenancetest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Registry is an in-memory OCI registry: blob upload, manifests by digest
// and the referrers API. Repositories share one store.
type Registry struct {
	mu        sync.Mutex
	blobs     map[string][]byte
	manifests map[string][]byte
	uploads   int
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{blobs: map[string][]byte{}, manifests: map[string][]byte{}}
}

// Serve starts the registry on an httptest server; the caller closes it.
func (f *Registry) Serve() *httptest.Server { return httptest.NewServer(f) }

// AddImage stores an image manifest and returns its digest.
func (f *Registry) AddImage(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	body := []byte(`{"schemaVersion":2,"mediaType":"` + ocispec.MediaTypeImageManifest + `","annotations":{"name":"` + name + `"}}`)
	d := digest.FromBytes(body).String()
	f.manifests[d] = body
	return d
}

// TamperBlobs appends a byte to every blob containing marker, as a
// registry that altered stored content would.
func (f *Registry) TamperBlobs(marker string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for d, data := range f.blobs {
		if bytes.Contains(data, []byte(marker)) {
			f.blobs[d] = append(data, ' ')
		}
	}
}

// ServeHTTP implements the subset of the OCI distribution API the
// provenance client uses.
func (f *Registry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !strings.HasPrefix(r.URL.Path, "/v2/") {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v2/")
	last := path[strings.LastIndex(path, "/")+1:]
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/blobs/uploads/"):
		f.uploads++
		w.Header().Set("Location", fmt.Sprintf("/v2/%supload-%d", path, f.uploads))
		w.WriteHeader(http.StatusAccepted)
	case r.Method == http.MethodPut && strings.Contains(path, "/blobs/uploads/"):
		data, _ := io.ReadAll(r.Body)
		want := r.URL.Query().Get("digest")
		if digest.FromBytes(data).String() != want {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.blobs[want] = data
		w.WriteHeader(http.StatusCreated)
	case strings.Contains(path, "/blobs/"):
		data, ok := f.blobs[last]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(data)
	case strings.Contains(path, "/manifests/"):
		f.manifest(w, r, last)
	case strings.Contains(path, "/referrers/"):
		f.referrers(w, r, last)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *Registry) manifest(w http.ResponseWriter, r *http.Request, ref string) {
	if r.Method == http.MethodPut {
		data, _ := io.ReadAll(r.Body)
		if digest.FromBytes(data).String() != ref {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.manifests[ref] = data
		w.WriteHeader(http.StatusCreated)
		return
	}
	data, ok := f.manifests[ref]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", ocispec.MediaTypeImageManifest)
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	if r.Method == http.MethodGet {
		_, _ = w.Write(data)
	}
}

func (f *Registry) referrers(w http.ResponseWriter, r *http.Request, subject string) {
	index := ocispec.Index{MediaType: ocispec.MediaTypeImageIndex, Manifests: []ocispec.Descriptor{}}
	index.SchemaVersion = 2
	for ref, data := range f.manifests {
		var m ocispec.Manifest
		if json.Unmarshal(data, &m) != nil || m.Subject == nil || m.Subject.Digest.String() != subject {
			continue
		}
		if at := r.URL.Query().Get("artifactType"); at != "" && m.ArtifactType != at {
			continue
		}
		index.Manifests = append(index.Manifests, ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageManifest, Digest: digest.Digest(ref),
			Size: int64(len(data)), ArtifactType: m.ArtifactType,
		})
	}
	w.Header().Set("Content-Type", ocispec.MediaTypeImageIndex)
	_ = json.NewEncoder(w).Encode(index)
}
