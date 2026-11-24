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

package images

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// fake serves h and counts the requests it receives.
type fake struct {
	srv   *httptest.Server
	count atomic.Int32
}

func newFake(t *testing.T, h http.HandlerFunc) (*fake, *Client) {
	t.Helper()
	f := &fake{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.count.Add(1)
		h(w, r)
	}))
	t.Cleanup(f.srv.Close)
	c, err := NewClient(f.srv.URL, WithHTTPClient(f.srv.Client()))
	require.NoError(t, err)
	return f, c
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, `{"errors":[{"code":"`+code+`","message":"`+msg+`"}]}`)
}

const (
	testDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
)

func TestClient_Ping(t *testing.T) {
	_, c := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v2/", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	require.NoError(t, c.Ping(context.Background()))

	_, denied := newFake(t, func(w http.ResponseWriter, _ *http.Request) {
		writeErr(w, http.StatusUnauthorized, "UNAUTHORIZED", "m")
	})
	err := denied.Ping(context.Background())
	var re *RegistryError
	require.ErrorAs(t, err, &re)
	require.Equal(t, http.StatusUnauthorized, re.Status)
	require.Equal(t, "UNAUTHORIZED", re.Code)
	require.Equal(t, "m", re.Message)

	_, plain := newFake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "not json")
	})
	require.ErrorAs(t, plain.Ping(context.Background()), &re)
	require.Equal(t, "UNKNOWN", re.Code)
}

func TestClient_CatalogAndTags(t *testing.T) {
	_, c := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		switch r.URL.EscapedPath() {
		case "/v2/_catalog":
			_, _ = io.WriteString(w, `{"repositories":["a","team/app"]}`)
		case "/v2/team/app/tags/list":
			_, _ = io.WriteString(w, `{"name":"team/app","tags":["v1","v2"]}`)
		default:
			t.Errorf("unexpected path %q", r.URL.EscapedPath())
			w.WriteHeader(http.StatusNotFound)
		}
	})
	repos, err := c.Catalog(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"a", "team/app"}, repos)
	tags, err := c.Tags(context.Background(), "team/app")
	require.NoError(t, err)
	require.Equal(t, []string{"v1", "v2"}, tags)
}

func TestClient_PushBlob(t *testing.T) {
	content := []byte("layer bytes")
	want := sha256Digest(content)
	_, c := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v2/team/app/blobs/uploads/", r.URL.Path)
		require.Equal(t, want, r.URL.Query().Get("digest"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, content, body)
		w.Header().Set(HeaderContentDigest, want)
		w.WriteHeader(http.StatusCreated)
	})
	got, err := c.PushBlob(context.Background(), "team/app", content)
	require.NoError(t, err)
	require.Equal(t, want, got)

	_, bare := newFake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	got, err = bare.PushBlob(context.Background(), "app", content)
	require.NoError(t, err)
	require.Equal(t, want, got)

	_, started := newFake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	_, err = started.PushBlob(context.Background(), "app", content)
	var re *RegistryError
	require.ErrorAs(t, err, &re)
	require.Equal(t, http.StatusAccepted, re.Status)

	_, bad := newFake(t, func(w http.ResponseWriter, _ *http.Request) {
		writeErr(w, http.StatusBadRequest, "DIGEST_INVALID", "bad")
	})
	_, err = bad.PushBlob(context.Background(), "app", content)
	require.ErrorAs(t, err, &re)
	require.Equal(t, "DIGEST_INVALID", re.Code)
}

func TestClient_BlobExists(t *testing.T) {
	status := http.StatusOK
	_, c := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodHead, r.Method)
		require.Equal(t, "/v2/app/blobs/"+testDigest, r.URL.Path)
		w.WriteHeader(status)
	})
	ok, err := c.BlobExists(context.Background(), "app", testDigest)
	require.NoError(t, err)
	require.True(t, ok)

	status = http.StatusNotFound
	ok, err = c.BlobExists(context.Background(), "app", testDigest)
	require.NoError(t, err)
	require.False(t, ok)

	status = http.StatusForbidden
	_, err = c.BlobExists(context.Background(), "app", testDigest)
	var re *RegistryError
	require.ErrorAs(t, err, &re)
	require.Equal(t, http.StatusForbidden, re.Status)
}

func TestClient_GetBlob(t *testing.T) {
	content := []byte("blob")
	digest := sha256Digest(content)
	served := content
	_, c := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v2/app/blobs/"+digest, r.URL.Path)
		_, _ = w.Write(served)
	})
	got, err := c.GetBlob(context.Background(), "app", digest)
	require.NoError(t, err)
	require.Equal(t, content, got)

	served = []byte("tampered")
	_, err = c.GetBlob(context.Background(), "app", digest)
	var dm *DigestMismatchError
	require.ErrorAs(t, err, &dm)
	require.Equal(t, digest, dm.Want)
	require.Equal(t, sha256Digest(served), dm.Got)
}

