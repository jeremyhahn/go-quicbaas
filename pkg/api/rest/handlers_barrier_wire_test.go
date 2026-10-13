//go:build rest

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

package rest

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	qrdbrest "github.com/jeremyhahn/go-qrdb/sdk/go/transport/rest"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Barrier REST wire contract — go-quicbaas#988.
//
// The go-qrdb SDK's REST transport marshals go-qrdb's own
// transport.Barrier*Request structs straight into the request body these
// handlers decode, so those structs ARE the wire format (CLAUDE.md, "SDK
// Boundary": the stack is pre-v1 with exactly one). go-qrdb v0.19.0-alpha
// retyped every barrier credential from `string` to wipeable `[]byte`, which
// makes `secret` a BASE64 string in the JSON body. The handlers here kept
// decoding it into a `string` and handed that base64 text to the seal
// strategy, so `quicbaas barrier unseal --transport rest` failed on a node it
// had just sealed with:
//
//	quicraft/seal: unseal failed (strategy=passphrase): decryption failed
//
// The tests below drive the REAL go-qrdb REST transport — the exact client
// the CLI uses — against this package's router, so a divergence between the
// two sides fails here rather than on an operator's sealed node.
//
// One limit is stated outright rather than left to be discovered: decoding
// catches MALFORMED base64 and nothing else. A raw passphrase that happens to
// be valid base64 decodes into different bytes and is accepted, silently.
// =============================================================================

// wirePassphrase is deliberately non-ASCII and non-UTF-8-clean. A credential
// is a byte string, not text: a test that only ever sends "s3cr3t" passes
// just as well against a handler that treats the field as text, which is the
// bug this file exists to catch.
var wirePassphrase = []byte{'c', 'o', 'r', 'r', 'e', 'c', 't', 0x00, 0xff, 0xfe, ' ', 'h', 'o', 'r', 's', 'e'}

