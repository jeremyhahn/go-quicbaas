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
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

// HeaderRegistrySession carries the attested session ID: the registry sets
// it on the answer to a request with valid evidence, and the client sends it
// on every later request.
const HeaderRegistrySession = "X-Registry-Session"

// HeaderAttestationEvidence carries attestation evidence over a nonce from
// Challenge.
const HeaderAttestationEvidence = "X-Attestation-Evidence"

// HeaderContentDigest is the header in which the registry names the digest
// of a blob or manifest.
const HeaderContentDigest = "Docker-Content-Digest"

// HeaderOCISubject is the header in which the registry echoes the subject of
// a pushed manifest.
const HeaderOCISubject = "OCI-Subject"

// HeaderOCIFiltersApplied lists the referrers filters the registry applied.
const HeaderOCIFiltersApplied = "OCI-Filters-Applied"

// MediaTypeOCIManifest is the OCI image manifest media type.
const MediaTypeOCIManifest = "application/vnd.oci.image.manifest.v1+json"

// MediaTypeOCIIndex is the OCI image index media type.
const MediaTypeOCIIndex = "application/vnd.oci.image.index.v1+json"

// MediaTypeDockerManifest is the Docker image manifest v2 media type.
const MediaTypeDockerManifest = "application/vnd.docker.distribution.manifest.v2+json"

// MediaTypeDockerManifestList is the Docker manifest list media type.
const MediaTypeDockerManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"

// MaxManifestBytes bounds the manifest and listing bodies the client reads;
// it is the registry's own manifest limit.
const MaxManifestBytes int64 = 4 << 20

// maxErrorBytes bounds the error envelopes the client reads.
const maxErrorBytes int64 = 64 << 10

var (
	// nameRE is the OCI distribution repository name grammar.
	nameRE = regexp.MustCompile(`^[a-z0-9]+(?:(?:\.|_|__|-+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:\.|_|__|-+)[a-z0-9]+)*)*$`)
	// tagRE is the OCI distribution tag grammar.
	tagRE = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)
	// digestRE is `<algorithm>:<hex>`.
	digestRE = regexp.MustCompile(`^[a-z0-9]+(?:[+._-][a-z0-9]+)*:[a-f0-9]+$`)
)

// Client speaks the OCI distribution API at /v2/. It is safe for concurrent
// use.
type Client struct {
	base     *url.URL
	basePath string
	http     *http.Client

	mu      sync.Mutex
	session string
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient sets the HTTP client, which carries the TLS client
// certificate the registry authenticates. nil keeps http.DefaultClient.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.http = hc
		}
	}
}

// WithSession starts the client with a known registry session.
func WithSession(id string) Option {
	return func(c *Client) { c.session = id }
}

