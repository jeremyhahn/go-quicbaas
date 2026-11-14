//go:build vector && qrdb_vector

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

package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/inference"
)

// Defaults for the NPU embedding provider. The dimensionality and token limit
// describe all-MiniLM-L6-v2: 384-wide vectors and a 512-token ceiling, which is
// the model's own max_position_embeddings and not a device restriction. Text
// beyond that limit must be chunked on any device, and the sidecar does so.
const (
	// DefaultNPUModel is the embedding model the sidecar serves by default.
	DefaultNPUModel = "all-MiniLM-L6-v2"

	// DefaultNPUDimensions is the width of an all-MiniLM-L6-v2 vector.
	DefaultNPUDimensions = 384

	// DefaultNPUMaxTokens is the model's positional limit, not the NPU's.
	DefaultNPUMaxTokens = 512

	// DefaultNPUMaxBatchSize bounds how many texts travel in one HTTP request.
	// The sidecar accepts any batch size and buckets internally; this only
	// keeps individual request bodies to a sane size.
	DefaultNPUMaxBatchSize = 64

	// npuEmbedPath is the sidecar's embedding endpoint.
	npuEmbedPath = "/v1/embed"

	// npuHealthPath is the sidecar's health and device-report endpoint.
	npuHealthPath = "/v1/health"
)

// DefaultNPUTimeout is the per-request HTTP timeout for sidecar calls. The
// first request after a cold start may pay a one-off shape-compilation cost
// (measured at 7.8s to precompile the full bucket set), so this is generous.
var DefaultNPUTimeout = 30 * time.Second

// SecretResolver resolves an indirect credential reference to its value. The
// embedding layer never stores raw credentials in configuration; it resolves
// them once at provider construction.
type SecretResolver interface {
	// ResolveSecret returns the credential named by ref.
	ResolveSecret(ctx context.Context, ref inference.SecretRef) (string, error)
}

// NPUConfig holds configuration for the NPU embedding provider. The provider
// speaks HTTP to an OpenVINO sidecar that owns the device; nothing in this
// process links against a vendor SDK.
type NPUConfig struct {
	// BaseURL is the sidecar endpoint. It is required: there is no default
	// address, and NewNPUProvider refuses an empty one.
	BaseURL string

	// Model names the model the sidecar should serve. Defaults to
	// DefaultNPUModel.
	Model string

	// Dimensions is the vector width the sidecar is expected to return. A
	// response of any other width is rejected rather than silently indexed.
	Dimensions int

	// MaxTokens is the model's token ceiling, reported through MaxTokens().
	MaxTokens int

	// Timeout is the per-request HTTP timeout. Defaults to DefaultNPUTimeout.
	Timeout time.Duration

	// MaxBatchSize bounds texts per HTTP request. Defaults to
	// DefaultNPUMaxBatchSize. Callers may submit larger batches; the provider
	// splits them and reassembles results in order.
	MaxBatchSize int

	// APIKey is a bearer token for the sidecar, when it requires one. Prefer
	// APIKeyRef so that credentials do not sit in configuration files.
	APIKey string

	// APIKeyRef is an indirect reference to the sidecar credential, resolved
	// via Secrets at construction time.
	APIKeyRef inference.SecretRef

	// Secrets resolves APIKeyRef. Required only when APIKeyRef is non-zero.
	Secrets SecretResolver

	// HTTPClient optionally supplies a pre-configured client, for callers that
	// need custom TLS or proxy behaviour. When nil a client is built from
	// Timeout.
	HTTPClient *http.Client
}

// DefaultNPUConfig returns the default NPU configuration.
func DefaultNPUConfig() NPUConfig {
	return NPUConfig{
		Model:        DefaultNPUModel,
		Dimensions:   DefaultNPUDimensions,
		MaxTokens:    DefaultNPUMaxTokens,
		Timeout:      DefaultNPUTimeout,
		MaxBatchSize: DefaultNPUMaxBatchSize,
	}
}

