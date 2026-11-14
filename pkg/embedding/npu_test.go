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
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/inference"
)

// vec returns a deterministic vector of the given width so tests can assert on
// exact values without depending on a model.
func vec(width int, seed float32) []float32 {
	out := make([]float32, width)
	for i := range out {
		out[i] = seed + float32(i)/1000.0
	}
	return out
}

// recordingSidecar is a real HTTP server standing in for the Python sidecar. It
// is not a mock of the code under test: the provider's whole job is to speak
// this protocol correctly, so the protocol is what the test drives.
type recordingSidecar struct {
	// testing.TB so the same sidecar serves both tests and benchmarks.
	tb testing.TB

	mu       sync.Mutex
	requests []npuEmbedRequest

	// respond builds a response for a decoded request. When nil a well-formed
	// response of the right arity is returned.
	respond func(w http.ResponseWriter, req npuEmbedRequest)

	dimensions int
	device     string
}

func newRecordingSidecar(tb testing.TB) *recordingSidecar {
	tb.Helper()
	return &recordingSidecar{tb: tb, dimensions: DefaultNPUDimensions, device: "NPU"}
}

func (s *recordingSidecar) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == npuHealthPath {
		s.serveHealth(w)
		return
	}

	if r.URL.Path != npuEmbedPath {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req npuEmbedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()

	if s.respond != nil {
		s.respond(w, req)
		return
	}

	embeddings := make([][]float32, len(req.Texts))
	for i := range req.Texts {
		embeddings[i] = vec(s.dimensions, float32(i))
	}
	s.writeJSON(w, npuEmbedResponse{
		Embeddings: embeddings,
		Model:      req.Model,
		Dimensions: s.dimensions,
		Device:     s.device,
	})
}

func (s *recordingSidecar) serveHealth(w http.ResponseWriter) {
	s.writeJSON(w, NPUHealth{
		Status:          "ok",
		Device:          s.device,
		Model:           DefaultNPUModel,
		Dimensions:      s.dimensions,
		MaxTokens:       DefaultNPUMaxTokens,
		CompiledBuckets: [][2]int{{1, 128}, {8, 128}},
		NPUAvailable:    s.device == "NPU",
	})
}

func (s *recordingSidecar) writeJSON(w http.ResponseWriter, v any) {
	s.tb.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.tb.Errorf("sidecar could not encode response: %v", err)
	}
}

// recorded returns a copy of the requests the sidecar observed.
func (s *recordingSidecar) recorded() []npuEmbedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]npuEmbedRequest, len(s.requests))
	copy(out, s.requests)
	return out
}

// startSidecar wires a recordingSidecar to a live loopback server.
func startSidecar(tb testing.TB, s *recordingSidecar) *httptest.Server {
	tb.Helper()
	srv := httptest.NewServer(s)
	tb.Cleanup(srv.Close)
	return srv
}

// newTestProvider builds a provider pointed at srv.
func newTestProvider(tb testing.TB, srv *httptest.Server, mutate func(*NPUConfig)) *NPUProvider {
	tb.Helper()
	cfg := DefaultNPUConfig()
	cfg.BaseURL = srv.URL
	if mutate != nil {
		mutate(&cfg)
	}
	p, err := NewNPUProvider(context.Background(), cfg)
	if err != nil {
		tb.Fatalf("NewNPUProvider: unexpected error: %v", err)
	}
	tb.Cleanup(func() {
		if closeErr := p.Close(); closeErr != nil {
			tb.Errorf("Close: unexpected error: %v", closeErr)
		}
	})
	return p
}

// -----------------------------------------------------------------------------
// Happy path
// -----------------------------------------------------------------------------

func TestNPUProviderEmbedReturnsVector(t *testing.T) {
	sidecar := newRecordingSidecar(t)
	srv := startSidecar(t, sidecar)
	p := newTestProvider(t, srv, nil)

	got, err := p.Embed(context.Background(), "hello npu")
	if err != nil {
		t.Fatalf("Embed: unexpected error: %v", err)
	}
	if len(got) != DefaultNPUDimensions {
		t.Fatalf("Embed: got width %d, want %d", len(got), DefaultNPUDimensions)
	}

	reqs := sidecar.recorded()
	if len(reqs) != 1 {
		t.Fatalf("Embed: sidecar saw %d requests, want 1", len(reqs))
	}
	if len(reqs[0].Texts) != 1 || reqs[0].Texts[0] != "hello npu" {
		t.Errorf("Embed: sidecar saw texts %q, want [\"hello npu\"]", reqs[0].Texts)
	}
	if reqs[0].Model != DefaultNPUModel {
		t.Errorf("Embed: sidecar saw model %q, want %q", reqs[0].Model, DefaultNPUModel)
	}
}