// newQRDBRESTClient starts an httptest server over the supplied router and
// returns a connected go-qrdb REST transport pointed at it.
//
// Connect's reachability probe (GET /health) is allowed to fail exactly as it
// is in production: sdk/go/qrdb.go's buildQRDBClient swallows the health-check
// error because QuicBaaS does not export go-qrdb's Health RPC even though
// every other endpoint on the same connection works. The HTTP client is fully
// built by the time the probe runs, so the transport is usable either way.
func newQRDBRESTClient(t *testing.T, s *Server) *qrdbrest.Transport {
	t.Helper()

	srv := httptest.NewServer(s.router)
	t.Cleanup(srv.Close)

	tr, err := qrdbrest.New(qrdbtransport.WithAddress(srv.URL))
	if err != nil {
		t.Fatalf("qrdbrest.New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	if err := tr.Connect(context.Background()); err != nil && !strings.Contains(err.Error(), "health check failed") {
		t.Fatalf("connect: %v", err)
	}
	return tr
}

// TestBarrierWire_UnsealCarriesExactSecretBytes is the regression test for the
// release blocker: the passphrase the CLI holds must be the passphrase the
// barrier service is asked to unseal with, byte for byte.
//
// On fc5592cf (v0.11.0-alpha) the handler decoded `secret` as a string, so the
// service received the BASE64 TEXT of the passphrase and every REST unseal
// failed to decrypt.
func TestBarrierWire_UnsealCarriesExactSecretBytes(t *testing.T) {
	var got []byte
	svc := &fakeBarrierService{
		// Copy: the handler wipes the slice once the call returns.
		unsealFn: func(_ context.Context, secret []byte) error {
			got = append([]byte(nil), secret...)
			return nil
		},
	}
	client := newQRDBRESTClient(t, barrierServer(t, svc))

	if err := client.BarrierUnseal(context.Background(), &qrdbtransport.BarrierUnsealRequest{
		Secret: append([]byte(nil), wirePassphrase...),
	}); err != nil {
		t.Fatalf("BarrierUnseal over the qrdb REST transport: %v", err)
	}
	if !bytes.Equal(got, wirePassphrase) {
		t.Fatalf("secret reached the service as %q, want %q", got, wirePassphrase)
	}
}

// TestBarrierWire_InitializeCarriesExactSecretBytes asserts the same property
// for `barrier init`, whose secret field changed in the same go-qrdb release.
func TestBarrierWire_InitializeCarriesExactSecretBytes(t *testing.T) {
	var got []byte
	svc := &fakeBarrierService{
		initializeFn: func(_ context.Context, secret []byte) error {
			got = append([]byte(nil), secret...)
			return nil
		},
	}
	client := newQRDBRESTClient(t, barrierServer(t, svc))

	if err := client.BarrierInitialize(context.Background(), &qrdbtransport.BarrierInitializeRequest{
		Secret:          append([]byte(nil), wirePassphrase...),
		BarrierStrategy: "passphrase",
	}); err != nil {
		t.Fatalf("BarrierInitialize over the qrdb REST transport: %v", err)
	}
	if !bytes.Equal(got, wirePassphrase) {
		t.Fatalf("secret reached the service as %q, want %q", got, wirePassphrase)
	}
}

// TestBarrierWire_ShamirInitializeCarriesExactSecretBytes asserts the property
// for `barrier init-shamir`, and that the M-of-N parameters riding alongside
// the credential survive the same round trip.
func TestBarrierWire_ShamirInitializeCarriesExactSecretBytes(t *testing.T) {
	var (
		got                []byte
		gotThr, gotTotal   int
		wantThr, wantTotal = 2, 3
	)
	svc := &fakeBarrierService{
		initShamirFn: func(_ context.Context, secret []byte, threshold, total int) (*service.ShamirInitInfo, error) {
			got = append([]byte(nil), secret...)
			gotThr, gotTotal = threshold, total
			return &service.ShamirInitInfo{Threshold: threshold, TotalShares: total, ShareCount: total}, nil
		},
	}
	client := newQRDBRESTClient(t, barrierServer(t, svc))

	resp, err := client.BarrierInitializeShamir(context.Background(), &qrdbtransport.BarrierInitializeShamirRequest{
		Secret:      append([]byte(nil), wirePassphrase...),
		Threshold:   wantThr,
		TotalShares: wantTotal,
	})
	if err != nil {
		t.Fatalf("BarrierInitializeShamir over the qrdb REST transport: %v", err)
	}
	if !bytes.Equal(got, wirePassphrase) {
		t.Fatalf("secret reached the service as %q, want %q", got, wirePassphrase)
	}
	if gotThr != wantThr || gotTotal != wantTotal {
		t.Fatalf("quorum reached the service as %d-of-%d, want %d-of-%d", gotThr, gotTotal, wantThr, wantTotal)
	}
	if resp.Threshold != wantThr || resp.TotalShares != wantTotal {
		t.Fatalf("response decoded as %d-of-%d, want %d-of-%d", resp.Threshold, resp.TotalShares, wantThr, wantTotal)
	}
}

// TestBarrierWire_ShareVerbsCarryShareStrings covers the share-bearing verbs,
// whose fields go-qrdb deliberately kept as `string` (a Shamir share is
// already a printable encoding). They are asserted here so a future
// over-correction that retypes them to []byte — and so base64-wraps an
// already-encoded share — is caught by the same file.
func TestBarrierWire_ShareVerbsCarryShareStrings(t *testing.T) {
	const share = "1-8f3a2b"
	var (
		gotShare  string
		gotShares []string
	)
	svc := &fakeBarrierService{
		unsealWithShareFn: func(_ context.Context, s string) (*service.UnsealShareProgress, error) {
			gotShare = s
			return &service.UnsealShareProgress{Required: 2, Submitted: 1}, nil
		},
		unsealWithSharesFn: func(_ context.Context, shares []string) error {
			gotShares = shares
			return nil
		},
	}
	client := newQRDBRESTClient(t, barrierServer(t, svc))
	ctx := context.Background()

	progress, err := client.BarrierUnsealWithShare(ctx, &qrdbtransport.BarrierUnsealShareRequest{Share: share})
	if err != nil {
		t.Fatalf("BarrierUnsealWithShare: %v", err)
	}
	if gotShare != share {
		t.Fatalf("share reached the service as %q, want %q", gotShare, share)
	}
	if progress.Required != 2 || progress.Submitted != 1 {
		t.Fatalf("progress decoded as %d/%d, want 1/2", progress.Submitted, progress.Required)
	}

	if err := client.BarrierUnsealWithShares(ctx, &qrdbtransport.BarrierUnsealSharesRequest{
		Shares: []string{share, "2-11c4de"},
	}); err != nil {
		t.Fatalf("BarrierUnsealWithShares: %v", err)
	}
	if len(gotShares) != 2 || gotShares[0] != share {
		t.Fatalf("shares reached the service as %q", gotShares)
	}
}

// TestBarrierWire_QuorumVerbsCarryCounts covers rekey, recovery-key generation
// and recovery, root-token minting and recovery status — the barrier verbs
// that carry no credential. They share this file because they share the wire
// contract: each request shape is an alias of the go-qrdb transport type, so a
// field renamed upstream fails to compile rather than silently decoding zero.
func TestBarrierWire_QuorumVerbsCarryCounts(t *testing.T) {
	var (
		gotRekeyThr, gotRekeyTotal int
		gotGenThr, gotGenTotal     int
		gotKeys, gotTokenShares    []string
	)
	svc := &fakeBarrierService{
		rekeyFn: func(_ context.Context, threshold, total int) (*service.ShamirInitInfo, error) {
			gotRekeyThr, gotRekeyTotal = threshold, total
			return &service.ShamirInitInfo{Threshold: threshold, TotalShares: total, ShareCount: total}, nil
		},
		genRecoveryFn: func(_ context.Context, threshold, total int) (*service.RecoveryKeysInfo, error) {
			gotGenThr, gotGenTotal = threshold, total
			return &service.RecoveryKeysInfo{Threshold: threshold, Total: total, ShareCount: total}, nil
		},
		recoverWithKeysFn: func(_ context.Context, keys []string) error { gotKeys = keys; return nil },
		genRootTokenFn: func(_ context.Context, shares []string) (*service.RootTokenInfo, error) {
			gotTokenShares = shares
			return &service.RootTokenInfo{Token: "root-tok"}, nil
		},
	}
	client := newQRDBRESTClient(t, barrierServer(t, svc))
	ctx := context.Background()

	rekey, err := client.BarrierRekey(ctx, &qrdbtransport.BarrierRekeyRequest{Threshold: 3, Total: 5})
	if err != nil {
		t.Fatalf("BarrierRekey: %v", err)
	}
	if gotRekeyThr != 3 || gotRekeyTotal != 5 || rekey.Threshold != 3 || rekey.TotalShares != 5 {
		t.Fatalf("rekey quorum: service saw %d-of-%d, client decoded %d-of-%d",
			gotRekeyThr, gotRekeyTotal, rekey.Threshold, rekey.TotalShares)
	}

	keys, err := client.BarrierGenerateRecoveryKeys(ctx, &qrdbtransport.BarrierGenerateRecoveryKeysRequest{
		Threshold: 2, Total: 4,
	})
	if err != nil {
		t.Fatalf("BarrierGenerateRecoveryKeys: %v", err)
	}
	if gotGenThr != 2 || gotGenTotal != 4 || keys.Threshold != 2 || keys.Total != 4 {
		t.Fatalf("recovery quorum: service saw %d-of-%d, client decoded %d-of-%d",
			gotGenThr, gotGenTotal, keys.Threshold, keys.Total)
	}

	if err := client.BarrierRecoverWithKeys(ctx, &qrdbtransport.BarrierRecoverWithKeysRequest{
		Keys: []string{"k1", "k2"},
	}); err != nil {
		t.Fatalf("BarrierRecoverWithKeys: %v", err)
	}
	if len(gotKeys) != 2 {
		t.Fatalf("recovery keys reached the service as %q", gotKeys)
	}

	tok, err := client.BarrierGenerateRootToken(ctx, &qrdbtransport.BarrierGenerateRootTokenRequest{
		Shares: []string{"s1", "s2"},
	})
	if err != nil {
		t.Fatalf("BarrierGenerateRootToken: %v", err)
	}
	if len(gotTokenShares) != 2 || tok.Token != "root-tok" {
		t.Fatalf("root token: service saw shares %q, client decoded token %q", gotTokenShares, tok.Token)
	}
}

// TestBarrierWire_StatusDecodesIntoQRDBResponse closes the loop on the read
// path: the status body this package emits must decode into go-qrdb's
// BarrierStatusResponse, since that is what `barrier status` renders.
func TestBarrierWire_StatusDecodesIntoQRDBResponse(t *testing.T) {
	svc := &fakeBarrierService{
		statusFn: func(context.Context) (*service.BarrierStatusInfo, error) {
			return &service.BarrierStatusInfo{Sealed: true, Strategy: "passphrase", HardwareBacked: false}, nil
		},
	}
	client := newQRDBRESTClient(t, barrierServer(t, svc))

	resp, err := client.BarrierStatus(context.Background())
	if err != nil {
		t.Fatalf("BarrierStatus over the qrdb REST transport: %v", err)
	}
	if !resp.Sealed || resp.Strategy != "passphrase" {
		t.Fatalf("status decoded as sealed=%v strategy=%q, want sealed=true strategy=passphrase",
			resp.Sealed, resp.Strategy)
	}
}

// TestBarrierWire_MalformedBase64SecretIsRejected is the error-handling half
// of the contract: a credential field that is not well-formed base64 is a
// malformed body, refused at the edge with 400 rather than forwarded to the
// barrier. "s3cr3t" is the literal the pre-fix tests sent, so this pins the
// exact shape that used to be accepted as a passphrase.
//
// This is NOT a guarantee that raw text is always caught — see
// TestBarrierWire_RawCredentialThatIsValidBase64DecodesToOtherBytes for the
// limit.
func TestBarrierWire_MalformedBase64SecretIsRejected(t *testing.T) {
	called := false
	svc := &fakeBarrierService{
		unsealFn: func(context.Context, []byte) error { called = true; return nil },
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/v1/barrier/unseal", `{"secret":"s3cr3t"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a malformed base64 secret, got %d (body=%s)", w.Code, w.Body.String())
	}
	assertCredentialEncodingRefusal(t, w, "s3cr3t")
	if called {
		t.Fatal("a malformed credential reached the barrier service")
	}
}

