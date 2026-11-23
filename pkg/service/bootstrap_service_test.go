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
	"testing"

	xkbootstrap "github.com/jeremyhahn/go-xkms/sdk/go/bootstrap"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// newFakeBootstrapService builds a BootstrapService whose fetch seam is
// replaced by fn, so the four mode dispatchers can be exercised without any
// real DNS / TCP / TLS network I/O.
func newFakeBootstrapService(fn bootstrapFetchFunc) *BootstrapService {
	svc := NewBootstrapService(nil)
	svc.fetch = fn
	return svc
}

// captureConfig returns a fetcher that records the last Config it received and
// replies with a fixed bundle, plus a pointer to the captured config.
func captureConfig(bundle *xkbootstrap.CABundle) (bootstrapFetchFunc, *xkbootstrap.Config) {
	captured := &xkbootstrap.Config{}
	fn := func(_ context.Context, cfg xkbootstrap.Config) (*xkbootstrap.CABundle, error) {
		*captured = cfg
		return bundle, nil
	}
	return fn, captured
}

func sampleBundle() *xkbootstrap.CABundle {
	return &xkbootstrap.CABundle{
		PEM:          []byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"),
		Certificates: [][]byte{{0x01, 0x02, 0x03}, {0x04, 0x05}},
		ContentType:  "application/pem-certificate-chain",
	}
}

// -----------------------------------------------------------------------------
// Success paths — one per mode, asserting both the response shape and that the
// correct xkms Mode + sub-config was assembled from the request.
// -----------------------------------------------------------------------------

func TestBootstrapService_Auto_Success(t *testing.T) {
	fn, cfg := captureConfig(sampleBundle())
	svc := newFakeBootstrapService(fn)

	resp, err := svc.Auto(context.Background(), &BootstrapRequest{
		ServerURL:      "https://kms.example.com:8443",
		SPKIPin:        "abc123",
		Hostname:       "kms.example.com",
		NoiseStaticKey: "deadbeef",
	})
	if err != nil {
		t.Fatalf("Auto returned error: %v", err)
	}
	if resp.Method != string(xkbootstrap.ModeAuto) {
		t.Errorf("method = %q, want %q", resp.Method, xkbootstrap.ModeAuto)
	}
	if cfg.Mode != xkbootstrap.ModeAuto {
		t.Errorf("cfg.Mode = %q, want auto", cfg.Mode)
	}
	// Auto must offer Direct plus every supplied pinned strategy.
	if cfg.Direct == nil {
		t.Error("auto cfg.Direct = nil, want direct fallback offered")
	}
	if cfg.DANE == nil || cfg.Noise == nil || cfg.SPKI == nil {
		t.Errorf("auto cfg missing sub-config: DANE=%v Noise=%v SPKI=%v", cfg.DANE, cfg.Noise, cfg.SPKI)
	}
	if cfg.ServerURL != "https://kms.example.com:8443" {
		t.Errorf("cfg.ServerURL = %q", cfg.ServerURL)
	}

	// Bundle is serialised: PEM verbatim, DER certs base64-encoded.
	if resp.BundlePEM == "" {
		t.Error("bundle_pem is empty")
	}
	if len(resp.Certificates) != 2 {
		t.Fatalf("certificates len = %d, want 2", len(resp.Certificates))
	}
	want := base64.StdEncoding.EncodeToString([]byte{0x01, 0x02, 0x03})
	if resp.Certificates[0] != want {
		t.Errorf("certificates[0] = %q, want %q", resp.Certificates[0], want)
	}
	if resp.ContentType != "application/pem-certificate-chain" {
		t.Errorf("content_type = %q", resp.ContentType)
	}
}

func TestBootstrapService_Dane_Success(t *testing.T) {
	fn, cfg := captureConfig(sampleBundle())
	svc := newFakeBootstrapService(fn)

	resp, err := svc.Dane(context.Background(), &BootstrapRequest{
		ServerURL: "https://kms.example.com:8443",
		Hostname:  "kms.example.com",
		DNSServer: "8.8.8.8:53",
	})
	if err != nil {
		t.Fatalf("Dane returned error: %v", err)
	}
	if cfg.Mode != xkbootstrap.ModeDANE {
		t.Errorf("cfg.Mode = %q, want dane", cfg.Mode)
	}
	if cfg.DANE == nil || cfg.DANE.Hostname != "kms.example.com" || cfg.DANE.DNSServer != "8.8.8.8:53" {
		t.Errorf("cfg.DANE not populated from request: %+v", cfg.DANE)
	}
	if resp.Method != string(xkbootstrap.ModeDANE) {
		t.Errorf("method = %q, want dane", resp.Method)
	}
}