func TestNPUProviderEmbedBatchPreservesOrder(t *testing.T) {
	sidecar := newRecordingSidecar(t)
	// Echo an index-tagged first component so order is verifiable.
	sidecar.respond = func(w http.ResponseWriter, req npuEmbedRequest) {
		embeddings := make([][]float32, len(req.Texts))
		for i, text := range req.Texts {
			v := vec(DefaultNPUDimensions, 0)
			// Encode the text's trailing digit into the vector head.
			v[0] = float32(text[len(text)-1] - '0')
			embeddings[i] = v
		}
		sidecar.writeJSON(w, npuEmbedResponse{
			Embeddings: embeddings,
			Dimensions: DefaultNPUDimensions,
			Device:     "NPU",
		})
	}
	srv := startSidecar(t, sidecar)
	p := newTestProvider(t, srv, nil)

	texts := []string{"t0", "t1", "t2", "t3", "t4"}
	got, err := p.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("EmbedBatch: unexpected error: %v", err)
	}
	if len(got) != len(texts) {
		t.Fatalf("EmbedBatch: got %d vectors, want %d", len(got), len(texts))
	}
	for i, v := range got {
		if int(v[0]) != i {
			t.Errorf("EmbedBatch: vector %d carries tag %v, want %d — order not preserved", i, v[0], i)
		}
	}
}

// -----------------------------------------------------------------------------
// Empty input
// -----------------------------------------------------------------------------

func TestNPUProviderEmbedBatchEmptySliceMakesNoRequest(t *testing.T) {
	sidecar := newRecordingSidecar(t)
	srv := startSidecar(t, sidecar)
	p := newTestProvider(t, srv, nil)

	got, err := p.EmbedBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("EmbedBatch(nil): unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("EmbedBatch(nil): got %d vectors, want 0", len(got))
	}
	if n := len(sidecar.recorded()); n != 0 {
		t.Errorf("EmbedBatch(nil): sidecar saw %d requests, want 0", n)
	}
}

func TestNPUProviderEmbedRejectsEmptyText(t *testing.T) {
	sidecar := newRecordingSidecar(t)
	srv := startSidecar(t, sidecar)
	p := newTestProvider(t, srv, nil)

	for _, text := range []string{"", "   ", "\t\n"} {
		_, err := p.Embed(context.Background(), text)
		if err == nil {
			t.Fatalf("Embed(%q): expected an error, got nil", text)
		}
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("Embed(%q): error %v does not match ErrInvalidInput", text, err)
		}
		var typed *NPUInvalidInputError
		if !errors.As(err, &typed) {
			t.Fatalf("Embed(%q): error %v is not an *NPUInvalidInputError", text, err)
		}
		if typed.Index != -1 {
			t.Errorf("Embed(%q): typed.Index = %d, want -1 for a single-text call", text, typed.Index)
		}
	}

	if n := len(sidecar.recorded()); n != 0 {
		t.Errorf("empty text must be rejected before any request; sidecar saw %d", n)
	}
}

func TestNPUProviderEmbedBatchReportsOffendingIndex(t *testing.T) {
	sidecar := newRecordingSidecar(t)
	srv := startSidecar(t, sidecar)
	p := newTestProvider(t, srv, nil)

	_, err := p.EmbedBatch(context.Background(), []string{"ok", "also ok", ""})
	if err == nil {
		t.Fatal("EmbedBatch: expected an error for an empty element, got nil")
	}
	var typed *NPUInvalidInputError
	if !errors.As(err, &typed) {
		t.Fatalf("EmbedBatch: error %v is not an *NPUInvalidInputError", err)
	}
	if typed.Index != 2 {
		t.Errorf("EmbedBatch: typed.Index = %d, want 2", typed.Index)
	}
}

// -----------------------------------------------------------------------------
// Unreachable sidecar
// -----------------------------------------------------------------------------

