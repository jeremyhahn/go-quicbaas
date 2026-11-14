package provenance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// maxReferrerBytes bounds every manifest, index and blob the client reads.
const maxReferrerBytes = 4 << 20

// Registry stores and finds provenance as OCI referrers through the OCI
// distribution API, including the referrers API (distribution spec v1.1).
type Registry struct {
	base   *url.URL
	client *http.Client
}

// NewRegistry returns a client for the registry at baseURL
// ("https://registry.example.com"). client carries the transport and the
// registry credentials; nil is http.DefaultClient.
func NewRegistry(baseURL string, client *http.Client) (*Registry, error) {
	base, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("provenance: registry URL %q is not absolute", baseURL)
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &Registry{base: base, client: client}, nil
}

// RegistryError is a registry response the client could not use.
type RegistryError struct {
	Op     string
	Status int
}

func (e *RegistryError) Error() string {
	return fmt.Sprintf("provenance: registry %s: HTTP %d", e.Op, e.Status)
}

// Push stores signed as a referrer of the image repository@imageDigest:
// an OCI manifest of artifact type MediaTypeEnvelope whose subject is the
// image and whose layers are the envelope and the signer chain. It returns
// the referrer manifest's digest.
func (r *Registry) Push(ctx context.Context, repository, imageDigest string, signed *Signed) (string, error) {
	if signed == nil {
		return "", errors.New("provenance: nothing to push")
	}
	subject, err := r.head(ctx, repository, imageDigest)
	if err != nil {
		return "", err
	}
	envelope, err := json.Marshal(signed.Envelope)
	if err != nil {
		return "", fmt.Errorf("provenance: encode envelope: %w", err)
	}
	chain := bytes.Join(signed.SignerChainPEM, nil)
	empty := ocispec.DescriptorEmptyJSON
	layers := []ocispec.Descriptor{descriptor(MediaTypeEnvelope, envelope), descriptor(MediaTypeSignerChain, chain)}
	for _, blob := range []struct {
		desc ocispec.Descriptor
		data []byte
	}{{empty, empty.Data}, {layers[0], envelope}, {layers[1], chain}} {
		if err := r.putBlob(ctx, repository, blob.desc.Digest, blob.data); err != nil {
			return "", err
		}
	}
	manifest, err := json.Marshal(ocispec.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ocispec.MediaTypeImageManifest,
		ArtifactType: MediaTypeEnvelope,
		Config:       ocispec.Descriptor{MediaType: empty.MediaType, Digest: empty.Digest, Size: empty.Size},
		Layers:       layers,
		Subject:      &subject,
	})
	if err != nil {
		return "", fmt.Errorf("provenance: encode manifest: %w", err)
	}
	ref := digest.FromBytes(manifest)
	resp, err := r.do(ctx, http.MethodPut, r.path(repository, "manifests", ref.String()), ocispec.MediaTypeImageManifest, manifest, "")
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", &RegistryError{Op: "push manifest", Status: resp.StatusCode}
	}
	return ref.String(), nil
}

// Fetch returns every provenance referrer of repository@imageDigest. It
// checks each fetched manifest and blob against its digest but verifies no
// signature: that is the Verifier's.
func (r *Registry) Fetch(ctx context.Context, repository, imageDigest string) ([]*Signed, error) {
	q := url.Values{"artifactType": {MediaTypeEnvelope}}
	body, err := r.get(ctx, r.path(repository, "referrers", imageDigest)+"?"+q.Encode(), ocispec.MediaTypeImageIndex, "")
	if err != nil {
		return nil, err
	}
	var index ocispec.Index
	if err := json.Unmarshal(body, &index); err != nil {
		return nil, fmt.Errorf("provenance: decode referrers index: %w", err)
	}
	var out []*Signed
	for _, m := range index.Manifests {
		if m.ArtifactType != MediaTypeEnvelope {
			continue
		}
		signed, err := r.fetchReferrer(ctx, repository, imageDigest, m.Digest)
		if err != nil {
			return nil, err
		}
		out = append(out, signed)
	}
	return out, nil
}

func (r *Registry) fetchReferrer(ctx context.Context, repository, imageDigest string, ref digest.Digest) (*Signed, error) {
	body, err := r.get(ctx, r.path(repository, "manifests", ref.String()), ocispec.MediaTypeImageManifest, ref)
	if err != nil {
		return nil, err
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("provenance: decode referrer %s: %w", ref, err)
	}
	if manifest.Subject == nil || manifest.Subject.Digest.String() != imageDigest {
		return nil, fmt.Errorf("provenance: referrer %s is not a referrer of %s", ref, imageDigest)
	}
	signed := &Signed{}
	for _, layer := range manifest.Layers {
		data, err := r.get(ctx, r.path(repository, "blobs", layer.Digest.String()), "", layer.Digest)
		if err != nil {
			return nil, err
		}
		switch layer.MediaType {
		case MediaTypeEnvelope:
			if err := json.Unmarshal(data, &signed.Envelope); err != nil {
				return nil, fmt.Errorf("provenance: decode envelope in %s: %w", ref, err)
			}
		case MediaTypeSignerChain:
			signed.SignerChainPEM = splitPEM(data)
		}
	}
	return signed, nil
}