// NewClient returns a client for the registry at baseURL, an absolute http
// or https URL of the listener that serves /v2/.
func NewClient(baseURL string, opts ...Option) (*Client, error) {
	if baseURL == "" {
		return nil, &InvalidArgumentError{Field: "baseURL", Reason: "is empty"}
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, &InvalidArgumentError{Field: "baseURL", Reason: err.Error()}
	}
	if !u.IsAbs() || u.Host == "" {
		return nil, &InvalidArgumentError{Field: "baseURL", Reason: "is not an absolute URL"}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, &InvalidArgumentError{Field: "baseURL", Reason: "scheme must be http or https"}
	}
	c := &Client{
		base:     u,
		basePath: strings.TrimRight(u.Path, "/"),
		http:     http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Session returns the registry session the client holds, or "".
func (c *Client) Session() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session
}

func (c *Client) setSession(id string) {
	c.mu.Lock()
	c.session = id
	c.mu.Unlock()
}

// PushedManifest is the registry's answer to a manifest push.
type PushedManifest struct {
	// Digest is the manifest's digest.
	Digest string
	// Subject is the subject the registry echoed, or "".
	Subject string
}

// Manifest is a pulled manifest.
type Manifest struct {
	// Body is the manifest's bytes.
	Body []byte
	// MediaType is the manifest's media type.
	MediaType string
	// Digest is the manifest's digest.
	Digest string
}

// Descriptor names a piece of content in a referrers index.
type Descriptor struct {
	// MediaType is the referrer's media type.
	MediaType string `json:"mediaType"`
	// Digest is the referrer's digest.
	Digest string `json:"digest"`
	// Size is the referrer's size in bytes.
	Size int64 `json:"size"`
	// ArtifactType is the referrer's artifact type.
	ArtifactType string `json:"artifactType,omitempty"`
	// Annotations are the referrer's annotations.
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ReferrerList is the answer to a referrers query.
type ReferrerList struct {
	// Descriptors are the referrers.
	Descriptors []Descriptor
	// FiltersApplied reports that the registry applied the artifactType
	// filter.
	FiltersApplied bool
}

// request is one call to the registry.
type request struct {
	method   string
	path     string
	query    url.Values
	header   http.Header
	body     []byte
	evidence string
}

// do sends req and returns the 2xx response, whose body the caller closes.
// A non-2xx answer is a *RegistryError.
func (c *Client) do(ctx context.Context, req request) (*http.Response, error) {
	u := *c.base
	u.Path = c.basePath + "/v2/" + req.path
	u.RawPath = ""
	u.RawQuery = ""
	if len(req.query) > 0 {
		u.RawQuery = req.query.Encode()
	}
	var body io.Reader
	if req.body != nil {
		body = bytes.NewReader(req.body)
	}
	hr, err := http.NewRequestWithContext(ctx, req.method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("images: build %s request: %w", req.method, err)
	}
	for k, vs := range req.header {
		for _, v := range vs {
			hr.Header.Add(k, v)
		}
	}
	if req.evidence != "" {
		hr.Header.Set(HeaderAttestationEvidence, req.evidence)
	} else if sid := c.Session(); sid != "" {
		hr.Header.Set(HeaderRegistrySession, sid)
	}
	resp, err := c.http.Do(hr)
	if err != nil {
		return nil, fmt.Errorf("images: %s %s: %w", req.method, u.Path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer func() { _ = resp.Body.Close() }()
		return nil, registryError(resp)
	}
	if sid := resp.Header.Get(HeaderRegistrySession); sid != "" {
		c.setSession(sid)
	}
	return resp, nil
}

// registryError turns a non-2xx response into a *RegistryError.
func registryError(resp *http.Response) error {
	e := &RegistryError{Status: resp.StatusCode, Code: "UNKNOWN"}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBytes))
	if err != nil || len(data) == 0 {
		return e
	}
	var env struct {
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(data, &env) != nil || len(env.Errors) == 0 || env.Errors[0].Code == "" {
		return e
	}
	e.Code = env.Errors[0].Code
	e.Message = env.Errors[0].Message
	return e
}

// readBounded reads at most limit bytes of body.
func readBounded(body io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("images: read response: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, &ResponseTooLargeError{Limit: limit}
	}
	return data, nil
}

// getJSON GETs path and decodes the bounded JSON answer into v.
func (c *Client) getJSON(ctx context.Context, path string, v any) error {
	resp, err := c.do(ctx, request{method: http.MethodGet, path: path})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := readBounded(resp.Body, MaxManifestBytes)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("images: decode %s: %w", path, err)
	}
	return nil
}

func checkName(name string) error {
	if name == "" {
		return &InvalidArgumentError{Field: "name", Reason: "is empty"}
	}
	if !nameRE.MatchString(name) {
		return &InvalidArgumentError{Field: "name", Reason: "is not a repository name: " + name}
	}
	return nil
}

func checkDigest(digest string) error {
	if !digestRE.MatchString(digest) {
		return &InvalidArgumentError{Field: "digest", Reason: "is not <algorithm>:<hex>: " + digest}
	}
	return nil
}

// checkReference accepts a tag or a digest and reports whether it is a
// digest.
func checkReference(ref string) (bool, error) {
	if strings.Contains(ref, ":") {
		if err := checkDigest(ref); err != nil {
			return false, err
		}
		return true, nil
	}
	if !tagRE.MatchString(ref) {
		return false, &InvalidArgumentError{Field: "reference", Reason: "is not a tag or digest: " + ref}
	}
	return false, nil
}

// digestOf returns the digest of data under the algorithm of want.
func digestOf(want string, data []byte) (string, error) {
	alg, _, _ := strings.Cut(want, ":")
	var h hash.Hash
	switch alg {
	case "sha256":
		h = sha256.New()
	case "sha512":
		h = sha512.New()
	default:
		return "", &InvalidArgumentError{Field: "digest", Reason: "unsupported algorithm " + alg}
	}
	_, _ = h.Write(data)
	return alg + ":" + hex.EncodeToString(h.Sum(nil)), nil
}

// verify checks that data has digest want.
func verify(want string, data []byte) error {
	got, err := digestOf(want, data)
	if err != nil {
		return err
	}
	if got != want {
		return &DigestMismatchError{Want: want, Got: got}
	}
	return nil
}

func sha256Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Ping checks that the registry serves the distribution API and accepts the
// caller.
func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.do(ctx, request{method: http.MethodGet})
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// Catalog lists the registry's repositories.
func (c *Client) Catalog(ctx context.Context) ([]string, error) {
	var out struct {
		Repositories []string `json:"repositories"`
	}
	if err := c.getJSON(ctx, "_catalog", &out); err != nil {
		return nil, err
	}
	return out.Repositories, nil
}

// Tags lists the tags of repository name.
func (c *Client) Tags(ctx context.Context, name string) ([]string, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	var out struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	if err := c.getJSON(ctx, name+"/tags/list", &out); err != nil {
		return nil, err
	}
	return out.Tags, nil
}

// PushBlob uploads content to repository name in one request and returns
// its sha256 digest.
func (c *Client) PushBlob(ctx context.Context, name string, content []byte) (string, error) {
	if err := checkName(name); err != nil {
		return "", err
	}
	if content == nil {
		content = []byte{}
	}
	digest := sha256Digest(content)
	resp, err := c.do(ctx, request{
		method: http.MethodPost,
		path:   name + "/blobs/uploads/",
		query:  url.Values{"digest": {digest}},
		header: http.Header{"Content-Type": {"application/octet-stream"}},
		body:   content,
	})
	if err != nil {
		return "", err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", &RegistryError{Status: resp.StatusCode, Code: "UNKNOWN", Message: "blob upload not committed"}
	}
	if got := resp.Header.Get(HeaderContentDigest); got != "" && got != digest {
		return "", &DigestMismatchError{Want: digest, Got: got}
	}
	return digest, nil
}

// BlobExists reports whether repository name holds the blob digest.
func (c *Client) BlobExists(ctx context.Context, name, digest string) (bool, error) {
	if err := checkName(name); err != nil {
		return false, err
	}
	if err := checkDigest(digest); err != nil {
		return false, err
	}
	resp, err := c.do(ctx, request{method: http.MethodHead, path: name + "/blobs/" + digest})
	if err != nil {
		var re *RegistryError
		if errors.As(err, &re) && re.Status == http.StatusNotFound {
			return false, nil
		}
		return false, err
	}
	_ = resp.Body.Close()
	return true, nil
}

// GetBlob downloads the blob digest from repository name and checks that
// its content has that digest.
func (c *Client) GetBlob(ctx context.Context, name, digest string) ([]byte, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	if err := checkDigest(digest); err != nil {
		return nil, err
	}
	if _, err := digestOf(digest, nil); err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, request{method: http.MethodGet, path: name + "/blobs/" + digest})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("images: read blob: %w", err)
	}
	if err := verify(digest, data); err != nil {
		return nil, err
	}
	return data, nil
}

