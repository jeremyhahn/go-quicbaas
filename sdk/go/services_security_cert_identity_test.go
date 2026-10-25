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

package quicbaas

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// certBindingDirectory is a UserServicer double whose thumbprint index is a
// plain map, the shape of qrdb's server-side secondary index.
type certBindingDirectory struct {
	*directoryUserServicer
	byThumbprint map[string]*qrdbsdk.User
}

func (d *certBindingDirectory) UserByCertThumbprint(_ context.Context, thumbprint string) (*qrdbsdk.User, error) {
	u, ok := d.byThumbprint[thumbprint]
	if !ok {
		return nil, &qrdbsdk.UserNotFoundError{Lookup: "cert_spki/" + thumbprint}
	}
	return u, nil
}

func newTestCert(t *testing.T) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "device"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	return cert
}

func TestAuthUserByCertificate_ResolvesBoundUserAndNotOthers(t *testing.T) {
	admin, device := newTestCert(t), newTestCert(t)
	auth := newCertIdentityEmbeddedAuth(t, map[string]*qrdbsdk.User{
		ComputeSPKIThumbprint(admin):  {ID: 1, Username: "admin"},
		ComputeSPKIThumbprint(device): {ID: 2, Username: "device"},
	})

	for cert, want := range map[*x509.Certificate]string{admin: "admin", device: "device"} {
		got, err := auth.UserByCertificate(context.Background(), cert)
		if err != nil {
			t.Fatalf("UserByCertificate: %v", err)
		}
		if got.Username != want {
			t.Errorf("resolved %q, want %q", got.Username, want)
		}
	}
}

func TestAuthUserByCertificate_InvalidCertificate(t *testing.T) {
	auth := newCertIdentityEmbeddedAuth(t, map[string]*qrdbsdk.User{})

	for name, cert := range map[string]*x509.Certificate{"nil": nil, "no public key": {}} {
		_, err := auth.UserByCertificate(context.Background(), cert)
		if !sdkerrors.IsInvalidInput(err) {
			t.Errorf("%s: want InvalidInputError, got %T: %v", name, err, err)
		}
	}
}

func TestAuthUserByCertThumbprint_InvalidInput(t *testing.T) {
	auth := newCertIdentityEmbeddedAuth(t, map[string]*qrdbsdk.User{})

	_, err := auth.UserByCertThumbprint(context.Background(), "")
	if !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("want InvalidInputError, got %T: %v", err, err)
	}
}

func TestAuthUserByCertThumbprint_Found(t *testing.T) {
	cert := newTestCert(t)
	tp := ComputeSPKIThumbprint(cert)
	auth := newCertIdentityEmbeddedAuth(t, map[string]*qrdbsdk.User{tp: {ID: 7, Username: "ops"}})

	got, err := auth.UserByCertThumbprint(context.Background(), tp)
	if err != nil || got.ID != 7 {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

// codedNotFoundDirectory reports an unbound thumbprint as qrdb's coded
// not_found error rather than UserNotFoundError.
type codedNotFoundDirectory struct {
	*directoryUserServicer
}

func (codedNotFoundDirectory) UserByCertThumbprint(context.Context, string) (*qrdbsdk.User, error) {
	return nil, qrdbsdk.NewError(qrdbsdk.ErrNotFound, "UserByCertThumbprint", "no user bound")
}

func TestAuthUserByCertThumbprint_CodedNotFoundIsTypedNotFound(t *testing.T) {
	dir := codedNotFoundDirectory{directoryUserServicer: newDirectory()}
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{KV: newMemKV(), Admin: stubAdminService{}, User: dir})

	_, err := c.Security().Auth().UserByCertThumbprint(context.Background(), "unbound")
	if !sdkerrors.IsNotFound(err) {
		t.Fatalf("want NotFoundError, got %T: %v", err, err)
	}
}
