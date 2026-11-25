package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// credentialRecorder is an https server that records the method, path and
// body of every request and answers from a per-route table.
type credentialRecorder struct {
	mu    sync.Mutex
	seen  []string
	body  map[string][]byte
	reply map[string]string
}

func newCredentialServer(t *testing.T, reply map[string]string) (*CredentialClient, *credentialRecorder) {
	t.Helper()
	rec := &credentialRecorder{body: map[string][]byte{}, reply: reply}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.EscapedPath()
		b, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.seen = append(rec.seen, key)
		rec.body[key] = b
		rec.mu.Unlock()
		out, ok := reply[key]
		if !ok {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(out))
	}))
	t.Cleanup(srv.Close)
	tr, err := New(&transport.Config{Address: srv.URL, MaxRetries: 0}, WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	return tr.Credentials(), rec
}

// TestCredentialClient_VerbsHitGoQRDBRoutes pins that every credential verb
// sends go-qrdb's REST method and path, with the name path-escaped, and that
// the 204 verbs succeed on an empty body.
func TestCredentialClient_VerbsHitGoQRDBRoutes(t *testing.T) {
	c, rec := newCredentialServer(t, map[string]string{
		"GET /v1/credentials/strategy":         `{"strategy":"manual"}`,
		"GET /v1/credentials":                  `{"names":["a"]}`,
		"POST /v1/credentials/a%2Fb/rotate":    `{"previous_version":1,"new_version":2}`,
		"GET /v1/credentials/a%2Fb/versions/3": `{"version":3}`,
		"GET /v1/credentials/a%2Fb/versions":   `{}`,
		"POST /v1/credentials/a%2Fb/gc":        `{}`,
		"GET /v1/credentials/a%2Fb/value":      `{"value":"AP+A/g=="}`,
	})
	ctx := context.Background()
	const name = "a/b"
	cases := []struct {
		want string
		call func() error
	}{
		{"POST /v1/credentials/submit", func() error {
			return c.CredentialSubmit(ctx, &qrdbtransport.CredentialSubmitRequest{Name: name, Value: []byte("v")})
		}},
		{"GET /v1/credentials/strategy", func() error {
			resp, err := c.CredentialStrategy(ctx)
			if err == nil && resp.Strategy != "manual" {
				t.Errorf("strategy: got %q", resp.Strategy)
			}
			return err
		}},
		{"DELETE /v1/credentials/a%2Fb", func() error { return c.CredentialDelete(ctx, name) }},
		{"GET /v1/credentials", func() error { _, err := c.CredentialList(ctx, nil); return err }},
		{"POST /v1/credentials/a%2Fb/rotate", func() error {
			_, err := c.CredentialRotate(ctx, &qrdbtransport.CredentialRotateRequest{Name: name, NewValue: []byte("n")})
			return err
		}},
		{"GET /v1/credentials/a%2Fb/versions/3", func() error {
			_, err := c.CredentialRetrieveVersion(ctx, &qrdbtransport.CredentialRetrieveVersionRequest{Name: name, Version: 3})
			return err
		}},
		{"GET /v1/credentials/a%2Fb/versions", func() error {
			_, err := c.CredentialListVersions(ctx, &qrdbtransport.CredentialListVersionsRequest{Name: name})
			return err
		}},
		{"POST /v1/credentials/a%2Fb/gc", func() error {
			_, err := c.CredentialGCExpired(ctx, &qrdbtransport.CredentialGCRequest{Name: name})
			return err
		}},
		{"POST /v1/credentials/store", func() error { return c.CredentialStore(ctx, name, []byte("v")) }},
		{"GET /v1/credentials/a%2Fb/value", func() error { _, err := c.CredentialRetrieve(ctx, name); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			rec.mu.Lock()
			before := len(rec.seen)
			rec.mu.Unlock()
			if err := tc.call(); err != nil {
				t.Fatalf("%s: %v", tc.want, err)
			}
			rec.mu.Lock()
			defer rec.mu.Unlock()
			if len(rec.seen) != before+1 || rec.seen[before] != tc.want {
				t.Fatalf("requests: got %v, want one %q", rec.seen[before:], tc.want)
			}
		})
	}
}