func TestNPUProviderUnreachableSidecar(t *testing.T) {
	// Bind a server then close it, so the port is almost certainly dead. This
	// exercises the real dial path rather than simulating a failure.
	srv := httptest.NewServer(newRecordingSidecar(t))
	deadURL := srv.URL
	srv.Close()

	cfg := DefaultNPUConfig()
	cfg.BaseURL = deadURL
	cfg.Timeout = 2 * time.Second
	p, err := NewNPUProvider(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewNPUProvider must succeed even when the sidecar is down: %v", err)
	}
	defer func() {
		if closeErr := p.Close(); closeErr != nil {
			t.Errorf("Close: unexpected error: %v", closeErr)
		}
	}()

	_, err = p.Embed(context.Background(), "anything")
	if err == nil {
		t.Fatal("Embed against a dead sidecar: expected an error, got nil")
	}
	if !errors.Is(err, ErrNPUUnavailable) {
		t.Errorf("error %v does not match ErrNPUUnavailable", err)
	}
	var typed *NPUUnavailableError
	if !errors.As(err, &typed) {
		t.Fatalf("error %v is not an *NPUUnavailableError", err)
	}
	if typed.Endpoint != deadURL {
		t.Errorf("typed.Endpoint = %q, want %q", typed.Endpoint, deadURL)
	}
	if typed.Err == nil {
		t.Error("typed.Err must carry the underlying transport cause")
	}
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Errorf("a down sidecar must match ErrProviderUnavailable; got %v", err)
	}

	if _, err := p.Health(context.Background()); !errors.Is(err, ErrNPUUnavailable) {
		t.Errorf("Health against a dead sidecar: error %v does not match ErrNPUUnavailable", err)
	}
}

func TestNPUProviderContextCancellation(t *testing.T) {
	// A sidecar that blocks until the request context is cancelled. No sleeps:
	// the handler waits on the request's own Done channel.
	blocking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	srv := httptest.NewServer(blocking)
	t.Cleanup(srv.Close)

	cfg := DefaultNPUConfig()
	cfg.BaseURL = srv.URL
	p, err := NewNPUProvider(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewNPUProvider: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, embedErr := p.Embed(ctx, "will be cancelled")
		done <- embedErr
	}()
	cancel()

	embedErr := <-done
	if embedErr == nil {
		t.Fatal("Embed with a cancelled context: expected an error, got nil")
	}
	if !errors.Is(embedErr, ErrNPUUnavailable) {
		t.Errorf("error %v does not match ErrNPUUnavailable", embedErr)
	}
	if !errors.Is(embedErr, context.Canceled) {
		t.Errorf("error %v must unwrap to context.Canceled", embedErr)
	}
}

// -----------------------------------------------------------------------------
// Sidecar returning an error
// -----------------------------------------------------------------------------

// TestNPUProviderSidecarReplyErrors maps each kind of bad sidecar reply to
// the typed error Embed returns for it.
func TestNPUProviderSidecarReplyErrors(t *testing.T) {
	tests := []struct {
		name    string
		respond func(t *testing.T, w http.ResponseWriter)
		check   func(t *testing.T, err error)
	}{
		{
			name: "structured JSON error",
			respond: func(t *testing.T, w http.ResponseWriter) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				body := npuErrorResponse{}
				body.Error.Code = "compile_failed"
				body.Error.Message = "NPU refused shape [8,512]"
				if err := json.NewEncoder(w).Encode(body); err != nil {
					t.Errorf("encode: %v", err)
				}
			},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, ErrNPUSidecar) {
					t.Errorf("error %v does not match ErrNPUSidecar", err)
				}
				var typed *NPUSidecarError
				if !errors.As(err, &typed) {
					t.Fatalf("error %v is not an *NPUSidecarError", err)
				}
				if typed.StatusCode != http.StatusInternalServerError {
					t.Errorf("typed.StatusCode = %d, want 500", typed.StatusCode)
				}
				if typed.Code != "compile_failed" {
					t.Errorf("typed.Code = %q, want %q", typed.Code, "compile_failed")
				}
				if !strings.Contains(typed.Message, "refused shape") {
					t.Errorf("typed.Message = %q, want it to carry the sidecar explanation", typed.Message)
				}
			},
		},
		{
			name: "plain-text error",
			respond: func(_ *testing.T, w http.ResponseWriter) {
				http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			},
			check: func(t *testing.T, err error) {
				var typed *NPUSidecarError
				if !errors.As(err, &typed) {
					t.Fatalf("error %v is not an *NPUSidecarError", err)
				}
				if typed.StatusCode != http.StatusServiceUnavailable {
					t.Errorf("typed.StatusCode = %d, want 503", typed.StatusCode)
				}
				if typed.Message != "service unavailable" {
					t.Errorf("typed.Message = %q, want the plain body text", typed.Message)
				}
			},
		},
		{
			name: "malformed success body",
			respond: func(t *testing.T, w http.ResponseWriter) {
				w.Header().Set("Content-Type", "application/json")
				if _, err := w.Write([]byte("{not json")); err != nil {
					t.Errorf("write: %v", err)
				}
			},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, ErrNPUMalformedResponse) {
					t.Fatalf("error %v does not match ErrNPUMalformedResponse", err)
				}
				var typed *NPUMalformedResponseError
				if !errors.As(err, &typed) {
					t.Fatalf("error %v is not an *NPUMalformedResponseError", err)
				}
				if typed.Err == nil {
					t.Error("typed.Err must carry the decode cause")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sidecar := newRecordingSidecar(t)
			sidecar.respond = func(w http.ResponseWriter, _ npuEmbedRequest) {
				tt.respond(t, w)
			}
			srv := startSidecar(t, sidecar)
			p := newTestProvider(t, srv, nil)

			_, err := p.Embed(context.Background(), "text")
			if err == nil {
				t.Fatal("expected an error from a bad sidecar reply, got nil")
			}
			tt.check(t, err)
		})
	}
}

