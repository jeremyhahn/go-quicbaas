package rest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// TestCeremonyClient_MethodPathAndBearer drives every ceremony verb through
// the SDK's REST transport and checks that each reaches the go-qrdb REST
// route with the go-qrdb HTTP method and carries the platform bearer.
func TestCeremonyClient_MethodPathAndBearer(t *testing.T) {
	const token = "t-1700"
	ctx := context.Background()

	tests := []struct {
		name   string
		method string
		path   string
		call   func(c *CeremonyClient) error
	}{
		{"CeremonyInitialize", http.MethodPost, "/v1/ceremony/initialize", func(c *CeremonyClient) error {
			_, err := c.CeremonyInitialize(ctx, &qrdbtransport.CeremonyInitializeRequest{})
			return err
		}},
		{"CeremonyStatus", http.MethodGet, "/v1/ceremony/status", func(c *CeremonyClient) error {
			_, err := c.CeremonyStatus(ctx)
			return err
		}},
		{"CeremonyClaimCertBegin", http.MethodPost, "/v1/ceremony/claim-cert/begin", func(c *CeremonyClient) error {
			_, err := c.CeremonyClaimCertBegin(ctx, &qrdbtransport.ClaimCertBeginRequest{})
			return err
		}},
		{"CeremonyClaimCertComplete", http.MethodPost, "/v1/ceremony/claim-cert/complete", func(c *CeremonyClient) error {
			_, err := c.CeremonyClaimCertComplete(ctx, &qrdbtransport.ClaimCertCompleteRequest{})
			return err
		}},
		{"CeremonyBeginClaimShare", http.MethodPost, "/v1/ceremony/officer-share/begin", func(c *CeremonyClient) error {
			_, err := c.CeremonyBeginClaimShare(ctx, &qrdbtransport.BeginClaimShareRequest{})
			return err
		}},
		{"CeremonyCompleteClaimShare", http.MethodPost, "/v1/ceremony/officer-share/complete", func(c *CeremonyClient) error {
			_, err := c.CeremonyCompleteClaimShare(ctx, &qrdbtransport.CompleteClaimShareRequest{})
			return err
		}},
		{"CeremonySignCSR", http.MethodPost, "/v1/ceremony/sign-csr", func(c *CeremonyClient) error {
			_, err := c.CeremonySignCSR(ctx, &qrdbtransport.SignCSRInitRequest{})
			return err
		}},
		{"CeremonyClaimShareBegin", http.MethodPost, "/v1/ceremony/claim-share/begin", func(c *CeremonyClient) error {
			_, err := c.CeremonyClaimShareBegin(ctx, &qrdbtransport.CeremonyClaimShareBeginRequest{})
			return err
		}},
		{"CeremonyClaimShareComplete", http.MethodPost, "/v1/ceremony/claim-share/complete", func(c *CeremonyClient) error {
			_, err := c.CeremonyClaimShareComplete(ctx, &qrdbtransport.CeremonyClaimShareCompleteRequest{})
			return err
		}},
		{"CeremonySubmitUnsealShare", http.MethodPost, "/v1/barrier/unseal-share", func(c *CeremonyClient) error {
			_, err := c.CeremonySubmitUnsealShare(ctx, &qrdbtransport.CeremonySubmitUnsealShareRequest{})
			return err
		}},
		{"CeremonyEnrollExternalOfficer", http.MethodPost, "/v1/init/enroll-external-officer", func(c *CeremonyClient) error {
			_, err := c.CeremonyEnrollExternalOfficer(ctx, &qrdbtransport.EnrollExternalOfficerRequest{})
			return err
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var (
				mu                 sync.Mutex
				method, path, auth string
				hits               int
			)
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/health" {
					w.WriteHeader(http.StatusOK)
					return
				}
				mu.Lock()
				method, path, auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
				hits++
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			t.Cleanup(srv.Close)

			tr, err := New(&transport.Config{Address: srv.URL, TLSConfig: trustServer(srv), MaxRetries: 0}, WithAuthToken(token))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = tr.Close() })

			if err := tc.call(tr.Ceremony()); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}

			mu.Lock()
			defer mu.Unlock()
			if hits != 1 {
				t.Fatalf("requests reaching the server: got %d, want 1", hits)
			}
			if method != tc.method {
				t.Errorf("method: got %s, want %s", method, tc.method)
			}
			if path != tc.path {
				t.Errorf("path: got %s, want %s", path, tc.path)
			}
			if want := "Bearer " + token; auth != want {
				t.Errorf("Authorization: got %q, want %q", auth, want)
			}
		})
	}
}

// TestCeremonyClient_EmptyResponseIsAnError checks that CeremonyStatus
// refuses an empty 200 or 204 body instead of returning a zero
// CeremonyStatusResponse, which would report a state the server never sent.
func TestCeremonyClient_EmptyResponseIsAnError(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNoContent} {
		t.Run(fmt.Sprintf("CeremonyStatus/%d", status), func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/health" {
					w.WriteHeader(http.StatusOK)
					return
				}
				w.WriteHeader(status)
			}))
			t.Cleanup(srv.Close)

			tr, err := New(&transport.Config{Address: srv.URL, TLSConfig: trustServer(srv), MaxRetries: 0})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = tr.Close() })

			if _, err := tr.Ceremony().CeremonyStatus(context.Background()); err == nil {
				t.Fatalf("CeremonyStatus on an empty %d body: got nil error, want an error", status)
			}
		})
	}
}