func TestClient_PutManifest(t *testing.T) {
	body := []byte(`{"schemaVersion":2}`)
	subject := ""
	f, c := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPut, r.Method)
		require.Equal(t, "/v2/team/app/manifests/v1", r.URL.Path)
		require.Equal(t, MediaTypeOCIManifest, r.Header.Get("Content-Type"))
		got, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, body, got)
		w.Header().Set(HeaderContentDigest, "sha256:abc")
		if subject != "" {
			w.Header().Set(HeaderOCISubject, subject)
		}
		w.WriteHeader(http.StatusCreated)
	})
	pushed, err := c.PutManifest(context.Background(), "team/app", "v1", MediaTypeOCIManifest, body)
	require.NoError(t, err)
	require.Equal(t, "sha256:abc", pushed.Digest)
	require.Empty(t, pushed.Subject)

	subject = testDigest
	pushed, err = c.PutManifest(context.Background(), "team/app", "v1", MediaTypeOCIManifest, body)
	require.NoError(t, err)
	require.Equal(t, testDigest, pushed.Subject)

	before := f.count.Load()
	_, err = c.PutManifest(context.Background(), "team/app", "v1", "", body)
	var ia *InvalidArgumentError
	require.ErrorAs(t, err, &ia)
	require.Equal(t, "mediaType", ia.Field)
	require.Equal(t, before, f.count.Load())
}

func TestClient_GetManifest(t *testing.T) {
	body := []byte(`{"schemaVersion":2,"mediaType":"` + MediaTypeOCIManifest + `"}`)
	digest := sha256Digest(body)
	served := body
	_, c := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		accept := r.Header.Get("Accept")
		for _, mt := range []string{MediaTypeOCIManifest, MediaTypeOCIIndex, MediaTypeDockerManifest, MediaTypeDockerManifestList} {
			require.Contains(t, accept, mt)
		}
		w.Header().Set("Content-Type", MediaTypeOCIManifest)
		w.Header().Set(HeaderContentDigest, sha256Digest(served))
		_, _ = w.Write(served)
	})
	m, err := c.GetManifest(context.Background(), "app", "v1")
	require.NoError(t, err)
	require.Equal(t, body, m.Body)
	require.Equal(t, MediaTypeOCIManifest, m.MediaType)
	require.Equal(t, digest, m.Digest)

	m, err = c.GetManifest(context.Background(), "app", digest)
	require.NoError(t, err)
	require.Equal(t, digest, m.Digest)

	served = []byte(`{"schemaVersion":2,"other":true}`)
	_, err = c.GetManifest(context.Background(), "app", digest)
	var dm *DigestMismatchError
	require.ErrorAs(t, err, &dm)
	require.Equal(t, digest, dm.Want)
}

func TestClient_DeleteManifest(t *testing.T) {
	status := http.StatusAccepted
	_, c := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodDelete, r.Method)
		require.Equal(t, "/v2/app/manifests/v1", r.URL.Path)
		if status != http.StatusAccepted {
			writeErr(w, status, "MANIFEST_UNKNOWN", "gone")
			return
		}
		w.WriteHeader(status)
	})
	require.NoError(t, c.DeleteManifest(context.Background(), "app", "v1"))

	status = http.StatusNotFound
	err := c.DeleteManifest(context.Background(), "app", "v1")
	var re *RegistryError
	require.ErrorAs(t, err, &re)
	require.Equal(t, http.StatusNotFound, re.Status)
	require.Equal(t, "MANIFEST_UNKNOWN", re.Code)
	require.Equal(t, "gone", re.Message)
}

func TestClient_Referrers(t *testing.T) {
	const artifactType = "application/vnd.example+type; v=1"
	_, c := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v2/app/referrers/"+testDigest, r.URL.Path)
		if at := r.URL.Query().Get("artifactType"); at != "" {
			require.Equal(t, artifactType, at)
			require.NotContains(t, r.URL.RawQuery, " ")
			w.Header().Set(HeaderOCIFiltersApplied, "artifactType")
		}
		w.Header().Set("Content-Type", MediaTypeOCIIndex)
		_, _ = io.WriteString(w, `{"schemaVersion":2,"mediaType":"`+MediaTypeOCIIndex+`","manifests":[`+
			`{"mediaType":"`+MediaTypeOCIManifest+`","digest":"sha256:aa","size":12,`+
			`"artifactType":"`+artifactType+`","annotations":{"k":"v"}}]}`)
	})
	list, err := c.Referrers(context.Background(), "app", testDigest, "")
	require.NoError(t, err)
	require.False(t, list.FiltersApplied)
	require.Equal(t, []Descriptor{{
		MediaType:    MediaTypeOCIManifest,
		Digest:       "sha256:aa",
		Size:         12,
		ArtifactType: artifactType,
		Annotations:  map[string]string{"k": "v"},
	}}, list.Descriptors)

	list, err = c.Referrers(context.Background(), "app", testDigest, artifactType)
	require.NoError(t, err)
	require.True(t, list.FiltersApplied)
	require.Len(t, list.Descriptors, 1)
}

