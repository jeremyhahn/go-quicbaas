//go:build codec_json

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

package qrdbadapter

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strconv"
	"sync"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

// pivArbitration is a PIV activator wired to the shared qrdb node the way the
// server wires it: users, invites, activation claims and login factors all
// live in the node, so every race below is decided by the store.
type pivArbitration struct {
	act     *iam.PIVActivator
	users   *QRDBUserStorage
	invites iam.InviteStore
	factors qrdbsdk.UserAuthFactorServicer
	ca      *x509.Certificate
	caKey   *ecdsa.PrivateKey
	serial  int64
}

func newPIVArbitration(t *testing.T) *pivArbitration {
	t.Helper()
	n := qrdbnode.Shared(t)
	resolver := n.Node.Services().StoreResolver
	tenantInvites, err := qrdbsdk.NewTenantInviteStore(resolver)
	if err != nil {
		t.Fatalf("NewTenantInviteStore: %v", err)
	}
	claims, err := iam.NewUserActivationClaims(resolver)
	if err != nil {
		t.Fatalf("NewUserActivationClaims: %v", err)
	}
	factors := n.Node.UserAuthFactorService()
	enroller, err := iam.NewCertLoginFactorEnroller(factors)
	if err != nil {
		t.Fatalf("NewCertLoginFactorEnroller: %v", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "PIV Arbitration Root"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA: %v", err)
	}
	ca, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)

	users := newNodeUserStorage(t)
	invites := iam.NewQRDBInviteStore(tenantInvites)
	act, err := iam.NewPIVActivator(iam.PIVActivatorDependencies{
		Roots:            roots,
		UserStorage:      users,
		InviteStore:      invites,
		LoginFactors:     enroller,
		ActivationClaims: claims,
	})
	if err != nil {
		t.Fatalf("NewPIVActivator: %v", err)
	}
	return &pivArbitration{act: act, users: users, invites: invites, factors: factors, ca: ca, caKey: key, serial: 1}
}

// leaf issues a client certificate chaining to the test CA.
func (p *pivArbitration) leaf(t *testing.T) (*x509.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	p.serial++
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(p.serial),
		Subject:      pkix.Name{CommonName: "PIV Arbitration Client " + strconv.FormatInt(p.serial, 10)},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, p.ca, &key.PublicKey, p.caKey)
	if err != nil {
		t.Fatalf("create leaf: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	return cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// pendingUser creates a pending user and the given number of live invites for
// it, as a provisioning retried that many times leaves behind.
func (p *pivArbitration) pendingUser(t *testing.T, invites int) (*iam.User, []string) {
	t.Helper()
	ctx := tenantContext(testTenant)
	u := newNodeUser(uniqueSubject())
	u.Lifecycle = iam.LifecyclePending
	if err := p.users.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	tokens := make([]string, invites)
	for i := range tokens {
		token, _, err := p.invites.Create(ctx, testTenant, u.ID, time.Hour)
		if err != nil {
			t.Fatalf("create invite %d: %v", i, err)
		}
		tokens[i] = token
	}
	return u, tokens
}

// requireOneLoginPath fails unless the user holds exactly one factor, active,
// and is bound to want's certificate alone.
func (p *pivArbitration) requireOneLoginPath(t *testing.T, userID string, want, other *x509.Certificate) {
	t.Helper()
	ctx := tenantContext(testTenant)
	numericID, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		t.Fatalf("user id %q: %v", userID, err)
	}
	factors, err := p.factors.List(ctx, numericID)
	if err != nil {
		t.Fatalf("List factors: %v", err)
	}
	if len(factors) != 1 || factors[0].RevokedAt != nil {
		t.Fatalf("factors = %+v, want exactly one active factor", factors)
	}
	active, err := p.users.GetUser(ctx, testTenant, userID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if active.Lifecycle != iam.LifecycleActive {
		t.Fatalf("lifecycle = %q, want active", active.Lifecycle)
	}
	if !active.HasPIVThumbprint(qrdbsdk.ComputeSPKIThumbprint(want)) || active.HasPIVThumbprint(qrdbsdk.ComputeSPKIThumbprint(other)) {
		t.Fatalf("user bound to %v, want only the winning certificate", active.PIVThumbprints())
	}
}

// TestPIVActivate_TwoTokensForOneUserEnrollOneFactor races two activations of
// one pending user presenting different invite tokens. Each token is
// single-use only on its own, so without the user claim both would win their
// redemption and enroll a factor. The node must pick exactly one winner.
func TestPIVActivate_TwoTokensForOneUserEnrollOneFactor(t *testing.T) {
	p := newPIVArbitration(t)

	const rounds = 5
	for round := range rounds {
		u, tokens := p.pendingUser(t, 2)
		certs := make([]*x509.Certificate, 2)
		pems := make([][]byte, 2)
		for i := range certs {
			certs[i], pems[i] = p.leaf(t)
		}

		start := make(chan struct{})
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i := range tokens {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				errs[i] = p.act.Activate(tenantContext(testTenant), testTenant, tokens[i], pems[i])
			}(i)
		}
		close(start)
		wg.Wait()

		winner, loser := 0, 1
		if errs[0] != nil {
			winner, loser = 1, 0
		}
		if errs[winner] != nil {
			t.Fatalf("round %d: no activation won: %v, %v", round, errs[0], errs[1])
		}
		if !iam.IsUserNotPending(errs[loser]) {
			t.Fatalf("round %d: losing activation = %T: %v, want the user-not-pending conflict", round, errs[loser], errs[loser])
		}
		p.requireOneLoginPath(t, u.ID, certs[winner], certs[loser])
	}
}

// TestPIVActivate_RetriedProvisioningLeavesOneLoginPath covers the sequential
// form: a provisioning retry minted a second invite, the user activated with
// one of them, and the other must not add a second login factor afterwards.
func TestPIVActivate_RetriedProvisioningLeavesOneLoginPath(t *testing.T) {
	p := newPIVArbitration(t)
	ctx := tenantContext(testTenant)
	u, tokens := p.pendingUser(t, 2)
	first, firstPEM := p.leaf(t)
	second, secondPEM := p.leaf(t)

	if err := p.act.Activate(ctx, testTenant, tokens[1], firstPEM); err != nil {
		t.Fatalf("Activate with the retry's invite: %v", err)
	}
	if err := p.act.Activate(ctx, testTenant, tokens[0], secondPEM); !iam.IsUserNotPending(err) {
		t.Fatalf("Activate with the earlier invite = %v, want the user-not-pending conflict", err)
	}
	p.requireOneLoginPath(t, u.ID, first, second)
}
