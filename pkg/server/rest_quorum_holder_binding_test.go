//go:build rest && !integration

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

package server

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// quorumHolder is one share holder's client side over REST: its certificate
// key, the CA-issued certificate and the share it claimed.
type quorumHolder struct {
	name  string
	key   *ecdsa.PrivateKey
	csr   string
	cert  *x509.Certificate
	share []byte
}

func newQuorumHolder(t *testing.T, name string) *quorumHolder {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: name},
	}, key)
	require.NoError(t, err)
	return &quorumHolder{
		name: name,
		key:  key,
		csr:  string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})),
	}
}

// tlsState is the connection state of a holder whose certificate the REST
// listener verified.
func (h *quorumHolder) tlsState() *tls.ConnectionState {
	return &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{h.cert},
		VerifiedChains:   [][]*x509.Certificate{{h.cert}},
	}
}

// restHolderCall posts body to handler as the holder whose TLS state is
// state (anonymous when nil) and decodes the response into out.
func restHolderCall(t *testing.T, handler http.HandlerFunc, username string, state *tls.ConnectionState, body, out any) int {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("username", username)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.TLS = state
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code == http.StatusOK && out != nil {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), out))
	}
	return rec.Code
}

// TestRESTQuorumBegin_AnonymousBeginCannotEvictVerifiedHolderChallenge drives
// the REST holder handlers against the real qrdb holder engine: a holder's
// verified begin is bound to its certificate, so the anonymous begins an
// attacker issues for the same holder cannot evict it before it is answered.
func TestRESTQuorumBegin_AnonymousBeginCannotEvictVerifiedHolderChallenge(t *testing.T) {
	cfg := config.Default()
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = string(qrdbsdk.StrategyShamir)
	cfg.Seal.StrategyExplicit = true
	cfg.Seal.Threshold = 2
	cfg.Seal.TotalShares = 2
	cfg.Storage.DataDir = t.TempDir()
	cfg.IAM.SPIFFETrustDomain = testTrustDomain

	svc, _, ceremonyCore, closer, err := buildDataPlaneBarrierService(cfg, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })
	client := dataPlaneCeremonyClient(t, svc, ceremonyCore)

	holders := []*quorumHolder{newQuorumHolder(t, "so-alice"), newQuorumHolder(t, "so-bob")}
	officers := make([]qrdbtransport.CeremonyOfficerRequest, 0, len(holders))
	for _, h := range holders {
		officers = append(officers, qrdbtransport.CeremonyOfficerRequest{
			Username:   h.name,
			Role:       qrdbtransport.RoleSecurityOfficer,
			CSRPEM:     h.csr,
			Designated: "mtls",
		})
	}
	adminCert := string(newBYOAdminCertPEM(t))
	initResp, err := client.Ceremony().CeremonyInitialize(t.Context(), &qrdbtransport.CeremonyInitializeRequest{
		SOPin:            "dataplane-so-pin",
		UserPin:          "dataplane-user-pin",
		Hostname:         "node.example.test",
		AdminCertPEM:     adminCert,
		AdminCertPoolPEM: adminCert,
		Threshold:        2,
		Policy:           qrdbtransport.PolicyDesignated,
		Officers:         officers,
	})
	require.NoError(t, err)
	require.Len(t, initResp.Holders, len(holders))
	for i, h := range holders {
		block, _ := pem.Decode([]byte(initResp.Holders[i].CertPEM))
		require.NotNil(t, block, "%s: issued certificate is not PEM", h.name)
		h.cert, err = x509.ParseCertificate(block.Bytes)
		require.NoError(t, err)
	}

	restSrv, err := rest.NewServer(testutil.NewMockQuicBaaS(), rest.DefaultConfig(), buildTestLogger(t))
	require.NoError(t, err)
	restSrv.SetBarrierService(service.NewBarrierService(client.Barrier()))

	alice := holders[0]
	var begin qrdbtransport.ShareClaimBeginResponse
	require.Equal(t, http.StatusOK, restHolderCall(t, restSrv.HandleBarrierShareClaimBegin, alice.name, alice.tlsState(),
		&qrdbtransport.ShareClaimBeginRequest{}, &begin))
	nonce, err := hex.DecodeString(begin.NonceHex)
	require.NoError(t, err)
	claimSig, err := qrdbsdk.SignShareClaim(alice.key, qrdbsdk.SystemClaimLabel, alice.name, nonce)
	require.NoError(t, err)
	var claimed qrdbtransport.ShareClaimCompleteResponse
	require.Equal(t, http.StatusOK, restHolderCall(t, restSrv.HandleBarrierShareClaimComplete, alice.name, alice.tlsState(),
		&qrdbtransport.ShareClaimCompleteRequest{NonceHex: begin.NonceHex, Signature: claimSig}, &claimed))
	envelope, err := qrdbsdk.ParseShareEnvelope(claimed.Envelope)
	require.NoError(t, err)
	alice.share, err = envelope.OpenWithKey(alice.key)
	require.NoError(t, err)

	_, err = client.Barrier().BarrierRekeyStart(t.Context(), &qrdbtransport.RekeyStartRequest{
		Domain:    qrdbtransport.DomainUnseal,
		Threshold: 2,
		SOPin:     "dataplane-so-pin",
	})
	require.NoError(t, err)

	beginReq := &qrdbtransport.QuorumBeginRequest{Domain: qrdbtransport.DomainUnseal, Op: qrdbtransport.OpRekey, Username: alice.name}
	var challenge qrdbtransport.QuorumChallenge
	require.Equal(t, http.StatusOK, restHolderCall(t, restSrv.HandleBarrierQuorumBegin, "", alice.tlsState(), beginReq, &challenge))

	// An anonymous caller fills every pending slot for the holder's op.
	for range 3 {
		restHolderCall(t, restSrv.HandleBarrierQuorumBegin, "", nil, beginReq, nil)
	}

	response, err := qrdbsdk.SignQuorumChallenge(alice.key, challenge.ChallengeID,
		challenge.MethodData[qrdbsdk.QuorumChallengePurposeKey], challenge.Nonce)
	require.NoError(t, err)
	var outcome qrdbtransport.QuorumOutcome
	code := restHolderCall(t, restSrv.HandleBarrierQuorumSubmit, "", alice.tlsState(), &qrdbtransport.QuorumSubmitRequest{
		Domain:      qrdbtransport.DomainUnseal,
		Op:          qrdbtransport.OpRekey,
		Username:    alice.name,
		ChallengeID: challenge.ChallengeID,
		Factor:      challenge.Factor,
		Response:    response,
		Share:       alice.share,
	}, &outcome)
	require.Equal(t, http.StatusOK, code, "the verified holder's challenge must survive anonymous begins")
	require.Equal(t, 1, outcome.Submitted)
}