// PutManifest pushes body, of media type mediaType, to repository name at
// reference (a tag or a digest).
func (c *Client) PutManifest(ctx context.Context, name, reference, mediaType string, body []byte) (*PushedManifest, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	isDigest, err := checkReference(reference)
	if err != nil {
		return nil, err
	}
	if mediaType == "" {
		return nil, &InvalidArgumentError{Field: "mediaType", Reason: "is empty"}
	}
	if isDigest {
		if err := verify(reference, body); err != nil {
			return nil, err
		}
	}
	resp, err := c.do(ctx, request{
		method: http.MethodPut,
		path:   name + "/manifests/" + reference,
		header: http.Header{"Content-Type": {mediaType}},
		body:   body,
	})
	if err != nil {
		return nil, err
	}
	_ = resp.Body.Close()
	digest := resp.Header.Get(HeaderContentDigest)
	if digest == "" {
		digest = sha256Digest(body)
	}
	return &PushedManifest{Digest: digest, Subject: resp.Header.Get(HeaderOCISubject)}, nil
}

// manifestAccept lists the manifest media types GetManifest accepts.
var manifestAccept = strings.Join([]string{
	MediaTypeOCIManifest,
	MediaTypeOCIIndex,
	MediaTypeDockerManifest,
	MediaTypeDockerManifestList,
}, ", ")

