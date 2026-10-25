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

package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ceremonyRequest stands in for qrdb's CeremonyInitializeRequest: it exposes
// the admin-credential getters the interceptor reads.
type ceremonyRequest struct {
	adminCertPEM  string
	adminCSRPEM   string
	adminGenerate bool
}

func (r *ceremonyRequest) GetAdminCertPem() string { return r.adminCertPEM }
func (r *ceremonyRequest) GetAdminCsrPem() string  { return r.adminCSRPEM }
func (r *ceremonyRequest) GetAdminGenerate() bool  { return r.adminGenerate }

// errHandlerSentinel is returned by the recording handler so the test can
// prove the interceptor returns the handler's error unchanged.
var errHandlerSentinel = errors.New("handler error")

// invokeCeremonyCARefusal runs the interceptor for method with req and
// reports whether the handler ran with req itself, and what the interceptor
// returned.
func invokeCeremonyCARefusal(t *testing.T, method string, req any) (handled bool, resp any, err error) {
	t.Helper()
	handlerResp := &struct{ name string }{name: "handler response"}
	handler := func(_ context.Context, got any) (any, error) {
		if got != req {
			t.Errorf("handler received %v, want the original request %v", got, req)
		}
		handled = true
		return handlerResp, errHandlerSentinel
	}
	resp, err = ceremonyCARefusalUnaryInterceptor()(
		context.Background(), req, &ggrpc.UnaryServerInfo{FullMethod: method}, handler)
	if handled && resp != handlerResp {
		t.Errorf("interceptor returned %v, want the handler's response", resp)
	}
	return handled, resp, err
}

func TestCeremonyCARefusalUnaryInterceptor_OtherMethodsPassThrough(t *testing.T) {
	// A CA-needing request on any other method is not the interceptor's
	// concern: it must reach the handler unchanged.
	methods := []string{
		"/qrdb.v1.QRDBService/Get",
		"/qrdb.v1.QRDBService/CeremonyStatus",
		"/quicbaas.v1.CeremonyService/CeremonyInitialize",
	}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := &ceremonyRequest{adminCSRPEM: "csr", adminGenerate: true}
			handled, _, err := invokeCeremonyCARefusal(t, method, req)
			if !handled {
				t.Fatal("handler was not called")
			}
			if !errors.Is(err, errHandlerSentinel) {
				t.Errorf("got error %v, want the handler's error", err)
			}
		})
	}
}

func TestCeremonyCARefusalUnaryInterceptor_BringYourOwnCertPassesThrough(t *testing.T) {
	req := &ceremonyRequest{adminCertPEM: "-----BEGIN CERTIFICATE-----"}
	handled, _, err := invokeCeremonyCARefusal(t, qrdbCeremonyInitializeMethod, req)
	if !handled {
		t.Fatal("bring-your-own-certificate initialization did not reach the handler")
	}
	if !errors.Is(err, errHandlerSentinel) {
		t.Errorf("got error %v, want the handler's error", err)
	}
}

func TestCeremonyCARefusalUnaryInterceptor_CAModesStopBeforeHandler(t *testing.T) {
	modes := map[string]*ceremonyRequest{
		ceremonyModeCSRSign:  {adminCSRPEM: "csr"},
		ceremonyModeGenerate: {adminGenerate: true},
	}
	for mode, req := range modes {
		t.Run(mode, func(t *testing.T) {
			handled, resp, err := invokeCeremonyCARefusal(t, qrdbCeremonyInitializeMethod, req)
			if handled {
				t.Fatal("a CA-needing request reached the handler")
			}
			if resp != nil {
				t.Errorf("got response %v, want nil", resp)
			}
			want := (&CeremonyCATransportRefusedError{Mode: mode}).Error()
			if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), want) {
				t.Errorf("got %v, want FailedPrecondition carrying %q", err, want)
			}
		})
	}
}
