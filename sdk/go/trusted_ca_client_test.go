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
	"errors"
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// TestTrustedCA_AccessorRoutesEveryVerb pins the #1330 SDK re-export of the
// go-xkms CAService: Security().TrustedCA() sends each verb to the
// quicbaas.TrustedCA service with go-xkms's own request type and returns
// go-xkms's own response type.
func TestTrustedCA_AccessorRoutesEveryVerb(t *testing.T) {
	var gotService, gotMethod string
	var gotReq interface{}
	var reply interface{}
	mt := &mockTransport{requestFn: func(_ context.Context, service, method string, req interface{}) (interface{}, error) {
		gotService, gotMethod, gotReq = service, method, req
		return reply, nil
	}}
	sec := &securityService{c: &client{transport: mt}}
	ca := sec.TrustedCA()
	if ca == nil || sec.TrustedCA() != ca {
		t.Fatal("TrustedCA() must return one non-nil service")
	}
	ctx := context.Background()

	cases := []struct {
		method string
		req    interface{}
		resp   interface{}
		call   func() (interface{}, error)
	}{
		{"GetCABundle", &xkmstransport.GetCABundleRequest{}, &xkmstransport.GetCABundleResponse{}, nil},
		{"GetCACertificate", &xkmstransport.GetCACertificateRequest{}, &xkmstransport.GetCACertificateResponse{}, nil},
		{"SignCSR", &xkmstransport.SignCSRRequest{}, &xkmstransport.SignCSRResponse{}, nil},
		{"IssueCertificate", &xkmstransport.IssueCertificateRequest{}, &xkmstransport.IssueCertificateResponse{}, nil},
		{"RevokeCertificate", &xkmstransport.RevokeCertificateRequest{}, &xkmstransport.RevokeCertificateResponse{}, nil},
		{"GenerateCRL", &xkmstransport.GenerateCRLRequest{}, &xkmstransport.GenerateCRLResponse{}, nil},
		{"IsRevoked", &xkmstransport.IsRevokedRequest{SerialNumber: "0a"}, &xkmstransport.IsRevokedResponse{Revoked: true}, nil},
	}
	for i := range cases {
		tc := &cases[i]
		switch r := tc.req.(type) {
		case *xkmstransport.GetCABundleRequest:
			tc.call = func() (interface{}, error) { return ca.GetCABundle(ctx, r) }
		case *xkmstransport.GetCACertificateRequest:
			tc.call = func() (interface{}, error) { return ca.GetCACertificate(ctx, r) }
		case *xkmstransport.SignCSRRequest:
			tc.call = func() (interface{}, error) { return ca.SignCSR(ctx, r) }
		case *xkmstransport.IssueCertificateRequest:
			tc.call = func() (interface{}, error) { return ca.IssueCertificate(ctx, r) }
		case *xkmstransport.RevokeCertificateRequest:
			tc.call = func() (interface{}, error) { return ca.RevokeCertificate(ctx, r) }
		case *xkmstransport.GenerateCRLRequest:
			tc.call = func() (interface{}, error) { return ca.GenerateCRL(ctx, r) }
		case *xkmstransport.IsRevokedRequest:
			tc.call = func() (interface{}, error) { return ca.IsRevoked(ctx, r) }
		}
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			reply = tc.resp
			got, err := tc.call()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.resp {
				t.Errorf("got %#v, want the transport's %#v", got, tc.resp)
			}
			if gotService != trustedCAServiceName || gotMethod != tc.method || gotReq != tc.req {
				t.Errorf("routed %s.%s with %#v, want %s.%s with the caller's request", gotService, gotMethod, gotReq, trustedCAServiceName, tc.method)
			}
		})
	}
}

// TestTrustedCA_RejectsBadInput keeps the typed contract: nil request, nil
// context, a wrong response type and a transport error never yield a result.
func TestTrustedCA_RejectsBadInput(t *testing.T) {
	calls := 0
	mt := &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		calls++
		return &xkmstransport.SignCSRResponse{}, nil
	}}
	ca := newTrustedCAService(mt)
	ctx := context.Background()

	if _, err := ca.IsRevoked(ctx, nil); err == nil {
		t.Error("nil request accepted")
	}
	//nolint:staticcheck // a nil context is the case under test
	if _, err := ca.GetCABundle(nil, &xkmstransport.GetCABundleRequest{}); err == nil {
		t.Error("nil context accepted")
	}
	if calls != 0 {
		t.Errorf("%d round-trips before validation, want 0", calls)
	}
	if _, err := ca.IsRevoked(ctx, &xkmstransport.IsRevokedRequest{SerialNumber: "0a"}); err == nil {
		t.Error("a response of the wrong type was accepted")
	}

	boom := errors.New("boom")
	failing := newTrustedCAService(&mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return nil, boom
	}})
	if _, err := failing.GenerateCRL(ctx, &xkmstransport.GenerateCRLRequest{}); !errors.Is(err, boom) {
		t.Errorf("transport error = %v, want %v", err, boom)
	}
	if _, err := newTrustedCAService(nil).GenerateCRL(ctx, &xkmstransport.GenerateCRLRequest{}); err == nil {
		t.Error("nil transport accepted")
	}
}
