//go:build !integration

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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	stderrors "errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmsacme "github.com/jeremyhahn/go-xkms/sdk/go/acme"
)

// fakeACMECA is a configurable ACMECA used to exercise the ACME service Issue
// verb without standing up a full certificate authority. It satisfies the
// narrow service.ACMECA capability only.
type fakeACMECA struct {
	certPEM  []byte
	chainPEM []byte
	err      error
	gotCSR   []byte
}

func (f *fakeACMECA) SignCSRPEM(_ context.Context, csrPEM []byte) ([]byte, []byte, error) {
	f.gotCSR = csrPEM
	if f.err != nil {
		return nil, nil, f.err
	}
	return f.certPEM, f.chainPEM, nil
}

// newTestACMEService builds an acmeService directly (same-package access) with
// the supplied issuance CA. It bypasses NewACMEService — which requires a full
// TCGCA + storage factory to construct the xkms RestService — so the
// service-layer control-verb logic can be unit-tested in isolation. The
// RFC 8555 round-trip against the real composed server is covered by the
// integration suite.
func newTestACMEService(issueCA ACMECA) *acmeService {
	return &acmeService{
		directoryURL: "https://acme.test.example/acme/directory",
		issueCA:      issueCA,
		challenge:    map[string]xkmsacme.ACMEChallengeVerifierFunc{},
		canonical:    testCanonicalVerifiers(),
	}
}

// testCanonicalVerifiers is the production verifier set, bound to a challenge
// client at go-xkms's secure default.
func testCanonicalVerifiers() map[string]xkmsacme.ACMEChallengeVerifierFunc {
	return canonicalChallengeVerifiers(xkmsacme.NewACMEChallengeClient(xkmsacme.ACMEChallengeClientConfig{}))
}

// selfSignedLeafPEM generates a throwaway self-signed certificate PEM whose
// serial the Issue verb echoes back. Used to assert serial extraction.
func selfSignedLeafPEM(t *testing.T, serial *big.Int) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "acme-test-leaf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestNewACMEService_NotConfigured(t *testing.T) {
	t.Run("nil params returns nil servicer", func(t *testing.T) {
		svc, err := NewACMEService(nil)
		assert.Nil(t, svc)
		assert.NoError(t, err)
	})

	t.Run("missing CA returns nil servicer", func(t *testing.T) {
		svc, err := NewACMEService(&ACMEServiceParams{
			BaseRESTURI:    "https://x",
			StorageFactory: nil,
		})
		assert.Nil(t, svc)
		assert.NoError(t, err)
	})

	t.Run("missing base URI returns nil servicer", func(t *testing.T) {
		svc, err := NewACMEService(&ACMEServiceParams{})
		assert.Nil(t, svc)
		assert.NoError(t, err)
	})
}

func TestACMEService_RegisterChallenge(t *testing.T) {
	svc := newTestACMEService(nil)
	ctx := context.Background()

	t.Run("http-01 registers", func(t *testing.T) {
		resp, err := svc.RegisterChallenge(ctx, &ACMERegisterChallengeRequest{Name: xkmsacme.ACMEChallengeTypeHTTP01})
		require.NoError(t, err)
		assert.True(t, resp.Registered)
		svc.mu.Lock()
		_, ok := svc.challenge[xkmsacme.ACMEChallengeTypeHTTP01]
		svc.mu.Unlock()
		assert.True(t, ok, "verifier installed into shared challenge map")
	})

	t.Run("dns-01 registers", func(t *testing.T) {
		resp, err := svc.RegisterChallenge(ctx, &ACMERegisterChallengeRequest{Name: xkmsacme.ACMEChallengeTypeDNS01})
		require.NoError(t, err)
		assert.True(t, resp.Registered)
	})

	t.Run("nil request rejected", func(t *testing.T) {
		_, err := svc.RegisterChallenge(ctx, nil)
		assert.ErrorIs(t, err, ErrACMENilRequest)
	})

	t.Run("empty name rejected", func(t *testing.T) {
		_, err := svc.RegisterChallenge(ctx, &ACMERegisterChallengeRequest{})
		assert.ErrorIs(t, err, ErrACMEEmptyChallengeName)
	})

	t.Run("unknown challenge rejected", func(t *testing.T) {
		_, err := svc.RegisterChallenge(ctx, &ACMERegisterChallengeRequest{Name: "tls-alpn-01"})
		assert.ErrorIs(t, err, ErrACMEUnknownChallenge)
	})
}

