package client_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	acmeclient "github.com/jeremyhahn/go-xkms/pkg/acme/client"
	"github.com/jeremyhahn/go-xkms/pkg/acme/entities"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/acme/client"
)

func TestARINames_AreTheGoXKMSNames(t *testing.T) {
	for _, tc := range []struct {
		name      string
		local, up any
	}{
		{"NewClient", client.NewClient, acmeclient.NewClient},
		{"DefaultConfig", client.DefaultConfig, acmeclient.DefaultConfig},
		{"CrossSignerFromClient", client.CrossSignerFromClient, acmeclient.CrossSignerFromClient},
		{"GetCertificateID", client.GetCertificateID, acmeclient.GetCertificateID},
		{"GetCertificateIDFromHash", client.GetCertificateIDFromHash, acmeclient.GetCertificateIDFromHash},
		{"ParseCertificateID", client.ParseCertificateID, acmeclient.ParseCertificateID},
		{"ValidateCertificateID", client.ValidateCertificateID, acmeclient.ValidateCertificateID},
		{"BuildRenewalInfoURL", client.BuildRenewalInfoURL, acmeclient.BuildRenewalInfoURL},
		{"ComputeRenewalTime", client.ComputeRenewalTime, acmeclient.ComputeRenewalTime},
		{"NeedsRenewal", client.NeedsRenewal, acmeclient.NeedsRenewal},
		{"IsAutoRenewalCanceled", client.IsAutoRenewalCanceled, acmeclient.IsAutoRenewalCanceled},
		{"IsAutoRenewalCancellationInvalid", client.IsAutoRenewalCancellationInvalid, acmeclient.IsAutoRenewalCancellationInvalid},
		{"IsAutoRenewalExpired", client.IsAutoRenewalExpired, acmeclient.IsAutoRenewalExpired},
		{"IsAutoRenewalRevocationNotSupported", client.IsAutoRenewalRevocationNotSupported, acmeclient.IsAutoRenewalRevocationNotSupported},
	} {
		if reflect.ValueOf(tc.local).Pointer() != reflect.ValueOf(tc.up).Pointer() {
			t.Errorf("%s is not go-xkms's %s", tc.name, tc.name)
		}
	}
	for _, tc := range []struct {
		name      string
		local, up reflect.Type
	}{
		{"Client", reflect.TypeOf(client.Client{}), reflect.TypeOf(acmeclient.Client{})},
		{"Params", reflect.TypeOf(client.Params{}), reflect.TypeOf(acmeclient.Params{})},
		{"Config", reflect.TypeOf(client.Config{}), reflect.TypeOf(acmeclient.Config{})},
		{"RenewalInfo", reflect.TypeOf(client.RenewalInfo{}), reflect.TypeOf(acmeclient.RenewalInfo{})},
		{"CrossSignConfig", reflect.TypeOf(client.CrossSignConfig{}), reflect.TypeOf(acmeclient.CrossSignConfig{})},
	} {
		if tc.local != tc.up {
			t.Errorf("%s is %v, want the go-xkms type %v", tc.name, tc.local, tc.up)
		}
	}
	if !errors.Is(client.ErrARINotSupported, acmeclient.ErrARINotSupported) ||
		!errors.Is(client.ErrRenewalInfoFetchFailed, acmeclient.ErrRenewalInfoFetchFailed) {
		t.Error("the ARI sentinels are not go-xkms's")
	}
}

// ariCertificate is a certificate carrying the AKI and serial an ARI
// certificate ID is computed from.
func ariCertificate(t *testing.T) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:   big.NewInt(0x1662),
		Subject:        pkix.Name{CommonName: "device.example"},
		NotBefore:      time.Now().Add(-time.Hour),
		NotAfter:       time.Now().Add(90 * 24 * time.Hour),
		AuthorityKeyId: []byte{0x16, 0x62, 0xac, 0xe0},
		SubjectKeyId:   []byte{0x16, 0x62, 0xac, 0xe0},
		DNSNames:       []string{"device.example"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

// ariDirectory serves an RFC 8555 directory, advertising renewalInfo when
// ari is set, and records the certificate IDs renewalInfo was asked for.
func ariDirectory(t *testing.T, ari bool, start, end time.Time) (*httptest.Server, func() []string) {
	t.Helper()
	var (
		mu    sync.Mutex
		asked []string
	)
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/directory", func(w http.ResponseWriter, _ *http.Request) {
		dir := map[string]string{
			"newNonce":   srv.URL + "/new-nonce",
			"newAccount": srv.URL + "/new-account",
			"newOrder":   srv.URL + "/new-order",
			"revokeCert": srv.URL + "/revoke-cert",
			"keyChange":  srv.URL + "/key-change",
		}
		if ari {
			dir["renewalInfo"] = srv.URL + "/renewal-info"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(dir)
	})
	mux.HandleFunc("/new-nonce", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Replay-Nonce", "nonce-1662")
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/renewal-info/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, strings.TrimPrefix(r.URL.Path, "/renewal-info/"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "21600")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"suggestedWindow": map[string]string{
				"start": start.UTC().Format(time.RFC3339),
				"end":   end.UTC().Format(time.RFC3339),
			},
		})
	})
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), asked...)
	}
}