// withDefaults returns a copy of cfg with unset fields filled in.
func (c NPUConfig) withDefaults() NPUConfig {
	if c.Model == "" {
		c.Model = DefaultNPUModel
	}
	if c.Dimensions <= 0 {
		c.Dimensions = DefaultNPUDimensions
	}
	if c.MaxTokens <= 0 {
		c.MaxTokens = DefaultNPUMaxTokens
	}
	if c.Timeout <= 0 {
		c.Timeout = DefaultNPUTimeout
	}
	if c.MaxBatchSize <= 0 {
		c.MaxBatchSize = DefaultNPUMaxBatchSize
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	return c
}

// Compile-time assertion that NPUProvider satisfies Provider.
var _ Provider = (*NPUProvider)(nil)

// NPUProvider generates embeddings on an Intel NPU by way of an OpenVINO
// sidecar service.
//
// The provider is deliberately thin: tokenisation, shape bucketing, padding,
// masked mean pooling, the compiled-shape cache, chunking of over-long text,
// and fallback to CPU when the device refuses a shape all belong to the
// sidecar. That keeps the vendor runtime out of this binary entirely and lets
// the device live on a different host from the daemon.
//
// The reason to use this provider is not throughput. Measured at sequence
// length 128, the NPU serves 264 embeddings/s at batch 8 against 224/s on the
// CPU and 144/s on the GPU — roughly CPU-equivalent. The value is that
// embedding work stops competing with builds, tests, and peer review for the
// CPU cores and the GPU.
type NPUProvider struct {
	config NPUConfig
	client *http.Client
	apiKey string

	// lastDevice records the device the sidecar last reported serving from, so
	// operators can tell whether transparent CPU fallback is in effect. Stored
	// atomically because EmbedBatch is safe for concurrent use.
	lastDevice atomic.Pointer[string]
}

// NewNPUProvider creates an NPU embedding provider. The context is used only to
// resolve APIKeyRef, when one is configured; it does not bound the provider's
// lifetime. No request is made to the sidecar here, so construction succeeds
// even when the sidecar is not yet running.
func NewNPUProvider(ctx context.Context, config NPUConfig) (*NPUProvider, error) {
	cfg := config.withDefaults()

	if cfg.BaseURL == "" {
		return nil, &NPUConfigError{
			Field:    "base_url",
			Reason:   "must not be empty",
			sentinel: ErrNPUEndpointRequired,
		}
	}

	apiKey := cfg.APIKey
	if !cfg.APIKeyRef.IsZero() {
		if cfg.Secrets == nil {
			return nil, &NPUConfigError{
				Field:    "secrets",
				Reason:   "APIKeyRef is set but no resolver was supplied",
				sentinel: ErrNPUSecretResolverRequired,
			}
		}
		resolved, err := cfg.Secrets.ResolveSecret(ctx, cfg.APIKeyRef)
		if err != nil {
			return nil, &NPUSecretResolutionError{
				Engine: cfg.APIKeyRef.Engine,
				Path:   cfg.APIKeyRef.Path,
				Err:    err,
			}
		}
		apiKey = resolved
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}

	p := &NPUProvider{config: cfg, client: client, apiKey: apiKey}
	unknown := deviceUnknown
	p.lastDevice.Store(&unknown)
	return p, nil
}

// deviceUnknown is the reported device before any successful request.
const deviceUnknown = "unknown"

// npuEmbedRequest is the sidecar request body.
type npuEmbedRequest struct {
	Texts []string `json:"texts"`
	Model string   `json:"model,omitempty"`
}

// npuEmbedResponse is the sidecar success response body.
type npuEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	Model      string      `json:"model"`
	Dimensions int         `json:"dimensions"`
	Device     string      `json:"device"`

	// TotalTokens and Chunked are part of the wire contract and are decoded so
	// the shape is validated, but they are not surfaced: embedding.Provider
	// returns vectors and has nowhere to put per-request accounting. They are
	// not silently dropped plumbing -- there is no plumbing to drop them from.
	// A caller that needs them should query the sidecar directly.
	TotalTokens int    `json:"total_tokens"`
	Chunked     []bool `json:"chunked"`
}

// npuErrorResponse is the sidecar failure response body.
type npuErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Embed generates an embedding for a single text.
func (p *NPUProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if strings.TrimSpace(text) == "" {
		return nil, &NPUInvalidInputError{Index: -1, Reason: "text must not be empty"}
	}

	out, err := p.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	// EmbedBatch already validates that the count matches the request, so a
	// single-element request yields exactly one vector here.
	return out[0], nil
}