// TestCredentialClient_StoreRetrieveBinaryRoundTrip pins that a binary value
// travels as base64 in {"name","value"} and comes back byte for byte.
func TestCredentialClient_StoreRetrieveBinaryRoundTrip(t *testing.T) {
	value := []byte{0x00, 0xff, 0x80, 0xfe}
	c, rec := newCredentialServer(t, map[string]string{
		"GET /v1/credentials/bin/value": `{"value":"AP+A/g=="}`,
	})
	ctx := context.Background()
	if err := c.CredentialStore(ctx, "bin", value); err != nil {
		t.Fatalf("CredentialStore: %v", err)
	}
	var sent struct {
		Name  string `json:"name"`
		Value []byte `json:"value"`
	}
	rec.mu.Lock()
	raw := rec.body["POST /v1/credentials/store"]
	rec.mu.Unlock()
	if err := json.Unmarshal(raw, &sent); err != nil {
		t.Fatalf("decode store body %q: %v", raw, err)
	}
	if sent.Name != "bin" || !bytes.Equal(sent.Value, value) {
		t.Fatalf("store body: got name %q value %x, want %q %x", sent.Name, sent.Value, "bin", value)
	}
	got, err := c.CredentialRetrieve(ctx, "bin")
	if err != nil {
		t.Fatalf("CredentialRetrieve: %v", err)
	}
	if !bytes.Equal(got, value) {
		t.Fatalf("CredentialRetrieve: got %x, want %x", got, value)
	}
}

// TestCredentialClient_RefusesNilAndEmptyName pins that the named verbs
// refuse a nil request or an empty name before anything is sent.
func TestCredentialClient_RefusesNilAndEmptyName(t *testing.T) {
	c, rec := newCredentialServer(t, nil)
	ctx := context.Background()
	calls := map[string]func() error{
		"rotate nil":   func() error { _, err := c.CredentialRotate(ctx, nil); return err },
		"rotate empty": func() error { _, err := c.CredentialRotate(ctx, &qrdbtransport.CredentialRotateRequest{}); return err },
		"version nil":  func() error { _, err := c.CredentialRetrieveVersion(ctx, nil); return err },
		"version empty": func() error {
			_, err := c.CredentialRetrieveVersion(ctx, &qrdbtransport.CredentialRetrieveVersionRequest{})
			return err
		},
		"versions nil": func() error { _, err := c.CredentialListVersions(ctx, nil); return err },
		"versions empty": func() error {
			_, err := c.CredentialListVersions(ctx, &qrdbtransport.CredentialListVersionsRequest{})
			return err
		},
		"gc nil":   func() error { _, err := c.CredentialGCExpired(ctx, nil); return err },
		"gc empty": func() error { _, err := c.CredentialGCExpired(ctx, &qrdbtransport.CredentialGCRequest{}); return err },
	}
	for name, call := range calls {
		if err := call(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.seen) != 0 {
		t.Fatalf("requests sent: %v, want none", rec.seen)
	}
}

// TestCredentialClient_SecretVerbsRefuseHTTPTarget pins that the verbs that
// carry a credential value refuse a plaintext http hop before anything is
// sent, as the ceremony and barrier secret verbs do.
func TestCredentialClient_SecretVerbsRefuseHTTPTarget(t *testing.T) {
	var hits atomic.Int32
	plain := countingServer(&hits)
	t.Cleanup(plain.Close)
	tr, err := New(&transport.Config{Address: plain.URL, MaxRetries: 0})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	c := tr.Credentials()
	ctx := context.Background()

	requireRefusal(t, c.CredentialSubmit(ctx, &qrdbtransport.CredentialSubmitRequest{Name: "n", Value: []byte("v")}), "CredentialSubmit over http")
	_, err = c.CredentialRotate(ctx, &qrdbtransport.CredentialRotateRequest{Name: "n", NewValue: []byte("v")})
	requireRefusal(t, err, "CredentialRotate over http")
	_, err = c.CredentialRetrieveVersion(ctx, &qrdbtransport.CredentialRetrieveVersionRequest{Name: "n"})
	requireRefusal(t, err, "CredentialRetrieveVersion over http")
	requireRefusal(t, c.CredentialStore(ctx, "n", []byte("v")), "CredentialStore over http")
	_, err = c.CredentialRetrieve(ctx, "n")
	requireRefusal(t, err, "CredentialRetrieve over http")

	if n := hits.Load(); n != 0 {
		t.Fatalf("requests reaching the plaintext server: got %d, want 0", n)
	}
}
