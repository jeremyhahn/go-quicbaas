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

//go:build awskms

package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/retry"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// kmsFaultWatchdog bounds every retry subtest. It is a hang detector, not a
// timing assertion: the SDK's backoff is far below it.
const kmsFaultWatchdog = 30 * time.Second

// kmsFaultResponse is one scripted reply of the fault-injecting KMS endpoint.
type kmsFaultResponse struct {
	status int
	header map[string]string
	body   string
}

// kmsFaultServer is an httptest KMS endpoint speaking the KMS JSON 1.1
// protocol. Each operation (the X-Amz-Target suffix) replies from its script
// queue first and then from its fallback; an operation with no fallback
// echoes the request as a successful Encrypt or Decrypt.
type kmsFaultServer struct {
	*httptest.Server

	mu       sync.Mutex
	calls    map[string]int
	scripts  map[string][]kmsFaultResponse
	fallback map[string]*kmsFaultResponse
}

func newKMSFaultServer(t *testing.T) *kmsFaultServer {
	t.Helper()
	s := &kmsFaultServer{
		calls:    map[string]int{},
		scripts:  map[string][]kmsFaultResponse{},
		fallback: map[string]*kmsFaultResponse{},
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.Close)
	return s
}

// script queues one-shot replies for op, consumed before the fallback.
func (s *kmsFaultServer) script(op string, rs ...kmsFaultResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scripts[op] = append(s.scripts[op], rs...)
}

// always makes op reply with r once its script queue is empty.
func (s *kmsFaultServer) always(op string, r kmsFaultResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fallback[op] = &r
}

// count returns how many requests op has received.
func (s *kmsFaultServer) count(op string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[op]
}