func TestNPUProviderResultCountMismatch(t *testing.T) {
	sidecar := newRecordingSidecar(t)
	sidecar.respond = func(w http.ResponseWriter, req npuEmbedRequest) {
		// Return one fewer vector than requested.
		embeddings := make([][]float32, len(req.Texts)-1)
		for i := range embeddings {
			embeddings[i] = vec(DefaultNPUDimensions, 0)
		}
		sidecar.writeJSON(w, npuEmbedResponse{
			Embeddings: embeddings,
			Dimensions: DefaultNPUDimensions,
			Device:     "NPU",
		})
	}
	srv := startSidecar(t, sidecar)
	p := newTestProvider(t, srv, nil)

	_, err := p.EmbedBatch(context.Background(), []string{"a", "b", "c"})
	if !errors.Is(err, ErrNPUMalformedResponse) {
		t.Fatalf("error %v does not match ErrNPUMalformedResponse", err)
	}
	var typed *NPUResultCountError
	if !errors.As(err, &typed) {
		t.Fatalf("error %v is not an *NPUResultCountError", err)
	}
	if typed.Expected != 3 || typed.Actual != 2 {
		t.Errorf("typed = {Expected:%d Actual:%d}, want {3 2}", typed.Expected, typed.Actual)
	}
}

func TestNPUProviderDimensionsMismatchIsRejected(t *testing.T) {
	sidecar := newRecordingSidecar(t)
	// A 768-wide vector where 384 was configured. Indexing this would corrupt
	// the vector store, so it must be an error rather than a warning.
	sidecar.dimensions = 768
	srv := startSidecar(t, sidecar)
	p := newTestProvider(t, srv, nil)

	_, err := p.Embed(context.Background(), "wrong width")
	if !errors.Is(err, ErrDimensionsMismatch) {
		t.Fatalf("error %v does not match ErrDimensionsMismatch", err)
	}
	var typed *NPUDimensionsMismatchError
	if !errors.As(err, &typed) {
		t.Fatalf("error %v is not an *NPUDimensionsMismatchError", err)
	}
	if typed.Expected != DefaultNPUDimensions || typed.Actual != 768 {
		t.Errorf("typed = {Expected:%d Actual:%d}, want {384 768}", typed.Expected, typed.Actual)
	}
}

// -----------------------------------------------------------------------------
// Over-long text and oversized batches
// -----------------------------------------------------------------------------

// TestNPUProviderPassesOverLongTextThrough asserts the provider does not
// truncate or reject text beyond the model's 512-token ceiling. Chunking is the
// sidecar's job — 512 is the model's max_position_embeddings, not a device
// limit, so the same chunking would be needed on CPU or GPU.
func TestNPUProviderPassesOverLongTextThrough(t *testing.T) {
	sidecar := newRecordingSidecar(t)
	sidecar.respond = func(w http.ResponseWriter, req npuEmbedRequest) {
		embeddings := make([][]float32, len(req.Texts))
		chunked := make([]bool, len(req.Texts))
		for i := range req.Texts {
			embeddings[i] = vec(DefaultNPUDimensions, 0)
			chunked[i] = true
		}
		sidecar.writeJSON(w, npuEmbedResponse{
			Embeddings: embeddings,
			Dimensions: DefaultNPUDimensions,
			Device:     "NPU",
			Chunked:    chunked,
		})
	}
	srv := startSidecar(t, sidecar)
	p := newTestProvider(t, srv, nil)

	// Comfortably past 512 tokens.
	long := strings.Repeat("chunk me please ", 500)
	got, err := p.Embed(context.Background(), long)
	if err != nil {
		t.Fatalf("Embed on over-long text: unexpected error: %v", err)
	}
	if len(got) != DefaultNPUDimensions {
		t.Fatalf("Embed: got width %d, want %d", len(got), DefaultNPUDimensions)
	}

	reqs := sidecar.recorded()
	if len(reqs) != 1 {
		t.Fatalf("sidecar saw %d requests, want 1", len(reqs))
	}
	if reqs[0].Texts[0] != long {
		t.Error("provider must send over-long text unmodified; the sidecar owns chunking")
	}
}

