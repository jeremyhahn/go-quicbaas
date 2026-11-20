package rest

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// tenantWireServerState is what the tenant wire server answers with and
// what it last received.
type tenantWireServerState struct {
	mu       sync.Mutex
	status   int
	reply    string
	location string
	method   string
	path     string
	body     []byte
}

// answer sets the status, JSON body and Location of the next answers.
func (s *tenantWireServerState) answer(status int, reply, location string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.reply, s.location = status, reply, location
	s.method, s.path, s.body = "", "", nil
}

// received returns the method, path and body of the last request.
func (s *tenantWireServerState) received() (string, string, []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.method, s.path, s.body
}

// tenantWireServer is a TLS server that answers GET /health with 200 and
// every other request with the state's status, JSON body and Location,
// recording the request.
func tenantWireServer(t *testing.T) (*httptest.Server, *tenantWireServerState) {
	t.Helper()
	state := &tenantWireServerState{status: http.StatusOK}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		body, _ := io.ReadAll(r.Body)
		state.mu.Lock()
		state.method, state.path, state.body = r.Method, r.URL.Path, body
		status, reply, location := state.status, state.reply, state.location
		state.mu.Unlock()
		if location != "" {
			w.Header().Set("Location", location)
		}
		if reply != "" {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(status)
		if reply != "" {
			_, _ = w.Write([]byte(reply))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, state
}

// tenantWireServerNoFollow builds a transport WithHTTPClient whose client
// never follows a redirect: net/http hands it the 3xx response itself.
func tenantWireServerNoFollow(t *testing.T, srv *httptest.Server) *Transport {
	t.Helper()
	tr, err := New(&transport.Config{Address: srv.URL, MaxRetries: 0},
		WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: &http.Transport{TLSClientConfig: trustServer(srv)},
		}))
	if err != nil {
		t.Fatalf("New WithHTTPClient (no follow): %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	return tr
}

// tenantWireServerTransports returns the built, supplied and
// supplied-no-follow transports for srv.
func tenantWireServerTransports(t *testing.T, srv *httptest.Server) map[string]*Transport {
	t.Helper()
	trs := newTLSTransports(t, srv)
	trs["supplied-no-follow"] = tenantWireServerNoFollow(t, srv)
	return trs
}

// tenantWireServerRequest checks the method and path of the last request
// and that its JSON body equals want (nil: an empty body).
func tenantWireServerRequest(t *testing.T, state *tenantWireServerState, method, path string, want map[string]any) {
	t.Helper()
	gotMethod, gotPath, body := state.received()
	if gotMethod != method || gotPath != path {
		t.Fatalf("request: got %s %s, want %s %s", gotMethod, gotPath, method, path)
	}
	if want == nil {
		if len(body) != 0 {
			t.Fatalf("request body: got %q, want none", body)
		}
		return
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("request body %q: %v", body, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("request body: got %v, want %v", got, want)
	}
}

func TestTenantClient_WireContract(t *testing.T) {
	srv, state := tenantWireServer(t)
	const base = "/v1/tenants/t1/barrier"
	const csr = "-----BEGIN CERTIFICATE REQUEST-----\nMIIB\n-----END CERTIFICATE REQUEST-----\n"
	const cert = "-----BEGIN CERTIFICATE-----\nMIIC\n-----END CERTIFICATE-----\n"
	officers := `{"officers":[{"username":"alice","share_index":1,"auth_factor":"passkey","share_pending":true},{"username":"bob","share_index":2,"auth_factor":"totp"}]}`

	checkOfficers := func(t *testing.T, got []*qrdbtransport.OfficerStatus) {
		t.Helper()
		if len(got) != 2 || got[0].Username != "alice" || got[0].ShareIndex != 1 ||
			got[0].AuthFactor != "passkey" || !got[0].SharePending ||
			got[1].Username != "bob" || got[1].ShareIndex != 2 || got[1].SharePending {
			t.Fatalf("officers: got %+v", got)
		}
	}

	for name, tr := range tenantWireServerTransports(t, srv) {
		tc := tr.Tenants(nil)
		ctx := context.Background()
		t.Run(name, func(t *testing.T) {
			t.Run("Init204", func(t *testing.T) {
				state.answer(http.StatusNoContent, "", "")
				got, err := tc.TenantBarrierInit(ctx, &qrdbtransport.TenantBarrierInitRequest{
					TenantID: "t1", Passphrase: []byte("pw"), Strategy: "passphrase",
				})
				if err != nil {
					t.Fatalf("TenantBarrierInit: %v", err)
				}
				if got.TenantID != "t1" || got.Strategy != "passphrase" || got.ShareCount != 0 {
					t.Fatalf("TenantBarrierInit 204: got %+v", got)
				}
				tenantWireServerRequest(t, state, http.MethodPost, base+"/init",
					map[string]any{"passphrase": "cHc=", "strategy": "passphrase"})
			})
			t.Run("Init200", func(t *testing.T) {
				state.answer(http.StatusOK, `{"tenant_id":"t1","strategy":"shamir","threshold":2,"total_shares":3,"share_count":3}`, "")
				got, err := tc.TenantBarrierInit(ctx, &qrdbtransport.TenantBarrierInitRequest{
					TenantID: "t1", Threshold: 2, Shares: 3, Strategy: "shamir",
				})
				if err != nil {
					t.Fatalf("TenantBarrierInit: %v", err)
				}
				if got.TenantID != "t1" || got.Strategy != "shamir" || got.Threshold != 2 ||
					got.TotalShares != 3 || got.ShareCount != 3 {
					t.Fatalf("TenantBarrierInit 200: got %+v", got)
				}
				tenantWireServerRequest(t, state, http.MethodPost, base+"/init",
					map[string]any{"threshold": float64(2), "shares": float64(3), "strategy": "shamir"})
			})
			t.Run("UnsealKey", func(t *testing.T) {
				state.answer(http.StatusNoContent, "", "")
				if err := tc.TenantBarrierUnseal(ctx, &qrdbtransport.TenantBarrierUnsealRequest{
					TenantID: "t1", Key: []byte("key"), Share: []byte("ignored"),
				}); err != nil {
					t.Fatalf("TenantBarrierUnseal: %v", err)
				}
				tenantWireServerRequest(t, state, http.MethodPost, base+"/unseal", map[string]any{"key": "a2V5"})
			})
			t.Run("UnsealShareAsKey", func(t *testing.T) {
				state.answer(http.StatusNoContent, "", "")
				if err := tc.TenantBarrierUnseal(ctx, &qrdbtransport.TenantBarrierUnsealRequest{
					TenantID: "t1", Share: []byte("share"),
				}); err != nil {
					t.Fatalf("TenantBarrierUnseal: %v", err)
				}
				tenantWireServerRequest(t, state, http.MethodPost, base+"/unseal", map[string]any{"key": "c2hhcmU="})
			})
			for _, status := range []int{http.StatusNoContent, http.StatusOK} {
				t.Run("Seal"+strconv.Itoa(status), func(t *testing.T) {
					reply := ""
					if status == http.StatusOK {
						reply = `{"sealed":true}`
					}
					state.answer(status, reply, "")
					if err := tc.TenantBarrierSeal(ctx, &qrdbtransport.TenantBarrierSealRequest{TenantID: "t1"}); err != nil {
						t.Fatalf("TenantBarrierSeal: %v", err)
					}
					tenantWireServerRequest(t, state, http.MethodPost, base+"/seal", nil)
				})
			}
			t.Run("UnsealShare", func(t *testing.T) {
				state.answer(http.StatusOK, `{"tenant_id":"t1","required":3,"submitted":2,"complete":false,"sealed":true}`, "")
				got, err := tc.TenantBarrierUnsealShare(ctx, &qrdbtransport.TenantBarrierUnsealShareRequest{TenantID: "t1", Share: "s1"})
				if err != nil {
					t.Fatalf("TenantBarrierUnsealShare: %v", err)
				}
				if got.Required != 3 || got.Submitted != 2 || got.Complete || !got.Sealed {
					t.Fatalf("TenantBarrierUnsealShare: got %+v", got)
				}
				tenantWireServerRequest(t, state, http.MethodPost, base+"/unseal-share", map[string]any{"share": "s1"})
			})
			t.Run("Status", func(t *testing.T) {
				state.answer(http.StatusOK, `{"tenant_id":"t1","sealed":true,"initialized":true,"strategy":"shamir","hardware_backed":true,"shamir_threshold":2,"shamir_total":3}`, "")
				got, err := tc.TenantBarrierStatus(ctx, &qrdbtransport.TenantBarrierStatusRequest{TenantID: "t1"})
				if err != nil {
					t.Fatalf("TenantBarrierStatus: %v", err)
				}
				if got.TenantID != "t1" || !got.Sealed || !got.Initialized || got.Strategy != "shamir" ||
					!got.HardwareBacked || got.ShamirThreshold != 2 || got.ShamirTotal != 3 {
					t.Fatalf("TenantBarrierStatus: got %+v", got)
				}
				tenantWireServerRequest(t, state, http.MethodGet, base+"/status", nil)
			})
			t.Run("OfficerRegister", func(t *testing.T) {
				state.answer(http.StatusOK, `{"cert_pem":`+strconv.Quote(cert)+`,"share_index":4}`, "")
				got, err := tc.TenantBarrierOfficerRegister(ctx, &qrdbtransport.TenantOfficerRegisterRequest{
					TenantID: "t1", Username: "alice", CSRPEM: []byte(csr), AuthenticatorType: "passkey",
				})
				if err != nil {
					t.Fatalf("TenantBarrierOfficerRegister: %v", err)
				}
				if string(got.CertPEM) != cert || got.ShareIndex != 4 {
					t.Fatalf("TenantBarrierOfficerRegister: got %+v", got)
				}
				tenantWireServerRequest(t, state, http.MethodPost, base+"/officers",
					map[string]any{"username": "alice", "csr_pem": csr, "authenticator_type": "passkey"})
			})
			t.Run("OfficerList", func(t *testing.T) {
				state.answer(http.StatusOK, officers, "")
				got, err := tc.TenantBarrierOfficerList(ctx, "t1")
				if err != nil {
					t.Fatalf("TenantBarrierOfficerList: %v", err)
				}
				checkOfficers(t, got)
				tenantWireServerRequest(t, state, http.MethodGet, base+"/officers", nil)
			})
			t.Run("ClaimStatus", func(t *testing.T) {
				state.answer(http.StatusOK, officers, "")
				got, err := tc.TenantBarrierOfficerClaimStatus(ctx, "t1")
				if err != nil {
					t.Fatalf("TenantBarrierOfficerClaimStatus: %v", err)
				}
				checkOfficers(t, got)
				tenantWireServerRequest(t, state, http.MethodGet, base+"/officers/claim-status", nil)
			})
			t.Run("ClaimBegin", func(t *testing.T) {
				state.answer(http.StatusOK, `{"nonce_hex":"abcd","expires_at":"2026-01-02T03:04:05Z"}`, "")
				got, err := tc.TenantBarrierShareClaimBegin(ctx, &qrdbtransport.TenantShareClaimBeginRequest{TenantID: "t1", Username: "alice"})
				if err != nil {
					t.Fatalf("TenantBarrierShareClaimBegin: %v", err)
				}
				if got.NonceHex != "abcd" || got.ExpiresAt.IsZero() {
					t.Fatalf("TenantBarrierShareClaimBegin: got %+v", got)
				}
				tenantWireServerRequest(t, state, http.MethodPost, base+"/officers/alice/claim/begin",
					map[string]any{"tenant_id": "t1", "username": "alice"})
			})
			t.Run("ClaimComplete", func(t *testing.T) {
				state.answer(http.StatusOK, `{"share_index":2,"share_bytes":"c2hhcmU="}`, "")
				got, err := tc.TenantBarrierShareClaimComplete(ctx, &qrdbtransport.TenantShareClaimCompleteRequest{
					TenantID: "t1", Username: "alice", NonceHex: "abcd", Signature: []byte("sig"),
				})
				if err != nil {
					t.Fatalf("TenantBarrierShareClaimComplete: %v", err)
				}
				if got.ShareIndex != 2 || string(got.ShareBytes) != "share" {
					t.Fatalf("TenantBarrierShareClaimComplete: got index %d, %d share bytes", got.ShareIndex, len(got.ShareBytes))
				}
				tenantWireServerRequest(t, state, http.MethodPost, base+"/officers/alice/claim/complete",
					map[string]any{"tenant_id": "t1", "username": "alice", "nonce_hex": "abcd", "signature": "c2ln"})
			})
			t.Run("SealVote", func(t *testing.T) {
				state.answer(http.StatusOK, `{"required":2,"submitted":1,"complete":false}`, "")
				got, err := tc.TenantBarrierShamirSealVote(ctx, &qrdbtransport.TenantShamirSealVoteRequest{TenantID: "t1", Share: "s1"})
				if err != nil {
					t.Fatalf("TenantBarrierShamirSealVote: %v", err)
				}
				if got.Required != 2 || got.Submitted != 1 || got.Complete {
					t.Fatalf("TenantBarrierShamirSealVote: got %+v", got)
				}
				tenantWireServerRequest(t, state, http.MethodPost, base+"/seal-vote",
					map[string]any{"tenant_id": "t1", "share": "s1"})
			})
		})
	}
}

// tenantWireServerVerbs calls every tenant barrier verb of tc, each
// returning only its error.
func tenantWireServerVerbs(tc *TenantClient) map[string]func(context.Context) error {
	return map[string]func(context.Context) error{
		"Init": func(ctx context.Context) error {
			_, err := tc.TenantBarrierInit(ctx, &qrdbtransport.TenantBarrierInitRequest{TenantID: "t1", Passphrase: []byte("pw")})
			return err
		},
		"Unseal": func(ctx context.Context) error {
			return tc.TenantBarrierUnseal(ctx, &qrdbtransport.TenantBarrierUnsealRequest{TenantID: "t1", Key: []byte("key")})
		},
		"Seal": func(ctx context.Context) error {
			return tc.TenantBarrierSeal(ctx, &qrdbtransport.TenantBarrierSealRequest{TenantID: "t1"})
		},
		"UnsealShare": func(ctx context.Context) error {
			_, err := tc.TenantBarrierUnsealShare(ctx, &qrdbtransport.TenantBarrierUnsealShareRequest{TenantID: "t1", Share: "s1"})
			return err
		},
		"Status": func(ctx context.Context) error {
			_, err := tc.TenantBarrierStatus(ctx, &qrdbtransport.TenantBarrierStatusRequest{TenantID: "t1"})
			return err
		},
		"OfficerRegister": func(ctx context.Context) error {
			_, err := tc.TenantBarrierOfficerRegister(ctx, &qrdbtransport.TenantOfficerRegisterRequest{
				TenantID: "t1", Username: "alice", CSRPEM: []byte("csr"),
			})
			return err
		},
		"OfficerList": func(ctx context.Context) error {
			_, err := tc.TenantBarrierOfficerList(ctx, "t1")
			return err
		},
		"ClaimStatus": func(ctx context.Context) error {
			_, err := tc.TenantBarrierOfficerClaimStatus(ctx, "t1")
			return err
		},
		"ClaimBegin": func(ctx context.Context) error {
			_, err := tc.TenantBarrierShareClaimBegin(ctx, &qrdbtransport.TenantShareClaimBeginRequest{TenantID: "t1", Username: "alice"})
			return err
		},
		"ClaimComplete": func(ctx context.Context) error {
			_, err := tc.TenantBarrierShareClaimComplete(ctx, &qrdbtransport.TenantShareClaimCompleteRequest{
				TenantID: "t1", Username: "alice", NonceHex: "abcd", Signature: []byte("sig"),
			})
			return err
		},
		"SealVote": func(ctx context.Context) error {
			_, err := tc.TenantBarrierShamirSealVote(ctx, &qrdbtransport.TenantShamirSealVoteRequest{TenantID: "t1", Share: "s1"})
			return err
		},
	}
}

// tenantWireServerRefusal requires an internal error naming status.
func tenantWireServerRefusal(t *testing.T, err error, status int) {
	t.Helper()
	if err == nil {
		t.Fatalf("got nil error, want a refusal of HTTP %d", status)
	}
	if code := errors.Code(err); code != errors.CodeInternal {
		t.Fatalf("error code: got %v, want %v (%v)", code, errors.CodeInternal, err)
	}
	if !strings.Contains(err.Error(), strconv.Itoa(status)) {
		t.Fatalf("the refusal does not name HTTP %d: %v", status, err)
	}
}

func TestTenantClient_RefusesUnexpectedStatus(t *testing.T) {
	srv, state := tenantWireServer(t)
	const reply = `{"tenant_id":"t1","sealed":false,"officers":[],"required":1,"submitted":1,"complete":true}`

	// A 307 without a Location is handed back by net/http as the response.
	for name, tr := range tenantWireServerTransports(t, srv) {
		verbs := tenantWireServerVerbs(tr.Tenants(nil))
		for _, verb := range []string{"Init", "Unseal", "Seal"} {
			t.Run("307-no-location/"+name+"/"+verb, func(t *testing.T) {
				state.answer(http.StatusTemporaryRedirect, reply, "")
				tenantWireServerRefusal(t, verbs[verb](context.Background()), http.StatusTemporaryRedirect)
			})
		}
	}

	// A client that never follows redirects is handed every 3xx.
	verbs := tenantWireServerVerbs(tenantWireServerNoFollow(t, srv).Tenants(nil))
	for verb, call := range verbs {
		t.Run("302-supplied-no-follow/"+verb, func(t *testing.T) {
			state.answer(http.StatusFound, reply, "/v1/elsewhere")
			tenantWireServerRefusal(t, call(context.Background()), http.StatusFound)
		})
	}
}

// tenantRedirectTransports mirrors newTLSTransports with the platform bearer
// set on both the built and the supplied transport.
func tenantRedirectTransports(t *testing.T, srv *httptest.Server) map[string]*Transport {
	t.Helper()
	cfg := trustServer(srv)
	built, err := New(&transport.Config{Address: srv.URL, TLSConfig: cfg, MaxRetries: 0}, WithAuthToken("tok-2558"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	supplied, err := New(&transport.Config{Address: srv.URL, MaxRetries: 0},
		WithHTTPClient(&http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: cfg}}),
		WithAuthToken("tok-2558"))
	if err != nil {
		t.Fatalf("New WithHTTPClient: %v", err)
	}
	t.Cleanup(func() { _ = built.Close(); _ = supplied.Close() })
	return map[string]*Transport{"built": built, "supplied": supplied}
}

// tenantRedirectTarget answers every request with a sealed status and
// records whether any request arrived with an Authorization header.
func tenantRedirectTarget(sawBearer *atomic.Bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			sawBearer.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"tenant_id":"t1","sealed":true}`)
	})
}

// tenantRedirectOrigin is a TLS server that 307s the tenant barrier status
// and seal paths to target(origin)+path+suffix and serves the rest with next.
func tenantRedirectOrigin(t *testing.T, target func(*httptest.Server) string, suffix string, next http.Handler) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tenants/t1/barrier/status", "/v1/tenants/t1/barrier/seal":
			http.Redirect(w, r, target(srv)+r.URL.Path+suffix, http.StatusTemporaryRedirect)
			return
		}
		next.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestTenantClient_BearerRedirects pins that a bearer-carrying tenant verb
// never follows a redirect to plaintext http or to another host:port: the
// call fails with ErrInsecureRedirect (CodeForbidden) and the bearer never
// reaches the target. A same-host https redirect is still followed.
func TestTenantClient_BearerRedirects(t *testing.T) {
	rows := []struct {
		name   string
		refuse bool
		origin func(t *testing.T, saw *atomic.Bool) *httptest.Server
	}{
		{"https to http downgrade", true, func(t *testing.T, saw *atomic.Bool) *httptest.Server {
			plain := httptest.NewServer(tenantRedirectTarget(saw))
			t.Cleanup(plain.Close)
			return tenantRedirectOrigin(t, func(*httptest.Server) string { return plain.URL }, "", http.NotFoundHandler())
		}},
		{"https to another host", true, func(t *testing.T, saw *atomic.Bool) *httptest.Server {
			other := httptest.NewTLSServer(tenantRedirectTarget(saw))
			t.Cleanup(other.Close)
			return tenantRedirectOrigin(t, func(*httptest.Server) string { return other.URL }, "", http.NotFoundHandler())
		}},
		{"same-host https", false, func(t *testing.T, saw *atomic.Bool) *httptest.Server {
			return tenantRedirectOrigin(t, func(s *httptest.Server) string { return s.URL }, "-moved", tenantRedirectTarget(saw))
		}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			var saw atomic.Bool
			origin := row.origin(t, &saw)
			for name, tr := range tenantRedirectTransports(t, origin) {
				tc := tr.Tenants(nil)
				verbs := map[string]func(context.Context) error{
					"Status": func(ctx context.Context) error {
						st, err := tc.TenantBarrierStatus(ctx, &qrdbtransport.TenantBarrierStatusRequest{TenantID: "t1"})
						if err == nil && !st.Sealed {
							t.Errorf("Status: sealed = false, want true")
						}
						return err
					},
					"Seal": func(ctx context.Context) error {
						return tc.TenantBarrierSeal(ctx, &qrdbtransport.TenantBarrierSealRequest{TenantID: "t1"})
					},
				}
				for verb, call := range verbs {
					t.Run(name+"/"+verb, func(t *testing.T) {
						saw.Store(false)
						err := call(context.Background())
						if !row.refuse {
							if err != nil {
								t.Fatalf("same-host redirect: %v", err)
							}
							if !saw.Load() {
								t.Fatalf("same-host target got no bearer")
							}
							return
						}
						if !stderrors.Is(err, ErrInsecureRedirect) {
							t.Errorf("err = %v, want ErrInsecureRedirect", err)
						}
						if got := errors.Code(err); got != errors.CodeForbidden {
							t.Errorf("code = %v, want CodeForbidden", got)
						}
						if errors.IsRetryable(err) {
							t.Errorf("refusal is retryable: %v", err)
						}
						if saw.Load() {
							t.Errorf("redirect target received the bearer")
						}
					})
				}
			}
		})
	}
}

// TestTenantClient_SecretHopRefusalIsForbidden pins that the secret-hop
// guard's refusal of a tenant unseal (an http base, or a redirect to http)
// reaches the caller as CodeForbidden and not retryable, and that no request
// reaches the plaintext server.
func TestTenantClient_SecretHopRefusalIsForbidden(t *testing.T) {
	var hits atomic.Int32
	plain := countingServer(&hits)
	t.Cleanup(plain.Close)

	requireForbidden := func(t *testing.T, err error) {
		t.Helper()
		if err == nil {
			t.Fatal("got nil error, want a refusal")
		}
		if got := errors.Code(err); got != errors.CodeForbidden {
			t.Errorf("code = %v, want CodeForbidden: %v", got, err)
		}
		if errors.IsRetryable(err) {
			t.Errorf("refusal is retryable: %v", err)
		}
		if stderrors.Is(err, ErrInsecureRedirect) {
			t.Errorf("err = %v, want the secret-hop refusal, not ErrInsecureRedirect", err)
		}
	}
	unseal := func(tr *Transport) error {
		return tr.Tenants(nil).TenantBarrierUnseal(context.Background(),
			&qrdbtransport.TenantBarrierUnsealRequest{TenantID: "t1", Key: []byte("k")})
	}

	t.Run("http base", func(t *testing.T) {
		tr, err := New(&transport.Config{Address: plain.URL, MaxRetries: 0})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		t.Cleanup(func() { _ = tr.Close() })
		requireForbidden(t, unseal(tr))
	})

	t.Run("redirect", func(t *testing.T) {
		srv := redirectingTLSServer("/v1/tenants/t1/barrier/unseal",
			func(*httptest.Server) string { return plain.URL + "/v1/tenants/t1/barrier/unseal" },
			recordingHandler(new(atomic.Int32)))
		t.Cleanup(srv.Close)
		for name, tr := range newTLSTransports(t, srv) {
			t.Run(name, func(t *testing.T) {
				requireForbidden(t, unseal(tr))
			})
		}
	})

	if n := hits.Load(); n != 0 {
		t.Fatalf("plaintext server received %d requests, want 0", n)
	}
}