// =============================================================================
// Per-tenant barrier
// =============================================================================

// TestTenantBarrierWire_InitCarriesExactPassphraseBytes asserts the per-tenant
// init body decodes go-qrdb's TenantBarrierInitRequest, whose Passphrase moved
// to []byte in the same release.
//
// The request is marshalled from the real go-qrdb type and POSTed to this
// package's route rather than driven through the go-qrdb transport: that
// transport addresses the per-tenant barrier at the root /v1/tenants/{id}/...
// prefix, which quicbaas does not mount (the embedded go-qrdb node owns /v1
// tenant routes). The wire SHAPE under test is identical either way.
func TestTenantBarrierWire_InitCarriesExactPassphraseBytes(t *testing.T) {
	var (
		got      []byte
		gotStrat string
	)
	svc := &fakeTenantBarrierService{
		initFn: func(_ context.Context, tenantID string, passphrase []byte, _, _ int, strategy string) (*qrdbtransport.TenantBarrierInitResponse, error) {
			got = append([]byte(nil), passphrase...)
			gotStrat = strategy
			return &qrdbtransport.TenantBarrierInitResponse{TenantID: tenantID}, nil
		},
	}
	body, err := json.Marshal(&qrdbtransport.TenantBarrierInitRequest{
		TenantID:   "tenant-a",
		Passphrase: append([]byte(nil), wirePassphrase...),
		Strategy:   "passphrase",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	w := httptest.NewRecorder()
	tenantBarrierServer(t, svc).HandleTenantBarrierInit(w,
		tenantBarrierReq(http.MethodPost, "tenant-a", string(body)))

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !bytes.Equal(got, wirePassphrase) {
		t.Fatalf("passphrase reached the service as %q, want %q", got, wirePassphrase)
	}
	if gotStrat != "passphrase" {
		t.Fatalf("strategy reached the service as %q, want %q", gotStrat, "passphrase")
	}
}

// TestTenantBarrierWire_MalformedBase64PassphraseIsRejected is the
// error-handling half: a passphrase field that is not well-formed base64 is a
// malformed body and must never reach the tenant barrier.
func TestTenantBarrierWire_MalformedBase64PassphraseIsRejected(t *testing.T) {
	called := false
	svc := &fakeTenantBarrierService{
		initFn: func(context.Context, string, []byte, int, int, string) (*qrdbtransport.TenantBarrierInitResponse, error) {
			called = true
			return &qrdbtransport.TenantBarrierInitResponse{}, nil
		},
	}
	w := httptest.NewRecorder()
	tenantBarrierServer(t, svc).HandleTenantBarrierInit(w,
		tenantBarrierReq(http.MethodPost, "tenant-a",
			`{"strategy":"passphrase","passphrase":"s3cr3t"}`))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a malformed base64 passphrase, got %d (body=%s)", w.Code, w.Body.String())
	}
	assertCredentialEncodingRefusal(t, w, "s3cr3t")
	if called {
		t.Fatal("a malformed credential reached the tenant barrier service")
	}
}

// assertCredentialEncodingRefusal checks that a 400 for a malformed credential
// carries BarrierCredentialEncodingError and does not echo the rejected text,
// which may be a raw credential the caller forgot to encode.
func assertCredentialEncodingRefusal(t *testing.T, w *httptest.ResponseRecorder, rejected string) {
	t.Helper()
	var resp ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if want := (&BarrierCredentialEncodingError{}).Error(); resp.Error != want {
		t.Fatalf("error = %q, want %q", resp.Error, want)
	}
	if strings.Contains(w.Body.String(), rejected) {
		t.Fatalf("the refusal echoed the rejected credential: %s", w.Body.String())
	}
}

// TestBarrierWire_MalformedJSONIsNotACredentialEncodingError keeps the typed
// refusal specific: a body that is not JSON at all is still "invalid JSON".
func TestBarrierWire_MalformedJSONIsNotACredentialEncodingError(t *testing.T) {
	w := doBarrierReq(t, barrierServer(t, &fakeBarrierService{}), http.MethodPost, "/v1/barrier/unseal", `{"secret":`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a truncated body, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), (&BarrierCredentialEncodingError{}).Error()) {
		t.Fatalf("a truncated body was reported as a credential encoding error: %s", w.Body.String())
	}
}