// TestNPUProviderSplitsOversizedBatch covers a batch larger than any compiled
// bucket. The provider splits it to bound request bodies; every text must still
// come back, in order, exactly once.
func TestNPUProviderSplitsOversizedBatch(t *testing.T) {
	sidecar := newRecordingSidecar(t)
	sidecar.respond = func(w http.ResponseWriter, req npuEmbedRequest) {
		embeddings := make([][]float32, len(req.Texts))
		for i, text := range req.Texts {
			v := vec(DefaultNPUDimensions, 0)
			// Encode the caller's ordinal, parsed back out of "t<N>".
			var n int
			for _, c := range text[1:] {
				n = n*10 + int(c-'0')
			}
			v[0] = float32(n)
			embeddings[i] = v
		}
		sidecar.writeJSON(w, npuEmbedResponse{
			Embeddings: embeddings,
			Dimensions: DefaultNPUDimensions,
			Device:     "NPU",
		})
	}
	srv := startSidecar(t, sidecar)

	const (
		total     = 37
		batchSize = 8
	)
	p := newTestProvider(t, srv, func(c *NPUConfig) { c.MaxBatchSize = batchSize })

	texts := make([]string, total)
	for i := range texts {
		texts[i] = "t" + itoa(i)
	}

	got, err := p.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("EmbedBatch: unexpected error: %v", err)
	}
	if len(got) != total {
		t.Fatalf("EmbedBatch: got %d vectors, want %d", len(got), total)
	}
	for i, v := range got {
		if int(v[0]) != i {
			t.Fatalf("vector %d carries ordinal %v — results misaligned across request splits", i, v[0])
		}
	}

	// 37 texts at 8 per request is 5 requests: 8,8,8,8,5.
	reqs := sidecar.recorded()
	wantRequests := (total + batchSize - 1) / batchSize
	if len(reqs) != wantRequests {
		t.Errorf("sidecar saw %d requests, want %d", len(reqs), wantRequests)
	}
	for i, r := range reqs {
		if len(r.Texts) > batchSize {
			t.Errorf("request %d carried %d texts, above MaxBatchSize %d", i, len(r.Texts), batchSize)
		}
	}
	if got, want := len(reqs[len(reqs)-1].Texts), total%batchSize; got != want {
		t.Errorf("final request carried %d texts, want the %d-text remainder", got, want)
	}
}

// itoa avoids importing strconv for a single use in table construction.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// -----------------------------------------------------------------------------
// Device reporting and transparent CPU fallback
// -----------------------------------------------------------------------------

// TestNPUProviderReportsCPUFallback is the observability guarantee that makes
// transparent fallback safe: the sidecar always answers, so a missing NPU looks
// like success. LastDevice is how an operator finds out.
func TestNPUProviderReportsCPUFallback(t *testing.T) {
	sidecar := newRecordingSidecar(t)
	sidecar.device = "CPU"
	srv := startSidecar(t, sidecar)
	p := newTestProvider(t, srv, nil)

	if got := p.LastDevice(); got != deviceUnknown {
		t.Errorf("LastDevice before any call = %q, want %q", got, deviceUnknown)
	}

	if _, err := p.Embed(context.Background(), "served from cpu"); err != nil {
		t.Fatalf("Embed: unexpected error: %v", err)
	}

	if got := p.LastDevice(); got != "CPU" {
		t.Errorf("LastDevice = %q, want %q — fallback must be visible", got, "CPU")
	}
}

func TestNPUProviderHealthReportsDeviceAndBuckets(t *testing.T) {
	sidecar := newRecordingSidecar(t)
	srv := startSidecar(t, sidecar)
	p := newTestProvider(t, srv, nil)

	health, err := p.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: unexpected error: %v", err)
	}
	if health.Status != "ok" {
		t.Errorf("health.Status = %q, want %q", health.Status, "ok")
	}
	if health.Device != "NPU" || !health.NPUAvailable {
		t.Errorf("health = {Device:%q NPUAvailable:%v}, want {NPU true}", health.Device, health.NPUAvailable)
	}
	if health.Dimensions != DefaultNPUDimensions {
		t.Errorf("health.Dimensions = %d, want %d", health.Dimensions, DefaultNPUDimensions)
	}
	if health.MaxTokens != DefaultNPUMaxTokens {
		t.Errorf("health.MaxTokens = %d, want %d", health.MaxTokens, DefaultNPUMaxTokens)
	}
	if len(health.CompiledBuckets) == 0 {
		t.Error("health.CompiledBuckets must report the precompiled shape set")
	}
	if got := p.LastDevice(); got != "NPU" {
		t.Errorf("Health must update LastDevice; got %q", got)
	}
}

