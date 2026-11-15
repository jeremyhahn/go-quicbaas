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
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// TestTCGCA_AccessorRoutesEveryVerb pins Security().TCGCA() (#1330): one
// service per client, and every go-xkms TCGCAService verb is one
// quicbaas.TCGCA round-trip carrying the caller's request.
func TestTCGCA_AccessorRoutesEveryVerb(t *testing.T) {
	var gotService, gotMethod string
	var gotReq, reply interface{}
	mt := &mockTransport{requestFn: func(_ context.Context, service, method string, req interface{}) (interface{}, error) {
		gotService, gotMethod, gotReq = service, method, req
		return reply, nil
	}}
	sec := &securityService{c: &client{transport: mt}}
	ca := sec.TCGCA()
	if ca == nil || sec.TCGCA() != ca {
		t.Fatal("TCGCA() must return one non-nil service")
	}
	ctx := context.Background()

	ek := &xkmstransport.IssueEKCertificateRequest{CommonName: "dev"}
	ak := &xkmstransport.IssueAKCertificateRequest{CommonName: "ak"}
	csr := &xkmstransport.SignTCGCSRRequest{CommonName: "dev"}
	enroll := &xkmstransport.EnrollDeviceRequest{CommonName: "dev"}
	cases := []struct {
		method string
		req    interface{}
		resp   interface{}
		call   func() (interface{}, error)
	}{
		{"IssueEKCertificate", ek, &xkmstransport.IssueEKCertificateResponse{SerialNumber: "0a"}, func() (interface{}, error) { return ca.IssueEKCertificate(ctx, ek) }},
		{"IssueAKCertificate", ak, &xkmstransport.IssueAKCertificateResponse{SerialNumber: "0b"}, func() (interface{}, error) { return ca.IssueAKCertificate(ctx, ak) }},
		{"SignTCGCSR", csr, &xkmstransport.SignTCGCSRResponse{IAKCertDER: []byte("IAK")}, func() (interface{}, error) { return ca.SignTCGCSR(ctx, csr) }},
		{"EnrollDevice", enroll, &xkmstransport.EnrollDeviceResponse{IAKCertDER: []byte("IAK")}, func() (interface{}, error) { return ca.EnrollDevice(ctx, enroll) }},
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
			if gotService != tcgCAServiceName || gotMethod != tc.method || gotReq != tc.req {
				t.Errorf("routed %s.%s with %#v, want %s.%s with the caller's request", gotService, gotMethod, gotReq, tcgCAServiceName, tc.method)
			}
		})
	}
}

// TestTCGCA_RejectsBadInput covers a nil request, a nil context, a missing
// transport and a response of the wrong type.
func TestTCGCA_RejectsBadInput(t *testing.T) {
	ctx := context.Background()
	if _, err := newTCGCAService(&mockTransport{}).EnrollDevice(ctx, nil); err == nil {
		t.Error("nil request accepted")
	}
	var nilCtx context.Context // a nil context is the case under test
	if _, err := newTCGCAService(&mockTransport{}).EnrollDevice(nilCtx, &xkmstransport.EnrollDeviceRequest{}); err == nil {
		t.Error("nil context accepted")
	}
	if _, err := newTCGCAService(nil).IssueEKCertificate(ctx, &xkmstransport.IssueEKCertificateRequest{}); err == nil {
		t.Error("missing transport accepted")
	}
	wrong := &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return &xkmstransport.IsRevokedResponse{}, nil
	}}
	if _, err := newTCGCAService(wrong).SignTCGCSR(ctx, &xkmstransport.SignTCGCSRRequest{}); err == nil {
		t.Error("wrong response type accepted")
	}
}