func TestClient_Attestation(t *testing.T) {
	nonce := []byte("nonce-bytes")
	var deny atomic.Bool
	var rotate atomic.Bool
	_, c := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v2/_attestation/challenge":
			_, _ = io.WriteString(w, `{"nonce":"`+base64.StdEncoding.EncodeToString(nonce)+`"}`)
		case r.Header.Get(HeaderAttestationEvidence) != "":
			require.Empty(t, r.Header.Get(HeaderRegistrySession))
			if deny.Load() {
				writeErr(w, http.StatusForbidden, "DENIED", "bad evidence")
				return
			}
			require.Equal(t, "evidence", r.Header.Get(HeaderAttestationEvidence))
			w.Header().Set(HeaderRegistrySession, "sess-1")
		case r.URL.Path == "/v2/_catalog":
			require.Empty(t, r.Header.Get(HeaderAttestationEvidence))
			if rotate.Load() {
				require.Equal(t, "sess-2", r.Header.Get(HeaderRegistrySession))
			} else {
				require.Equal(t, "sess-1", r.Header.Get(HeaderRegistrySession))
				w.Header().Set(HeaderRegistrySession, "sess-2")
				rotate.Store(true)
			}
			_, _ = io.WriteString(w, `{"repositories":[]}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	got, err := c.Challenge(context.Background())
	require.NoError(t, err)
	require.Equal(t, nonce, got)

	require.NoError(t, c.Attest(context.Background(), "evidence"))
	require.Equal(t, "sess-1", c.Session())

	_, err = c.Catalog(context.Background())
	require.NoError(t, err)
	require.Equal(t, "sess-2", c.Session())
	_, err = c.Catalog(context.Background())
	require.NoError(t, err)

	deny.Store(true)
	err = c.Attest(context.Background(), "evidence")
	var re *RegistryError
	require.ErrorAs(t, err, &re)
	require.Equal(t, http.StatusForbidden, re.Status)
	require.Empty(t, c.Session())
}

func TestClient_WithSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "known", r.Header.Get(HeaderRegistrySession))
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL, WithSession("known"))
	require.NoError(t, err)
	require.Equal(t, "known", c.Session())
	require.NoError(t, c.Ping(context.Background()))
}

func TestClient_NewClient(t *testing.T) {
	for _, bad := range []string{"", "registry.example/v2", "/relative", "ftp://registry.example", "://bad"} {
		_, err := NewClient(bad)
		var ia *InvalidArgumentError
		require.ErrorAs(t, err, &ia, "url %q", bad)
		require.Equal(t, "baseURL", ia.Field)
	}

	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL+"/", WithHTTPClient(nil))
	require.NoError(t, err)
	require.Same(t, http.DefaultClient, c.http)
	require.NoError(t, c.Ping(context.Background()))
	require.Equal(t, "/v2/", path)

	c, err = NewClient(srv.URL + "/prefix/")
	require.NoError(t, err)
	require.NoError(t, c.Ping(context.Background()))
	require.Equal(t, "/prefix/v2/", path)
}

func TestClient_InputValidation(t *testing.T) {
	f, c := newFake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	ctx := context.Background()
	calls := map[string]func() error{
		"tags empty name":       func() error { _, err := c.Tags(ctx, ""); return err },
		"tags bad name":         func() error { _, err := c.Tags(ctx, "Bad/../name"); return err },
		"push empty name":       func() error { _, err := c.PushBlob(ctx, "", []byte("x")); return err },
		"exists empty name":     func() error { _, err := c.BlobExists(ctx, "", testDigest); return err },
		"exists bad digest":     func() error { _, err := c.BlobExists(ctx, "app", "nothex"); return err },
		"exists upper digest":   func() error { _, err := c.BlobExists(ctx, "app", "sha256:ABC"); return err },
		"get bad digest":        func() error { _, err := c.GetBlob(ctx, "app", "sha256:"); return err },
		"get unknown algorithm": func() error { _, err := c.GetBlob(ctx, "app", "md5:abcd"); return err },
		"put bad reference":     func() error { _, err := c.PutManifest(ctx, "app", "a/b", MediaTypeOCIManifest, nil); return err },
		"get manifest bad ref":  func() error { _, err := c.GetManifest(ctx, "app", "sha256:zz"); return err },
		"delete empty name":     func() error { return c.DeleteManifest(ctx, "", "v1") },
		"referrers bad digest":  func() error { _, err := c.Referrers(ctx, "app", "v1", ""); return err },
		"attest empty evidence": func() error { return c.Attest(ctx, "") },
	}
	for name, call := range calls {
		err := call()
		var ia *InvalidArgumentError
		require.ErrorAs(t, err, &ia, name)
		require.True(t, strings.HasPrefix(err.Error(), "images: invalid "), name)
	}
	require.Zero(t, f.count.Load())
}