func (r *Registry) head(ctx context.Context, repository, imageDigest string) (ocispec.Descriptor, error) {
	d, err := digest.Parse(imageDigest)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("provenance: image digest: %w", err)
	}
	accept := ocispec.MediaTypeImageManifest + ", " + ocispec.MediaTypeImageIndex
	resp, err := r.do(ctx, http.MethodHead, r.path(repository, "manifests", d.String()), "", nil, accept)
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ocispec.Descriptor{}, &RegistryError{Op: "find image " + d.String(), Status: resp.StatusCode}
	}
	return ocispec.Descriptor{MediaType: resp.Header.Get("Content-Type"), Digest: d, Size: resp.ContentLength}, nil
}

func (r *Registry) putBlob(ctx context.Context, repository string, d digest.Digest, data []byte) error {
	resp, err := r.do(ctx, http.MethodPost, r.path(repository, "blobs", "uploads")+"/", "", nil, "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return &RegistryError{Op: "start upload", Status: resp.StatusCode}
	}
	loc, err := r.base.Parse(resp.Header.Get("Location"))
	if err != nil {
		return fmt.Errorf("provenance: upload location: %w", err)
	}
	q := loc.Query()
	q.Set("digest", d.String())
	loc.RawQuery = q.Encode()
	resp, err = r.doURL(ctx, http.MethodPut, loc.String(), "application/octet-stream", data, "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return &RegistryError{Op: "upload blob", Status: resp.StatusCode}
	}
	return nil
}

func (r *Registry) get(ctx context.Context, path, accept string, want digest.Digest) ([]byte, error) {
	resp, err := r.do(ctx, http.MethodGet, path, "", nil, accept)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &RegistryError{Op: "get " + path, Status: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxReferrerBytes+1))
	if err != nil {
		return nil, fmt.Errorf("provenance: read %s: %w", path, err)
	}
	if len(data) > maxReferrerBytes {
		return nil, fmt.Errorf("provenance: %s exceeds %d bytes", path, maxReferrerBytes)
	}
	if want != "" && digest.FromBytes(data) != want {
		return nil, fmt.Errorf("provenance: %s does not match its digest", path)
	}
	return data, nil
}

func (r *Registry) path(repository, kind, ref string) string {
	return "/v2/" + strings.Trim(repository, "/") + "/" + kind + "/" + ref
}

func (r *Registry) do(ctx context.Context, method, path, contentType string, body []byte, accept string) (*http.Response, error) {
	return r.doURL(ctx, method, r.base.String()+path, contentType, body, accept)
}

func (r *Registry) doURL(ctx context.Context, method, target, contentType string, body []byte, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("provenance: %s %s: %w", method, target, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provenance: %s %s: %w", method, target, err)
	}
	return resp, nil
}

func descriptor(mediaType string, data []byte) ocispec.Descriptor {
	return ocispec.Descriptor{MediaType: mediaType, Digest: digest.FromBytes(data), Size: int64(len(data))}
}

// splitPEM splits concatenated PEM certificates, keeping each block's
// encoding.
func splitPEM(data []byte) [][]byte {
	const begin = "-----BEGIN "
	var out [][]byte
	for {
		start := bytes.Index(data, []byte(begin))
		if start < 0 {
			return out
		}
		next := bytes.Index(data[start+len(begin):], []byte(begin))
		if next < 0 {
			return append(out, data[start:])
		}
		end := start + len(begin) + next
		out = append(out, data[start:end])
		data = data[end:]
	}
}

// RepositoryOf returns the repository path of an image reference
// ("registry.example.com/team/app@sha256:..." gives "team/app"). A first
// path component with a dot or a port, or "localhost", is the registry
// host and is dropped.
func RepositoryOf(image string) string {
	name, _, _ := strings.Cut(image, "@")
	if slash := strings.LastIndex(name, "/"); strings.LastIndex(name, ":") > slash {
		name = name[:strings.LastIndex(name, ":")]
	}
	if host, rest, ok := strings.Cut(name, "/"); ok && (strings.ContainsAny(host, ".:") || host == "localhost") {
		return rest
	}
	return name
}