func TestBootstrapService_Noise_Success(t *testing.T) {
	fn, cfg := captureConfig(sampleBundle())
	svc := newFakeBootstrapService(fn)

	resp, err := svc.Noise(context.Background(), &BootstrapRequest{
		ServerURL:      "https://kms.example.com:8443",
		NoiseAddr:      "kms.example.com:8445",
		NoiseStaticKey: "deadbeefcafe",
	})
	if err != nil {
		t.Fatalf("Noise returned error: %v", err)
	}
	if cfg.Mode != xkbootstrap.ModeNoise {
		t.Errorf("cfg.Mode = %q, want noise", cfg.Mode)
	}
	if cfg.Noise == nil || cfg.Noise.ServerAddr != "kms.example.com:8445" || cfg.Noise.ServerStaticKey != "deadbeefcafe" {
		t.Errorf("cfg.Noise not populated from request: %+v", cfg.Noise)
	}
	if resp.Method != string(xkbootstrap.ModeNoise) {
		t.Errorf("method = %q, want noise", resp.Method)
	}
}

func TestBootstrapService_Spki_Success(t *testing.T) {
	fn, cfg := captureConfig(sampleBundle())
	svc := newFakeBootstrapService(fn)

	resp, err := svc.Spki(context.Background(), &BootstrapRequest{
		ServerURL: "https://kms.example.com:8443",
		SPKIPin:   "0123456789abcdef",
	})
	if err != nil {
		t.Fatalf("Spki returned error: %v", err)
	}
	if cfg.Mode != xkbootstrap.ModeSPKI {
		t.Errorf("cfg.Mode = %q, want spki", cfg.Mode)
	}
	if cfg.SPKI == nil || cfg.SPKI.Pin != "0123456789abcdef" {
		t.Errorf("cfg.SPKI not populated from request: %+v", cfg.SPKI)
	}
	if resp.Method != string(xkbootstrap.ModeSPKI) {
		t.Errorf("method = %q, want spki", resp.Method)
	}
}

// A Noise request carrying only the P-256 key attaches the Noise config: the
// approved suite does not need the Curve25519 key.
func TestBootstrapService_Noise_P256KeyOnly(t *testing.T) {
	fn, cfg := captureConfig(sampleBundle())
	svc := newFakeBootstrapService(fn)

	_, err := svc.Noise(context.Background(), &BootstrapRequest{
		ServerURL:          "https://kms.example.com:8443",
		NoiseP256StaticKey: "04abcdef",
	})
	if err != nil {
		t.Fatalf("Noise returned error: %v", err)
	}
	if cfg.Noise == nil {
		t.Fatal("cfg.Noise = nil, want the P-256 key attached")
	}
	if cfg.Noise.ServerP256StaticKey != "04abcdef" {
		t.Errorf("cfg.Noise.ServerP256StaticKey = %q, want 04abcdef", cfg.Noise.ServerP256StaticKey)
	}
	if cfg.Noise.ServerStaticKey != "" {
		t.Errorf("cfg.Noise.ServerStaticKey = %q, want empty", cfg.Noise.ServerStaticKey)
	}
	if cfg.Noise.Suite != 0 {
		t.Errorf("cfg.Noise.Suite = %d, want 0 (policy)", cfg.Noise.Suite)
	}
}

// A pinned suite name maps onto its go-xkms suite byte.
func TestBootstrapService_Noise_PinnedSuite(t *testing.T) {
	for _, tc := range []struct {
		name string
		want byte
	}{
		{BootstrapNoiseSuiteP256, xkbootstrap.SuiteP256AESGCM},
		{BootstrapNoiseSuiteNK, xkbootstrap.SuiteNoiseNK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fn, cfg := captureConfig(sampleBundle())
			svc := newFakeBootstrapService(fn)

			_, err := svc.Noise(context.Background(), &BootstrapRequest{
				ServerURL:          "https://kms.example.com:8443",
				NoiseStaticKey:     "deadbeef",
				NoiseP256StaticKey: "04abcdef",
				NoiseSuite:         tc.name,
			})
			if err != nil {
				t.Fatalf("Noise returned error: %v", err)
			}
			if cfg.Noise == nil || cfg.Noise.Suite != tc.want {
				t.Fatalf("cfg.Noise = %+v, want Suite %d", cfg.Noise, tc.want)
			}
		})
	}
}