// EmbedBatch generates embeddings for multiple texts, preserving input order.
//
// An empty batch is valid and returns an empty result without contacting the
// sidecar. Batches larger than MaxBatchSize are split across several requests
// and reassembled; the sidecar itself accepts any batch size, so the split is
// only about bounding request bodies.
func (p *NPUProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	for i, text := range texts {
		if strings.TrimSpace(text) == "" {
			return nil, &NPUInvalidInputError{Index: i, Reason: "text must not be empty"}
		}
	}

	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += p.config.MaxBatchSize {
		end := min(start+p.config.MaxBatchSize, len(texts))

		batch, err := p.embedChunk(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
	}

	return out, nil
}

// embedChunk issues one sidecar request for a batch already bounded by
// MaxBatchSize and validates the response against the provider's contract.
func (p *NPUProvider) embedChunk(ctx context.Context, texts []string) (out [][]float32, err error) {
	body, err := json.Marshal(npuEmbedRequest{Texts: texts, Model: p.config.Model})
	if err != nil {
		return nil, &NPUMalformedResponseError{
			Endpoint: p.config.BaseURL,
			Reason:   "could not encode request",
			Err:      err,
		}
	}

	url := p.config.BaseURL + npuEmbedPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, &NPUUnavailableError{Endpoint: p.config.BaseURL, Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, &NPUUnavailableError{Endpoint: p.config.BaseURL, Err: err}
	}
	defer func() {
		// Surface a close failure only when the request otherwise succeeded,
		// so a real embedding error is never masked by a teardown error.
		if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
			out, err = nil, &NPUUnavailableError{Endpoint: p.config.BaseURL, Err: closeErr}
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, p.sidecarError(resp)
	}

	var decoded npuEmbedResponse
	if decodeErr := json.NewDecoder(resp.Body).Decode(&decoded); decodeErr != nil {
		return nil, &NPUMalformedResponseError{
			Endpoint: p.config.BaseURL,
			Reason:   "could not decode response body",
			Err:      decodeErr,
		}
	}

	if len(decoded.Embeddings) != len(texts) {
		return nil, &NPUResultCountError{
			Endpoint: p.config.BaseURL,
			Expected: len(texts),
			Actual:   len(decoded.Embeddings),
		}
	}

	// A vector's meaning depends on the model that produced it, and two models
	// of the same width produce silently incomparable vectors. Reject a mismatch
	// rather than index something that will only show up as degraded retrieval
	// much later. The sidecar refuses a mismatched request too; this is the
	// belt-and-braces check for a sidecar reconfigured underneath us.
	if decoded.Model != "" && decoded.Model != p.config.Model {
		return nil, &NPUModelMismatchError{
			Endpoint:  p.config.BaseURL,
			Requested: p.config.Model,
			Served:    decoded.Model,
		}
	}

	// The sidecar's own declared width must agree with the configuration before
	// the vectors are examined. A sidecar that reports one width and sends
	// another is malfunctioning, and catching it here names the problem instead
	// of reporting it as a per-vector mismatch.
	if decoded.Dimensions != 0 && decoded.Dimensions != p.config.Dimensions {
		return nil, &NPUDimensionsMismatchError{
			Endpoint: p.config.BaseURL,
			Expected: p.config.Dimensions,
			Actual:   decoded.Dimensions,
			Index:    -1,
		}
	}

	for i, vec := range decoded.Embeddings {
		if len(vec) != p.config.Dimensions {
			return nil, &NPUDimensionsMismatchError{
				Endpoint: p.config.BaseURL,
				Expected: p.config.Dimensions,
				Actual:   len(vec),
				Index:    i,
			}
		}
	}

	if decoded.Device != "" {
		device := decoded.Device
		p.lastDevice.Store(&device)
	}

	return decoded.Embeddings, nil
}

// sidecarError builds a typed error from a non-200 sidecar response, preferring
// the sidecar's structured error payload when it supplied one.
func (p *NPUProvider) sidecarError(resp *http.Response) error {
	out := &NPUSidecarError{
		Endpoint:   p.config.BaseURL,
		StatusCode: resp.StatusCode,
	}

	// A body is advisory here: the status alone is a complete error. Read it
	// best-effort to enrich the message.
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, npuMaxErrorBody))
	if readErr != nil || len(raw) == 0 {
		return out
	}

	var decoded npuErrorResponse
	if json.Unmarshal(raw, &decoded) == nil && decoded.Error.Message != "" {
		out.Code = decoded.Error.Code
		out.Message = decoded.Error.Message
		return out
	}

	out.Message = strings.TrimSpace(string(raw))
	return out
}