func TestACMEService_Issue(t *testing.T) {
	ctx := context.Background()
	serial := big.NewInt(0x4d2)
	leaf := selfSignedLeafPEM(t, serial)
	chain := []byte("-----BEGIN CERTIFICATE-----\nchain\n-----END CERTIFICATE-----")

	t.Run("issues and echoes serial", func(t *testing.T) {
		ca := &fakeACMECA{certPEM: leaf, chainPEM: chain}
		svc := newTestACMEService(ca)
		resp, err := svc.Issue(ctx, &ACMEIssueRequest{
			CSRPEM:         []byte("CSR"),
			ChallengeType:  xkmsacme.ACMEChallengeTypeHTTP01,
			ChallengeValue: "example.com",
		})
		require.NoError(t, err)
		assert.Equal(t, leaf, resp.CertificatePEM)
		assert.Equal(t, chain, resp.ChainPEM)
		assert.Equal(t, serial.Text(16), resp.SerialNumber)
		assert.Equal(t, []byte("CSR"), ca.gotCSR)
	})

	t.Run("nil request rejected", func(t *testing.T) {
		svc := newTestACMEService(&fakeACMECA{})
		_, err := svc.Issue(ctx, nil)
		assert.ErrorIs(t, err, ErrACMENilRequest)
	})

	t.Run("empty csr rejected", func(t *testing.T) {
		svc := newTestACMEService(&fakeACMECA{})
		_, err := svc.Issue(ctx, &ACMEIssueRequest{ChallengeType: "http-01"})
		assert.ErrorIs(t, err, ErrACMEEmptyCSR)
	})

	t.Run("empty challenge type rejected", func(t *testing.T) {
		svc := newTestACMEService(&fakeACMECA{})
		_, err := svc.Issue(ctx, &ACMEIssueRequest{CSRPEM: []byte("CSR")})
		assert.ErrorIs(t, err, ErrACMEEmptyChallengeType)
	})

	t.Run("missing CA returns typed error", func(t *testing.T) {
		svc := newTestACMEService(nil)
		_, err := svc.Issue(ctx, &ACMEIssueRequest{CSRPEM: []byte("CSR"), ChallengeType: "http-01"})
		assert.ErrorIs(t, err, ErrACMECAMissing)
	})

	t.Run("CA error propagates", func(t *testing.T) {
		boom := stderrors.New("sign failed")
		svc := newTestACMEService(&fakeACMECA{err: boom})
		_, err := svc.Issue(ctx, &ACMEIssueRequest{CSRPEM: []byte("CSR"), ChallengeType: "http-01"})
		require.Error(t, err)
		assert.ErrorIs(t, err, boom)
	})
}

func TestACMEService_DirectoryURL(t *testing.T) {
	svc := newTestACMEService(nil)
	assert.Equal(t, "https://acme.test.example/acme/directory", svc.DirectoryURL())
}

func TestSerialFromCertPEM(t *testing.T) {
	t.Run("parses serial", func(t *testing.T) {
		leaf := selfSignedLeafPEM(t, big.NewInt(255))
		assert.Equal(t, "ff", serialFromCertPEM(leaf))
	})
	t.Run("empty PEM yields empty serial", func(t *testing.T) {
		assert.Equal(t, "", serialFromCertPEM(nil))
	})
	t.Run("garbage PEM yields empty serial", func(t *testing.T) {
		assert.Equal(t, "", serialFromCertPEM([]byte("not a pem")))
	})
}