// An unknown suite name is refused as invalid input before any fetch.
func TestBootstrapService_Noise_UnknownSuite_IsInvalidInput(t *testing.T) {
	svc := newFakeBootstrapService(func(context.Context, xkbootstrap.Config) (*xkbootstrap.CABundle, error) {
		t.Fatal("fetch must not be called for an unknown noise suite")
		return nil, nil
	})

	for name, call := range map[string]func(context.Context, *BootstrapRequest) (*BootstrapBundleResponse, error){
		"noise": svc.Noise,
		"auto":  svc.Auto,
	} {
		t.Run(name, func(t *testing.T) {
			resp, err := call(context.Background(), &BootstrapRequest{
				ServerURL:          "https://kms.example.com:8443",
				NoiseP256StaticKey: "04abcdef",
				NoiseSuite:         "noise_ik",
			})
			if resp != nil {
				t.Errorf("resp = %v, want nil", resp)
			}
			var qbErr *errors.QBError
			if !stderrors.As(err, &qbErr) || qbErr.Code != errors.ErrInvalidInput {
				t.Fatalf("err = %v, want invalid-input QBError", err)
			}
			if !stderrors.Is(err, errBootstrapUnknownNoiseSuite) {
				t.Errorf("err does not wrap errBootstrapUnknownNoiseSuite: %v", err)
			}
		})
	}
}

// The Noise suite refusals are caller configuration, not server faults: each
// maps to invalid input and keeps its sentinel reachable through errors.Is.
func TestBootstrapService_SuiteErrors_AreInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		sentinel error
	}{
		{"key missing", &xkbootstrap.SuiteError{Suite: xkbootstrap.SuiteP256AESGCM, Cause: xkbootstrap.ErrSuiteKeyMissing}, xkbootstrap.ErrSuiteKeyMissing},
		{"key invalid", &xkbootstrap.SuiteError{Suite: xkbootstrap.SuiteNoiseNK, Cause: xkbootstrap.ErrSuiteKeyInvalid}, xkbootstrap.ErrSuiteKeyInvalid},
		{"unavailable", &xkbootstrap.SuiteError{Suite: xkbootstrap.SuiteNoiseNK, Cause: xkbootstrap.ErrSuiteUnavailable}, xkbootstrap.ErrSuiteUnavailable},
		{"no suite", xkbootstrap.ErrNoSuite, xkbootstrap.ErrNoSuite},
		{"no suite in fetch error", &xkbootstrap.FetchError{Mode: xkbootstrap.ModeNoise, Cause: xkbootstrap.ErrNoSuite}, xkbootstrap.ErrNoSuite},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newFakeBootstrapService(func(context.Context, xkbootstrap.Config) (*xkbootstrap.CABundle, error) {
				return nil, tc.err
			})
			_, err := svc.Noise(context.Background(), &BootstrapRequest{
				ServerURL:          "https://kms.example.com:8443",
				NoiseP256StaticKey: "04abcdef",
			})
			var qbErr *errors.QBError
			if !stderrors.As(err, &qbErr) {
				t.Fatalf("err = %v, want *errors.QBError", err)
			}
			if qbErr.Code != errors.ErrInvalidInput {
				t.Errorf("code = %v, want ErrInvalidInput", qbErr.Code)
			}
			if !stderrors.Is(err, tc.sentinel) {
				t.Errorf("err does not wrap %v: %v", tc.sentinel, err)
			}
		})
	}
}

// Auto with only the P-256 key still offers the Noise strategy.
func TestBootstrapService_Auto_P256Material(t *testing.T) {
	fn, cfg := captureConfig(sampleBundle())
	svc := newFakeBootstrapService(fn)

	_, err := svc.Auto(context.Background(), &BootstrapRequest{
		ServerURL:          "https://kms.example.com:8443",
		NoiseP256StaticKey: "04abcdef",
		NoiseSuite:         BootstrapNoiseSuiteP256,
	})
	if err != nil {
		t.Fatalf("Auto returned error: %v", err)
	}
	if cfg.Noise == nil || cfg.Noise.ServerP256StaticKey != "04abcdef" || cfg.Noise.Suite != xkbootstrap.SuiteP256AESGCM {
		t.Fatalf("auto cfg.Noise = %+v, want the P-256 key and pinned suite", cfg.Noise)
	}
}

// -----------------------------------------------------------------------------
// Error paths.
// -----------------------------------------------------------------------------