// npuMaxErrorBody bounds how much of an error body is read into a message.
const npuMaxErrorBody = 4 << 10

// Dimensions returns the configured embedding vector dimensionality.
func (p *NPUProvider) Dimensions() int { return p.config.Dimensions }

// MaxTokens returns the model's token ceiling. For all-MiniLM-L6-v2 this is
// 512, which comes from the model's max_position_embeddings rather than from
// the NPU. The sidecar chunks text that exceeds it.
func (p *NPUProvider) MaxTokens() int { return p.config.MaxTokens }

// Name returns "npu".
func (p *NPUProvider) Name() string { return NPUProviderName }

// NPUProviderName is the registry name for the NPU embedding provider.
const NPUProviderName = "npu"

// BaseURL returns the configured sidecar endpoint. Used by diagnostics.
func (p *NPUProvider) BaseURL() string { return p.config.BaseURL }

// LastDevice reports the device the sidecar last said it served a request
// from — "NPU" or "CPU". It reads "unknown" until the first success.
//
// This matters operationally: the sidecar falls back to the CPU transparently
// so that it always answers, which means a misconfigured device shows up as
// healthy embeddings rather than as an error. LastDevice is how an operator
// notices that the NPU is not actually in use.
func (p *NPUProvider) LastDevice() string {
	if d := p.lastDevice.Load(); d != nil {
		return *d
	}
	return deviceUnknown
}

// NPUHealth is the sidecar's self-report.
type NPUHealth struct {
	// Status is "ok" when the sidecar can serve embeddings.
	Status string `json:"status"`

	// Device is the device currently in use: "NPU" or "CPU".
	Device string `json:"device"`

	// Model is the model the sidecar has loaded.
	Model string `json:"model"`

	// Dimensions is the vector width the loaded model produces.
	Dimensions int `json:"dimensions"`

	// MaxTokens is the loaded model's token ceiling.
	MaxTokens int `json:"max_tokens"`

	// CompiledBuckets lists the (batch, length) shapes already compiled.
	CompiledBuckets [][2]int `json:"compiled_buckets"`

	// NPUAvailable reports whether the level-zero loader found the NPU. When
	// this is false the sidecar is serving from the CPU.
	NPUAvailable bool `json:"npu_available"`

	// FallbackShapes lists (batch, length, device) for every shape that
	// compiled on something other than the preferred device. A non-empty list
	// means the NPU refused those shapes and the CPU took them, which Device
	// alone cannot express when the fallback is only partial.
	FallbackShapes [][3]any `json:"fallback_shapes,omitempty"`
}

// Health queries the sidecar's health endpoint. It is the supported way to
// discover whether the NPU is actually in use, since embedding calls succeed
// either way.
func (p *NPUProvider) Health(ctx context.Context) (health *NPUHealth, err error) {
	url := p.config.BaseURL + npuHealthPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &NPUUnavailableError{Endpoint: p.config.BaseURL, Err: err}
	}
	req.Header.Set("Accept", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, &NPUUnavailableError{Endpoint: p.config.BaseURL, Err: err}
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
			health, err = nil, &NPUUnavailableError{Endpoint: p.config.BaseURL, Err: closeErr}
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, p.sidecarError(resp)
	}

	var decoded NPUHealth
	if decodeErr := json.NewDecoder(resp.Body).Decode(&decoded); decodeErr != nil {
		return nil, &NPUMalformedResponseError{
			Endpoint: p.config.BaseURL,
			Reason:   "could not decode health body",
			Err:      decodeErr,
		}
	}

	if decoded.Device != "" {
		device := decoded.Device
		p.lastDevice.Store(&device)
	}

	return &decoded, nil
}

// Close releases the provider's idle HTTP connections. It is safe to call once;
// the provider must not be used afterwards.
func (p *NPUProvider) Close() error {
	// Only tear down transports this provider owns. A caller-supplied client
	// may be shared, so closing its connections is not ours to do.
	if p.config.HTTPClient == nil {
		p.client.CloseIdleConnections()
	}
	return nil
}
