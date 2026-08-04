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

package service

import (
	"context"
	"encoding/base64"
	stderrors "errors"
	"log/slog"
	"time"

	xkbootstrap "github.com/jeremyhahn/go-xkms/sdk/go/bootstrap"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// Bootstrap (trust-anchor discovery) service.
//
// The BootstrapServicer is a pure composition layer over the canonical
// go-xkms trust-bootstrap library (go-xkms/pkg/trust/bootstrap, reached here
// through its SDK re-export). It does NOT reimplement DANE/TLSA verification,
// the Noise_NK handshake, or SPKI pinning — every one of those strategies
// lives in go-truststrap and is dispatched by xkms's single Fetch entry
// point. This service maps the four REST bootstrap modes onto that Fetch call
// and serialises the resulting CA bundle.
//
// Each method performs a single outbound fetch on behalf of the caller: the
// daemon acts as the bootstrap relay for clients (e.g. the web UI) that cannot
// themselves perform raw DNSSEC, Noise, or pinned-TLS handshakes. The caller
// supplies the target server URL plus whatever trust material the chosen mode
// requires; this service verifies the server with that material and returns
// the established trust anchor.
// =============================================================================

// bootstrapPerMethodTimeout caps each individual outbound bootstrap attempt so
// a stalled DNS / TCP / TLS handshake cannot pin a request goroutine open past
// the deadline. It is applied as the truststrap connect/operation timeout for
// every mode and as the per-fallback budget under auto.
const bootstrapPerMethodTimeout = 10 * time.Second

// BootstrapServicer is the trust-bootstrap facade the REST handlers consult
// for the /api/v1/bootstrap/{auto,dane,noise,spki} endpoints. Each method
// dispatches one canonical xkms bootstrap.Fetch using the strategy named by
// the method, returning the established CA bundle. Implementations are pure
// composition over go-xkms — they hold no trust state of their own.
type BootstrapServicer interface {
	// Auto delegates trust-anchor selection to xkms: it tries every
	// configured strategy (direct + any supplied DANE/Noise/SPKI material)
	// in priority order and returns the first verified bundle.
	Auto(ctx context.Context, req *BootstrapRequest) (*BootstrapBundleResponse, error)

	// Dane verifies the server certificate against DNSSEC-validated TLSA
	// records before fetching the bundle over HTTPS.
	Dane(ctx context.Context, req *BootstrapRequest) (*BootstrapBundleResponse, error)

	// Noise establishes a Noise_NK channel pinned by the server's static
	// public key and fetches the bundle in-band.
	Noise(ctx context.Context, req *BootstrapRequest) (*BootstrapBundleResponse, error)

	// Spki verifies the server certificate against a pre-shared SHA-256 pin
	// of its SubjectPublicKeyInfo and fetches the bundle over HTTPS.
	Spki(ctx context.Context, req *BootstrapRequest) (*BootstrapBundleResponse, error)
}

// BootstrapRequest carries the target plus the per-mode trust material a
// single bootstrap attempt needs. ServerURL is required by every mode except
// pure Noise (which can derive its address from ServerURL or take an explicit
// NoiseAddr). The StoreType / Algorithm filters are accepted for parity with
// the typed SDK request shape; they are reserved for server-side bundle
// filtering and do not affect the verification strategy.
type BootstrapRequest struct {
	// ServerURL is the canonical server URL to bootstrap trust against
	// (e.g. "https://kms.example.com:8443").
	ServerURL string `json:"server_url,omitempty"`

	// StoreType optionally filters the returned bundle by store class
	// ("root", "intermediate", "leaf", "end-entity", or empty for all).
	StoreType string `json:"store_type,omitempty"`

	// Algorithm optionally filters the returned bundle by key algorithm
	// ("RSA", "ECDSA", "Ed25519", or empty for all).
	Algorithm string `json:"algorithm,omitempty"`

	// Hostname is the DNSSEC zone under which TLSA records are published.
	// Required for Dane; optional auto knob.
	Hostname string `json:"hostname,omitempty"`

	// DNSServer optionally pins the recursive resolver for the TLSA lookup
	// (e.g. "8.8.8.8:53"). Dane only.
	DNSServer string `json:"dns_server,omitempty"`

	// NoiseAddr is the explicit Noise bootstrap address ("host:port"). When
	// empty the address is derived from ServerURL. Noise only.
	NoiseAddr string `json:"noise_addr,omitempty"`

	// NoiseStaticKey is the hex-encoded 32-byte Curve25519 server public
	// key. Required for Noise; optional auto knob.
	NoiseStaticKey string `json:"noise_static_key,omitempty"`

	// SPKIPin is the hex-encoded SHA-256 SPKI pin of the server certificate
	// (64 hex chars). Required for Spki; optional auto knob.
	SPKIPin string `json:"spki_pin,omitempty"`
}

// BootstrapBundleResponse is the established trust anchor. It mirrors the
// typed SDK BootstrapBundleResponse / truststrap CA bundle shape.
type BootstrapBundleResponse struct {
	// Method is the bootstrap strategy that produced this bundle.
	Method string `json:"method"`

	// BundlePEM is the PEM-encoded certificate chain.
	BundlePEM string `json:"bundle_pem"`

	// Certificates are the base64-encoded individual DER certificates that
	// make up the bundle.
	Certificates []string `json:"certificates,omitempty"`

	// ContentType records what the server reported the bundle as
	// (e.g. "application/pem-certificate-chain").
	ContentType string `json:"content_type"`

	// Metadata carries optional descriptive key/value pairs about how the
	// bundle was verified.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Typed sentinel errors for the bootstrap service.
var (
	// ErrBootstrapNilRequest indicates a nil request was supplied.
	ErrBootstrapNilRequest = &errors.QBError{Code: errors.ErrInvalidInput, Op: "BootstrapService", Err: errBootstrapNilRequest}

	// ErrBootstrapNilBundle indicates xkms returned a nil bundle without an
	// error — a contract violation that must surface rather than panic.
	ErrBootstrapNilBundle = &errors.QBError{Code: errors.ErrInternal, Op: "BootstrapService", Err: errBootstrapNilBundle}
)

var (
	errBootstrapNilRequest = stderrors.New("bootstrap request must not be nil")
	errBootstrapNilBundle  = stderrors.New("bootstrap produced no CA bundle")
)

// bootstrapFetchFunc is the seam onto the canonical xkms bootstrap.Fetch. It
// is overridable in tests so the service can be exercised without performing
// real DNS / TCP / TLS network I/O.
type bootstrapFetchFunc func(ctx context.Context, cfg xkbootstrap.Config) (*xkbootstrap.CABundle, error)

// BootstrapService is the production BootstrapServicer. It composes the
// canonical xkms trust-bootstrap library and holds no trust state.
type BootstrapService struct {
	fetch  bootstrapFetchFunc
	logger *slog.Logger
}

// compile-time assertion that BootstrapService satisfies BootstrapServicer.
var _ BootstrapServicer = (*BootstrapService)(nil)

// NewBootstrapService creates a BootstrapService backed by the canonical xkms
// bootstrap.Fetch dispatcher. A nil logger falls back to slog.Default so
// bootstrap attempts always leave an audit trail.
func NewBootstrapService(logger *slog.Logger) *BootstrapService {
	if logger == nil {
		logger = slog.Default()
	}
	return &BootstrapService{fetch: xkbootstrap.Fetch, logger: logger}
}

// Auto bootstraps trust by letting xkms try every configured strategy.
func (s *BootstrapService) Auto(ctx context.Context, req *BootstrapRequest) (*BootstrapBundleResponse, error) {
	if req == nil {
		return nil, ErrBootstrapNilRequest
	}
	cfg := s.baseConfig(req)
	cfg.Mode = xkbootstrap.ModeAuto
	// Direct is always offered under auto: when the transport already
	// enforces trust (localhost, mTLS) a direct HTTPS fetch is the
	// last-resort fallback after any supplied pinned strategies.
	if req.ServerURL != "" {
		cfg.Direct = &xkbootstrap.DirectConfig{}
	}
	s.applyDANE(&cfg, req)
	s.applyNoise(&cfg, req)
	s.applySPKI(&cfg, req)
	return s.run(ctx, "BootstrapService.Auto", xkbootstrap.ModeAuto, cfg)
}

// Dane bootstraps trust via DNSSEC-validated TLSA records.
func (s *BootstrapService) Dane(ctx context.Context, req *BootstrapRequest) (*BootstrapBundleResponse, error) {
	if req == nil {
		return nil, ErrBootstrapNilRequest
	}
	cfg := s.baseConfig(req)
	cfg.Mode = xkbootstrap.ModeDANE
	s.applyDANE(&cfg, req)
	return s.run(ctx, "BootstrapService.Dane", xkbootstrap.ModeDANE, cfg)
}

// Noise bootstraps trust via a Noise_NK handshake pinned by the server key.
func (s *BootstrapService) Noise(ctx context.Context, req *BootstrapRequest) (*BootstrapBundleResponse, error) {
	if req == nil {
		return nil, ErrBootstrapNilRequest
	}
	cfg := s.baseConfig(req)
	cfg.Mode = xkbootstrap.ModeNoise
	s.applyNoise(&cfg, req)
	return s.run(ctx, "BootstrapService.Noise", xkbootstrap.ModeNoise, cfg)
}

// Spki bootstraps trust via a pre-shared SHA-256 SPKI pin.
func (s *BootstrapService) Spki(ctx context.Context, req *BootstrapRequest) (*BootstrapBundleResponse, error) {
	if req == nil {
		return nil, ErrBootstrapNilRequest
	}
	cfg := s.baseConfig(req)
	cfg.Mode = xkbootstrap.ModeSPKI
	s.applySPKI(&cfg, req)
	return s.run(ctx, "BootstrapService.Spki", xkbootstrap.ModeSPKI, cfg)
}

// baseConfig builds the mode-independent portion of an xkms bootstrap Config.
func (s *BootstrapService) baseConfig(req *BootstrapRequest) xkbootstrap.Config {
	return xkbootstrap.Config{
		ServerURL:        req.ServerURL,
		PerMethodTimeout: bootstrapPerMethodTimeout,
		Logger:           s.logger,
	}
}

// applyDANE attaches a DANE sub-config when DANE material is present.
func (s *BootstrapService) applyDANE(cfg *xkbootstrap.Config, req *BootstrapRequest) {
	if req.Hostname == "" {
		return
	}
	cfg.DANE = &xkbootstrap.DANEConfig{
		Hostname:  req.Hostname,
		DNSServer: req.DNSServer,
	}
}

// applyNoise attaches a Noise sub-config when Noise material is present.
func (s *BootstrapService) applyNoise(cfg *xkbootstrap.Config, req *BootstrapRequest) {
	if req.NoiseStaticKey == "" {
		return
	}
	cfg.Noise = &xkbootstrap.NoiseConfig{
		ServerAddr:      req.NoiseAddr,
		ServerStaticKey: req.NoiseStaticKey,
	}
}

// applySPKI attaches an SPKI sub-config when an SPKI pin is present.
func (s *BootstrapService) applySPKI(cfg *xkbootstrap.Config, req *BootstrapRequest) {
	if req.SPKIPin == "" {
		return
	}
	cfg.SPKI = &xkbootstrap.SPKIConfig{Pin: req.SPKIPin}
}

// run dispatches the prepared config to xkms and serialises the result.
func (s *BootstrapService) run(ctx context.Context, op string, mode xkbootstrap.Mode, cfg xkbootstrap.Config) (*BootstrapBundleResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: op, Err: err}
	}

	bundle, err := s.fetch(ctx, cfg)
	if err != nil {
		return nil, mapBootstrapError(op, err)
	}
	if bundle == nil {
		return nil, ErrBootstrapNilBundle
	}

	return bundleResponse(mode, bundle), nil
}

