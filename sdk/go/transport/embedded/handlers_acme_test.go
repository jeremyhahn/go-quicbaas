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

package embedded

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmsacme "github.com/jeremyhahn/go-xkms/sdk/go/acme"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	acmeclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/acme"
)

// errACME is a sentinel returned by the fake ACME servicer to drive
// error-propagation paths through the embedded handlers.
var errACME = errors.New("acme: boom")

// fakeACMEService is a configurable ACMEServicer. When failNext is true every
// control verb returns errACME; otherwise it returns a populated response and
// records the request it received.
type fakeACMEService struct {
	failNext     bool
	gotChallenge string
	gotCSR       []byte
	gotIdents    []service.ACMEOrderIdentifier
	gotCertURL   string
	gotRevoke    bool
	renewErr     error
}

func (f *fakeACMEService) RegisterChallenge(_ context.Context, req *service.ACMERegisterChallengeRequest) (*service.ACMERegisterChallengeResponse, error) {
	if f.failNext {
		return nil, errACME
	}
	f.gotChallenge = req.Name
	return &service.ACMERegisterChallengeResponse{Registered: true}, nil
}

func (f *fakeACMEService) Issue(_ context.Context, req *service.ACMEIssueRequest) (*service.ACMEIssueResponse, error) {
	if f.failNext {
		return nil, errACME
	}
	f.gotCSR = req.CSRPEM
	return &service.ACMEIssueResponse{
		CertificatePEM: []byte("CERT"),
		ChainPEM:       []byte("CHAIN"),
		SerialNumber:   "ff",
	}, nil
}

func (f *fakeACMEService) NewOrder(_ context.Context, req *service.ACMENewOrderRequest) (*service.ACMENewOrderResponse, error) {
	if f.failNext {
		return nil, errACME
	}
	f.gotIdents = req.Identifiers
	return &service.ACMENewOrderResponse{
		OrderID:     7,
		Status:      "pending",
		Identifiers: req.Identifiers,
		Finalize:    "https://acme.test/acme/order/7/finalize",
		URL:         "https://acme.test/acme/order/7",
	}, nil
}

func (f *fakeACMEService) FinalizeOrder(_ context.Context, req *service.ACMEFinalizeOrderRequest) (*service.ACMEFinalizeOrderResponse, error) {
	if f.failNext {
		return nil, errACME
	}
	f.gotCSR = req.CSRPEM
	return &service.ACMEFinalizeOrderResponse{
		CertificatePEM: []byte("CERT"),
		ChainPEM:       []byte("CHAIN"),
		SerialNumber:   "ff",
	}, nil
}

func (f *fakeACMEService) Renew(_ context.Context, req *service.ACMERenewRequest) (*service.ACMERenewResponse, error) {
	f.gotCertURL = req.CertURL
	f.gotRevoke = req.Revoke
	resp := &service.ACMERenewResponse{
		CertificatePEM: []byte("CERT"),
		SerialNumber:   req.CertURL,
		Revoked:        req.Revoke,
		Renewable:      false,
	}
	if f.renewErr != nil {
		return resp, f.renewErr
	}
	return resp, nil
}

func (f *fakeACMEService) DirectoryURL() string { return "https://acme.test/acme/directory" }

func (f *fakeACMEService) RestServicer() xkmsacme.ACMERestServicer { return nil }

// acmeMock embeds the base mock and overrides ACME().
type acmeMock struct {
	*mockQuicBaaS
	acme service.ACMEServicer
}

func (m *acmeMock) ACME() service.ACMEServicer { return m.acme }

func newACMETransport(t *testing.T, acme service.ACMEServicer) *Transport {
	t.Helper()
	tr, err := New(&acmeMock{mockQuicBaaS: &mockQuicBaaS{}, acme: acme})
	require.NoError(t, err)
	return tr
}

func TestTransport_ACMERegisterChallenge(t *testing.T) {
	fake := &fakeACMEService{}
	tr := newACMETransport(t, fake)

	resp, err := tr.Request(context.Background(), "quicbaas.ACME", "RegisterChallenge",
		&acmeclient.RegisterChallengeRequest{Name: "http-01"})
	require.NoError(t, err)
	typed, ok := resp.(*acmeclient.RegisterChallengeResponse)
	require.True(t, ok)
	assert.True(t, typed.Registered)
	assert.Equal(t, "http-01", fake.gotChallenge)
}

func TestTransport_ACMEIssue(t *testing.T) {
	fake := &fakeACMEService{}
	tr := newACMETransport(t, fake)

	resp, err := tr.Request(context.Background(), "quicbaas.ACME", "Issue",
		&acmeclient.IssueRequest{
			CSRPEM:        []byte("CSR"),
			ChallengeType: "http-01",
		})
	require.NoError(t, err)
	typed, ok := resp.(*acmeclient.IssueResponse)
	require.True(t, ok)
	assert.Equal(t, []byte("CERT"), typed.CertificatePEM)
	assert.Equal(t, []byte("CHAIN"), typed.ChainPEM)
	assert.Equal(t, "ff", typed.SerialNumber)
	assert.Equal(t, []byte("CSR"), fake.gotCSR)
}