// TestNPUProviderHealthErrors maps each kind of bad /health reply to the
// sentinel Health returns for it.
func TestNPUProviderHealthErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr error
	}{
		{
			name: "malformed body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				if _, err := w.Write([]byte("not json at all")); err != nil {
					t.Errorf("write: %v", err)
				}
			},
			wantErr: ErrNPUMalformedResponse,
		},
		{
			name: "sidecar error status",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "model not loaded", http.StatusServiceUnavailable)
			},
			wantErr: ErrNPUSidecar,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			t.Cleanup(srv.Close)

			p := newTestProvider(t, srv, nil)
			if _, err := p.Health(context.Background()); !errors.Is(err, tt.wantErr) {
				t.Errorf("Health: error %v does not match %v", err, tt.wantErr)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Provider contract and configuration
// -----------------------------------------------------------------------------

func TestNPUProviderContract(t *testing.T) {
	sidecar := newRecordingSidecar(t)
	srv := startSidecar(t, sidecar)
	p := newTestProvider(t, srv, nil)

	if got := p.Name(); got != NPUProviderName {
		t.Errorf("Name() = %q, want %q", got, NPUProviderName)
	}
	if got := p.Dimensions(); got != DefaultNPUDimensions {
		t.Errorf("Dimensions() = %d, want %d", got, DefaultNPUDimensions)
	}
	// 512 is all-MiniLM-L6-v2's max_position_embeddings, not an NPU limit.
	if got := p.MaxTokens(); got != 512 {
		t.Errorf("MaxTokens() = %d, want 512", got)
	}
	if got := p.BaseURL(); got != srv.URL {
		t.Errorf("BaseURL() = %q, want %q", got, srv.URL)
	}
}

// TestNPUConfig checks the configuration NewNPUProvider settles on: zero
// fields take the package defaults, DefaultNPUConfig carries the documented
// values, and trailing slashes are trimmed from the base URL.
func TestNPUConfig(t *testing.T) {
	trailingSlash := DefaultNPUConfig()
	trailingSlash.BaseURL = "http://npu.internal:8077///"

	tests := []struct {
		name          string
		cfg           NPUConfig
		wantBaseURL   string
		wantModel     string
		wantDims      int
		wantMaxTokens int
	}{
		{
			name:          "zero config takes the defaults",
			cfg:           NPUConfig{},
			wantBaseURL:   DefaultNPUURL,
			wantModel:     DefaultNPUModel,
			wantDims:      DefaultNPUDimensions,
			wantMaxTokens: DefaultNPUMaxTokens,
		},
		{
			name:          "DefaultNPUConfig carries the documented values",
			cfg:           DefaultNPUConfig(),
			wantBaseURL:   DefaultNPUURL,
			wantModel:     DefaultNPUModel,
			wantDims:      384,
			wantMaxTokens: 512,
		},
		{
			name:          "trailing slashes are trimmed from the base URL",
			cfg:           trailingSlash,
			wantBaseURL:   "http://npu.internal:8077",
			wantModel:     DefaultNPUModel,
			wantDims:      384,
			wantMaxTokens: 512,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewNPUProvider(context.Background(), tt.cfg)
			if err != nil {
				t.Fatalf("NewNPUProvider: unexpected error: %v", err)
			}
			t.Cleanup(func() {
				if closeErr := p.Close(); closeErr != nil {
					t.Errorf("Close: %v", closeErr)
				}
			})

			if got := p.BaseURL(); got != tt.wantBaseURL {
				t.Errorf("BaseURL() = %q, want %q", got, tt.wantBaseURL)
			}
			if got := p.Dimensions(); got != tt.wantDims {
				t.Errorf("Dimensions() = %d, want %d", got, tt.wantDims)
			}
			if got := p.MaxTokens(); got != tt.wantMaxTokens {
				t.Errorf("MaxTokens() = %d, want %d", got, tt.wantMaxTokens)
			}
			if got := p.config.Model; got != tt.wantModel {
				t.Errorf("Model = %q, want %q", got, tt.wantModel)
			}
			if got := p.config.MaxBatchSize; got != DefaultNPUMaxBatchSize {
				t.Errorf("MaxBatchSize = %d, want %d", got, DefaultNPUMaxBatchSize)
			}
			if got := p.config.Timeout; got != DefaultNPUTimeout {
				t.Errorf("Timeout = %v, want %v", got, DefaultNPUTimeout)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Credentials
// -----------------------------------------------------------------------------

// staticResolver is a real SecretResolver over an in-memory map.
type staticResolver struct {
	values map[string]string
	err    error
}

func (r *staticResolver) ResolveSecret(_ context.Context, ref inference.SecretRef) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	v, ok := r.values[ref.Engine+"/"+ref.Path+"/"+ref.Key]
	if !ok {
		return "", ErrProviderNotFound
	}
	return v, nil
}

func TestNPUProviderResolvesSecretRefAndSendsBearer(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(npuEmbedResponse{
			Embeddings: [][]float32{vec(DefaultNPUDimensions, 0)},
			Dimensions: DefaultNPUDimensions,
			Device:     "NPU",
		}); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	ref := inference.SecretRef{Engine: "secrets", Path: "tenants/acme/npu", Key: "token"}
	cfg := DefaultNPUConfig()
	cfg.BaseURL = srv.URL
	cfg.APIKeyRef = ref
	cfg.Secrets = &staticResolver{values: map[string]string{
		"secrets/tenants/acme/npu/token": "s3cr3t",
	}}

	p, err := NewNPUProvider(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewNPUProvider: %v", err)
	}
	if _, err := p.Embed(context.Background(), "authed"); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if gotAuth != "Bearer s3cr3t" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer s3cr3t")
	}
}

func TestNPUProviderSecretRefWithoutResolver(t *testing.T) {
	cfg := DefaultNPUConfig()
	cfg.APIKeyRef = inference.SecretRef{Engine: "secrets", Path: "p", Key: "k"}

	_, err := NewNPUProvider(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected an error when APIKeyRef is set without a resolver")
	}
	if !errors.Is(err, ErrNPUSecretResolverRequired) {
		t.Errorf("error %v does not match ErrNPUSecretResolverRequired", err)
	}
	var typed *NPUConfigError
	if !errors.As(err, &typed) {
		t.Fatalf("error %v is not an *NPUConfigError", err)
	}
	if typed.Field != "secrets" {
		t.Errorf("typed.Field = %q, want %q", typed.Field, "secrets")
	}
}

func TestNPUProviderSecretResolutionFailure(t *testing.T) {
	cfg := DefaultNPUConfig()
	cfg.APIKeyRef = inference.SecretRef{Engine: "secrets", Path: "missing", Key: "k"}
	cfg.Secrets = &staticResolver{err: ErrInvalidCredentials}

	_, err := NewNPUProvider(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected an error when the resolver fails")
	}
	if !errors.Is(err, ErrNPUSecretResolution) {
		t.Errorf("error %v does not match ErrNPUSecretResolution", err)
	}
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("error %v must unwrap to the resolver's cause", err)
	}
	var typed *NPUSecretResolutionError
	if !errors.As(err, &typed) {
		t.Fatalf("error %v is not an *NPUSecretResolutionError", err)
	}
	if typed.Path != "missing" {
		t.Errorf("typed.Path = %q, want %q", typed.Path, "missing")
	}
}

func TestNPUProviderNoAuthHeaderWithoutCredential(t *testing.T) {
	var hadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadAuth = r.Header["Authorization"]
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(npuEmbedResponse{
			Embeddings: [][]float32{vec(DefaultNPUDimensions, 0)},
			Dimensions: DefaultNPUDimensions,
			Device:     "NPU",
		}); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	p := newTestProvider(t, srv, nil)
	if _, err := p.Embed(context.Background(), "no auth"); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if hadAuth {
		t.Error("no Authorization header must be sent when no credential is configured")
	}
}

// -----------------------------------------------------------------------------
// Concurrency
// -----------------------------------------------------------------------------

// TestNPUProviderConcurrentEmbedBatch exercises the provider under -race. The
// device report is the only mutable state and is stored atomically.
func TestNPUProviderConcurrentEmbedBatch(t *testing.T) {
	sidecar := newRecordingSidecar(t)
	srv := startSidecar(t, sidecar)
	p := newTestProvider(t, srv, nil)

	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)

	wg.Add(workers)
	for i := range workers {
		go func(n int) {
			defer wg.Done()
			if _, err := p.EmbedBatch(context.Background(), []string{"a" + itoa(n), "b" + itoa(n)}); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent EmbedBatch: %v", err)
	}
	if got := p.LastDevice(); got != "NPU" {
		t.Errorf("LastDevice = %q, want %q", got, "NPU")
	}
}

// -----------------------------------------------------------------------------
// Registration with the Manager
// -----------------------------------------------------------------------------

func TestNPUProviderRegistersWithManager(t *testing.T) {
	sidecar := newRecordingSidecar(t)
	srv := startSidecar(t, sidecar)
	p := newTestProvider(t, srv, nil)

	// The Manager's own Config.Provider names the *default* provider, and
	// go-qrdb validates that name against its allowlist of built-in providers
	// (openai, ollama, lmstudio, voyage). "npu" is not on that list, so it
	// cannot yet be named as the config default -- it is registered alongside
	// the default instead. That is the intended end state for this change
	// anyway: Ollama stays the default until the owner switches it.
	mgr, err := NewManager(Config{
		Provider:   "ollama",
		Model:      "nomic-embed-text",
		APIURL:     "http://127.0.0.1:11434",
		Dimensions: 768,
	})
	if err != nil {
		t.Fatalf("NewManager: unexpected error: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := mgr.Close(); closeErr != nil {
			t.Errorf("manager Close: %v", closeErr)
		}
	})

	if err := mgr.RegisterProvider(NPUProviderName, p); err != nil {
		t.Fatalf("RegisterProvider: unexpected error: %v", err)
	}

	got, err := mgr.GetProvider(NPUProviderName)
	if err != nil {
		t.Fatalf("GetProvider: unexpected error: %v", err)
	}
	if got.Name() != NPUProviderName {
		t.Errorf("registered provider Name() = %q, want %q", got.Name(), NPUProviderName)
	}
}

// -----------------------------------------------------------------------------
// The sidecar must not serve a different model
// -----------------------------------------------------------------------------

// TestNPUProviderServedModelCheck guards a silent index-corruption path. A
// vector's meaning depends on the model that produced it, and two models of the
// same width yield mutually incomparable vectors -- so a mismatch has no visible
// symptom until retrieval quality collapses. It must be an error. A sidecar
// that omits the field is not asserting a different model, so that is accepted.
func TestNPUProviderServedModelCheck(t *testing.T) {
	tests := []struct {
		name        string
		servedModel string
		wantErr     bool
	}{
		{name: "a different model is rejected", servedModel: "some-other-model", wantErr: true},
		{name: "an omitted model is accepted", servedModel: "", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sidecar := newRecordingSidecar(t)
			sidecar.respond = func(w http.ResponseWriter, _ npuEmbedRequest) {
				sidecar.writeJSON(w, npuEmbedResponse{
					Embeddings: [][]float32{vec(DefaultNPUDimensions, 0)},
					Model:      tt.servedModel,
					Dimensions: DefaultNPUDimensions,
					Device:     "NPU",
				})
			}
			srv := startSidecar(t, sidecar)
			p := newTestProvider(t, srv, nil)

			_, err := p.Embed(context.Background(), "text")
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("an omitted model field must be accepted: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrNPUModelMismatch) {
				t.Errorf("error %v does not match ErrNPUModelMismatch", err)
			}
			var typed *NPUModelMismatchError
			if !errors.As(err, &typed) {
				t.Fatalf("error %v is not an *NPUModelMismatchError", err)
			}
			if typed.Requested != DefaultNPUModel || typed.Served != tt.servedModel {
				t.Errorf("typed = {Requested:%q Served:%q}", typed.Requested, typed.Served)
			}
		})
	}
}