// fakeACMECertResolver is a configurable ACMECertResolver used to exercise the
// Renew verb without standing up a full certificate authority.
type fakeACMECertResolver struct {
	certPEM      []byte
	resolveErr   error
	revokeErr    error
	gotSerial    string
	gotReason    int
	revokeCalled bool
}

func (f *fakeACMECertResolver) CertificateBySerial(serialHex string) ([]byte, error) {
	f.gotSerial = serialHex
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	return f.certPEM, nil
}

func (f *fakeACMECertResolver) RevokeBySerial(serialHex string, reason int) error {
	f.revokeCalled = true
	f.gotSerial = serialHex
	f.gotReason = reason
	return f.revokeErr
}

// newTestACMEServiceFull builds an acmeService with a base URI plus issuance
// and resolver capabilities for the client-verb tests.
func newTestACMEServiceFull(issueCA ACMECA, resolver ACMECertResolver) *acmeService {
	return &acmeService{
		directoryURL: "https://acme.test.example/acme/directory",
		baseURI:      "https://acme.test.example",
		issueCA:      issueCA,
		certResolver: resolver,
		challenge:    map[string]xkmsacme.ACMEChallengeVerifierFunc{},
		canonical:    testCanonicalVerifiers(),
	}
}

func TestACMEService_NewOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("creates pending order with finalize url", func(t *testing.T) {
		svc := newTestACMEServiceFull(&fakeACMECA{}, nil)
		resp, err := svc.NewOrder(ctx, &ACMENewOrderRequest{
			Identifiers:   []ACMEOrderIdentifier{{Type: "dns", Value: "example.com"}},
			ChallengeType: xkmsacme.ACMEChallengeTypeHTTP01,
		})
		require.NoError(t, err)
		assert.NotZero(t, resp.OrderID)
		assert.Equal(t, string(xkmsacme.OrderStatusPending), resp.Status)
		require.Len(t, resp.Identifiers, 1)
		assert.Equal(t, "example.com", resp.Identifiers[0].Value)
		assert.Contains(t, resp.URL, "/acme/order/")
		assert.Contains(t, resp.Finalize, "/finalize")
	})

	t.Run("distinct order ids", func(t *testing.T) {
		svc := newTestACMEServiceFull(&fakeACMECA{}, nil)
		ids := map[uint64]struct{}{}
		for i := 0; i < 8; i++ {
			resp, err := svc.NewOrder(ctx, &ACMENewOrderRequest{
				Identifiers: []ACMEOrderIdentifier{{Type: "dns", Value: "example.com"}},
			})
			require.NoError(t, err)
			ids[resp.OrderID] = struct{}{}
		}
		assert.Len(t, ids, 8, "order IDs must be unique")
	})

	t.Run("nil request rejected", func(t *testing.T) {
		svc := newTestACMEServiceFull(&fakeACMECA{}, nil)
		_, err := svc.NewOrder(ctx, nil)
		assert.ErrorIs(t, err, ErrACMENilRequest)
	})

	t.Run("no identifiers rejected", func(t *testing.T) {
		svc := newTestACMEServiceFull(&fakeACMECA{}, nil)
		_, err := svc.NewOrder(ctx, &ACMENewOrderRequest{})
		assert.ErrorIs(t, err, ErrACMENoIdentifiers)
	})
}