// bundleResponse converts an xkms CABundle into the wire response shape.
func bundleResponse(mode xkbootstrap.Mode, bundle *xkbootstrap.CABundle) *BootstrapBundleResponse {
	resp := &BootstrapBundleResponse{
		Method:      string(mode),
		BundlePEM:   string(bundle.PEM),
		ContentType: bundle.ContentType,
	}
	if len(bundle.Certificates) > 0 {
		resp.Certificates = make([]string, len(bundle.Certificates))
		for i, der := range bundle.Certificates {
			resp.Certificates[i] = base64.StdEncoding.EncodeToString(der)
		}
	}
	return resp
}

// mapBootstrapError maps an xkms bootstrap error onto a typed QBError. The
// configuration-shaped sentinels (invalid mode, missing per-mode config,
// missing server URL) are caller errors -> invalid input; everything else
// (a verification or transport failure) is an internal fetch failure.
func mapBootstrapError(op string, err error) error {
	switch {
	case stderrors.Is(err, xkbootstrap.ErrInvalidMode),
		stderrors.Is(err, xkbootstrap.ErrModeNotConfigured),
		stderrors.Is(err, xkbootstrap.ErrServerURLRequired):
		return &errors.QBError{Code: errors.ErrInvalidInput, Op: op, Err: err}
	default:
		return &errors.QBError{Code: errors.ErrInternal, Op: op, Err: err}
	}
}