// TestNPUProviderRejectsADeclaredWidthMismatch covers the sidecar's own
// `dimensions` field disagreeing with configuration, which is distinct from the
// vectors themselves being the wrong width.
func TestNPUProviderRejectsADeclaredWidthMismatch(t *testing.T) {
	sidecar := newRecordingSidecar(t)
	sidecar.respond = func(w http.ResponseWriter, _ npuEmbedRequest) {
		// Correctly-sized vectors, but the sidecar claims a different width --
		// which means the sidecar is malfunctioning and should be named as such.
		sidecar.writeJSON(w, npuEmbedResponse{
			Embeddings:  [][]float32{vec(DefaultNPUDimensions, 0)},
			Model:       DefaultNPUModel,
			Dimensions:  768,
			Device:      "NPU",
			TotalTokens: 4,
			Chunked:     []bool{false},
		})
	}
	srv := startSidecar(t, sidecar)
	p := newTestProvider(t, srv, nil)

	_, err := p.Embed(context.Background(), "text")
	if !errors.Is(err, ErrDimensionsMismatch) {
		t.Fatalf("error %v does not match ErrDimensionsMismatch", err)
	}

	var typed *NPUDimensionsMismatchError
	if !errors.As(err, &typed) {
		t.Fatalf("error %v is not an *NPUDimensionsMismatchError", err)
	}
	if typed.Index != -1 {
		t.Errorf("typed.Index = %d, want -1 for a declared-width mismatch", typed.Index)
	}
	if !strings.Contains(err.Error(), "declared width") {
		t.Errorf("message %q should distinguish this from a per-vector mismatch", err.Error())
	}
}