func TestACMEService_FinalizeOrder(t *testing.T) {
	ctx := context.Background()
	serial := big.NewInt(0xabc)
	leaf := selfSignedLeafPEM(t, serial)
	chain := []byte("-----BEGIN CERTIFICATE-----\nchain\n-----END CERTIFICATE-----")

	t.Run("signs csr and echoes serial", func(t *testing.T) {
		ca := &fakeACMECA{certPEM: leaf, chainPEM: chain}
		svc := newTestACMEServiceFull(ca, nil)
		resp, err := svc.FinalizeOrder(ctx, &ACMEFinalizeOrderRequest{
			OrderURL: "https://acme.test.example/acme/order/1",
			CSRPEM:   []byte("CSR"),
		})
		require.NoError(t, err)
		assert.Equal(t, leaf, resp.CertificatePEM)
		assert.Equal(t, chain, resp.ChainPEM)
		assert.Equal(t, serial.Text(16), resp.SerialNumber)
		assert.Equal(t, []byte("CSR"), ca.gotCSR)
	})

	t.Run("nil request rejected", func(t *testing.T) {
		svc := newTestACMEServiceFull(&fakeACMECA{}, nil)
		_, err := svc.FinalizeOrder(ctx, nil)
		assert.ErrorIs(t, err, ErrACMENilRequest)
	})

	t.Run("empty csr rejected", func(t *testing.T) {
		svc := newTestACMEServiceFull(&fakeACMECA{}, nil)
		_, err := svc.FinalizeOrder(ctx, &ACMEFinalizeOrderRequest{})
		assert.ErrorIs(t, err, ErrACMEEmptyCSR)
	})

	t.Run("missing CA returns typed error", func(t *testing.T) {
		svc := newTestACMEServiceFull(nil, nil)
		_, err := svc.FinalizeOrder(ctx, &ACMEFinalizeOrderRequest{CSRPEM: []byte("CSR")})
		assert.ErrorIs(t, err, ErrACMECAMissing)
	})

	t.Run("CA error propagates", func(t *testing.T) {
		boom := stderrors.New("sign failed")
		svc := newTestACMEServiceFull(&fakeACMECA{err: boom}, nil)
		_, err := svc.FinalizeOrder(ctx, &ACMEFinalizeOrderRequest{CSRPEM: []byte("CSR")})
		require.Error(t, err)
		assert.ErrorIs(t, err, boom)
	})
}

// TestACMEService_NewOrderToFinalize exercises the full broker-as-CA order ->
// finalize flow in-process, producing a real signed leaf from a real CSR.
func TestACMEService_NewOrderToFinalize(t *testing.T) {
	ctx := context.Background()
	serial := big.NewInt(0x1234)
	leaf := selfSignedLeafPEM(t, serial)
	ca := &fakeACMECA{certPEM: leaf, chainPEM: []byte("chain")}
	svc := newTestACMEServiceFull(ca, nil)

	order, err := svc.NewOrder(ctx, &ACMENewOrderRequest{
		Identifiers:   []ACMEOrderIdentifier{{Type: "dns", Value: "host.example.com"}},
		ChallengeType: xkmsacme.ACMEChallengeTypeHTTP01,
	})
	require.NoError(t, err)
	require.NotEmpty(t, order.Finalize)

	// A real PEM-encoded CSR for the ordered identifier.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: "host.example.com"},
		DNSNames: []string{"host.example.com"},
	}, key)
	require.NoError(t, err)
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	fin, err := svc.FinalizeOrder(ctx, &ACMEFinalizeOrderRequest{
		OrderURL: order.Finalize,
		CSRPEM:   csrPEM,
	})
	require.NoError(t, err)
	assert.Equal(t, leaf, fin.CertificatePEM)
	assert.Equal(t, serial.Text(16), fin.SerialNumber)
	assert.Equal(t, csrPEM, ca.gotCSR)
}