// untouchedDAO satisfies the client's storage parameters; ARI reads no
// account, order or certificate, so any call into it panics the test.
type untouchedDAO[E qrdbsdk.Entity] struct{ qrdbsdk.GenericDAO[E] }

func ariClient(t *testing.T, directoryURL string) *client.Client {
	t.Helper()
	signer, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cfg := client.DefaultConfig()
	cfg.DirectoryURL = directoryURL
	cfg.Timeout = 5 * time.Second
	cfg.RetryAttempts = 0
	c, err := client.NewClient(&client.Params{
		AccountEmail:       "ari@device.example",
		AccountSigner:      signer,
		Config:             cfg,
		Storage:            untouchedDAO[*entities.ACMEAccount]{},
		CertificateStorage: untouchedDAO[*entities.ACMECertificate]{},
		OrderStorage:       untouchedDAO[*entities.ACMEOrder]{},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// A go-trusted-platform device asks the CA when to renew through sdk/go:
// the ARI certificate ID it computes is the one the server is asked for, and
// the window the server suggests is the one the device reads back.
func TestARI_RenewalInfoThroughTheReexport(t *testing.T) {
	cert := ariCertificate(t)
	certID := client.GetCertificateID(cert)
	if err := client.ValidateCertificateID(certID); err != nil {
		t.Fatalf("ValidateCertificateID(%q): %v", certID, err)
	}
	aki, serial, err := client.ParseCertificateID(certID)
	if err != nil {
		t.Fatalf("ParseCertificateID: %v", err)
	}
	if string(aki) != string(cert.AuthorityKeyId) || new(big.Int).SetBytes(serial).Cmp(cert.SerialNumber) != 0 {
		t.Errorf("ParseCertificateID = %x, %x, want the certificate's AKI %x and serial %v",
			aki, serial, cert.AuthorityKeyId, cert.SerialNumber)
	}

	start := time.Now().Add(-time.Hour).Truncate(time.Second)
	end := time.Now().Add(time.Hour).Truncate(time.Second)
	srv, asked := ariDirectory(t, true, start, end)
	c := ariClient(t, srv.URL+"/directory")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ok, err := c.SupportsARI(ctx)
	if err != nil || !ok {
		t.Fatalf("SupportsARI = %v, %v, want true", ok, err)
	}
	info, err := c.GetRenewalInfo(ctx, certID)
	if err != nil {
		t.Fatalf("GetRenewalInfo: %v", err)
	}
	if !info.SuggestedWindow.Start.Equal(start) || !info.SuggestedWindow.End.Equal(end) {
		t.Errorf("window = %v..%v, want %v..%v",
			info.SuggestedWindow.Start, info.SuggestedWindow.End, start, end)
	}
	if !info.ShouldRenewNow() {
		t.Error("ShouldRenewNow = false inside the suggested window")
	}
	if got := asked(); len(got) != 1 || got[0] != certID {
		t.Errorf("renewalInfo asked for %q, want [%q]", got, certID)
	}
}

func TestARI_ServerWithoutRenewalInfoIsRefused(t *testing.T) {
	srv, asked := ariDirectory(t, false, time.Now(), time.Now())
	c := ariClient(t, srv.URL+"/directory")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if ok, err := c.SupportsARI(ctx); err != nil || ok {
		t.Fatalf("SupportsARI = %v, %v, want false", ok, err)
	}
	_, err := c.GetRenewalInfo(ctx, client.GetCertificateID(ariCertificate(t)))
	if !errors.Is(err, client.ErrARINotSupported) {
		t.Errorf("GetRenewalInfo error = %v, want ErrARINotSupported", err)
	}
	if got := asked(); len(got) != 0 {
		t.Errorf("renewalInfo was asked for %q on a server that does not advertise it", got)
	}
}