func TestTransport_ACMEOperations_ServiceError(t *testing.T) {
	tr := newACMETransport(t, &fakeACMEService{failNext: true})

	_, err := tr.Request(context.Background(), "quicbaas.ACME", "RegisterChallenge",
		&acmeclient.RegisterChallengeRequest{Name: "http-01"})
	require.ErrorIs(t, err, errACME)

	_, err = tr.Request(context.Background(), "quicbaas.ACME", "Issue",
		&acmeclient.IssueRequest{CSRPEM: []byte("CSR"), ChallengeType: "http-01"})
	require.ErrorIs(t, err, errACME)
}

func TestTransport_ACMEOperations_WrongType(t *testing.T) {
	tr := newACMETransport(t, &fakeACMEService{})

	_, err := tr.Request(context.Background(), "quicbaas.ACME", "RegisterChallenge", &service.PutRequest{})
	require.Error(t, err)

	_, err = tr.Request(context.Background(), "quicbaas.ACME", "Issue", &service.PutRequest{})
	require.Error(t, err)
}

func TestTransport_ACMEOperations_NotConfigured(t *testing.T) {
	tr, err := New(&mockQuicBaaS{})
	require.NoError(t, err)

	_, err = tr.Request(context.Background(), "quicbaas.ACME", "RegisterChallenge",
		&acmeclient.RegisterChallengeRequest{Name: "http-01"})
	require.Error(t, err)

	_, err = tr.Request(context.Background(), "quicbaas.ACME", "Issue",
		&acmeclient.IssueRequest{CSRPEM: []byte("CSR"), ChallengeType: "http-01"})
	require.Error(t, err)
}

func TestTransport_ACMENewOrder(t *testing.T) {
	fake := &fakeACMEService{}
	tr := newACMETransport(t, fake)

	resp, err := tr.Request(context.Background(), "quicbaas.ACME", "NewOrder",
		&acmeclient.NewOrderRequest{
			Identifiers:   []acmeclient.OrderIdentifier{{Type: "dns", Value: "example.com"}},
			ChallengeType: "http-01",
		})
	require.NoError(t, err)
	typed, ok := resp.(*acmeclient.NewOrderResponse)
	require.True(t, ok)
	assert.Equal(t, uint64(7), typed.OrderID)
	assert.Equal(t, "pending", typed.Status)
	require.Len(t, typed.Identifiers, 1)
	assert.Equal(t, "example.com", typed.Identifiers[0].Value)
	require.Len(t, fake.gotIdents, 1)
	assert.Equal(t, "example.com", fake.gotIdents[0].Value)
}

func TestTransport_ACMEFinalizeOrder(t *testing.T) {
	fake := &fakeACMEService{}
	tr := newACMETransport(t, fake)

	resp, err := tr.Request(context.Background(), "quicbaas.ACME", "FinalizeOrder",
		&acmeclient.FinalizeOrderRequest{
			OrderURL: "https://acme.test/acme/order/7",
			CSRPEM:   []byte("CSR"),
		})
	require.NoError(t, err)
	typed, ok := resp.(*acmeclient.FinalizeOrderResponse)
	require.True(t, ok)
	assert.Equal(t, []byte("CERT"), typed.CertificatePEM)
	assert.Equal(t, "ff", typed.SerialNumber)
	assert.Equal(t, []byte("CSR"), fake.gotCSR)
}

func TestTransport_ACMERenew(t *testing.T) {
	fake := &fakeACMEService{renewErr: errACME}
	tr := newACMETransport(t, fake)

	_, err := tr.Request(context.Background(), "quicbaas.ACME", "Renew",
		&acmeclient.RenewRequest{CertURL: "ff", Revoke: true})
	require.ErrorIs(t, err, errACME)
	assert.Equal(t, "ff", fake.gotCertURL)
	assert.True(t, fake.gotRevoke)
}

func TestTransport_ACMENewVerbs_WrongType(t *testing.T) {
	tr := newACMETransport(t, &fakeACMEService{})

	_, err := tr.Request(context.Background(), "quicbaas.ACME", "NewOrder", &service.PutRequest{})
	require.Error(t, err)
	_, err = tr.Request(context.Background(), "quicbaas.ACME", "FinalizeOrder", &service.PutRequest{})
	require.Error(t, err)
	_, err = tr.Request(context.Background(), "quicbaas.ACME", "Renew", &service.PutRequest{})
	require.Error(t, err)
}