// TestTenantBarrierWire_UnsealCarriesExactKeyBytes covers the per-tenant
// unseal key, which go-qrdb already carried as []byte before v0.19.0-alpha —
// asserted here so the alias keeps it that way, and so the credential this
// route consumes is exercised alongside the one that changed.
func TestTenantBarrierWire_UnsealCarriesExactKeyBytes(t *testing.T) {
	var got []byte
	svc := &fakeTenantBarrierService{
		unsealFn: func(_ context.Context, _ string, key []byte) error {
			got = append([]byte(nil), key...)
			return nil
		},
	}
	body, err := json.Marshal(&qrdbtransport.TenantBarrierUnsealRequest{
		TenantID: "tenant-a",
		Key:      append([]byte(nil), wirePassphrase...),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	w := httptest.NewRecorder()
	tenantBarrierServer(t, svc).HandleTenantBarrierUnseal(w,
		tenantBarrierReq(http.MethodPost, "tenant-a", string(body)))

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !bytes.Equal(got, wirePassphrase) {
		t.Fatalf("key reached the service as %q, want %q", got, wirePassphrase)
	}
}

// TestTenantBarrierWire_UnsealWipesKeyOnAPartialDecode is the error-handling
// half, and the reason the cleanup is registered before the decode here too:
// `key` decodes, `share` then fails to, the request is refused, and the tenant
// root key would otherwise be left on the heap.
func TestTenantBarrierWire_UnsealWipesKeyOnAPartialDecode(t *testing.T) {
	var body tenantBarrierUnsealBody
	cleanup := wipeCredential(&body.Key)

	if err := json.Unmarshal([]byte(`{"key":"aHVudGVyMg==","share":42}`), &body); err == nil {
		t.Fatal("precondition: this body must fail to decode")
	}
	if !bytes.Equal(body.Key, []byte("hunter2")) {
		t.Fatalf("precondition: a partial decode must populate the key, got %q", body.Key)
	}

	cleanup()

	for i, b := range body.Key {
		if b != 0 {
			t.Fatalf("key byte %d survived the cleanup: %q", i, body.Key)
		}
	}
}

// =============================================================================
// Structural guard
// =============================================================================

// TestBarrierRequestShapesMatchQRDBTransport is the control that keeps this
// class of defect from returning: it walks every credential-bearing barrier
// request field on this REST surface and asserts it is []byte carrying the
// same JSON name go-qrdb gives it.
//
// It mirrors go-qrdb's own TestBarrierSecretFieldsAreWipeableBytes
// (sdk/go/transport/barrier_secret_types_test.go) on purpose — that test
// guards the client side of the wire, this one guards the server side, and a
// credential is only safe when both hold. The list is written out rather than
// discovered so that ADDING a credential field is a deliberate act.
func TestBarrierRequestShapesMatchQRDBTransport(t *testing.T) {
	byteSlice := reflect.TypeOf([]byte(nil))

	cases := []struct {
		name  string
		req   any
		field string
		json  string
	}{
		{"barrier initialize", barrierInitializeRequest{}, "Secret", "secret"},
		{"barrier unseal", barrierUnsealRequest{}, "Secret", "secret"},
		{"barrier shamir initialize", barrierShamirInitializeRequest{}, "Secret", "secret"},
		{"barrier enable encryption", barrierEnableEncryptionRequest{}, "Passphrase", "passphrase,omitempty"},
		{"tenant barrier init", tenantBarrierInitBody{}, "Passphrase", "passphrase,omitempty"},
		{"tenant barrier unseal", tenantBarrierUnsealBody{}, "Key", "key,omitempty"},
		{"tenant barrier enable encryption", tenantBarrierEnableEncryptionBody{}, "Passphrase", "passphrase,omitempty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			field, ok := reflect.TypeOf(tc.req).FieldByName(tc.field)
			if !ok {
				t.Fatalf("field %s is gone", tc.field)
			}
			if field.Type != byteSlice {
				t.Errorf("%s is %s, want []byte — a credential on this wire is "+
					"base64-encoded bytes, and a string field decodes the base64 "+
					"TEXT as the credential (go-quicbaas#988)", tc.field, field.Type)
			}
			if got := field.Tag.Get("json"); got != tc.json {
				t.Errorf("json tag is %q, want %q", got, tc.json)
			}
		})
	}
}

// TestBarrierRequestShapesAreQRDBAliases asserts the shapes go-qrdb already
// names are the SAME type, not a copy. Aliasing is what makes a field renamed
// or retyped upstream a compile error here instead of a silent mis-decode, so
// it is a property worth a test of its own: someone reinstating a local mirror
// to add one field would otherwise reopen go-quicbaas#988 undetected.
func TestBarrierRequestShapesAreQRDBAliases(t *testing.T) {
	cases := []struct {
		name       string
		local, sdk any
	}{
		{"initialize", barrierInitializeRequest{}, qrdbtransport.BarrierInitializeRequest{}},
		{"unseal", barrierUnsealRequest{}, qrdbtransport.BarrierUnsealRequest{}},
		{"shamir initialize", barrierShamirInitializeRequest{}, qrdbtransport.BarrierInitializeShamirRequest{}},
		{"unseal share", barrierShamirUnsealShareRequest{}, qrdbtransport.BarrierUnsealShareRequest{}},
		{"unseal shares", barrierShamirUnsealSharesRequest{}, qrdbtransport.BarrierUnsealSharesRequest{}},
		{"rekey", barrierRekeyRequest{}, qrdbtransport.BarrierRekeyRequest{}},
		{"recovery generate", barrierRecoveryGenerateRequest{}, qrdbtransport.BarrierGenerateRecoveryKeysRequest{}},
		{"recovery recover", barrierRecoveryRecoverRequest{}, qrdbtransport.BarrierRecoverWithKeysRequest{}},
		{"root token", barrierRootTokenRequest{}, qrdbtransport.BarrierGenerateRootTokenRequest{}},
		{"tenant init", tenantBarrierInitBody{}, qrdbtransport.TenantBarrierInitRequest{}},
		{"tenant unseal", tenantBarrierUnsealBody{}, qrdbtransport.TenantBarrierUnsealRequest{}},
		{"tenant unseal share", tenantBarrierUnsealShareBody{}, qrdbtransport.TenantBarrierUnsealShareRequest{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			local, sdk := reflect.TypeOf(tc.local), reflect.TypeOf(tc.sdk)
			if local != sdk {
				t.Fatalf("%s is a local copy of %s, not an alias of it", local, sdk)
			}
		})
	}
}

// =============================================================================
// Credential hygiene on the paths that never reach the service
// =============================================================================

// TestWipeCredential_ZeroesWhatTheDecodeProduced is the unit half of the
// cleanup contract: the closure must wipe the slice the decode left behind,
// not the nil slice that existed when the closure was registered.
//
// The body below is the exact hazard — `secret` is well-formed and `threshold`
// is not, so encoding/json populates the credential and THEN reports an error.
// The request is refused, the service is never called, and without this
// cleanup the plaintext stays on the heap.
func TestWipeCredential_ZeroesWhatTheDecodeProduced(t *testing.T) {
	var req barrierShamirInitializeRequest
	cleanup := wipeCredential(&req.Secret)

	if err := json.Unmarshal([]byte(`{"secret":"aHVudGVyMg==","threshold":"not-a-number"}`), &req); err == nil {
		t.Fatal("precondition: this body must fail to decode")
	}
	if !bytes.Equal(req.Secret, []byte("hunter2")) {
		t.Fatalf("precondition: a partial decode must populate the credential, got %q", req.Secret)
	}

	cleanup()

	for i, b := range req.Secret {
		if b != 0 {
			t.Fatalf("credential byte %d survived the cleanup: %q", i, req.Secret)
		}
	}
}

// TestWipeCredential_ToleratesAnAbsentCredential is the error-handling half:
// a body that carries no credential at all, or one that never decoded, leaves
// the field nil, and the cleanup still runs on every return path.
func TestWipeCredential_ToleratesAnAbsentCredential(t *testing.T) {
	var req barrierUnsealRequest
	cleanup := wipeCredential(&req.Secret)

	if err := json.Unmarshal([]byte(`{}`), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Secret != nil {
		t.Fatalf("precondition: an absent credential decodes to nil, got %q", req.Secret)
	}

	cleanup() // must not panic
}

// TestWipeCredential_CoversTheFinalSliceOnlyIsTheStatedGuarantee makes the
// boundary of the cleanup executable rather than prose, so the documentation
// in docs/barrier/api.md cannot quietly become an overclaim.
//
// The guarantee is that the slice the field FINALLY holds is destroyed, along
// with every alias of it — the copy the barrier service was handed, and the
// one that would appear under a stable name in a heap dump. It is not that no
// plaintext byte is ever allocated: `encoding/json` can allocate a value and
// then drop it, and nothing in this package holds a reference to reach.
//
// A duplicate JSON key is the sharpest case, so it is the one pinned here. Go
// takes the LAST occurrence, so `{"secret":"<a>","secret":null}` decodes the
// first value, allocates it, and then overwrites the field with nil. Cleanup
// wipes nil, because nil is what the field holds; the first allocation is
// unreferenced and only the collector reclaims it. That is the documented
// limit, and this test states it.
func TestWipeCredential_CoversTheFinalSliceOnlyIsTheStatedGuarantee(t *testing.T) {
	t.Run("the final slice is destroyed", func(t *testing.T) {
		var req barrierUnsealRequest
		cleanup := wipeCredential(&req.Secret)

		// Both keys carry a credential; the second wins.
		if err := json.Unmarshal([]byte(`{"secret":"Zmlyc3Q=","secret":"c2Vjb25k"}`), &req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !bytes.Equal(req.Secret, []byte("second")) {
			t.Fatalf("precondition: the last key wins, got %q", req.Secret)
		}
		// Alias it the way the barrier service holds the slice it was handed.
		alias := req.Secret

		cleanup()

		for i, b := range alias {
			if b != 0 {
				t.Fatalf("byte %d survived through an alias: %q", i, alias)
			}
		}
	})

	t.Run("an allocation the decoder dropped is out of reach", func(t *testing.T) {
		var req barrierUnsealRequest
		cleanup := wipeCredential(&req.Secret)

		if err := json.Unmarshal([]byte(`{"secret":"aHVudGVyMg==","secret":null}`), &req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.Secret != nil {
			t.Fatalf("precondition: the last key is null, so the field holds nil, got %q", req.Secret)
		}

		cleanup() // wipes nil, and must not panic doing so

		// Nothing is asserted about the discarded "hunter2" allocation
		// BECAUSE NOTHING CAN BE: no reference to it exists here. That is the
		// documented limit, not an oversight — see wipeCredential's comment
		// and docs/barrier/api.md.
	})
}

// TestBarrierWire_RejectedRequestsNeverReachTheService walks the branches that
// return before the service call — a malformed body and each quorum-validation
// refusal — and asserts none of them calls the barrier. These are the paths
// whose decoded credential wipeCredential exists to clean up; that the request
// is refused at all is the property observable from outside the handler.
func TestBarrierWire_RejectedRequestsNeverReachTheService(t *testing.T) {
	cases := []struct {
		name, path, body string
	}{
		{"malformed body", "/v1/barrier/shamir/initialize", `{"secret":"aHVudGVyMg==","threshold":"not-a-number"}`},
		{"zero threshold", "/v1/barrier/shamir/initialize", `{"secret":"aHVudGVyMg==","threshold":0,"total_shares":3}`},
		{"zero total", "/v1/barrier/shamir/initialize", `{"secret":"aHVudGVyMg==","threshold":2,"total_shares":0}`},
		{"threshold above total", "/v1/barrier/shamir/initialize", `{"secret":"aHVudGVyMg==","threshold":4,"total_shares":3}`},
		{"malformed base64 secret", "/v1/barrier/unseal", `{"secret":"s3cr3t"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			svc := &fakeBarrierService{
				initShamirFn: func(context.Context, []byte, int, int) (*service.ShamirInitInfo, error) {
					called = true
					return &service.ShamirInitInfo{}, nil
				},
				unsealFn: func(context.Context, []byte) error { called = true; return nil },
			}
			w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, tc.path, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (body=%s)", w.Code, w.Body.String())
			}
			if called {
				t.Fatal("a refused request reached the barrier service")
			}
		})
	}
}

// TestBarrierWire_RawCredentialThatIsValidBase64DecodesToOtherBytes states the
// limit of the wire's protection, so nobody reads "the server rejects a raw
// passphrase" into a guarantee it cannot make.
//
// Decoding catches MALFORMED base64 and nothing else. A raw passphrase that
// happens to be valid base64 — any run of base64 alphabet whose length is a
// multiple of four, and "password" is one — decodes successfully into DIFFERENT
// bytes, and the barrier is then keyed on those bytes. That is silent, so a
// caller must always encode its credential rather than rely on being told off.
func TestBarrierWire_RawCredentialThatIsValidBase64DecodesToOtherBytes(t *testing.T) {
	const raw = "password"
	var got []byte
	svc := &fakeBarrierService{
		unsealFn: func(_ context.Context, secret []byte) error {
			got = append([]byte(nil), secret...)
			return nil
		},
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/v1/barrier/unseal", `{"secret":"`+raw+`"}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 — %q is well-formed base64 — got %d", raw, w.Code)
	}
	if bytes.Equal(got, []byte(raw)) {
		t.Fatal("the raw text reached the service verbatim; this wire carries decoded bytes")
	}
	want, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("precondition: %q must be valid base64: %v", raw, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("service saw %q, want the decoding %q", got, want)
	}
}
