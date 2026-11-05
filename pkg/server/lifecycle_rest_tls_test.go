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

//go:build rest

package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// newTLSTestCert returns a self-signed certificate valid for 127.0.0.1, its
// PEM encoding, and the parsed leaf.
func newTLSTestCert(t *testing.T, name string) (tls.Certificate, []byte, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf},
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), leaf
}

// TestCloneTLSConfig_ConcurrentTLSRequestsWhileAnotherListenerIsBuilt serves
// HTTPS from one listener's clone while further listeners are built from the
// same shared config and append client CAs to their clones. Every request
// must succeed, and under -race no listener may write
// the pool another one is handshaking with.
func TestCloneTLSConfig_ConcurrentTLSRequestsWhileAnotherListenerIsBuilt(t *testing.T) {
	serverCert, _, serverLeaf := newTLSTestCert(t, "server")
	_, baseCAPEM, _ := newTLSTestCert(t, "base-ca")

	shared := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    x509.NewCertPool(),
		ClientAuth:   tls.VerifyClientCertIfGiven,
		MinVersion:   tls.VersionTLS12,
	}
	if !shared.ClientCAs.AppendCertsFromPEM(baseCAPEM) {
		t.Fatal("append base CA")
	}
	sharedBefore := shared.ClientCAs.Clone()

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	srv.TLS = cloneTLSConfig(shared)
	srv.StartTLS()
	t.Cleanup(srv.Close)

	roots := x509.NewCertPool()
	roots.AddCert(serverLeaf)
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, DisableKeepAlives: true},
	}

	const requests, builds = 32, 8
	var wg sync.WaitGroup
	errs := make(chan error, requests+builds)
	for i := 0; i < builds; i++ {
		_, caPEM, _ := newTLSTestCert(t, "listener-ca")
		wg.Add(1)
		go func() {
			defer wg.Done()
			listenerCfg := cloneTLSConfig(shared)
			if !listenerCfg.ClientCAs.AppendCertsFromPEM(caPEM) {
				t.Error("append listener CA to the cloned pool")
			}
		}()
	}
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Get(srv.URL)
			if err != nil {
				errs <- err
				return
			}
			if err := resp.Body.Close(); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent TLS request or listener build failed: %v", err)
	}

	if !shared.ClientCAs.Equal(sharedBefore) {
		t.Error("building a listener changed the shared ClientCAs pool")
	}
	if got := srv.TLS.NextProtos; len(got) != 1 || got[0] != "http/1.1" {
		t.Errorf("listener NextProtos = %v, want [http/1.1]", got)
	}
}

func TestCloneTLSConfig_NilAndNoClientCAs(t *testing.T) {
	if got := cloneTLSConfig(nil); got != nil {
		t.Errorf("cloneTLSConfig(nil) = %v, want nil", got)
	}
	got := cloneTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})
	if got.ClientCAs != nil {
		t.Errorf("ClientCAs = %v, want nil when the source has none", got.ClientCAs)
	}
}