func TestBootstrapService_NilRequest(t *testing.T) {
	svc := newFakeBootstrapService(func(context.Context, xkbootstrap.Config) (*xkbootstrap.CABundle, error) {
		t.Fatal("fetch must not be called for a nil request")
		return nil, nil
	})

	for _, tc := range []struct {
		name string
		call func() (*BootstrapBundleResponse, error)
	}{
		{"auto", func() (*BootstrapBundleResponse, error) { return svc.Auto(context.Background(), nil) }},
		{"dane", func() (*BootstrapBundleResponse, error) { return svc.Dane(context.Background(), nil) }},
		{"noise", func() (*BootstrapBundleResponse, error) { return svc.Noise(context.Background(), nil) }},
		{"spki", func() (*BootstrapBundleResponse, error) { return svc.Spki(context.Background(), nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := tc.call()
			if resp != nil {
				t.Errorf("resp = %v, want nil", resp)
			}
			if !stderrors.Is(err, ErrBootstrapNilRequest) {
				t.Fatalf("err = %v, want ErrBootstrapNilRequest", err)
			}
		})
	}
}

// A configuration-shaped xkms error (e.g. missing server URL or unconfigured
// mode) must surface as a typed invalid-input QBError, mapping to HTTP 400.
func TestBootstrapService_ConfigError_IsInvalidInput(t *testing.T) {
	svc := newFakeBootstrapService(func(context.Context, xkbootstrap.Config) (*xkbootstrap.CABundle, error) {
		return nil, xkbootstrap.ErrServerURLRequired
	})

	resp, err := svc.Dane(context.Background(), &BootstrapRequest{Hostname: "kms.example.com"})
	if resp != nil {
		t.Errorf("resp = %v, want nil", resp)
	}
	var qbErr *errors.QBError
	if !stderrors.As(err, &qbErr) {
		t.Fatalf("err = %v, want *errors.QBError", err)
	}
	if qbErr.Code != errors.ErrInvalidInput {
		t.Errorf("code = %v, want ErrInvalidInput", qbErr.Code)
	}
	if !stderrors.Is(err, xkbootstrap.ErrServerURLRequired) {
		t.Errorf("err does not wrap ErrServerURLRequired: %v", err)
	}
}

// A verification / transport failure (xkms FetchError) must surface as a typed
// internal QBError, mapping to HTTP 500.
func TestBootstrapService_FetchError_IsInternal(t *testing.T) {
	cause := &xkbootstrap.FetchError{Mode: xkbootstrap.ModeSPKI, Cause: stderrors.New("pin mismatch")}
	svc := newFakeBootstrapService(func(context.Context, xkbootstrap.Config) (*xkbootstrap.CABundle, error) {
		return nil, cause
	})

	_, err := svc.Spki(context.Background(), &BootstrapRequest{
		ServerURL: "https://kms.example.com:8443",
		SPKIPin:   "abc",
	})
	var qbErr *errors.QBError
	if !stderrors.As(err, &qbErr) {
		t.Fatalf("err = %v, want *errors.QBError", err)
	}
	if qbErr.Code != errors.ErrInternal {
		t.Errorf("code = %v, want ErrInternal", qbErr.Code)
	}
	if !stderrors.Is(err, xkbootstrap.ErrFetch) {
		t.Errorf("err does not match ErrFetch sentinel: %v", err)
	}
}

// A nil bundle with a nil error from xkms is a contract violation that must be
// reported, never returned as a successful empty response.
func TestBootstrapService_NilBundle(t *testing.T) {
	svc := newFakeBootstrapService(func(context.Context, xkbootstrap.Config) (*xkbootstrap.CABundle, error) {
		return nil, nil
	})

	resp, err := svc.Auto(context.Background(), &BootstrapRequest{ServerURL: "https://x:8443"})
	if resp != nil {
		t.Errorf("resp = %v, want nil", resp)
	}
	if !stderrors.Is(err, ErrBootstrapNilBundle) {
		t.Fatalf("err = %v, want ErrBootstrapNilBundle", err)
	}
}

// A cancelled context is rejected before any outbound fetch is attempted.
func TestBootstrapService_ContextCancelled(t *testing.T) {
	svc := newFakeBootstrapService(func(context.Context, xkbootstrap.Config) (*xkbootstrap.CABundle, error) {
		t.Fatal("fetch must not be called once the context is cancelled")
		return nil, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.Auto(ctx, &BootstrapRequest{ServerURL: "https://x:8443"})
	var qbErr *errors.QBError
	if !stderrors.As(err, &qbErr) {
		t.Fatalf("err = %v, want *errors.QBError", err)
	}
	if qbErr.Code != errors.ErrInternal {
		t.Errorf("code = %v, want ErrInternal", qbErr.Code)
	}
}

// Auto with no server URL and no material offers no strategy; xkms reports
// ErrModeNotConfigured which the service maps to invalid input.
func TestBootstrapService_Auto_NoMaterial_NoDirect(t *testing.T) {
	var gotCfg xkbootstrap.Config
	svc := newFakeBootstrapService(func(_ context.Context, cfg xkbootstrap.Config) (*xkbootstrap.CABundle, error) {
		gotCfg = cfg
		return nil, xkbootstrap.ErrModeNotConfigured
	})

	_, err := svc.Auto(context.Background(), &BootstrapRequest{})
	if gotCfg.Direct != nil {
		t.Error("Direct must not be offered when ServerURL is empty")
	}
	var qbErr *errors.QBError
	if !stderrors.As(err, &qbErr) || qbErr.Code != errors.ErrInvalidInput {
		t.Fatalf("err = %v, want invalid-input QBError", err)
	}
}