func TestACMEService_Renew(t *testing.T) {
	ctx := context.Background()
	leaf := selfSignedLeafPEM(t, big.NewInt(0xff))

	t.Run("resolves cert then reports needs-csr", func(t *testing.T) {
		res := &fakeACMECertResolver{certPEM: leaf}
		svc := newTestACMEServiceFull(nil, res)
		resp, err := svc.Renew(ctx, &ACMERenewRequest{CertURL: "ff"})
		assert.ErrorIs(t, err, ErrACMERenewNeedsCSR)
		require.NotNil(t, resp)
		assert.Equal(t, leaf, resp.CertificatePEM)
		assert.False(t, resp.Renewable)
		assert.False(t, resp.Revoked)
		assert.Equal(t, "ff", res.gotSerial)
	})

	t.Run("extracts serial from cert path url", func(t *testing.T) {
		res := &fakeACMECertResolver{certPEM: leaf}
		svc := newTestACMEServiceFull(nil, res)
		_, err := svc.Renew(ctx, &ACMERenewRequest{CertURL: "https://acme.test.example/acme/cert/FF"})
		assert.ErrorIs(t, err, ErrACMERenewNeedsCSR)
		assert.Equal(t, "ff", res.gotSerial)
	})

	t.Run("revoke requested", func(t *testing.T) {
		res := &fakeACMECertResolver{certPEM: leaf}
		svc := newTestACMEServiceFull(nil, res)
		resp, err := svc.Renew(ctx, &ACMERenewRequest{CertURL: "ff", Revoke: true})
		assert.ErrorIs(t, err, ErrACMERenewNeedsCSR)
		require.NotNil(t, resp)
		assert.True(t, resp.Revoked)
		assert.True(t, res.revokeCalled)
	})

	t.Run("cert not found", func(t *testing.T) {
		res := &fakeACMECertResolver{resolveErr: ErrACMECertNotFound}
		svc := newTestACMEServiceFull(nil, res)
		_, err := svc.Renew(ctx, &ACMERenewRequest{CertURL: "ff"})
		assert.ErrorIs(t, err, ErrACMECertNotFound)
	})

	t.Run("revoke error propagates", func(t *testing.T) {
		boom := stderrors.New("revoke failed")
		res := &fakeACMECertResolver{certPEM: leaf, revokeErr: boom}
		svc := newTestACMEServiceFull(nil, res)
		_, err := svc.Renew(ctx, &ACMERenewRequest{CertURL: "ff", Revoke: true})
		require.Error(t, err)
		assert.ErrorIs(t, err, boom)
	})

	t.Run("nil request rejected", func(t *testing.T) {
		svc := newTestACMEServiceFull(nil, &fakeACMECertResolver{})
		_, err := svc.Renew(ctx, nil)
		assert.ErrorIs(t, err, ErrACMENilRequest)
	})

	t.Run("empty url rejected", func(t *testing.T) {
		svc := newTestACMEServiceFull(nil, &fakeACMECertResolver{})
		_, err := svc.Renew(ctx, &ACMERenewRequest{})
		assert.ErrorIs(t, err, ErrACMEEmptyCertURL)
	})

	t.Run("missing resolver returns typed error", func(t *testing.T) {
		svc := newTestACMEServiceFull(nil, nil)
		_, err := svc.Renew(ctx, &ACMERenewRequest{CertURL: "ff"})
		assert.ErrorIs(t, err, ErrACMEResolverMissing)
	})
}

// TestCanonicalChallengeVerifiers_PrivateTargets pins the SSRF guard the
// http-01 verifier now runs behind: a challenge-validation client at go-xkms's
// default refuses a loopback target, and one built with
// AllowPrivateChallengeTargets (server.acme.allow_private_challenge_targets)
// reaches it and validates the key authorization.
func TestCanonicalChallengeVerifiers_PrivateTargets(t *testing.T) {
	const token, keyAuth = "challenge-token", "challenge-token.thumbprint"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/acme-challenge/"+token {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(keyAuth))
	}))
	defer srv.Close()
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	host, port, err := net.SplitHostPort(u.Host)
	require.NoError(t, err)

	guarded := canonicalChallengeVerifiers(xkmsacme.NewACMEChallengeClient(xkmsacme.ACMEChallengeClientConfig{}))
	err = guarded[xkmsacme.ACMEChallengeTypeHTTP01](nil, nil, host, port, token, keyAuth)
	require.Error(t, err, "the default client must refuse a loopback challenge target")

	private := canonicalChallengeVerifiers(xkmsacme.NewACMEChallengeClient(xkmsacme.ACMEChallengeClientConfig{AllowPrivateTargets: true}))
	require.NoError(t, private[xkmsacme.ACMEChallengeTypeHTTP01](nil, nil, host, port, token, keyAuth),
		"a client allowing private targets must validate a loopback challenge")
}