func (s *kmsFaultServer) handle(w http.ResponseWriter, r *http.Request) {
	target := r.Header.Get("X-Amz-Target")
	op := strings.TrimPrefix(target, "TrentService.")
	reqBody, _ := io.ReadAll(r.Body)

	s.mu.Lock()
	s.calls[op]++
	var resp *kmsFaultResponse
	if q := s.scripts[op]; len(q) > 0 {
		resp = &q[0]
		s.scripts[op] = q[1:]
	} else if fb := s.fallback[op]; fb != nil {
		resp = fb
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	if resp != nil {
		for k, v := range resp.header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(resp.status)
		_, _ = io.WriteString(w, resp.body)
		return
	}

	// Echo success: the "ciphertext" is the plaintext and vice versa, which is
	// enough for a seal/unseal round trip. []byte fields marshal as base64,
	// matching the KMS JSON wire format.
	var in struct {
		KeyId          string
		Plaintext      []byte
		CiphertextBlob []byte
	}
	_ = json.Unmarshal(reqBody, &in)
	keyID := in.KeyId
	if keyID == "" {
		keyID = "alias/barrier-root"
	}
	var out any
	switch op {
	case "Encrypt":
		out = struct {
			CiphertextBlob []byte
			KeyId          string
		}{in.Plaintext, keyID}
	case "Decrypt":
		out = struct {
			Plaintext []byte
			KeyId     string
		}{in.CiphertextBlob, keyID}
	default:
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"__type":"UnsupportedOperationException","message":"fault server: unscripted operation `+op+`"}`)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

// newFaultStrategy builds the production AWS KMS sealing strategy, through
// the real buildAWSKMSStrategy and the real SDK client, pointed at url. The
// SDK is isolated from the host's AWS configuration so only the SDK defaults
// (including the default retryer) apply.
func newFaultStrategy(t *testing.T, url string) qrdbsdk.SealingStrategy {
	t.Helper()
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	t.Setenv("AWS_CONFIG_FILE", missing)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", missing)
	for _, k := range []string{"AWS_PROFILE", "AWS_MAX_ATTEMPTS", "AWS_RETRY_MODE", "AWS_ENDPOINT_URL", "AWS_ENDPOINT_URL_KMS"} {
		t.Setenv(k, "")
	}
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	strategy, err := buildAWSKMSStrategy(awsKMSConfig(config.AWSSealConfig{
		Region:          "us-east-1",
		KeyID:           "alias/barrier-root",
		AccessKeyID:     "AKIDTEST",
		SecretAccessKey: "secret",
		Endpoint:        url,
	}))
	if err != nil {
		t.Fatalf("buildAWSKMSStrategy failed: %v", err)
	}
	return strategy
}

func newRootKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("failed to generate root key: %v", err)
	}
	return k
}

// withWatchdog runs fn and fails the test if it does not return within
// kmsFaultWatchdog.
func withWatchdog(t *testing.T, fn func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), kmsFaultWatchdog)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("operation did not finish within %s", kmsFaultWatchdog)
	}
}

// TestAWSKMSStrategy_RetryBehaviour drives the composed AWS KMS sealing
// strategy against a fault-injecting KMS endpoint, covering the SDK's
// default retry behaviour: transient recovery, the unmodeled 429 with
// x-amz-retry-after, retry exhaustion, terminal errors reaching the
// strategy, and a cancelled caller context.
func TestAWSKMSStrategy_RetryBehaviour(t *testing.T) {
	kmsInternal500 := kmsFaultResponse{status: http.StatusInternalServerError, body: `{"__type":"KMSInternalException","message":"internal"}`}
	kmsInternal503 := kmsFaultResponse{status: http.StatusServiceUnavailable, body: `{"__type":"KMSInternalException","message":"unavailable"}`}

	t.Run("transient 5xx then success", func(t *testing.T) {
		srv := newKMSFaultServer(t)
		srv.script("Encrypt", kmsInternal500)
		strategy := newFaultStrategy(t, srv.URL)
		rootKey := newRootKey(t)

		withWatchdog(t, func() {
			sealed, err := strategy.SealRootKey(context.Background(), rootKey, qrdbsdk.Credentials{})
			if err != nil {
				t.Errorf("SealRootKey failed after a transient 500: %v", err)
				return
			}
			if n := srv.count("Encrypt"); n != 2 {
				t.Errorf("Encrypt calls = %d, want 2", n)
			}
			recovered, err := strategy.UnsealRootKey(context.Background(), sealed, qrdbsdk.Credentials{})
			if err != nil {
				t.Errorf("UnsealRootKey failed: %v", err)
				return
			}
			if !bytes.Equal(recovered, rootKey) {
				t.Error("recovered root key does not match the original")
			}
		})
	})

	t.Run("unmodeled 429 with x-amz-retry-after then success", func(t *testing.T) {
		srv := newKMSFaultServer(t)
		srv.script("Encrypt", kmsFaultResponse{
			status: http.StatusTooManyRequests,
			header: map[string]string{"x-amz-retry-after": "50"},
			body:   `{"__type":"TooManyRequests","message":"slow down"}`,
		})
		strategy := newFaultStrategy(t, srv.URL)

		withWatchdog(t, func() {
			if _, err := strategy.SealRootKey(context.Background(), newRootKey(t), qrdbsdk.Credentials{}); err != nil {
				t.Errorf("SealRootKey failed after an unmodeled 429: %v", err)
			}
			if n := srv.count("Encrypt"); n != 2 {
				t.Errorf("Encrypt calls = %d, want 2", n)
			}
		})
	})

	t.Run("retry exhaustion", func(t *testing.T) {
		srv := newKMSFaultServer(t)
		srv.always("Encrypt", kmsInternal503)
		strategy := newFaultStrategy(t, srv.URL)

		withWatchdog(t, func() {
			_, err := strategy.SealRootKey(context.Background(), newRootKey(t), qrdbsdk.Credentials{})
			if err == nil {
				t.Error("SealRootKey succeeded against an endpoint that always returns 503")
				return
			}
			var maxErr *retry.MaxAttemptsError
			if !errors.As(err, &maxErr) {
				t.Errorf("error does not wrap *retry.MaxAttemptsError: %v", err)
			} else if maxErr.Attempt != 3 {
				t.Errorf("MaxAttemptsError.Attempt = %d, want 3", maxErr.Attempt)
			}
			if n := srv.count("Encrypt"); n != 3 {
				t.Errorf("Encrypt calls = %d, want 3", n)
			}
		})
	})

	t.Run("terminal error is not retried and reaches the strategy", func(t *testing.T) {
		t.Run("seal", func(t *testing.T) {
			srv := newKMSFaultServer(t)
			srv.always("Encrypt", kmsFaultResponse{status: http.StatusBadRequest, body: `{"__type":"DisabledException","message":"key disabled"}`})
			strategy := newFaultStrategy(t, srv.URL)

			withWatchdog(t, func() {
				_, err := strategy.SealRootKey(context.Background(), newRootKey(t), qrdbsdk.Credentials{})
				var disabled *kmstypes.DisabledException
				if !errors.As(err, &disabled) {
					t.Errorf("SealRootKey error does not wrap *types.DisabledException: %v", err)
				}
				if n := srv.count("Encrypt"); n != 1 {
					t.Errorf("Encrypt calls = %d, want 1", n)
				}
			})
		})

		t.Run("unseal", func(t *testing.T) {
			srv := newKMSFaultServer(t)
			srv.always("Decrypt", kmsFaultResponse{status: http.StatusBadRequest, body: `{"__type":"InvalidCiphertextException","message":"bad ciphertext"}`})
			strategy := newFaultStrategy(t, srv.URL)

			withWatchdog(t, func() {
				sealed, err := strategy.SealRootKey(context.Background(), newRootKey(t), qrdbsdk.Credentials{})
				if err != nil {
					t.Errorf("SealRootKey failed: %v", err)
					return
				}
				_, err = strategy.UnsealRootKey(context.Background(), sealed, qrdbsdk.Credentials{})
				var invalid *kmstypes.InvalidCiphertextException
				if !errors.As(err, &invalid) {
					t.Errorf("UnsealRootKey error does not wrap *types.InvalidCiphertextException: %v", err)
				}
				if n := srv.count("Decrypt"); n != 1 {
					t.Errorf("Decrypt calls = %d, want 1", n)
				}
			})
		})
	})

	// The caller's cancellation does not reach the KMS request: go-xkms's
	// awsKMSSymmetricEncrypter.Encrypt and Decrypt call the KMS client with
	// context.Background() rather than the caller's context, so a cancelled
	// SealRootKey still sends Encrypt and is bounded only by the SDK's
	// default max attempts.
	t.Run("cancelled context is bounded by max attempts", func(t *testing.T) {
		srv := newKMSFaultServer(t)
		srv.always("Encrypt", kmsInternal503)
		strategy := newFaultStrategy(t, srv.URL)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		withWatchdog(t, func() {
			_, err := strategy.SealRootKey(ctx, newRootKey(t), qrdbsdk.Credentials{})
			n := srv.count("Encrypt")
			t.Logf("cancelled SealRootKey: Encrypt calls=%d err=%v", n, err)
			if err == nil {
				t.Error("SealRootKey succeeded against an endpoint that always returns 503")
			}
			// The request is still sent despite the cancelled context.
			if n == 0 {
				t.Error("no Encrypt sent: cancellation now reaches the KMS client; update this subtest")
			}
			if n > 3 {
				t.Errorf("Encrypt calls = %d, want at most 3", n)
			}
		})
	})
}