// GetManifest pulls the manifest at reference (a tag or a digest) from
// repository name. When reference is a digest it checks the body against it.
func (c *Client) GetManifest(ctx context.Context, name, reference string) (*Manifest, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	isDigest, err := checkReference(reference)
	if err != nil {
		return nil, err
	}
	if isDigest {
		if _, err := digestOf(reference, nil); err != nil {
			return nil, err
		}
	}
	resp, err := c.do(ctx, request{
		method: http.MethodGet,
		path:   name + "/manifests/" + reference,
		header: http.Header{"Accept": {manifestAccept}},
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := readBounded(resp.Body, MaxManifestBytes)
	if err != nil {
		return nil, err
	}
	digest := resp.Header.Get(HeaderContentDigest)
	if isDigest {
		if err := verify(reference, body); err != nil {
			return nil, err
		}
		digest = reference
	} else if digest == "" {
		digest = sha256Digest(body)
	}
	return &Manifest{Body: body, MediaType: resp.Header.Get("Content-Type"), Digest: digest}, nil
}

// DeleteManifest deletes the manifest at reference from repository name.
func (c *Client) DeleteManifest(ctx context.Context, name, reference string) error {
	if err := checkName(name); err != nil {
		return err
	}
	if _, err := checkReference(reference); err != nil {
		return err
	}
	resp, err := c.do(ctx, request{method: http.MethodDelete, path: name + "/manifests/" + reference})
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// Referrers lists the manifests in repository name whose subject is digest,
// filtered by artifactType when it is not empty.
func (c *Client) Referrers(ctx context.Context, name, digest, artifactType string) (*ReferrerList, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	if err := checkDigest(digest); err != nil {
		return nil, err
	}
	var query url.Values
	if artifactType != "" {
		query = url.Values{"artifactType": {artifactType}}
	}
	resp, err := c.do(ctx, request{method: http.MethodGet, path: name + "/referrers/" + digest, query: query})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := readBounded(resp.Body, MaxManifestBytes)
	if err != nil {
		return nil, err
	}
	var index struct {
		Manifests []Descriptor `json:"manifests"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("images: decode referrers: %w", err)
	}
	out := &ReferrerList{Descriptors: index.Manifests}
	for _, f := range strings.Split(resp.Header.Get(HeaderOCIFiltersApplied), ",") {
		if strings.TrimSpace(f) == "artifactType" {
			out.FiltersApplied = true
		}
	}
	return out, nil
}

// Challenge asks the registry for an attestation nonce for the caller.
func (c *Client) Challenge(ctx context.Context) ([]byte, error) {
	resp, err := c.do(ctx, request{method: http.MethodPost, path: "_attestation/challenge"})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := readBounded(resp.Body, maxErrorBytes)
	if err != nil {
		return nil, err
	}
	var out struct {
		Nonce string `json:"nonce"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("images: decode challenge: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(out.Nonce)
	if err != nil {
		return nil, fmt.Errorf("images: decode nonce: %w", err)
	}
	return nonce, nil
}

// Attest presents evidence over a nonce from Challenge and keeps the
// session the registry returns. It drops any session held before.
func (c *Client) Attest(ctx context.Context, evidence string) error {
	if evidence == "" {
		return &InvalidArgumentError{Field: "evidence", Reason: "is empty"}
	}
	c.setSession("")
	resp, err := c.do(ctx, request{method: http.MethodGet, evidence: evidence})
	if err != nil {
		return err
	}
	return resp.Body.Close()
}
